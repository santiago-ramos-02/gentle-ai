package model

import "testing"

// TestClaudePresetForRecognizesPersistedPresets covers the round trip through
// state.json, which never stores the orchestrator row a preset table carries.
func TestClaudePresetForRecognizesPersistedPresets(t *testing.T) {
	for _, preset := range ClaudeModelPresets() {
		assignments, ok := ClaudePresetAssignments(preset.ID, nil)
		if !ok {
			t.Fatalf("preset %q has no assignments", preset.ID)
		}
		if got := ClaudePresetFor(assignments); got != preset.ID {
			t.Errorf("ClaudePresetFor(%s as chosen) = %q", preset.ID, got)
		}
		delete(assignments, "orchestrator")
		if got := ClaudePresetFor(assignments); got != preset.ID {
			t.Errorf("ClaudePresetFor(%s as persisted) = %q", preset.ID, got)
		}
	}
	custom, _ := ClaudePresetAssignments("balanced", nil)
	custom["odd-worker"] = ClaudePhaseAssignment{Model: ClaudeModelOpus, Effort: ClaudeEffortHigh}
	if got := ClaudePresetFor(custom); got != "" {
		t.Errorf("edited assignments read as preset %q", got)
	}
}
