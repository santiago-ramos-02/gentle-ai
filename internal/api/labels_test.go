package api

import "testing"

func TestStepLabelsReadAsWords(t *testing.T) {
	for id, want := range map[string]string{
		"agent:claude-code":               "Set up Claude Code",
		"agent:native-review:claude-code": "Add review agents to Claude Code",
		"agent-guidance:opencode":         "Add workflow guidance to OpenCode",
		"component:engram":                "Install Engram",
		"prepare:backup-snapshot":         "Back up agent files",
		"prepare:opencode-telemetry":      "Opencode telemetry",
		"something-new":                   "Something new",
	} {
		if got := stepLabel(id); got != want {
			t.Errorf("stepLabel(%q) = %q, want %q", id, got, want)
		}
	}
}
