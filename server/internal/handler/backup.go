package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	backupVersion    = 1
	importMaxBytes   = 10 << 20 // 10 MB
	backupTimeFormat = time.RFC3339
)

// --- Export / Import JSON types ---

type BackupData struct {
	Version    int           `json:"version"`
	ExportedAt string        `json:"exported_at"`
	AppVersion string        `json:"app_version"`
	Skills     []BackupSkill `json:"skills"`
	Agents     []BackupAgent `json:"agents"`
	Squads     []BackupSquad `json:"squads"`
}

type BackupSkill struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Content     string            `json:"content"`
	Config      json.RawMessage   `json:"config"`
	Files       []BackupSkillFile `json:"files"`
}

type BackupSkillFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type BackupAgent struct {
	Name               string          `json:"name"`
	Description        string          `json:"description"`
	Instructions       string          `json:"instructions"`
	RuntimeMode        string          `json:"runtime_mode"`
	RuntimeConfig      json.RawMessage `json:"runtime_config"`
	CustomArgs         json.RawMessage `json:"custom_args"`
	McpConfig          json.RawMessage `json:"mcp_config"`
	Model              *string         `json:"model"`
	ThinkingLevel      *string         `json:"thinking_level"`
	Visibility         string          `json:"visibility"`
	MaxConcurrentTasks int32           `json:"max_concurrent_tasks"`
	SkillNames         []string        `json:"skill_names"`
}

type BackupSquad struct {
	Name         string              `json:"name"`
	Description  string              `json:"description"`
	Instructions string              `json:"instructions"`
	LeaderName   string              `json:"leader_name"`
	Members      []BackupSquadMember `json:"members"`
}

type BackupSquadMember struct {
	MemberType string `json:"member_type"`
	Name       string `json:"name,omitempty"`
	Email      string `json:"email,omitempty"`
	Role       string `json:"role"`
}

type ImportRequest struct {
	BackupData
	RuntimeID string `json:"runtime_id"`
	Overwrite bool   `json:"overwrite"`
}

type ImportResultCounts struct {
	Skills int `json:"skills"`
	Agents int `json:"agents"`
	Squads int `json:"squads"`
}

type ImportResult struct {
	Created  ImportResultCounts `json:"created"`
	Skipped  ImportResultCounts `json:"skipped"`
	Warnings []string           `json:"warnings"`
	Errors   []string           `json:"errors"`
}

// --- Export handler ---

func (h *Handler) ExportBackup(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	wsUUID := parseUUID(workspaceID)
	ctx := r.Context()

	// Query param filters
	filterAgents := parseCSVParam(r, "agents")
	filterSkills := parseCSVParam(r, "skills")
	filterSquads := parseCSVParam(r, "squads")

	// 1. Load all un-archived skills (with content)
	skills, err := h.Queries.ListSkillsByWorkspace(ctx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skills")
		return
	}

	// 2. Batch-load all skill files
	skillFiles, err := h.Queries.ListSkillFilesByWorkspace(ctx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list skill files")
		return
	}
	filesBySkill := make(map[string][]db.SkillFile)
	for _, sf := range skillFiles {
		key := uuidToString(sf.SkillID)
		filesBySkill[key] = append(filesBySkill[key], sf)
	}

	// 3. Load un-archived agents
	agents, err := h.Queries.ListAgents(ctx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}

	// 4. Load agent-skill associations
	agentSkills, err := h.Queries.ListAgentSkillsByWorkspace(ctx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent skills")
		return
	}
	skillsByAgent := make(map[string][]string)
	for _, as := range agentSkills {
		key := uuidToString(as.AgentID)
		skillsByAgent[key] = append(skillsByAgent[key], as.Name)
	}

	// 5. Load un-archived squads
	squads, err := h.Queries.ListSquads(ctx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list squads")
		return
	}

	// 6. Batch-load squad members
	squadMemberMap := make(map[string][]db.SquadMember)
	for _, sq := range squads {
		members, err := h.Queries.ListSquadMembers(ctx, sq.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list squad members")
			return
		}
		squadMemberMap[uuidToString(sq.ID)] = members
	}

	// 7. Build agent ID → name map (for squad leader/member resolution)
	agentByID := make(map[string]string)
	for _, a := range agents {
		agentByID[uuidToString(a.ID)] = a.Name
	}

	// 8. Build member ID → email map (for workspace member resolution)
	membersWithUser, err := h.Queries.ListMembersWithUser(ctx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workspace members")
		return
	}
	memberEmailByID := make(map[string]string)
	for _, m := range membersWithUser {
		memberEmailByID[uuidToString(m.UserID)] = m.UserEmail
	}

	// Assemble export data
	data := BackupData{
		Version:    backupVersion,
		ExportedAt: time.Now().UTC().Format(backupTimeFormat),
		AppVersion: "",
	}

	// Skills
	for _, s := range skills {
		if len(filterSkills) > 0 && !containsStr(filterSkills, s.Name) {
			continue
		}
		bs := BackupSkill{
			Name:        s.Name,
			Description: s.Description,
			Content:     s.Content,
			Config:      normalizeJSON(s.Config),
		}
		for _, f := range filesBySkill[uuidToString(s.ID)] {
			bs.Files = append(bs.Files, BackupSkillFile{Path: f.Path, Content: f.Content})
		}
		if bs.Files == nil {
			bs.Files = []BackupSkillFile{}
		}
		data.Skills = append(data.Skills, bs)
	}

	// Agents
	for _, a := range agents {
		if len(filterAgents) > 0 && !containsStr(filterAgents, a.Name) {
			continue
		}
		ba := BackupAgent{
			Name:               a.Name,
			Description:        a.Description,
			Instructions:       a.Instructions,
			RuntimeMode:        a.RuntimeMode,
			RuntimeConfig:      normalizeJSON(a.RuntimeConfig),
			CustomArgs:         normalizeJSON(a.CustomArgs),
			McpConfig:          normalizeJSON(a.McpConfig),
			Model:              textToPtr(a.Model),
			ThinkingLevel:      textToPtr(a.ThinkingLevel),
			Visibility:         a.Visibility,
			MaxConcurrentTasks: a.MaxConcurrentTasks,
			SkillNames:         skillsByAgent[uuidToString(a.ID)],
		}
		if ba.SkillNames == nil {
			ba.SkillNames = []string{}
		}
		data.Agents = append(data.Agents, ba)
	}

	// Squads
	for _, sq := range squads {
		if len(filterSquads) > 0 && !containsStr(filterSquads, sq.Name) {
			continue
		}
		leaderName := agentByID[uuidToString(sq.LeaderID)]
		bs := BackupSquad{
			Name:         sq.Name,
			Description:  sq.Description,
			Instructions: sq.Instructions,
			LeaderName:   leaderName,
		}
		for _, m := range squadMemberMap[uuidToString(sq.ID)] {
			bm := BackupSquadMember{
				MemberType: m.MemberType,
				Role:       m.Role,
			}
			if m.MemberType == "agent" {
				bm.Name = agentByID[uuidToString(m.MemberID)]
			} else {
				bm.Email = memberEmailByID[uuidToString(m.MemberID)]
			}
			bs.Members = append(bs.Members, bm)
		}
		if bs.Members == nil {
			bs.Members = []BackupSquadMember{}
		}
		data.Squads = append(data.Squads, bs)
	}

	if data.Skills == nil {
		data.Skills = []BackupSkill{}
	}
	if data.Agents == nil {
		data.Agents = []BackupAgent{}
	}
	if data.Squads == nil {
		data.Squads = []BackupSquad{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="backup.json"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode backup")
	}
}

// --- Import handler ---

func (h *Handler) ImportBackup(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	wsUUID := parseUUID(workspaceID)
	ctx := r.Context()

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID := parseUUID(userID)

	r.Body = http.MaxBytesReader(w, r.Body, importMaxBytes)

	var req ImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(w, http.StatusRequestEntityTooLarge, "backup file exceeds 10MB limit")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Version != backupVersion {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported backup version %d (expected %d)", req.Version, backupVersion))
		return
	}

	runtimeUUID := pgtype.UUID{}
	if req.RuntimeID != "" {
		var parseOk bool
		runtimeUUID, parseOk = parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
		if !parseOk {
			return
		}
		// Validate runtime exists
		_, err := h.Queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{
			ID:          runtimeUUID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid runtime_id: runtime not found in workspace")
			return
		}
	}

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	result := ImportResult{
		Warnings: []string{},
		Errors:   []string{},
	}

	// Phase 1: Skills
	skillIDByName := make(map[string]pgtype.UUID)
	// Pre-load existing skills for name resolution
	existingSkills, err := qtx.ListSkillsByWorkspace(ctx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list existing skills")
		return
	}
	for _, s := range existingSkills {
		skillIDByName[s.Name] = s.ID
	}

	for _, bs := range req.Skills {
		if _, exists := skillIDByName[bs.Name]; exists {
			if !req.Overwrite {
				result.Skipped.Skills++
				continue
			}
			// Overwrite: delete existing and recreate
			existing, _ := qtx.GetSkillByWorkspaceAndName(ctx, db.GetSkillByWorkspaceAndNameParams{
				WorkspaceID: wsUUID,
				Name:        bs.Name,
			})
			// skill_file and agent_skill have ON DELETE CASCADE, so deleting the
			// skill row cleans up both automatically.
			if err := qtx.DeleteSkill(ctx, db.DeleteSkillParams{ID: existing.ID, WorkspaceID: wsUUID}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to delete skill for overwrite")
				return
			}
			delete(skillIDByName, bs.Name)
		}
		config := bs.Config
		if config == nil {
			config = json.RawMessage("{}")
		}
		skill, err := qtx.CreateSkill(ctx, db.CreateSkillParams{
			WorkspaceID: wsUUID,
			Name:        bs.Name,
			Description: bs.Description,
			Content:     bs.Content,
			Config:      config,
			CreatedBy:   userUUID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create skill %q: %v", bs.Name, err))
			return
		}
		skillIDByName[bs.Name] = skill.ID
		for _, f := range bs.Files {
			_, err := qtx.UpsertSkillFile(ctx, db.UpsertSkillFileParams{
				SkillID: skill.ID,
				Path:    f.Path,
				Content: f.Content,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create skill file %q/%q: %v", bs.Name, f.Path, err))
				return
			}
		}
		result.Created.Skills++
	}

	// Phase 2: Agents
	agentIDByName := make(map[string]pgtype.UUID)
	existingAgents, err := qtx.ListAllAgents(ctx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list existing agents")
		return
	}
	for _, a := range existingAgents {
		agentIDByName[a.Name] = a.ID
	}

	for _, ba := range req.Agents {
		if _, exists := agentIDByName[ba.Name]; exists {
			if !req.Overwrite {
				result.Skipped.Agents++
				continue
			}
			// Agent exists and overwrite is set. Agent itself is not recreated
			// (complex state: tasks, issues), but skill bindings may have been
			// lost during skill overwrite (CASCADE delete on skill_id FK).
			// Re-bind skills to the existing agent.
			agentID := agentIDByName[ba.Name]
			for _, skillName := range ba.SkillNames {
				skillID, found := skillIDByName[skillName]
				if !found {
					result.Warnings = append(result.Warnings, fmt.Sprintf("Agent %q: skill %q not found, binding skipped", ba.Name, skillName))
					continue
				}
				_ = qtx.AddAgentSkill(ctx, db.AddAgentSkillParams{AgentID: agentID, SkillID: skillID})
			}
			result.Skipped.Agents++
			result.Warnings = append(result.Warnings, fmt.Sprintf("Agent %q: already exists, re-bound %d skills", ba.Name, len(ba.SkillNames)))
			continue
		}

		rc := ba.RuntimeConfig
		if rc == nil {
			rc = json.RawMessage("{}")
		}
		ca := ba.CustomArgs
		if ca == nil {
			ca = json.RawMessage("[]")
		}
		var mc []byte
		if ba.McpConfig != nil {
			mc = ba.McpConfig
		}
		mct := int32(6)
		if ba.MaxConcurrentTasks > 0 {
			mct = ba.MaxConcurrentTasks
		}

		agent, err := qtx.CreateAgent(ctx, db.CreateAgentParams{
			WorkspaceID:        wsUUID,
			Name:               ba.Name,
			Description:        ba.Description,
			Instructions:       ba.Instructions,
			RuntimeMode:        ba.RuntimeMode,
			RuntimeConfig:      rc,
			RuntimeID:          runtimeUUID,
			Visibility:         ba.Visibility,
			MaxConcurrentTasks: mct,
			OwnerID:            userUUID,
			CustomEnv:          json.RawMessage("{}"),
			CustomArgs:         ca,
			McpConfig:          mc,
			Model:              ptrToText(ba.Model),
			ThinkingLevel:      ptrToText(ba.ThinkingLevel),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create agent %q: %v", ba.Name, err))
			return
		}
		agentIDByName[ba.Name] = agent.ID

		// Bind skills
		for _, skillName := range ba.SkillNames {
			skillID, found := skillIDByName[skillName]
			if !found {
				result.Warnings = append(result.Warnings, fmt.Sprintf("Agent %q: skill %q not found, binding skipped", ba.Name, skillName))
				continue
			}
			if err := qtx.AddAgentSkill(ctx, db.AddAgentSkillParams{AgentID: agent.ID, SkillID: skillID}); err != nil {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to bind skill %q to agent %q: %v", skillName, ba.Name, err))
				return
			}
		}
		result.Created.Agents++
	}

	// Phase 3: Squads
	for _, bsq := range req.Squads {
		// Check if squad already exists
		existingSquad, err := qtx.GetSquadByWorkspaceAndName(ctx, db.GetSquadByWorkspaceAndNameParams{
			WorkspaceID: wsUUID,
			Name:        bsq.Name,
		})
		if err == nil && existingSquad.ID.Valid {
			result.Skipped.Squads++
			continue
		}

		// Resolve leader
		leaderID, leaderFound := agentIDByName[bsq.LeaderName]
		if !leaderFound {
			result.Errors = append(result.Errors, fmt.Sprintf("Squad %q: leader agent %q not found, squad skipped", bsq.Name, bsq.LeaderName))
			continue
		}

		squad, err := qtx.CreateSquad(ctx, db.CreateSquadParams{
			WorkspaceID: wsUUID,
			Name:        bsq.Name,
			Description: bsq.Description,
			LeaderID:    leaderID,
			CreatorID:   userUUID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create squad %q: %v", bsq.Name, err))
			return
		}

		// Update instructions if provided (CreateSquad doesn't accept instructions)
		if bsq.Instructions != "" {
			instrText := pgtype.Text{String: bsq.Instructions, Valid: true}
			_, err := qtx.UpdateSquad(ctx, db.UpdateSquadParams{
				ID:           squad.ID,
				Instructions: instrText,
			})
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("Squad %q: failed to set instructions", bsq.Name))
			}
		}

		// Add members
		for _, bm := range bsq.Members {
			var memberID pgtype.UUID
			switch bm.MemberType {
			case "agent":
				id, found := agentIDByName[bm.Name]
				if !found {
					result.Warnings = append(result.Warnings, fmt.Sprintf("Squad %q: agent member %q not found, skipped", bsq.Name, bm.Name))
					continue
				}
				memberID = id
			case "member":
				member, err := qtx.GetMemberByEmailAndWorkspace(ctx, db.GetMemberByEmailAndWorkspaceParams{
					Email:       bm.Email,
					WorkspaceID: wsUUID,
				})
				if err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("Squad %q: workspace member %q not found, skipped", bsq.Name, bm.Email))
					continue
				}
				memberID = member.UserID
			default:
				result.Warnings = append(result.Warnings, fmt.Sprintf("Squad %q: unknown member_type %q, skipped", bsq.Name, bm.MemberType))
				continue
			}

			role := bm.Role
			if role == "" {
				role = "member"
			}
			_, err := qtx.AddSquadMember(ctx, db.AddSquadMemberParams{
				SquadID:    squad.ID,
				MemberType: bm.MemberType,
				MemberID:   memberID,
				Role:       role,
			})
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("Squad %q: failed to add member: %v", bsq.Name, err))
			}
		}
		result.Created.Squads++
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit import transaction")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// --- helpers ---

func parseCSVParam(r *http.Request, key string) []string {
	v := r.URL.Query().Get(key)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func normalizeJSON(b []byte) json.RawMessage {
	if len(b) == 0 {
		return nil
	}
	return json.RawMessage(b)
}
