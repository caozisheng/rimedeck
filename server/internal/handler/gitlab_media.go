package handler

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/gitlabtracker"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func detectGitlabImageContentType(header string, body []byte) string {
	declared := strings.ToLower(strings.TrimSpace(strings.Split(header, ";")[0]))
	sniffed := strings.ToLower(http.DetectContentType(body))
	if strings.HasPrefix(sniffed, "image/") {
		return sniffed
	}
	if strings.HasPrefix(declared, "image/") {
		return declared
	}
	return ""
}

const maxGitlabMediaSize = 4 << 20

func resolveGitlabMediaURL(instanceURL, raw string) (*url.URL, error) {
	base, err := url.Parse(strings.TrimRight(instanceURL, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("invalid tracker origin")
	}
	ref, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || ref.User != nil {
		return nil, fmt.Errorf("invalid media URL")
	}
	if !ref.IsAbs() {
		if !strings.HasPrefix(ref.Path, "/uploads/") {
			return nil, fmt.Errorf("unsupported media path")
		}
		ref = base.ResolveReference(ref)
	}
	if ref.Scheme != base.Scheme || !strings.EqualFold(ref.Host, base.Host) {
		return nil, fmt.Errorf("media origin mismatch")
	}
	if !strings.HasPrefix(ref.Path, "/uploads/") {
		return nil, fmt.Errorf("unsupported media path")
	}
	ref.Fragment = ""
	return ref, nil
}

func gitlabMediaCandidates(tracker db.GitlabTrackerConnection, raw string) ([]*url.URL, error) {
	base, err := resolveGitlabMediaURL(tracker.InstanceUrl, raw)
	if err != nil {
		return nil, err
	}
	parsedInstance, err := url.Parse(strings.TrimRight(tracker.InstanceUrl, "/"))
	if err != nil {
		return nil, err
	}
	pathSuffix := strings.TrimPrefix(base.Path, "/uploads/")
	paths := make([]string, 0, 4)
	if tracker.RemoteProjectID > 0 {
		paths = append(paths, fmt.Sprintf("/api/v4/projects/%d/uploads/%s", tracker.RemoteProjectID, pathSuffix))
	}
	paths = append(paths, base.Path)
	if tracker.PathWithNamespace != "" {
		project := strings.Trim(tracker.PathWithNamespace, "/")
		paths = append(paths,
			"/"+project+"/-/uploads/"+pathSuffix,
			"/"+project+"/uploads/"+pathSuffix,
		)
	}
	seen := make(map[string]struct{}, len(paths))
	out := make([]*url.URL, 0, len(paths))
	for _, p := range paths {
		candidate := *parsedInstance
		candidate.Path = p
		candidate.RawQuery = base.RawQuery
		candidate.Fragment = ""
		key := candidate.String()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, &candidate)
	}
	return out, nil
}

// GetGitlabMedia proxies a private GitLab image without exposing the tracker token.
func (h *Handler) GetGitlabMedia(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if issue.SourceType != "gitlab" || !issue.TrackerConnectionID.Valid {
		writeError(w, http.StatusNotFound, "GitLab media unavailable")
		return
	}
	tracker, err := h.Queries.GetGitlabTrackerConnection(r.Context(), issue.TrackerConnectionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "GitLab media unavailable")
		return
	}
	mediaURLs, err := gitlabMediaCandidates(tracker, r.URL.Query().Get("url"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid GitLab media URL")
		return
	}
	cipher, err := GitlabTrackerCipherProvider()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "GitLab media unavailable")
		return
	}
	token, err := cipher.Decrypt(tracker.TokenCiphertext)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "GitLab media unavailable")
		return
	}
	transport, err := gitlabtracker.NewClient(gitlabtracker.Config{})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "GitLab media unavailable")
		return
	}
	client := gitlabtracker.NewRestClient(transport, tracker.InstanceUrl, string(token))
	var successfulResp *http.Response
	var lastStatus int
	var lastErr error
	statuses := make([]string, 0, len(mediaURLs))
	for _, mediaURL := range mediaURLs {
		resp, fetchErr := client.FetchMedia(r.Context(), mediaURL.String())
		if fetchErr != nil {
			lastErr = fetchErr
			statuses = append(statuses, mediaURL.Path+":transport")
			continue
		}
		lastStatus = resp.StatusCode
		statuses = append(statuses, fmt.Sprintf("%s:%d", mediaURL.Path, resp.StatusCode))
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			successfulResp = resp
			break
		}
		resp.Body.Close()
	}
	if successfulResp == nil {
		slog.Warn("GitLab media upstream rejected all candidate URLs", "issue_id", uuidToString(issue.ID), "candidates", statuses, "status", lastStatus, "error", lastErr)
		writeError(w, http.StatusBadGateway, "failed to load GitLab media")
		return
	}
	defer successfulResp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(successfulResp.Body, maxGitlabMediaSize+1))
	if err != nil {
		slog.Warn("read GitLab media failed", "issue_id", uuidToString(issue.ID), "error", err)
		writeError(w, http.StatusBadGateway, "failed to load GitLab media")
		return
	}
	if len(body) > maxGitlabMediaSize {
		writeError(w, http.StatusRequestEntityTooLarge, "GitLab image is too large")
		return
	}
	contentType := detectGitlabImageContentType(successfulResp.Header.Get("Content-Type"), body)
	if contentType == "" {
		writeError(w, http.StatusUnsupportedMediaType, "GitLab media is not an image")
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
