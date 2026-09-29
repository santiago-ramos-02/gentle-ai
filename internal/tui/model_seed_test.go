package tui

import (
	"testing"

	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
	"github.com/gentleman-programming/gentle-ai/v4/internal/state"
	"github.com/gentleman-programming/gentle-ai/v4/internal/system"
)

// TestNewModelSeedsCodexAssignmentsFromState guards reinstalls: install
// persistence republishes the selection's Codex fields, so a selection that
// did not start from state would clear the user's Codex model configuration.
func TestNewModelSeedsCodexAssignmentsFromState(t *testing.T) {
	m := NewModel(system.DetectionResult{}, "test", state.InstallState{
		CodexModelAssignments:       map[string]string{"odd-worker": "xhigh"},
		CodexOrchestratorAssignment: &state.CodexOrchestratorAssignmentState{Model: "gpt-6-astra", Effort: "medium"},
		CodexCarrilModelAssignments: map[string]string{"sdd-strong": "gpt-6-astra"},
		CodexPhaseModelAssignments:  map[string]string{"odd-worker": "gpt-6-sol"},
		KiroModelAssignments:        map[string]string{"odd-worker": "qwen"},
	})
	sel := m.Selection
	if sel.CodexModelAssignments["odd-worker"] != model.CodexEffortXHigh ||
		sel.CodexOrchestratorAssignment == nil || sel.CodexOrchestratorAssignment.Model != "gpt-6-astra" ||
		sel.CodexCarrilModelAssignments["sdd-strong"] != "gpt-6-astra" ||
		sel.CodexPhaseModelAssignments["odd-worker"] != "gpt-6-sol" ||
		sel.KiroModelAssignments["odd-worker"] != model.KiroModelQwen {
		t.Fatalf("seeded selection = %+v", sel)
	}
}
