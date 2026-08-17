package gitlabtracker

import (
	"reflect"
	"testing"
)

func TestClassifyLabel(t *testing.T) {
	tests := []struct {
		name string
		want MappingKind
	}{
		{"workflow todo", "workflow"},
		{"workflow case insensitive", "workflow"},
		{"workflow backlog", "workflow"},
		{"priority high", "priority"},
		{"unknown workflow prefix", "none"},
		{"ordinary", "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := map[string]string{
				"workflow todo":             "workflow::todo",
				"workflow case insensitive": "WORKFLOW::IN-PROGRESS",
				"priority high":             "priority::high",
				"workflow backlog":          "workflow::backlog",
				"unknown workflow prefix":   "workflow::blocked",
				"ordinary":                  "bug",
			}[tt.name]
			if got := ClassifyLabel(input); got != tt.want {
				t.Fatalf("ClassifyLabel(%q) = %q, want %q", input, got, tt.want)
			}
		})
	}
}

func TestProjectIssueFields(t *testing.T) {
	tests := []struct {
		name, state  string
		labels       []string
		wantStatus   string
		wantPriority string
	}{
		{"defaults", "opened", nil, "backlog", "none"},
		{"workflow and priority", "opened", []string{"workflow::in-progress", "priority::high"}, "in_progress", "high"},
		{"precedence", "opened", []string{"workflow::todo", "workflow::done", "priority::low", "priority::medium"}, "done", "medium"},
		{"closed override", "closed", []string{"workflow::todo"}, "done", "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, priority := ProjectIssueFields(tt.state, tt.labels)
			if status != tt.wantStatus || priority != tt.wantPriority {
				t.Fatalf("ProjectIssueFields() = %q,%q, want %q,%q", status, priority, tt.wantStatus, tt.wantPriority)
			}
		})
	}
}

func TestProjectIssueFields_ReportedLabels(t *testing.T) {
	tests := []struct {
		name, label, wantStatus, wantPriority string
	}{
		{"backlog", "workflow::backlog", "backlog", "none"},
		{"in progress", "workflow::in-progress", "in_progress", "none"},
		{"high priority", "priority::high", "backlog", "high"},
		{"medium priority", "priority::medium", "backlog", "medium"},
		{"due-date payload unaffected", "workflow::in-progress", "in_progress", "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, priority := ProjectIssueFields("opened", []string{tt.label})
			if status != tt.wantStatus || priority != tt.wantPriority {
				t.Fatalf("ProjectIssueFields(%q) = %q,%q, want %q,%q", tt.label, status, priority, tt.wantStatus, tt.wantPriority)
			}
		})
	}
}

func TestCanonicalLabels(t *testing.T) {
	got := CanonicalLabels("in_review", "high", []string{"bug", "workflow::todo", "priority::low", "bug"})
	want := []string{"bug", "workflow::in-review", "priority::high"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CanonicalLabels() = %#v, want %#v", got, want)
	}
	if got := CanonicalLabels("backlog", "urgent", []string{"bug", "workflow::done", "priority::high"}); !reflect.DeepEqual(got, []string{"bug"}) {
		t.Fatalf("unmapped fields should remove mapping labels: %#v", got)
	}
}
