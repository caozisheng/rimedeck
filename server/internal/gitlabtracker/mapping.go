package gitlabtracker

import "strings"

// MappingKind identifies a GitLab label consumed by a native RimeDeck field.
type MappingKind string

const (
	MappingNone     MappingKind = "none"
	MappingWorkflow MappingKind = "workflow"
	MappingPriority MappingKind = "priority"
)

var workflowByLabel = map[string]string{
	"workflow::todo":        "todo",
	"workflow::in-progress": "in_progress",
	"workflow::in-review":   "in_review",
	"workflow::backlog":     "backlog",
	"workflow::done":        "done",
}

var labelByWorkflow = map[string]string{
	"backlog":     "workflow::backlog",
	"todo":        "workflow::todo",
	"in_progress": "workflow::in-progress",
	"in_review":   "workflow::in-review",
	"done":        "workflow::done",
}

var priorityByLabel = map[string]string{
	"priority::none":   "none",
	"priority::low":    "low",
	"priority::medium": "medium",
	"priority::high":   "high",
	"priority::urgent": "urgent",
}

var labelByPriority = map[string]string{
	"none":   "priority::none",
	"low":    "priority::low",
	"medium": "priority::medium",
	"high":   "priority::high",
	"urgent": "priority::urgent",
}

// ClassifyLabel returns the native-field dimension consumed by an exact label.
func ClassifyLabel(name string) MappingKind {
	name = strings.ToLower(strings.TrimSpace(name))
	if _, ok := workflowByLabel[name]; ok {
		return MappingWorkflow
	}
	if _, ok := priorityByLabel[name]; ok {
		return MappingPriority
	}
	return MappingNone
}

// ProjectIssueFields derives RimeDeck's status and priority from GitLab data.
func ProjectIssueFields(state string, labels []string) (status, priority string) {
	status = "backlog"
	priority = "none"
	for _, label := range labels {
		name := strings.ToLower(strings.TrimSpace(label))
		if value, ok := workflowByLabel[name]; ok && workflowRank(value) > workflowRank(status) {
			status = value
		}
		if value, ok := priorityByLabel[name]; ok && priorityRank(value) > priorityRank(priority) {
			priority = value
		}
	}
	if strings.EqualFold(state, "closed") {
		status = "done"
	}
	return status, priority
}

// CanonicalLabels replaces mapping labels with the canonical native values.
func CanonicalLabels(status, priority string, ordinary []string) []string {
	out := make([]string, 0, len(ordinary)+2)
	seen := make(map[string]struct{}, len(ordinary)+2)
	for _, label := range ordinary {
		name := strings.TrimSpace(label)
		if name == "" || ClassifyLabel(name) != MappingNone {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	if label, ok := labelByWorkflow[status]; ok {
		out = append(out, label)
	}
	if label, ok := labelByPriority[priority]; ok {
		out = append(out, label)
	}
	return out
}

func workflowRank(value string) int {
	switch value {
	case "todo":
		return 1
	case "in_progress":
		return 2
	case "in_review":
		return 3
	case "done":
		return 4
	default:
		return 0
	}
}
func priorityRank(value string) int {
	switch value {
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	case "urgent":
		return 4
	default:
		return 0
	}
}
