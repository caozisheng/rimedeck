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
		{"priority urgent", "priority"},
		{"unknown workflow prefix", "none"},
		{"ordinary", "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := map[string]string{
				"workflow todo":             "workflow::todo",
				"workflow case insensitive": "WORKFLOW::IN-PROGRESS",
				"workflow backlog":          "workflow::backlog",
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
		{"workflow and priority", "opened", []string{"workflow::in-progress", "priority::high"}, "in_progress", "high"},
		{"urgent priority precedence", "opened", []string{"priority::low", "priority::high", "priority::urgent", "priority::medium"}, "backlog", "urgent"},
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
		{"high priority", "priority::high", "backlog", "high"},
		{"medium priority", "priority::medium", "backlog", "medium"},
		{"urgent priority", "priority::urgent", "backlog", "urgent"},
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
	if got := CanonicalLabels("backlog", "urgent", []string{"bug", "workflow::done", "priority::high"}); !reflect.DeepEqual(got, []string{"bug", "priority::urgent"}) {
		t.Fatalf("canonical urgent should preserve the new remote mapping: %#v", got)
	}
}
func TestCanonicalLabels_UrgentAndNone(t *testing.T) {
	if got := CanonicalLabels("todo", "urgent", []string{"bug", "priority::low"}); !reflect.DeepEqual(got, []string{"bug", "workflow::todo", "priority::urgent"}) {
		t.Fatalf("urgent canonical labels = %#v", got)
	}
	if got := CanonicalLabels("todo", "none", []string{"priority::none", "priority::high"}); !reflect.DeepEqual(got, []string{"priority::none", "workflow::todo"}) {
		t.Fatalf("none canonical labels = %#v", got)
	}
}
