package daemon

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// remoteServer represents a connection to a remote Rimedeck server for
// compute sharing. Each remote server has its own Client, runtime IDs,
// and heartbeat goroutines — fully independent of the daemon's primary
// (local) server connection.
type remoteServer struct {
	client     *Client
	serverURL  string
	token      string
	runtimeIDs []string
	cancel     context.CancelFunc
	mu         sync.Mutex
}

// AddRemoteServer connects to a remote server, registers runtimes, and
// starts heartbeat goroutines. Called from the /remote/add health endpoint.
// workspaceID is the target workspace (from the pairing response); daemon
// tokens (mdt_*) cannot call /api/workspaces, so the caller must provide it.
func (d *Daemon) AddRemoteServer(ctx context.Context, serverURL, token, workspaceID string) ([]Runtime, error) {
	d.remotesMu.Lock()
	if _, exists := d.remoteServers[serverURL]; exists {
		d.remotesMu.Unlock()
		return nil, fmt.Errorf("remote server already connected: %s", serverURL)
	}
	d.remotesMu.Unlock()

	client := NewClient(serverURL)
	client.SetToken(token)
	client.SetVersion(d.cfg.CLIVersion)

	if workspaceID == "" {
		return nil, fmt.Errorf("workspace_id is required for remote server registration")
	}

	resp, err := d.registerRuntimesWithClient(ctx, client, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("register on remote workspace: %w", err)
	}

	var allRuntimes []Runtime
	var allRuntimeIDs []string
	for _, rt := range resp.Runtimes {
		allRuntimes = append(allRuntimes, rt)
		allRuntimeIDs = append(allRuntimeIDs, rt.ID)
	}

	if len(allRuntimeIDs) == 0 {
		return nil, fmt.Errorf("failed to register any runtimes on remote server")
	}

	// Start heartbeat goroutines for remote runtimes.
	remoteCtx, remoteCancel := context.WithCancel(d.rootCtx)
	rs := &remoteServer{
		client:     client,
		serverURL:  serverURL,
		token:      token,
		runtimeIDs: allRuntimeIDs,
		cancel:     remoteCancel,
	}

	for _, rid := range allRuntimeIDs {
		go d.runRemoteHeartbeat(remoteCtx, client, rid)
	}

	d.remotesMu.Lock()
	d.remoteServers[serverURL] = rs
	d.remotesMu.Unlock()

	d.logger.Info("remote server added", "server_url", serverURL, "runtimes", len(allRuntimeIDs))
	return allRuntimes, nil
}

// RemoveRemoteServer disconnects from a remote server: deregisters runtimes,
// stops heartbeats, and removes from the map.
func (d *Daemon) RemoveRemoteServer(serverURL string) error {
	d.remotesMu.Lock()
	rs, ok := d.remoteServers[serverURL]
	if !ok {
		d.remotesMu.Unlock()
		return fmt.Errorf("remote server not found: %s", serverURL)
	}
	delete(d.remoteServers, serverURL)
	d.remotesMu.Unlock()

	rs.cancel()

	if len(rs.runtimeIDs) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := rs.client.Deregister(ctx, rs.runtimeIDs); err != nil {
			d.logger.Warn("failed to deregister remote runtimes", "server_url", serverURL, "error", err)
		}
	}

	d.logger.Info("remote server removed", "server_url", serverURL)
	return nil
}

// deregisterAllRemotes cleans up all remote server connections on daemon shutdown.
func (d *Daemon) deregisterAllRemotes() {
	d.remotesMu.Lock()
	servers := make(map[string]*remoteServer, len(d.remoteServers))
	for k, v := range d.remoteServers {
		servers[k] = v
	}
	d.remotesMu.Unlock()

	for url, rs := range servers {
		rs.cancel()
		if len(rs.runtimeIDs) > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := rs.client.Deregister(ctx, rs.runtimeIDs); err != nil {
				d.logger.Warn("failed to deregister remote runtimes on shutdown", "server_url", url, "error", err)
			}
			cancel()
		}
	}
}

// ListRemoteServers returns the currently connected remote servers.
func (d *Daemon) ListRemoteServers() []RemoteServerInfo {
	d.remotesMu.Lock()
	defer d.remotesMu.Unlock()
	var result []RemoteServerInfo
	for url, rs := range d.remoteServers {
		result = append(result, RemoteServerInfo{
			ServerURL:    url,
			RuntimeCount: len(rs.runtimeIDs),
		})
	}
	return result
}

// RemoteServerInfo is the JSON response shape for remote server listings.
type RemoteServerInfo struct {
	ServerURL    string `json:"server_url"`
	RuntimeCount int    `json:"runtime_count"`
}

// registerRuntimesWithClient registers the daemon's agent runtimes against
// a specific server using the provided client. Factored out of
// registerRuntimesForWorkspace so remote servers can reuse the same logic.
func (d *Daemon) registerRuntimesWithClient(ctx context.Context, client *Client, workspaceID string) (*RegisterResponse, error) {
	d.logger.Debug("registering runtimes with client", "server_url", client.baseURL, "workspace_id", workspaceID)
	var runtimes []map[string]string
	for name, entry := range d.cfg.Agents {
		var version string
		var err error
		if entry.IsWSL {
			version, err = agent.DetectVersionWSL(ctx, entry.Path)
		} else {
			version, err = detectAgentVersion(ctx, entry.Path)
		}
		if err != nil {
			continue
		}
		if err := checkAgentMinVersion(name, version); err != nil {
			continue
		}
		d.setAgentVersion(name, version)
		displayName := name
		if len(name) > 0 {
			displayName = string(name[0]-32) + name[1:]
		}
		if d.cfg.DeviceName != "" {
			displayName = fmt.Sprintf("%s (%s)", displayName, d.cfg.DeviceName)
		}
		runtimes = append(runtimes, map[string]string{
			"name":    displayName,
			"type":    name,
			"version": version,
			"status":  "online",
		})
	}
	if len(runtimes) == 0 {
		return nil, fmt.Errorf("no agent runtimes could be registered")
	}

	req := map[string]any{
		"workspace_id":      workspaceID,
		"daemon_id":         d.cfg.DaemonID,
		"legacy_daemon_ids": d.cfg.LegacyDaemonIDs,
		"device_name":       d.cfg.DeviceName,
		"cli_version":       d.cfg.CLIVersion,
		"launched_by":       d.cfg.LaunchedBy,
		"runtimes":          runtimes,
	}

	resp, err := client.Register(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("register runtimes: %w", err)
	}
	if len(resp.Runtimes) == 0 {
		return nil, fmt.Errorf("register runtimes: empty response")
	}
	return resp, nil
}

// runRemoteHeartbeat runs the heartbeat loop for a single runtime on a
// remote server. Uses the provided client instead of d.client.
func (d *Daemon) runRemoteHeartbeat(ctx context.Context, client *Client, rid string) {
	interval := d.cfg.HeartbeatInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	jitter := time.Duration(rand.Int63n(int64(interval)))
	if jitter > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(jitter):
		}
	}

	sendBeat := func() {
		resp, err := client.SendHeartbeat(ctx, rid)
		if err != nil {
			if ctx.Err() == nil {
				d.logger.Warn("remote heartbeat failed", "runtime_id", rid, "server_url", client.baseURL, "error", err)
			}
			return
		}
		if resp != nil && resp.RuntimeGone {
			d.logger.Info("remote runtime gone, stopping heartbeat", "runtime_id", rid)
		}
	}

	sendBeat()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sendBeat()
		}
	}
}
