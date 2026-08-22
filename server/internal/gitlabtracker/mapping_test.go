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
		{"workflow backlog", "workflow"},
		{"workflow todo", "workflow"},
		{"workflow case insensitive", "workflow"},
		{"priority none", "priority"},
		{"priority low", "priority"},
		{"priority medium", "priority"},
		{"priority high", "priority"},
		{"priority urgent", "priority"},
		{"unknown workflow prefix", "none"},
		{"ordinary", "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := map[string]string{
				"workflow backlog":          "workflow::backlog",
				"workflow todo":             "workflow::todo",
				"workflow case insensitive": "WORKFLOW::IN-PROGRESS",
				"priority none":             "priority::none",
				"priority low":              "priority::low",
				"priority medium":           "priority::medium",
				"priority high":             "priority::high",
				"priority urgent":           "priority::urgent",
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
		{"explicit backlog and none", "opened", []string{"workflow::backlog", "priority::none"}, "backlog", "none"},
		{"workflow and priority", "opened", []string{"workflow::in-progress", "priority::high"}, "in_progress", "high"},
		{"urgent priority precedence", "opened", []string{"priority::low", "priority::high", "priority::urgent", "priority::medium"}, "backlog", "urgent"},
		{"precedence", "opened", []string{"workflow::todo", "workflow::done", "priority::low", "priority::medium"}, "done", "medium"},
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
		{"none priority", "priority::none", "backlog", "none"},
		{"low priority", "priority::low", "backlog", "low"},
		{"medium priority", "priority::medium", "backlog", "medium"},
		{"high priority", "priority::high", "backlog", "high"},
		{"urgent priority", "priority::urgent", "backlog", "urgent"},
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
	tests := []struct {
		name     string
		status   string
		priority string
		ordinary []string
		want     []string
	}{
		{"backlog and none", "backlog", "none", []string{"bug", "workflow::todo", "priority::high"}, []string{"bug", "workflow::backlog", "priority::none"}},
		{"all mapped values", "done", "urgent", []string{"bug"}, []string{"bug", "workflow::done", "priority::urgent"}},
		{"stale mappings replaced", "in_review", "low", []string{"bug", "workflow::todo", "priority::none", "priority::high"}, []string{"bug", "workflow::in-review", "priority::low"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanonicalLabels(tt.status, tt.priority, tt.ordinary); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("CanonicalLabels() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
