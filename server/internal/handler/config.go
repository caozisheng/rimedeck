package handler

import (
	"net/http"
	"os"
)

type AppConfig struct {
	CdnDomain string `json:"cdn_domain"`
	// Public auth config consumed by the web app at runtime so self-hosted
	// deployments do not need to rebuild the frontend image when operators
	// toggle signup.
	AllowSignup bool `json:"allow_signup"`
	// WorkspaceCreationDisabled mirrors the server-side
	// DISABLE_WORKSPACE_CREATION env var so the UI can hide every
	// "Create workspace" affordance on self-hosted instances.
	WorkspaceCreationDisabled bool `json:"workspace_creation_disabled,omitempty"`
}

func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	config := AppConfig{
		AllowSignup:               os.Getenv("ALLOW_SIGNUP") != "false",
		WorkspaceCreationDisabled: os.Getenv("DISABLE_WORKSPACE_CREATION") == "true",
	}
	if h.Storage != nil {
		config.CdnDomain = h.Storage.CdnDomain()
	}
	writeJSON(w, http.StatusOK, config)
}
