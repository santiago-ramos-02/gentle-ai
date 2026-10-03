package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gentleman-programming/gentle-ai/v4/internal/agentbuilder"
	"github.com/gentleman-programming/gentle-ai/v4/internal/components/communitytool"
	componentuninstall "github.com/gentleman-programming/gentle-ai/v4/internal/components/uninstall"
	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
	"github.com/gentleman-programming/gentle-ai/v4/internal/state"
)

func TestToolsInstallStreamsOutputAndRecordsTheTools(t *testing.T) {
	deps := testDeps(t)
	cwd := t.TempDir()
	listed := result[toolsResult](t, deps, "tools.list", "")
	if len(listed.Tools) != 1 || listed.Tools[0].ID != "codegraph" || listed.Tools[0].CLIAvailable || listed.Tools[0].Installed || listed.Tools[0].Agents == nil {
		t.Fatalf("tools = %+v", listed.Tools)
	}

	deps.InstallCommunityTool = func(id model.CommunityToolID, gotCwd, home string, runner communitytool.Runner) (communitytool.Result, error) {
		if gotCwd != cwd || home != deps.HomeDir {
			t.Errorf("install(%s) in %q with home %q", id, gotCwd, home)
		}
		if err := runner.Run("go", "version"); err != nil {
			return communitytool.Result{}, err
		}
		return communitytool.Result{Tool: id, CommandsRun: []string{"go version"}}, nil
	}
	lines, err := callAPI(t, deps, []string{"tools.install"}, `{"ids":["codegraph"],"cwd":`+quote(cwd)+`}`)
	if err != nil {
		t.Fatalf("tools.install error = %v: %v", err, lines)
	}
	if len(lines) != 2 || lines[0]["type"] != "log" || !strings.HasPrefix(lines[0]["message"].(string), "go version go") {
		t.Fatalf("lines = %v", lines)
	}
	persisted, err := state.Read(deps.HomeDir)
	if err != nil || !persisted.CommunityToolsConfigured || !slices.Equal(persisted.CommunityTools, []string{"codegraph"}) {
		t.Fatalf("state = %+v, %v", persisted, err)
	}
	if listed := result[toolsResult](t, deps, "tools.list", ""); !listed.Tools[0].Installed {
		t.Fatalf("tool not reported installed: %+v", listed.Tools[0])
	}

	failure(t, deps, []string{"tools.install"}, `{"ids":["codegraph"]}`, CodeInvalidParams)
	failure(t, deps, []string{"tools.install"}, `{"ids":["nope"],"cwd":`+quote(cwd)+`}`, CodeInvalidParams)
	deps.InstallCommunityTool = func(id model.CommunityToolID, _, _ string, _ communitytool.Runner) (communitytool.Result, error) {
		return communitytool.Result{Tool: id}, errors.New("pnpm missing")
	}
	failure(t, deps, []string{"tools.install"}, `{"ids":["codegraph"],"cwd":`+quote(cwd)+`}`, CodeFailed)
}

const generatedAgent = "# Release Notes Writer\n\n## Description\nWrites release notes.\n\n## Trigger\nWhen releasing.\n\n## Instructions\nSummarize merged changes.\n"

func TestBuilderGeneratesAndInstallsIntoDetectedAgents(t *testing.T) {
	deps := testDeps(t)
	engines := result[builderEnginesResult](t, deps, "builder.engines", "")
	if len(engines.Engines) != 4 || engines.Engines[0].ID != "claude-code" || !engines.Engines[0].Available || engines.Engines[1].Available {
		t.Fatalf("engines = %+v", engines)
	}

	deps.NewEngine = func(id model.AgentID) agentbuilder.GenerationEngine {
		return &agentbuilder.MockEngine{AgentIDVal: id, IsAvailable: true, Output: "```markdown\n" + generatedAgent + "```"}
	}
	generated := result[builderGenerateResult](t, deps, "builder.generate", `{"engine":"claude-code","prompt":"release notes"}`)
	claudeTarget := filepath.Join(deps.HomeDir, ".claude", "skills", "release-notes-writer", "SKILL.md")
	if generated.Agent.Name != "release-notes-writer" || !slices.Equal(generated.Targets, []string{claudeTarget}) || len(generated.Conflicts) != 0 {
		t.Fatalf("generated = %+v", generated)
	}

	installed := result[builderInstallResult](t, deps, "builder.install", fmt.Sprintf(`{"agent":{"name":"release-notes-writer","content":%q},"engine":"claude-code"}`, generated.Agent.Content))
	if !slices.Equal(installed.Files, []string{claudeTarget}) || installed.RenamedTo != "" || len(installed.Warnings) != 0 {
		t.Fatalf("installed = %+v", installed)
	}
	if _, err := os.Stat(claudeTarget); err != nil {
		t.Fatalf("skill not written: %v", err)
	}
	registry, err := agentbuilder.LoadRegistry(filepath.Join(deps.HomeDir, ".config", "gentle-ai", "custom-agents.json"))
	if err != nil || registry.FindByName("release-notes-writer") == nil {
		t.Fatalf("registry = %+v, %v", registry, err)
	}

	// A built-in name installs with a suffix.
	builtin := strings.Replace(generatedAgent, "Release Notes Writer", "Judgment Day", 1)
	renamed := result[builderInstallResult](t, deps, "builder.install", fmt.Sprintf(`{"agent":{"content":%q}}`, builtin))
	if renamed.RenamedTo != "judgment-day-custom" {
		t.Fatalf("renamed = %+v", renamed)
	}

	failure(t, deps, []string{"builder.install"}, fmt.Sprintf(`{"agent":{"name":"../escape","content":%q}}`, generatedAgent), CodeInvalidParams)
	failure(t, deps, []string{"builder.install"}, `{"agent":{"content":"no sections"}}`, CodeInvalidParams)
	failure(t, deps, []string{"builder.generate"}, `{"engine":"cursor","prompt":"x"}`, CodeInvalidParams)
	failure(t, deps, []string{"builder.generate"}, `{"engine":"codex","prompt":"x","sdd":{"mode":"standalone"}}`, CodeInvalidParams)
	deps.NewEngine = func(id model.AgentID) agentbuilder.GenerationEngine { return &agentbuilder.MockEngine{AgentIDVal: id} }
	failure(t, deps, []string{"builder.generate"}, `{"engine":"codex","prompt":"x"}`, CodeUnsupported)
}

func TestUninstallExpandsModesAndRunsTheirFollowUps(t *testing.T) {
	deps := testDeps(t)
	cwd := t.TempDir()
	cwdJSON := quote(cwd)

	partial := result[uninstallPlan](t, deps, "uninstall.plan", `{"mode":"partial","agents":["claude-code"],"components":["engram"],"cwd":`+cwdJSON+`}`)
	if !slices.Equal(partial.Agents, []string{"claude-code"}) || partial.EngramScopeAvailable || partial.EngramScope != "global" {
		t.Fatalf("partial plan = %+v", partial)
	}
	if err := os.MkdirAll(filepath.Join(cwd, ".engram"), 0o755); err != nil {
		t.Fatal(err)
	}
	full := result[uninstallPlan](t, deps, "uninstall.plan", `{"mode":"full","engramScope":"project","cwd":`+cwdJSON+`}`)
	if len(full.Agents) < 10 || !slices.Contains(full.Components, "engram") || !full.EngramScopeAvailable || full.EngramScope != "project" {
		t.Fatalf("full plan = %+v", full)
	}
	failure(t, deps, []string{"uninstall.plan"}, `{"mode":"partial","agents":["claude-code"],"cwd":`+cwdJSON+`}`, CodeInvalidParams)
	failure(t, deps, []string{"uninstall.plan"}, `{"mode":"full","agents":["claude-code"],"cwd":`+cwdJSON+`}`, CodeInvalidParams)
	failure(t, deps, []string{"uninstall.plan"}, `{"mode":"full","profiles":["cheap"],"cwd":`+cwdJSON+`}`, CodeInvalidParams)
	failure(t, deps, []string{"uninstall.plan"}, `{"mode":"everything","cwd":`+cwdJSON+`}`, CodeInvalidParams)
	// Without a project, project cleanup is unavailable rather than an error.
	if plan := result[uninstallPlan](t, deps, "uninstall.plan", `{"mode":"full"}`); plan.EngramScopeAvailable {
		t.Fatalf("no project should offer no project cleanup: %+v", plan)
	}
	failure(t, deps, []string{"uninstall.plan"}, `{"mode":"full","engramScope":"project"}`, CodeInvalidParams)

	var scope model.EngramUninstallScope
	deps.Uninstall = func(_ string, gotCwd string, agents []model.AgentID, components []model.ComponentID, engramScope model.EngramUninstallScope) (componentuninstall.Result, error) {
		scope = engramScope
		if gotCwd != cwd || len(agents) == 0 || len(components) == 0 {
			t.Errorf("uninstall(%q, %v, %v)", gotCwd, agents, components)
		}
		return componentuninstall.Result{ChangedFiles: []string{"/a"}, AgentsRemovedFromState: agents[:1]}, nil
	}
	calls := recordSync(&deps)
	clean := result[uninstallResult](t, deps, "uninstall.run", `{"mode":"clean-install","engramScope":"project","cwd":`+cwdJSON+`}`)
	if clean.Synced == nil || !slices.Equal(clean.Synced.Files, []string{"/changed"}) || len(*calls) != 1 || scope != model.EngramUninstallScopeProject || clean.BinaryRemoved || clean.RemovedFiles == nil {
		t.Fatalf("clean install = %+v, scope %q", clean, scope)
	}

	var removed string
	deps.Executable = func() (string, error) { return filepath.Join(cwd, "gentle-ai"), nil }
	deps.RemoveFile = func(path string) error { removed = path; return nil }
	gone := result[uninstallResult](t, deps, "uninstall.run", `{"mode":"full-remove","cwd":`+cwdJSON+`}`)
	if !gone.BinaryRemoved || removed != filepath.Join(cwd, "gentle-ai") || gone.Synced != nil {
		t.Fatalf("full remove = %+v, removed %q", gone, removed)
	}

	deps.Executable = func() (string, error) { return "/opt/homebrew/bin/gentle-ai", nil }
	brew := result[uninstallResult](t, deps, "uninstall.run", `{"mode":"full-remove","cwd":`+cwdJSON+`}`)
	if brew.BinaryRemoved || !strings.Contains(strings.Join(brew.ManualActions, "\n"), "brew uninstall gentle-ai") {
		t.Fatalf("homebrew full remove = %+v", brew)
	}

	deps.Uninstall = func(string, string, []model.AgentID, []model.ComponentID, model.EngramUninstallScope) (componentuninstall.Result, error) {
		return componentuninstall.Result{}, errors.New("locked")
	}
	failure(t, deps, []string{"uninstall.run"}, `{"mode":"full","cwd":`+cwdJSON+`}`, CodeFailed)
}

// Upstream reports a pending Pi CodeGraph integration as missing because Pi cannot confirm its
// MCP adapter loaded it. The API reports it as pending, so a host does not offer a setup that is
// already done.
func TestToolsListReportsPendingPiCodeGraphAsPending(t *testing.T) {
	deps := testDeps(t)
	deps.CommunityToolStatus = func(id model.CommunityToolID, _ string) communitytool.Status {
		return communitytool.Status{Tool: id, CLI: communitytool.AvailabilityAvailable, Agents: []communitytool.AgentStatus{
			{Agent: model.AgentPi, Name: "Pi", Status: communitytool.AgentStatusMissing, Detected: true, Reason: communitytool.ErrPiCodeGraphAdapterHealthUnavailable.Error()},
			{Agent: model.AgentClaudeCode, Name: "Claude Code", Status: communitytool.AgentStatusMissing, Detected: true, Reason: "detected agent but no CodeGraph MCP or instruction marker was found"},
		}}
	}
	listed := result[toolsResult](t, deps, "tools.list", "")
	agents := listed.Tools[0].Agents
	if agents[0].Status != "pending" || agents[0].Configured || !strings.Contains(agents[0].Reason, "cannot confirm") {
		t.Fatalf("Pi = %+v, want pending, not configured, saying what Pi cannot confirm", agents[0])
	}
	if agents[1].Status != "missing" {
		t.Fatalf("Claude Code = %+v, want missing unchanged", agents[1])
	}
}
