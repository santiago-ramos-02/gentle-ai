package model_test

import (
	"testing"

	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
)

func TestClaudeCustomModels(t *testing.T) {
	custom := model.ClaudeCustomModel("gpt-5.6-sol")
	if !custom.Valid() || custom.ModelID() != "gpt-5.6-sol" {
		t.Fatalf("custom model %q: valid=%v id=%q", custom, custom.Valid(), custom.ModelID())
	}
	if got := model.ClaudeEffortsForModel(custom); len(got) != 6 {
		t.Fatalf("custom efforts = %v, want every effort", got)
	}
	if !(model.ClaudePhaseAssignment{Model: custom, Effort: model.ClaudeEffortXHigh}).Valid() {
		t.Fatal("a custom model with an effort must be a valid assignment")
	}
	if model.ClaudeModelOpus.ModelID() != "opus" {
		t.Fatal("tiers are handed to Claude Code unchanged")
	}
	for _, invalid := range []string{"custom:", "custom:-lead", "custom:gpt 5", "custom:a\nmodel: x", "gpt-5.6-sol"} {
		if model.ClaudeModelAlias(invalid).Valid() {
			t.Errorf("%q must not be a valid Claude model", invalid)
		}
	}
}
