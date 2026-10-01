package api

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
	"github.com/gentleman-programming/gentle-ai/v4/internal/opencode"
	"github.com/gentleman-programming/gentle-ai/v4/internal/service"
	"github.com/gentleman-programming/gentle-ai/v4/internal/state"
)

// recordSync replaces the sync with a fake that records its overrides.
func recordSync(deps *Deps) *[]*model.SyncOverrides {
	calls := &[]*model.SyncOverrides{}
	deps.Sync = func(_ string, overrides *model.SyncOverrides) (service.SyncResult, error) {
		*calls = append(*calls, overrides)
		return service.SyncResult{Files: []string{"/changed"}}, nil
	}
	return calls
}

// A full sync brings every agent up to date, so it clears the post-upgrade flag the TUI
// otherwise clears on its next launch; a sync of some agents, or a failed one, leaves it.
func TestSyncClearsPendingSyncOnlyAfterAFullSync(t *testing.T) {
	deps := testDeps(t)
	recordSync(&deps)
	pending := func() bool {
		t.Helper()
		current, err := state.Read(deps.HomeDir)
		if err != nil {
			t.Fatal(err)
		}
		return current.PendingSync
	}
	if err := state.Write(deps.HomeDir, state.InstallState{
		InstalledAgents: []string{string(model.AgentClaudeCode), string(model.AgentCodex)},
		PendingSync:     true,
	}); err != nil {
		t.Fatal(err)
	}

	result[syncResult](t, deps, "sync", `{"agents":["codex"]}`)
	if !pending() {
		t.Fatal("a sync of some agents cleared pendingSync")
	}

	deps.Sync = func(string, *model.SyncOverrides) (service.SyncResult, error) {
		return service.SyncResult{}, errors.New("disk full")
	}
	failure(t, deps, []string{"sync"}, "", CodeFailed)
	if !pending() {
		t.Fatal("a failed sync cleared pendingSync")
	}

	recordSync(&deps)
	result[syncResult](t, deps, "sync", "")
	if pending() {
		t.Fatal("a full sync left pendingSync set")
	}
	if got := result[statusResult](t, deps, "status", ""); got.State.SyncNeeded {
		t.Fatal("status still reports a sync is needed")
	}
}

func TestSyncKeepsAbsentAndEmptyDistinct(t *testing.T) {
	deps := testDeps(t)
	calls := recordSync(&deps)

	got := result[syncResult](t, deps, "sync", "")
	if !slices.Equal(got.Files, []string{"/changed"}) || got.ManualActions == nil {
		t.Fatalf("sync = %+v", got)
	}
	result[syncResult](t, deps, "sync", `{"agents":["codex"]}`)
	result[syncResult](t, deps, "sync", `{"models":{"claudeModelAssignments":{},"kiroModelAssignments":{"odd-worker":"qwen"}}}`)
	if len(*calls) != 3 || (*calls)[0] != nil || !slices.Equal((*calls)[1].TargetAgents, []model.AgentID{model.AgentCodex}) {
		t.Fatalf("calls = %+v", *calls)
	}
	third := (*calls)[2]
	if third.ClaudeModelAssignments == nil || len(third.ClaudeModelAssignments) != 0 || third.ClaudePhaseAssignments != nil || third.KiroModelAssignments["odd-worker"] != model.KiroModelQwen {
		t.Fatalf("models overrides = %+v", third)
	}

	failure(t, deps, []string{"sync"}, `{"agents":["nope"]}`, CodeInvalidParams)
	failure(t, deps, []string{"sync"}, `{"models":{"claudePhaseAssignments":{"odd-worker":{"model":"haiku","effort":"max"}}}}`, CodeInvalidParams)
	failure(t, deps, []string{"sync"}, `{"models":{"codexModelAssignments":{"odd-worker":"extreme"}}}`, CodeInvalidParams)
	failure(t, deps, []string{"sync"}, `{"models":{"strictTdd":true}}`, CodeInvalidParams)
}

func TestModelsSetPresetsMatchThePickers(t *testing.T) {
	deps := testDeps(t)
	calls := recordSync(&deps)
	// A retired SDD key must survive a preset choice, as it does in the picker.
	if err := service.PersistAssignments(deps.HomeDir, model.Selection{
		ClaudePhaseAssignments: map[string]model.ClaudePhaseAssignment{"sdd-explore": {Model: model.ClaudeModelOpus}},
		CodexModelAssignments:  map[string]model.CodexEffort{"sdd-apply": model.CodexEffortLow},
	}); err != nil {
		t.Fatal(err)
	}

	result[syncResult](t, deps, "models.set", `{"agent":"claude-code","preset":"performance"}`)
	claude := (*calls)[0]
	want, _ := model.ClaudePresetAssignments("performance", map[string]model.ClaudePhaseAssignment{"sdd-explore": {Model: model.ClaudeModelOpus}})
	if !maps.Equal(claude.ClaudePhaseAssignments, want) || claude.ClaudePhaseAssignments["sdd-explore"].Model != model.ClaudeModelOpus || !slices.Equal(claude.TargetAgents, []model.AgentID{model.AgentClaudeCode}) {
		t.Fatalf("claude overrides = %+v", claude)
	}

	result[syncResult](t, deps, "models.set", `{"agent":"codex","preset":"powerful"}`)
	codex := (*calls)[1]
	if codex.CodexOrchestratorAssignment == nil || *codex.CodexOrchestratorAssignment != *model.CodexPresetOrchestratorAssignment("powerful") ||
		!maps.Equal(codex.CodexCarrilModelAssignments, model.CodexCarrilModelsForPreset("powerful")) ||
		codex.CodexPhaseModelAssignments == nil || len(codex.CodexPhaseModelAssignments) != 0 ||
		codex.CodexModelAssignments["sdd-apply"] != model.CodexEffortLow ||
		codex.CodexModelAssignments["odd-verify"] != model.CodexODDEffortsForPreset("powerful")["odd-verify"] {
		t.Fatalf("codex overrides = %+v", codex)
	}

	result[syncResult](t, deps, "models.set", `{"agent":"kiro-ide","preset":"open-weight"}`)
	if kiro := (*calls)[2]; !maps.Equal(kiro.KiroModelAssignments, model.KiroModelPresetOpenWeight()) {
		t.Fatalf("kiro overrides = %+v", kiro)
	}

	result[syncResult](t, deps, "models.set", `{"agent":"opencode","models":{"modelAssignments":{"gentle-orchestrator":{"providerId":"anthropic","modelId":"claude-opus"}}}}`)
	if opencodeCall := (*calls)[3]; opencodeCall.SDDMode != model.SDDModeMulti || opencodeCall.ModelAssignments["gentle-orchestrator"].ModelID != "claude-opus" || !slices.Equal(opencodeCall.TargetAgents, []model.AgentID{model.AgentOpenCode}) {
		t.Fatalf("opencode overrides = %+v", opencodeCall)
	}

	failure(t, deps, []string{"models.set"}, `{"agent":"opencode","preset":"balanced"}`, CodeInvalidParams)
	failure(t, deps, []string{"models.set"}, `{"agent":"claude-code","preset":"turbo"}`, CodeInvalidParams)
	failure(t, deps, []string{"models.set"}, `{"agent":"claude-code"}`, CodeInvalidParams)
	failure(t, deps, []string{"models.set"}, `{"agent":"claude-code","preset":"economy","models":{}}`, CodeInvalidParams)
	failure(t, deps, []string{"models.set"}, `{"agent":"claude-code","models":{"kiroModelAssignments":{}}}`, CodeInvalidParams)
	failure(t, deps, []string{"models.set"}, `{"agent":"cursor","preset":"balanced"}`, CodeInvalidParams)
	if len(*calls) != 4 {
		t.Fatalf("rejected calls reached sync: %d", len(*calls))
	}
}

func TestModelsGetReportsPresetsCurrentAndOptions(t *testing.T) {
	deps := testDeps(t)
	balanced, _ := model.ClaudePresetAssignments("balanced", nil)
	if err := service.PersistAssignments(deps.HomeDir, model.Selection{ClaudePhaseAssignments: balanced}); err != nil {
		t.Fatal(err)
	}

	claude := result[modelsGetResult](t, deps, "models.get", `{"agent":"claude-code"}`)
	if claude.CurrentPreset == nil || *claude.CurrentPreset != "balanced" || len(claude.Presets) != 4 || claude.Options.Claude == nil || claude.Options.Codex != nil {
		t.Fatalf("claude = %+v", claude)
	}
	if claude.Phases[0].Group != "odd" || claude.Options.Claude.Models[3].ID != "haiku" || len(claude.Options.Claude.Models[3].Efforts) != 1 {
		t.Fatalf("claude phases/options = %+v %+v", claude.Phases, claude.Options.Claude)
	}

	kiro := result[modelsGetResult](t, deps, "models.get", `{"agent":"kiro-ide"}`)
	if kiro.CurrentPreset != nil || kiro.Current["kiroModelAssignments"] == nil {
		t.Fatalf("kiro without state = %+v", kiro)
	}

	deps.CodexModels = func(context.Context) []string { return []string{"gpt-test"} }
	codex := result[modelsGetResult](t, deps, "models.get", `{"agent":"codex","discover":true}`)
	if len(codex.Options.Codex.Models) != 1 || codex.Options.Codex.Models[0].ID != "gpt-test" || len(codex.Options.Codex.Efforts) != 4 {
		t.Fatalf("codex options = %+v", codex.Options.Codex)
	}

	settings := filepath.Join(deps.HomeDir, ".config", "opencode", "opencode.json")
	if err := writeFile(settings, `{"agent":{"gentle-orchestrator":{"model":"local/qwen","variant":"high"},"reviewer":{"mode":"subagent"}},"provider":{"local":{"name":"Local","models":{"qwen":{"name":"Qwen","tool_call":true}}}}}`); err != nil {
		t.Fatal(err)
	}
	deps.OpenCodeCatalog = func(context.Context, string) (map[string]opencode.Provider, error) {
		return map[string]opencode.Provider{"remote": {ID: "remote", Name: "Remote", Models: map[string]opencode.Model{"big": {ID: "big", Name: "Big", ToolCall: true, Variants: []string{"low", "high"}}}}}, nil
	}
	oc := result[modelsGetResult](t, deps, "models.get", `{"agent":"opencode","discover":true}`)
	raw, _ := json.Marshal(oc.Current)
	if string(raw) != `{"modelAssignments":{"gentle-orchestrator":{"effort":"high","modelId":"qwen","providerId":"local"}}}` {
		t.Fatalf("opencode current = %s", raw)
	}
	names := []string{}
	for _, provider := range oc.Options.OpenCode.Providers {
		names = append(names, provider.ID)
	}
	if !slices.Equal(names, []string{"local", "remote"}) || !slices.Equal(oc.Options.OpenCode.Providers[1].Models[0].Variants, []string{"low", "high"}) {
		t.Fatalf("opencode providers = %+v", oc.Options.OpenCode.Providers)
	}
	if !slices.Contains(oc.Options.OpenCode.CustomAgents, "reviewer") || oc.Phases[len(oc.Phases)-1].Group != "custom" || oc.Phases[0].Group != "orchestrator" {
		t.Fatalf("opencode custom agents = %v, phases = %+v", oc.Options.OpenCode.CustomAgents, oc.Phases)
	}

	failure(t, deps, []string{"models.get"}, `{"agent":"opencode","cwd":"relative"}`, CodeInvalidParams)
}

// TestModelsSetPersistsThroughTheRealSync drives the production sync into a
// temp home, the same path the TUI's Configure Models screen takes.
func TestModelsSetPersistsThroughTheRealSync(t *testing.T) {
	deps := testDeps(t)
	deps.Sync = service.Sync
	if err := state.Write(deps.HomeDir, state.InstallState{InstalledAgents: []string{string(model.AgentClaudeCode)}, SelectionConfigured: true}); err != nil {
		t.Fatal(err)
	}
	result[syncResult](t, deps, "models.set", `{"agent":"claude-code","preset":"economy"}`)
	got, err := state.Read(deps.HomeDir)
	if err != nil {
		t.Fatal(err)
	}
	if got.ClaudePhaseAssignments["jd-judge-a"].Model != string(model.ClaudeModelHaiku) {
		t.Fatalf("persisted claude assignments = %+v", got.ClaudePhaseAssignments)
	}
	if preset := result[modelsGetResult](t, deps, "models.get", `{"agent":"claude-code"}`).CurrentPreset; preset == nil || *preset != "economy" {
		t.Fatalf("current preset after set = %v", preset)
	}
}
