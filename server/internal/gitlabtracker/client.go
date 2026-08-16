package gitlabtracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// DefaultPerPage matches GitLab's max per_page for our read surfaces
// (Labels / Issues). Bigger pages mean fewer round-trips on first import
// but always come back capped at 100 by GitLab regardless of what we ask.
const DefaultPerPage = 100

// Sentinel errors let handlers pattern-match without stringly-typed
// prefixes. Each maps to a stable last_error_code / safe user message
// so ErrInvalidToken never leaks a URL or actual token.
var (
	ErrInvalidToken     = errors.New("gitlab: invalid or expired token")
	ErrPermissionDenied = errors.New("gitlab: token lacks required permissions")
	ErrNotFound         = errors.New("gitlab: resource not found")
	ErrRemote           = errors.New("gitlab: unexpected remote response")
)

// RestClient is the minimal read-only surface Phase 2 needs. It is
// deliberately not an interface — swap-in stubs live at the transport
// level (httptest server) so the concrete client stays inspectable in
// panics and stack traces.
type RestClient struct {
	transport *Client
	baseURL   string
	token     string
}

// NewRestClient binds a transport, a normalized `https://host` base URL
// (no trailing slash), and a PAT. The base URL is only trimmed here; URL
// validation belongs upstream in ParseProjectURL so this constructor
// stays a pure wrapper.
func NewRestClient(transport *Client, baseURL, token string) *RestClient {
	return &RestClient{
		transport: transport,
		baseURL:   strings.TrimRight(baseURL, "/"),
		token:     token,
	}
}

// Project is the subset of GitLab's project payload we read. Anything
// beyond these fields is intentionally dropped — every extra field is a
// snapshot the sync worker would then have to track for revision-based
// idempotency, and Phase 2 only needs identity + web display + write
// permission signals.
type Project struct {
	ID                int64
	PathWithNamespace string
	WebURL            string
	DefaultBranch     string
	// CanWriteIssues is derived from the caller's max access_level (30 =
	// Developer or above). Used by /validate to surface a "read-only
	// token" hint before persisting.
	CanWriteIssues bool
	// CanConfigureWebhook requires Maintainer (40) or above. Same
	// permission bit the sync worker needs before it can even try to
	// provision the webhook in Phase 4.
	CanConfigureWebhook bool
}

// Label mirrors the fields we persist to issue_label(source_type='gitlab').
// `id` is the GitLab-scoped label id used for our (connection, gitlab_label_id)
// unique index; `is_project_label=false` marks a group-inherited label.
type Label struct {
	ID             int64
	Name           string
	Color          string
	Description    string
	IsProjectLabel bool
}

// Issue keeps only the fields Phase 2's import + Phase 3's outbox push
// touch. Missing fields (e.g. milestone, assignees) are tracked in
// last_remote_snapshot as raw JSON, not here.
type Issue struct {
	ID          int64
	IID         int32
	State       string
	Title       string
	Description string
	WebURL      string
	UpdatedAt   string
	Labels      []string
	Author      IssueAuthor
}

// IssueAuthor is optional — some GitLab installs strip author info from
// unauthenticated views. Zero-value means "no author info available".
type IssueAuthor struct {
	Name string
	URL  string
}

// ListIssuesOptions filters the /issues call. Zero values fall back to
// GitLab's defaults except for State, which defaults to "all" so the
// importer sees both opened and closed issues in one pass.
type ListIssuesOptions struct {
	State string
}

// GetProject resolves a `namespace/project` path to a Project. Uses the
// URL-encoded path form so nested subgroups (`group/sub/proj`) round-trip
// intact.
func (c *RestClient) GetProject(ctx context.Context, path string) (Project, error) {
	if c == nil {
		return Project{}, errors.New("gitlabtracker: nil RestClient")
	}
	encoded := strings.ReplaceAll(url.PathEscape(path), "/", "%2F")
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v4/projects/"+encoded, nil)
	if err != nil {
		return Project{}, err
	}
	resp, err := c.transport.Do(req)
	if err != nil {
		return Project{}, err
	}
	defer resp.Body.Close()
	if err := mapStatusError(resp.StatusCode); err != nil {
		return Project{}, err
	}
	var payload struct {
		ID                int64  `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"`
		WebURL            string `json:"web_url"`
		DefaultBranch     string `json:"default_branch"`
		Permissions       struct {
			ProjectAccess struct {
				AccessLevel int `json:"access_level"`
			} `json:"project_access"`
			GroupAccess struct {
				AccessLevel int `json:"access_level"`
			} `json:"group_access"`
		} `json:"permissions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Project{}, fmt.Errorf("%w: decode project: %w", ErrRemote, err)
	}
	access := payload.Permissions.ProjectAccess.AccessLevel
	if payload.Permissions.GroupAccess.AccessLevel > access {
		access = payload.Permissions.GroupAccess.AccessLevel
	}
	return Project{
		ID:                  payload.ID,
		PathWithNamespace:   payload.PathWithNamespace,
		WebURL:              payload.WebURL,
		DefaultBranch:       payload.DefaultBranch,
		CanWriteIssues:      access >= 30, // Developer or above per GitLab access levels.
		CanConfigureWebhook: access >= 40, // Maintainer or above.
	}, nil
}

// ListProjectLabels fetches every label attached to a project, including
// ancestor group labels. Follows the standard GitLab pagination header
// contract: `X-Next-Page` empty (or missing) marks the last page.
func (c *RestClient) ListProjectLabels(ctx context.Context, projectID int64) ([]Label, error) {
	if c == nil {
		return nil, errors.New("gitlabtracker: nil RestClient")
	}
	out := []Label{}
	page := 1
	for {
		u := fmt.Sprintf("/api/v4/projects/%d/labels?per_page=%d&include_ancestor_groups=true&page=%d",
			projectID, DefaultPerPage, page)
		req, err := c.newRequest(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.transport.Do(req)
		if err != nil {
			return nil, err
		}
		if err := mapStatusError(resp.StatusCode); err != nil {
			resp.Body.Close()
			return nil, err
		}
		var payload []struct {
			ID             int64  `json:"id"`
			Name           string `json:"name"`
			Color          string `json:"color"`
			Description    string `json:"description"`
			IsProjectLabel bool   `json:"is_project_label"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("%w: decode labels: %w", ErrRemote, err)
		}
		for _, p := range payload {
			out = append(out, Label{
				ID:             p.ID,
				Name:           p.Name,
				Color:          p.Color,
				Description:    p.Description,
				IsProjectLabel: p.IsProjectLabel,
			})
		}
		next := strings.TrimSpace(resp.Header.Get("X-Next-Page"))
		resp.Body.Close()
		if next == "" {
			break
		}
		n, err := strconv.Atoi(next)
		if err != nil || n <= page {
			break
		}
		page = n
	}
	return out, nil
}

// ListProjectIssues walks every page of the /issues endpoint. Callers
// filter with ListIssuesOptions; the same X-Next-Page contract applies
// as for labels.
func (c *RestClient) ListProjectIssues(ctx context.Context, projectID int64, opts ListIssuesOptions) ([]Issue, error) {
	if c == nil {
		return nil, errors.New("gitlabtracker: nil RestClient")
	}
	state := strings.TrimSpace(opts.State)
	if state == "" {
		state = "all"
	}
	out := []Issue{}
	page := 1
	for {
		u := fmt.Sprintf("/api/v4/projects/%d/issues?per_page=%d&state=%s&order_by=updated_at&sort=asc&page=%d",
			projectID, DefaultPerPage, state, page)
		req, err := c.newRequest(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.transport.Do(req)
		if err != nil {
			return nil, err
		}
		if err := mapStatusError(resp.StatusCode); err != nil {
			resp.Body.Close()
			return nil, err
		}
		var payload []struct {
			ID          int64    `json:"id"`
			IID         int32    `json:"iid"`
			State       string   `json:"state"`
			Title       string   `json:"title"`
			Description string   `json:"description"`
			WebURL      string   `json:"web_url"`
			UpdatedAt   string   `json:"updated_at"`
			Labels      []string `json:"labels"`
			Author      struct {
				Name   string `json:"name"`
				WebURL string `json:"web_url"`
			} `json:"author"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("%w: decode issues: %w", ErrRemote, err)
		}
		for _, p := range payload {
			out = append(out, Issue{
				ID:          p.ID,
				IID:         p.IID,
				State:       p.State,
				Title:       p.Title,
				Description: p.Description,
				WebURL:      p.WebURL,
				UpdatedAt:   p.UpdatedAt,
				Labels:      p.Labels,
				Author:      IssueAuthor{Name: p.Author.Name, URL: p.Author.WebURL},
			})
		}
		next := strings.TrimSpace(resp.Header.Get("X-Next-Page"))
		resp.Body.Close()
		if next == "" {
			break
		}
		n, err := strconv.Atoi(next)
		if err != nil || n <= page {
			break
		}
		page = n
	}
	return out, nil
}

// newRequest attaches the PRIVATE-TOKEN header and joins the path onto
// the base URL. Every REST call flows through here so the auth header
// contract is enforced in one place.
func (c *RestClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "rimedeck-gitlabtracker/1")
	return req, nil
}

// mapStatusError converts an HTTP status code to a sentinel error. 5xx
// falls under ErrRemote so callers can back off; any 2xx returns nil so
// the caller decodes the body.
func mapStatusError(status int) error {
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusUnauthorized:
		return ErrInvalidToken
	case status == http.StatusForbidden:
		return ErrPermissionDenied
	case status == http.StatusNotFound:
		return ErrNotFound
	default:
		return fmt.Errorf("%w: status %d", ErrRemote, status)
	}
}
