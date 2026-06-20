package daemon

import (
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// BuildPrompt constructs the task prompt for an agent CLI.
// Keep this minimal — detailed instructions live in CLAUDE.md / AGENTS.md
// injected by execenv.InjectRuntimeConfig. The provider string is threaded
// through to comment-triggered tasks' per-turn reply template; that template
// is provider-agnostic now (Linux/macOS → quoted-HEREDOC stdin, Windows →
// file) because the shell-layer corruption it guards against is not specific
// to any one provider (MUL-2904).
func BuildPrompt(task Task, provider string) string {
	if task.ChatSessionID != "" {
		return buildChatPrompt(task)
	}
	if task.TriggerCommentID != "" {
		return buildCommentPrompt(task, provider)
	}
	if task.AutopilotRunID != "" {
		return buildAutopilotPrompt(task)
	}
	if task.QuickCreatePrompt != "" {
		return buildQuickCreatePrompt(task)
	}
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	fmt.Fprintf(&b, "Your assigned issue ID is: %s\n\n", task.IssueID)
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then complete it.\n", task.IssueID)
	fmt.Fprintf(&b, "For comment history, follow the rule in your runtime procedure file (assignment-triggered tasks treat the read as mandatory). `multica issue comment list %s --output json` returns all comments for the issue (server caps at 2000). On long-running issues use `--recent 20 --output json` to read the 20 most recently active threads, then page older threads via the stderr `Next thread cursor: ...` line and the matching `--before` / `--before-id` until you have enough history. `--since <RFC3339>` is still available for incremental polling and may combine with `--recent`.\n", task.IssueID)
	return b.String()
}

// buildQuickCreatePrompt constructs a prompt for quick-create tasks. The issue
// has already been pre-created by the server with a placeholder title; the
// agent's job is to refine it (title, description, priority, optionally
// assignee) via a single `multica issue update` invocation.
func buildQuickCreatePrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a quick-create assistant for a Multica workspace.\n\n")
	fmt.Fprintf(&b, "An issue has already been created: **%s** (id: `%s`). ", task.IssueIdentifier, task.IssueID)
	b.WriteString("Your job is to refine it from the user's input below using a single `multica issue update` command. ")
	b.WriteString("The issue is already visible on the kanban with a placeholder title; the user is waiting for you to improve it.\n\n")
	fmt.Fprintf(&b, "User input:\n> %s\n\n", task.QuickCreatePrompt)

	b.WriteString("Field rules:\n\n")

	// title
	b.WriteString("- **title** (`--title`): required. A concise but semantically rich summary. If the input references external resources (PRs, issues, URLs), use your judgment on whether fetching the resource would produce a meaningfully better title — e.g. \"review PR #123\" → \"Review PR #123: Refactor auth module to OAuth2\". Strip filler words but preserve key semantic information.\n\n")

	// description
	b.WriteString("- **description** (`--description` or `--description-file`): The description is the executing agent's primary context. Aim for high fidelity — they should grasp the user's intent as if they had read the raw input themselves. Use a two-section structure:\n\n")
	b.WriteString("  1. **User request** — Faithfully restate what the user wants in their own words. Preserve specific names, identifiers, file paths, code snippets, and technical terms verbatim. Strip non-spec material before writing it (this is removal, not paraphrasing): verbal routing wrappers about creating the issue or routing it (e.g. \"create an issue\", \"分配给 X\", \"让 @X 处理\") and pure conversational fillers (e.g. \"对吧？\"). When in doubt, keep it.\n\n")
	b.WriteString("     CC exception: the platform auto-subscribes members whose `[@Name](mention://member/<uuid>)` link appears in the description. When the user wrote \"cc @Y\", strip the verbal \"cc\" wrapper from the User request body and append a final `CC: <mention link(s)>` line to the description so the cc routing still fires.\n\n")
	b.WriteString("  2. **Context** — include ONLY when the input cited external resources AND you successfully fetched them AND they produced verifiable facts worth recording. Summarize facts only (e.g. \"PR #45 changes auth to JWT\"), not interpretation or unsolicited reference implementations. If you have nothing factual to add, omit the section entirely — never use it as an apology log for resources you could not fetch.\n\n")
	b.WriteString("  Hard rules: never invent requirements, implementation details, or acceptance criteria the user did not express; never reduce multi-sentence input to a single vague sentence; never echo the title.\n\n")

	// priority
	b.WriteString("- **priority** (`--priority`): one of `urgent`, `high`, `medium`, `low`, or omit. Map P0/P1 → urgent/high; \"asap\" → urgent. If unspecified, omit (the issue keeps its default priority).\n\n")

	// assignee
	b.WriteString("- **assignee** (`--assignee-id` or `--assignee`): The issue already has a default assignee set by the server. Only change it if the user's input explicitly names a different person or team.\n")
	b.WriteString("    - When the user names someone (\"assign to X\" / \"@X\"), call `multica workspace member list --output json`, `multica agent list --output json`, and `multica squad list --output json` and find the matching entity by display name. On a clean unambiguous match, pass `--assignee-id <uuid>`. On no match or ambiguous match, do NOT pass assignee flags — instead append a final line to the description: `Unrecognized assignee: X`.\n")
	b.WriteString("    - Treat bare @-routing as an assignee directive even when the user did not write the English word \"assign\". This includes Chinese imperatives like `让 @独立团 review 这个 PR`, `给 @X 处理`, or `交给 @X`; strip the leading `@`/`＠` before matching display names.\n")
	b.WriteString("    - When the user did NOT name an assignee, do NOT pass any assignee flags — the default is already correct.\n\n")

	// Fields already set by the server — do not change
	b.WriteString("- **project**, **parent**, **status**: already set by the server. Do NOT pass `--project`, `--parent`, or `--status` flags.\n")
	b.WriteString("- **attachments**: do NOT pass `--attachment`. The flag only accepts LOCAL file paths. Any image URL in the user input is already markdown — keep it inline in `--description` instead.\n\n")

	// output format
	b.WriteString("Output format:\n")
	fmt.Fprintf(&b, "- Run exactly one `multica issue update %s --title \"...\" --description-file <path>` invocation (write description to a temp file first). Do not retry for any reason — even on non-zero exit.\n", task.IssueID)
	fmt.Fprintf(&b, "- After success, print exactly one line: `Updated %s: <new title>` and exit. No commentary, no follow-up tool calls.\n", task.IssueIdentifier)
	b.WriteString("- Do NOT call `multica issue create` — the issue already exists. Creating a new one would produce a duplicate.\n")
	b.WriteString("- Do NOT call `multica issue comment add` — there is nothing to comment on.\n")
	b.WriteString("- On CLI error, exit with the error as the only output. The platform writes a failure notification automatically.\n")
	return b.String()
}

// buildCommentPrompt constructs a prompt for comment-triggered tasks.
// The triggering comment content is embedded directly so the agent cannot
// miss it, even when stale output files exist in a reused workdir.
// The reply instructions (including the current TriggerCommentID as --parent)
// are re-emitted on every turn so resumed sessions cannot carry forward a
// previous turn's --parent UUID.
func buildCommentPrompt(task Task, provider string) string {
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	fmt.Fprintf(&b, "Your assigned issue ID is: %s\n\n", task.IssueID)
	if task.TriggerCommentContent != "" {
		authorLabel := "A user"
		if task.TriggerAuthorType == "agent" {
			name := task.TriggerAuthorName
			if name == "" {
				name = "another agent"
			}
			authorLabel = fmt.Sprintf("Another agent (%s)", name)
		}
		fmt.Fprintf(&b, "[NEW COMMENT] %s just left a new comment. Focus on THIS comment — do not confuse it with previous ones:\n\n", authorLabel)
		fmt.Fprintf(&b, "> %s\n\n", task.TriggerCommentContent)
		if task.TriggerAuthorType == "agent" {
			b.WriteString("⚠️ The triggering comment was posted by another agent. Decide whether a reply is warranted. If you produced actual work this turn (investigated, fixed something, answered a real question), post the result as a normal reply — that is NOT a noise comment, and the standard rule that final results must be delivered via comment still applies. If the triggering comment was a pure acknowledgment, thanks, or sign-off AND you produced no work this turn, do NOT reply — and do NOT post a comment saying 'No reply needed' or similar. Simply exit with no output. Silence is the preferred way to end agent-to-agent threads. If you do reply, do not @mention the other agent as a sign-off (that re-triggers them and starts a loop).\n\n")
		}
		if task.Agent != nil && strings.Contains(task.Agent.Instructions, "## Squad Operating Protocol") {
			fmt.Fprintf(&b, "⚠️ **Squad leader no_action rule:** If you decide no action is needed, call `multica squad activity %s no_action --reason \"...\"` and EXIT. DO NOT post any comment — not even one that says \"no action needed\" or \"exiting silently\". The squad activity call records your decision; a comment is redundant noise.\n\n", task.IssueID)
		}
	}
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then decide how to proceed.\n\n", task.IssueID)
	// Comment-reading pointer. Warm path with new comments: issue-wide
	// since-delta count, but steer the agent to read the triggering thread
	// first. Warm resumed path with no new comments: the trigger is already
	// injected, so don't force a duplicate thread read. Cold path: read the
	// triggering thread, not the flat timeline. Final fallback (no trigger id,
	// shouldn't happen here): plain read.
	if hint := execenv.BuildNewCommentsHint(task.IssueID, task.TriggerCommentID, task.NewCommentsSince, task.NewCommentCount); hint != "" {
		b.WriteString(hint)
	} else if task.PriorSessionID != "" {
		b.WriteString(execenv.BuildResumedCommentsHint(task.IssueID, task.TriggerCommentID))
	} else if cold := execenv.BuildColdCommentsHint(task.IssueID, task.TriggerCommentID); cold != "" {
		b.WriteString(cold)
	} else {
		fmt.Fprintf(&b, "Read the discussion: `multica issue comment list %s --output json` (long issue? use `--recent 20`).\n\n", task.IssueID)
	}
	b.WriteString(execenv.BuildCommentReplyInstructions(provider, task.IssueID, task.TriggerCommentID))
	return b.String()
}

// buildChatPrompt constructs a prompt for interactive chat tasks.
func buildChatPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a chat assistant for a Multica workspace.\n")
	b.WriteString("A user is chatting with you directly. Respond to their message.\n\n")
	if task.Agent != nil && len(task.Agent.Skills) > 0 {
		refs := ExtractSlashSkills(task.ChatMessage)
		if len(refs) > 0 {
			agentSkills := make(map[string]string, len(task.Agent.Skills))
			for _, s := range task.Agent.Skills {
				agentSkills[s.ID] = s.Name
			}

			selected := make([]string, 0, len(refs))
			seen := make(map[string]struct{}, len(refs))
			for _, ref := range refs {
				name, ok := agentSkills[ref.ID]
				if !ok {
					continue
				}
				if _, ok := seen[ref.ID]; ok {
					continue
				}
				seen[ref.ID] = struct{}{}
				selected = append(selected, name)
			}

			if len(selected) > 0 {
				b.WriteString("Explicitly selected skills:\n")
				for _, name := range selected {
					fmt.Fprintf(&b, "- %s\n", name)
				}
				b.WriteString("\n")
			}
		}
	}
	fmt.Fprintf(&b, "User message:\n%s\n", task.ChatMessage)
	// List attachments by id + filename so the agent can fetch them via
	// the CLI. We deliberately do NOT inline the URL: chat attachments
	// live behind a signed CDN with a short TTL, so by the time the agent
	// has finished thinking the URL embedded in the markdown body may
	// have expired. `multica attachment download <id>` re-signs at click
	// time and is the only reliable path.
	if len(task.ChatMessageAttachments) > 0 {
		b.WriteString("\nAttachments on this message:\n")
		for _, a := range task.ChatMessageAttachments {
			if a.ContentType != "" {
				fmt.Fprintf(&b, "- id=%s filename=%q content_type=%s\n", a.ID, a.Filename, a.ContentType)
			} else {
				fmt.Fprintf(&b, "- id=%s filename=%q\n", a.ID, a.Filename)
			}
		}
		b.WriteString("Use `multica attachment download <id>` to fetch each file locally before referring to it.\n")
		b.WriteString("When creating an issue that should preserve one of these attachments, pass `--attachment-id <id>` to `multica issue create` in addition to keeping the attachment markdown inline.\n")
	}
	return b.String()
}

// buildAutopilotPrompt constructs a prompt for run_only autopilot tasks.
func buildAutopilotPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	b.WriteString("This task was triggered by an Autopilot in run-only mode. There is no assigned Multica issue for this run.\n\n")
	fmt.Fprintf(&b, "Autopilot run ID: %s\n", task.AutopilotRunID)
	if task.AutopilotID != "" {
		fmt.Fprintf(&b, "Autopilot ID: %s\n", task.AutopilotID)
	}
	if task.AutopilotTitle != "" {
		fmt.Fprintf(&b, "Autopilot title: %s\n", task.AutopilotTitle)
	}
	if task.AutopilotSource != "" {
		fmt.Fprintf(&b, "Trigger source: %s\n", task.AutopilotSource)
	}
	if strings.TrimSpace(string(task.AutopilotTriggerPayload)) != "" {
		fmt.Fprintf(&b, "Trigger payload:\n%s\n", strings.TrimSpace(string(task.AutopilotTriggerPayload)))
	}
	b.WriteString("\nAutopilot instructions:\n")
	if strings.TrimSpace(task.AutopilotDescription) != "" {
		b.WriteString(task.AutopilotDescription)
		b.WriteString("\n\n")
	} else if task.AutopilotTitle != "" {
		fmt.Fprintf(&b, "%s\n\n", task.AutopilotTitle)
	} else {
		b.WriteString("No additional autopilot instructions were provided. Inspect the autopilot configuration before proceeding.\n\n")
	}
	if task.AutopilotID != "" {
		fmt.Fprintf(&b, "Start by running `multica autopilot get %s --output json` if you need the full autopilot configuration, then complete the instructions above.\n", task.AutopilotID)
	} else {
		b.WriteString("Complete the instructions above.\n")
	}
	b.WriteString("Do not run `multica issue get`; this run does not have an issue ID.\n")
	return b.String()
}
