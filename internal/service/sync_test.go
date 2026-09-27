package service

import (
	"testing"

	"github.com/gentleman-programming/gentle-ai/v3/internal/model"
)

// TestModelAssignmentsToStateWiresEffort verifies that modelAssignmentsToState
// includes the Effort field in the serialisable output.
func TestModelAssignmentsToStateWiresEffort(t *testing.T) {
	input := map[string]model.ModelAssignment{
		"sdd-apply": {ProviderID: "anthropic", ModelID: "claude-opus-4", Effort: "medium"},
	}
	got := modelAssignmentsToState(input)
	s := got["sdd-apply"]
	if s.Effort != "medium" {
		t.Errorf("modelAssignmentsToState Effort = %q, want %q", s.Effort, "medium")
	}
}
