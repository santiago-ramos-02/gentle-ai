package service

import (
	"testing"

	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
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

// Explicit assignments and resets must reach the model-writing sync stage even
// when persisted components omit it; a plain sync keeps that selection intact.
func TestApplyOverridesPreservesExplicitModelWork(t *testing.T) {
	for _, tc := range []struct {
		name      string
		overrides *model.SyncOverrides
		wantSDD   bool
	}{
		{name: "plain sync"},
		{name: "unrelated override", overrides: &model.SyncOverrides{SDDMode: model.SDDModeMulti}},
		{name: "reset models", overrides: &model.SyncOverrides{ModelAssignments: map[string]model.ModelAssignment{}}, wantSDD: true},
		{name: "assign models", overrides: &model.SyncOverrides{ModelAssignments: map[string]model.ModelAssignment{"worker": {ModelID: "custom-model"}}}, wantSDD: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selection := model.Selection{Components: []model.ComponentID{model.ComponentPersona}}
			ApplyOverrides(&selection, tc.overrides)
			ApplyOverrides(&selection, tc.overrides)
			if selection.HasComponent(model.ComponentSDD) != tc.wantSDD {
				t.Fatalf("SDD selected = %v, want %v", selection.HasComponent(model.ComponentSDD), tc.wantSDD)
			}
			wantComponents := 1
			if tc.wantSDD {
				wantComponents++
			}
			if len(selection.Components) != wantComponents || !selection.HasComponent(model.ComponentPersona) {
				t.Fatalf("sync components = %v; existing components must survive without duplicates", selection.Components)
			}
			if tc.overrides != nil && tc.overrides.ModelAssignments != nil {
				if selection.ModelAssignments == nil || len(selection.ModelAssignments) != len(tc.overrides.ModelAssignments) {
					t.Fatalf("explicit model assignments lost: %v", selection.ModelAssignments)
				}
			}
		})
	}
}
