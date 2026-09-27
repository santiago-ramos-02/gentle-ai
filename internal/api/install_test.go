package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/gentleman-programming/gentle-ai/v3/internal/model"
	"github.com/gentleman-programming/gentle-ai/v3/internal/pipeline"
	"github.com/gentleman-programming/gentle-ai/v3/internal/reviewtransaction"
	"github.com/gentleman-programming/gentle-ai/v3/internal/service"
	"github.com/gentleman-programming/gentle-ai/v3/internal/state"
	"github.com/gentleman-programming/gentle-ai/v3/internal/system"
	"github.com/gentleman-programming/gentle-ai/v3/internal/update"
	"github.com/gentleman-programming/gentle-ai/v3/internal/update/upgrade"
)

// fakeInstall records the install request and reports two steps.
func fakeInstall(deps *Deps, err error) *service.InstallRequest {
	var got service.InstallRequest
	deps.Install = func(_ string, request service.InstallRequest, onProgress pipeline.ProgressFunc) pipeline.ExecutionResult {
		got = request
		onProgress(pipeline.ProgressEvent{StepID: "agent:claude-code", Stage: pipeline.StageApply, Status: pipeline.StepStatusRunning})
		onProgress(pipeline.ProgressEvent{StepID: "agent:claude-code", Stage: pipeline.StageApply, Status: pipeline.StepStatusSucceeded})
		return pipeline.ExecutionResult{
			Apply:         pipeline.StageResult{Steps: []pipeline.StepResult{{StepID: "agent:claude-code", Status: pipeline.StepStatusSucceeded}, {StepID: "component:gga", Status: pipeline.StepStatusSkipped, Err: errors.New("gga missing")}}},
			ManualActions: []string{"restart your shell"},
			Err:           err,
		}
	}
	return &got
}

func TestInstallAnswersEveryInstallerQuestion(t *testing.T) {
	deps := testDeps(t)
	request := fakeInstall(&deps, nil)
	var rdd []bool
	deps.SetGlobalReviewMode = func(_ context.Context, repo string, enabled bool) (reviewtransaction.RDDModeStatus, error) {
		if repo != deps.HomeDir {
			t.Errorf("rdd repo = %q, want the home default", repo)
		}
		rdd = append(rdd, enabled)
		return reviewtransaction.RDDModeStatus{Global: reviewtransaction.RDDModeOff}, nil
	}

	lines, err := callAPI(t, deps, []string{"install"}, `{
		"selection": {"agents": ["claude-code", "opencode", "codex"]},
		"modelPresets": {"codex": "powerful", "claude-code": "economy"},
		"models": {"kiroModelAssignments": {"odd-worker": "qwen"}},
		"communityTools": ["codegraph"],
		"openCodePlugins": ["sub-agent-statusline"],
		"rdd": false,
		"background": {"opencode": "on"}
	}`)
	if err != nil {
		t.Fatalf("install error = %v: %v", err, lines)
	}
	if len(lines) != 3 || lines[0]["type"] != "progress" || lines[0]["step"] != "agent:claude-code" || lines[0]["stage"] != "apply" || lines[1]["status"] != "succeeded" {
		t.Fatalf("lines = %v", lines)
	}
	got := result[installResult](t, deps, "install", `{"selection":{"agents":["claude-code"]},"rdd":true}`)
	if len(got.Steps) != 2 || got.Steps[1].Error != "gga missing" || !slices.Equal(got.ManualActions, []string{"restart your shell"}) || got.RDDMode != "off" || !slices.Equal(rdd, []bool{false, true}) {
		t.Fatalf("result = %+v, rdd = %v", got, rdd)
	}

	// The first call's request: presets, explicit models, and answers applied.
	callAPI(t, deps, []string{"install"}, `{
		"selection": {"agents": ["claude-code", "opencode", "codex"]},
		"modelPresets": {"codex": "powerful", "claude-code": "economy"},
		"models": {"kiroModelAssignments": {"odd-worker": "qwen"}},
		"communityTools": ["codegraph"],
		"openCodePlugins": ["sub-agent-statusline"],
		"background": {"opencode": "on"}
	}`)
	sel := request.Selection
	economy, _ := model.ClaudePresetAssignments("economy", nil)
	if !maps.Equal(sel.ClaudePhaseAssignments, economy) || sel.KiroModelAssignments["odd-worker"] != model.KiroModelQwen ||
		!maps.Equal(sel.CodexCarrilModelAssignments, model.CodexCarrilModelsForPreset("powerful")) ||
		sel.CodexOrchestratorAssignment == nil || sel.CodexOrchestratorAssignment.Model != model.CodexPresetOrchestratorAssignment("powerful").Model {
		t.Fatalf("model assignments = %+v", sel)
	}
	if !slices.Equal(sel.CommunityTools, []model.CommunityToolID{model.CommunityToolCodeGraph}) || !slices.Equal(sel.OpenCodePlugins, []model.OpenCodeCommunityPluginID{model.OpenCodePluginSubAgentStatusline}) {
		t.Fatalf("optional setup = %+v %+v", sel.CommunityTools, sel.OpenCodePlugins)
	}
	if request.OpenCodeBackground != model.OpenCodeBackgroundOn || request.OpenCodeBackgroundPersist != model.OpenCodeBackgroundOn || !slices.Contains(request.Resolved.Agents, model.AgentCodex) {
		t.Fatalf("request = %+v", request)
	}
}

func TestInstallKeepsPersistedModelsAndPresetsSkills(t *testing.T) {
	deps := testDeps(t)
	request := fakeInstall(&deps, nil)
	if err := state.Write(deps.HomeDir, state.InstallState{CodexModelAssignments: map[string]string{"odd-worker": "xhigh"}}); err != nil {
		t.Fatal(err)
	}
	result[installResult](t, deps, "install", `{"selection":{"agents":["codex"],"preset":"custom","components":["skills"]}}`)
	if request.Selection.CodexModelAssignments["odd-worker"] != model.CodexEffortXHigh {
		t.Fatalf("persisted codex efforts were not republished: %+v", request.Selection.CodexModelAssignments)
	}
	if !slices.Equal(request.Selection.Skills, service.PreselectedSkills()) {
		t.Fatalf("custom skills = %v, want the picker's preselection", request.Selection.Skills)
	}
}

func TestInstallRejectsUnansweredAndInapplicableChoices(t *testing.T) {
	deps := testDeps(t)
	if msg := failure(t, deps, []string{"install"}, `{"selection":{"agents":["opencode"]}}`, CodeInvalidParams); !strings.Contains(msg, "background.opencode") {
		t.Errorf("unanswered background message = %q", msg)
	}
	failure(t, deps, []string{"install"}, `{"selection":{"agents":["claude-code"]},"openCodePlugins":["sub-agent-statusline"]}`, CodeInvalidParams)
	failure(t, deps, []string{"install"}, `{"selection":{"agents":["pi"]},"communityTools":["codegraph"],"background":{"pi":"off"}}`, CodeInvalidParams)
	failure(t, deps, []string{"install"}, `{"selection":{"agents":["claude-code"]},"background":{"pi":"on"}}`, CodeInvalidParams)
	failure(t, deps, []string{"install"}, `{"selection":{"agents":["claude-code"]},"modelPresets":{"opencode":"balanced"}}`, CodeInvalidParams)
	failure(t, deps, []string{"install"}, `{"selection":{"agents":["claude-code"]},"models":{"targetAgents":["codex"]}}`, CodeInvalidParams)
	failure(t, deps, []string{"install"}, `{"selection":{"agents":["claude-code"]},"communityTools":["nope"]}`, CodeInvalidParams)

	fakeInstall(&deps, errors.New("apply failed"))
	failure(t, deps, []string{"install"}, `{"selection":{"agents":["claude-code"]}}`, CodeFailed)
}

func TestInstallReportsAnUnsavedRDDChoiceAsManualAction(t *testing.T) {
	deps := testDeps(t)
	fakeInstall(&deps, nil)
	deps.SetGlobalReviewMode = func(context.Context, string, bool) (reviewtransaction.RDDModeStatus, error) {
		return reviewtransaction.RDDModeStatus{}, errors.New("state locked")
	}
	got := result[installResult](t, deps, "install", `{"selection":{"agents":["claude-code"]},"rdd":true}`)
	if got.RDDMode != "" || len(got.ManualActions) != 2 || !strings.Contains(got.ManualActions[1], "RDD mode was not saved") {
		t.Fatalf("result = %+v", got)
	}
}

func fakeUpgrade(deps *Deps, results ...upgrade.ToolUpgradeResult) *upgrade.ExecuteOptions {
	var options upgrade.ExecuteOptions
	deps.CheckUpdates = func(context.Context, string, system.PlatformProfile, []string) []update.UpdateResult {
		return []update.UpdateResult{{Tool: update.ToolInfo{Name: "engram"}, Status: update.UpdateAvailable}}
	}
	deps.Upgrade = func(_ context.Context, _ []update.UpdateResult, _ system.PlatformProfile, _ string, opts upgrade.ExecuteOptions) upgrade.UpgradeReport {
		options = opts
		// A spinner redraws its line with carriage returns before finishing.
		_, _ = io.WriteString(opts.Progress, "  ⠋ Upgrading engram...\r  ⠙ Upgrading engram...")
		_, _ = fmt.Fprintf(opts.Progress, "\r          \r  ✓ Upgrading engram\r\n")
		return upgrade.UpgradeReport{BackupID: "b1", Results: results}
	}
	return &options
}

func TestUpgradeSyncsOnRequestUnlessGentleAIReplacedItself(t *testing.T) {
	deps := testDeps(t)
	options := fakeUpgrade(&deps, upgrade.ToolUpgradeResult{ToolName: "engram", OldVersion: "1.0", NewVersion: "1.1", Status: upgrade.UpgradeSucceeded})
	calls := recordSync(&deps)
	// Without sync this is the TUI's "Upgrade tools": nothing is synced.
	plain := result[upgradeResult](t, deps, "upgrade", `{"backup":false}`)
	if len(plain.Upgraded) != 1 || len(plain.Files) != 0 || len(*calls) != 0 {
		t.Fatalf("plain upgrade = %+v, syncs = %d", plain, len(*calls))
	}
	lines, err := callAPI(t, deps, []string{"upgrade"}, `{"backup":false,"sync":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0]["type"] != "log" || lines[0]["message"] != "  ✓ Upgrading engram" {
		t.Fatalf("lines = %v", lines)
	}
	if !options.SkipBackup {
		t.Error("backup:false did not skip the backup")
	}
	got := result[upgradeResult](t, deps, "upgrade", `{"sync":true}`)
	if len(got.Upgraded) != 1 || got.Upgraded[0].NewVersion != "1.1" || got.RestartRequired || got.SyncPending || !slices.Equal(got.Files, []string{"/changed"}) || got.BackupID != "b1" || len(*calls) != 2 || (*calls)[0] != nil {
		t.Fatalf("upgrade = %+v, syncs = %d", got, len(*calls))
	}
	if options.SkipBackup {
		t.Error("default upgrade skipped the backup")
	}

	self := testDeps(t)
	fakeUpgrade(&self, upgrade.ToolUpgradeResult{ToolName: "gentle-ai", NewVersion: "v9.0.0", Status: upgrade.UpgradeSucceeded}, upgrade.ToolUpgradeResult{ToolName: "gga", Status: upgrade.UpgradeFailed, Err: errors.New("boom")})
	got = result[upgradeResult](t, self, "upgrade", `{"sync":true}`)
	if !got.RestartRequired || !got.SyncPending || len(got.Failed) != 1 || got.Failed[0].Error != "boom" {
		t.Fatalf("self upgrade = %+v", got)
	}
	if persisted, err := state.Read(self.HomeDir); err != nil || !persisted.PendingSync {
		t.Fatalf("pending sync not recorded: %+v, %v", persisted, err)
	}

	// A plain upgrade that replaces gentle-ai asks for a restart and defers nothing.
	plainSelf := testDeps(t)
	fakeUpgrade(&plainSelf, upgrade.ToolUpgradeResult{ToolName: "gentle-ai", NewVersion: "v9.0.0", Status: upgrade.UpgradeSucceeded})
	got = result[upgradeResult](t, plainSelf, "upgrade", "")
	if !got.RestartRequired || got.SyncPending {
		t.Fatalf("plain self upgrade = %+v", got)
	}
	if persisted, err := state.Read(plainSelf.HomeDir); err == nil && persisted.PendingSync {
		t.Fatal("plain upgrade recorded a pending sync")
	}
}

func TestUpgradeFailsWhenTheUpdateCheckFails(t *testing.T) {
	deps := testDeps(t)
	deps.CheckUpdates = func(context.Context, string, system.PlatformProfile, []string) []update.UpdateResult {
		return []update.UpdateResult{{Tool: update.ToolInfo{Name: "engram"}, Status: update.CheckFailed, Err: errors.New("offline")}}
	}
	failure(t, deps, []string{"upgrade"}, "", CodeFailed)
}
