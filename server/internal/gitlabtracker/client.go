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

// FetchMedia retrieves a same-origin GitLab upload with tracker credentials.
// Callers validate the URL against the tracker instance before calling.
func (c *RestClient) FetchMedia(ctx context.Context, rawURL string) (*http.Response, error) {
	if c == nil {
		return nil, errors.New("gitlabtracker: nil RestClient")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "image/*")
	req.Header.Set("X-RimeDeck-Same-Origin-Redirect", "1")
	return c.transport.Do(req)
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
	StartDate   string
	DueDate     string
	Labels      []string
	Author      IssueAuthor
}

// IssueAuthor is optional — some GitLab installs strip author info from
// unauthenticated views. Zero-value means "no author info available".
type IssueAuthor struct {
	Name string
	URL  string
}

// Note is the subset of an issue note required for bidirectional comment sync.
type Note struct {
	ID        int64
	Body      string
	System    bool
	CreatedAt string
	UpdatedAt string
	Author    NoteAuthor
}

type NoteAuthor struct {
	ID   int64
	Name string
	URL  string
}

type notePayload struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	System    bool   `json:"system"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Author    struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		WebURL string `json:"web_url"`
	} `json:"author"`
}

func noteFromPayload(p notePayload) Note {
	return Note{
		ID: p.ID, Body: p.Body, System: p.System,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		Author: NoteAuthor{ID: p.Author.ID, Name: p.Author.Name, URL: p.Author.WebURL},
	}
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
			StartDate   string   `json:"start_date"`
			DueDate     string   `json:"due_date"`
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
				StartDate:   p.StartDate,
				DueDate:     p.DueDate,
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

// ListIssueNotes returns every user-visible note for an issue in GitLab order.
func (c *RestClient) ListIssueNotes(ctx context.Context, projectID int64, iid int32) ([]Note, error) {
	if c == nil {
		return nil, errors.New("gitlabtracker: nil RestClient")
	}
	out := []Note{}
	page := 1
	for {
		path := fmt.Sprintf("/api/v4/projects/%d/issues/%d/notes?per_page=%d&sort=asc&order_by=created_at&page=%d", projectID, iid, DefaultPerPage, page)
		req, err := c.newRequest(ctx, http.MethodGet, path, nil)
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
		var payload []notePayload
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("%w: decode notes: %w", ErrRemote, err)
		}
		next := strings.TrimSpace(resp.Header.Get("X-Next-Page"))
		resp.Body.Close()
		for _, p := range payload {
			out = append(out, noteFromPayload(p))
		}
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

func (c *RestClient) CreateIssueNote(ctx context.Context, projectID int64, iid int32, body string) (Note, error) {
	return c.mutateIssueNote(ctx, http.MethodPost, fmt.Sprintf("/api/v4/projects/%d/issues/%d/notes", projectID, iid), body)
}

func (c *RestClient) UpdateIssueNote(ctx context.Context, projectID int64, iid int32, noteID int64, body string) (Note, error) {
	return c.mutateIssueNote(ctx, http.MethodPut, fmt.Sprintf("/api/v4/projects/%d/issues/%d/notes/%d", projectID, iid, noteID), body)
}

func (c *RestClient) mutateIssueNote(ctx context.Context, method, path, body string) (Note, error) {
	buf, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return Note{}, fmt.Errorf("%w: encode note: %w", ErrRemote, err)
	}
	req, err := c.newRequest(ctx, method, path, strings.NewReader(string(buf)))
	if err != nil {
		return Note{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.transport.Do(req)
	if err != nil {
		return Note{}, err
	}
	defer resp.Body.Close()
	if err := mapStatusError(resp.StatusCode); err != nil {
		return Note{}, err
	}
	var payload notePayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Note{}, fmt.Errorf("%w: decode note: %w", ErrRemote, err)
	}
	return noteFromPayload(payload), nil
}

func (c *RestClient) DeleteIssueNote(ctx context.Context, projectID int64, iid int32, noteID int64) error {
	if c == nil {
		return errors.New("gitlabtracker: nil RestClient")
	}
	req, err := c.newRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/v4/projects/%d/issues/%d/notes/%d", projectID, iid, noteID), nil)
	if err != nil {
		return err
	}
	resp, err := c.transport.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return mapStatusError(resp.StatusCode)
}

// CreateIssueRequest is the write-side counterpart of Issue. GitLab's REST
// API accepts `labels` as a comma-separated string on write; the client keeps
// the typed slice at this boundary and serializes it in mutateIssue.
type CreateIssueRequest struct {
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Labels      []string `json:"-"`
	StartDate   string   `json:"start_date,omitempty"`
	DueDate     string   `json:"due_date,omitempty"`
}

// UpdateIssueRequest carries only the fields worth mutating. StateEvent
// is the GitLab convention for close/reopen ("close" | "reopen").
type UpdateIssueRequest struct {
	Title       *string   `json:"title,omitempty"`
	Description *string   `json:"description,omitempty"`
	Labels      *[]string `json:"-"`
	StartDate   *string   `json:"start_date,omitempty"`
	DueDate     *string   `json:"due_date,omitempty"`
	StateEvent  *string   `json:"state_event,omitempty"`
}

// CreateIssue posts a new issue and returns GitLab's canonical response.
// Path uses the numeric project id so nested-subgroup URL encoding is
// out of the picture on the write path.
func (c *RestClient) CreateIssue(ctx context.Context, projectID int64, req CreateIssueRequest) (Issue, error) {
	if c == nil {
		return Issue{}, errors.New("gitlabtracker: nil RestClient")
	}
	return c.mutateIssue(ctx, http.MethodPost, fmt.Sprintf("/api/v4/projects/%d/issues", projectID), req)
}

// UpdateIssue mutates an existing issue by remote iid. Callers pass only
// the fields they want changed; nil fields are omitted from the body.
func (c *RestClient) UpdateIssue(ctx context.Context, projectID int64, iid int32, req UpdateIssueRequest) (Issue, error) {
	if c == nil {
		return Issue{}, errors.New("gitlabtracker: nil RestClient")
	}
	return c.mutateIssue(ctx, http.MethodPut, fmt.Sprintf("/api/v4/projects/%d/issues/%d", projectID, iid), req)
}

// CloseIssue is UpdateIssue with only state_event set. Kept as its own
// method so the sync worker's dispatch stays a switch on operation, not
// a switch on struct field presence.
func (c *RestClient) CloseIssue(ctx context.Context, projectID int64, iid int32) (Issue, error) {
	event := "close"
	return c.UpdateIssue(ctx, projectID, iid, UpdateIssueRequest{StateEvent: &event})
}

// ReopenIssue: same shape as CloseIssue, opposite event.
func (c *RestClient) ReopenIssue(ctx context.Context, projectID int64, iid int32) (Issue, error) {
	event := "reopen"
	return c.UpdateIssue(ctx, projectID, iid, UpdateIssueRequest{StateEvent: &event})
}

// DeleteIssue removes a remote issue. GitLab returns 204 on success and
// 404 when the issue is already gone; both are terminal-success from the
// worker's viewpoint — the worker maps 404 to the same local cleanup as
// 204 so a race with a manual delete on GitLab doesn't strand the row.
func (c *RestClient) DeleteIssue(ctx context.Context, projectID int64, iid int32) error {
	if c == nil {
		return errors.New("gitlabtracker: nil RestClient")
	}
	req, err := c.newRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/v4/projects/%d/issues/%d", projectID, iid), nil)
	if err != nil {
		return err
	}
	resp, err := c.transport.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return mapStatusError(resp.StatusCode)
}

// SetLabels is a UpdateIssue specialization: replaces the full label
// set. Callers pass the desired final list — GitLab's PUT with `labels`
// is destructive-replace, not merge, which matches design §8.3.
func (c *RestClient) SetLabels(ctx context.Context, projectID int64, iid int32, labels []string) (Issue, error) {
	safe := labels
	if safe == nil {
		safe = []string{}
	}
	return c.UpdateIssue(ctx, projectID, iid, UpdateIssueRequest{Labels: &safe})
}

// mutateIssue is the shared POST/PUT path. Encodes the body, sends,
// decodes into Issue on 2xx. Same auth + error mapping as reads.
func (c *RestClient) mutateIssue(ctx context.Context, method, path string, body any) (Issue, error) {
	wireBody := body
	switch request := body.(type) {
	case CreateIssueRequest:
		wireBody = struct {
			Title       string `json:"title"`
			Description string `json:"description,omitempty"`
			Labels      string `json:"labels,omitempty"`
			StartDate   string `json:"start_date,omitempty"`
			DueDate     string `json:"due_date,omitempty"`
		}{
			Title: request.Title, Description: request.Description,
			Labels: strings.Join(request.Labels, ","), StartDate: request.StartDate, DueDate: request.DueDate,
		}
	case UpdateIssueRequest:
		var labels *string
		if request.Labels != nil {
			joined := strings.Join(*request.Labels, ",")
			labels = &joined
		}
		wireBody = struct {
			Title       *string `json:"title,omitempty"`
			Description *string `json:"description,omitempty"`
			Labels      *string `json:"labels,omitempty"`
			StartDate   *string `json:"start_date,omitempty"`
			DueDate     *string `json:"due_date,omitempty"`
			StateEvent  *string `json:"state_event,omitempty"`
		}{
			Title: request.Title, Description: request.Description, Labels: labels,
			StartDate: request.StartDate, DueDate: request.DueDate, StateEvent: request.StateEvent,
		}
	}
	buf, err := json.Marshal(wireBody)
	if err != nil {
		return Issue{}, fmt.Errorf("%w: encode request: %w", ErrRemote, err)
	}
	req, err := c.newRequest(ctx, method, path, strings.NewReader(string(buf)))
	if err != nil {
		return Issue{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.transport.Do(req)
	if err != nil {
		return Issue{}, err
	}
	defer resp.Body.Close()
	if err := mapStatusError(resp.StatusCode); err != nil {
		return Issue{}, err
	}
	var payload struct {
		ID          int64    `json:"id"`
		IID         int32    `json:"iid"`
		State       string   `json:"state"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		WebURL      string   `json:"web_url"`
		UpdatedAt   string   `json:"updated_at"`
		StartDate   string   `json:"start_date"`
		DueDate     string   `json:"due_date"`
		Labels      []string `json:"labels"`
		Author      struct {
			Name   string `json:"name"`
			WebURL string `json:"web_url"`
		} `json:"author"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Issue{}, fmt.Errorf("%w: decode issue: %w", ErrRemote, err)
	}
	return Issue{
		ID:          payload.ID,
		IID:         payload.IID,
		State:       payload.State,
		Title:       payload.Title,
		Description: payload.Description,
		WebURL:      payload.WebURL,
		UpdatedAt:   payload.UpdatedAt,
		StartDate:   payload.StartDate,
		DueDate:     payload.DueDate,
		Labels:      payload.Labels,
		Author:      IssueAuthor{Name: payload.Author.Name, URL: payload.Author.WebURL},
	}, nil
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

// CreateProjectHookRequest is the write-side request for the project
// hook endpoint. Only the fields RimeDeck actually toggles are here —
// we opt into issues + notes + confidential variants because those map
// to the outbox operations Task 1's ingress already knows how to
// route.
type CreateProjectHookRequest struct {
	URL                    string `json:"url"`
	Token                  string `json:"token"`
	IssuesEvents           bool   `json:"issues_events"`
	ConfidentialIssues     bool   `json:"confidential_issues_events"`
	NoteEvents             bool   `json:"note_events"`
	ConfidentialNoteEvents bool   `json:"confidential_note_events"`
	EnableSSLVerification  bool   `json:"enable_ssl_verification"`
}

// CreateProjectHook registers a webhook against the given project and
// returns GitLab's assigned hook id so callers can delete/replace it
// later. Payload is minimal on purpose — everything else can flow via
// reconcile if the operator opts out.
func (c *RestClient) CreateProjectHook(ctx context.Context, projectID int64, req CreateProjectHookRequest) (int64, error) {
	if c == nil {
		return 0, errors.New("gitlabtracker: nil RestClient")
	}
	buf, err := json.Marshal(req)
	if err != nil {
		return 0, fmt.Errorf("%w: encode hook: %w", ErrRemote, err)
	}
	httpReq, err := c.newRequest(ctx, http.MethodPost, fmt.Sprintf("/api/v4/projects/%d/hooks", projectID), strings.NewReader(string(buf)))
	if err != nil {
		return 0, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.transport.Do(httpReq)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if err := mapStatusError(resp.StatusCode); err != nil {
		return 0, err
	}
	var payload struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, fmt.Errorf("%w: decode hook: %w", ErrRemote, err)
	}
	return payload.ID, nil
}

// DeleteProjectHook removes a previously-created webhook. GitLab
// returns 204 on success and 404 when the hook is already gone; both
// are non-errors from the operational viewpoint.
func (c *RestClient) DeleteProjectHook(ctx context.Context, projectID, hookID int64) error {
	if c == nil {
		return errors.New("gitlabtracker: nil RestClient")
	}
	req, err := c.newRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/v4/projects/%d/hooks/%d", projectID, hookID), nil)
	if err != nil {
		return err
	}
	resp, err := c.transport.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return mapStatusError(resp.StatusCode)
}
