package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gentleman-programming/gentle-ai/v4/internal/agentbuilder"
	"github.com/gentleman-programming/gentle-ai/v4/internal/backup"
	"github.com/gentleman-programming/gentle-ai/v4/internal/cli"
	"github.com/gentleman-programming/gentle-ai/v4/internal/components/communitytool"
	"github.com/gentleman-programming/gentle-ai/v4/internal/components/opencodeplugin"
	componentuninstall "github.com/gentleman-programming/gentle-ai/v4/internal/components/uninstall"
	"github.com/gentleman-programming/gentle-ai/v4/internal/doctor"
	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
	"github.com/gentleman-programming/gentle-ai/v4/internal/opencode"
	"github.com/gentleman-programming/gentle-ai/v4/internal/pipeline"
	"github.com/gentleman-programming/gentle-ai/v4/internal/reviewtransaction"
	"github.com/gentleman-programming/gentle-ai/v4/internal/service"
	"github.com/gentleman-programming/gentle-ai/v4/internal/state"
	"github.com/gentleman-programming/gentle-ai/v4/internal/system"
	"github.com/gentleman-programming/gentle-ai/v4/internal/update"
	"github.com/gentleman-programming/gentle-ai/v4/internal/update/upgrade"
)

// testDeps isolates a call in a temp home: HOME and USERPROFILE point there so
// services that resolve the home directory themselves stay inside it too.
func testDeps(t *testing.T) Deps {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv(cli.OpenCodeBackgroundSubagentsEnv, "")
	t.Setenv(cli.PiBackgroundSubagentsEnv, "")
	return Deps{
		Version: "v9.9.9-test",
		HomeDir: home,
		Detect: func(context.Context) (system.DetectionResult, error) {
			return system.DetectionResult{
				System: system.SystemInfo{OS: "linux", Arch: "amd64", Shell: "bash", Supported: true},
				Configs: []system.ConfigState{
					{Agent: string(model.AgentClaudeCode), Path: home + "/.claude", Exists: true, IsDirectory: true},
					{Agent: string(model.AgentOpenCode), Path: home + "/.config/opencode", Exists: false},
				},
			}, nil
		},
		EngineAvailable: func(id model.AgentID) bool { return id == model.AgentClaudeCode },
		Now:             func() time.Time { return time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC) },
		RestoreBackup:   func(backup.Manifest) error { return nil },
		InstallPlugins: func(string, []model.OpenCodeCommunityPluginID) ([]opencodeplugin.Result, error) {
			t.Fatal("unexpected plugin install")
			return nil, nil
		},
		UninstallPlugin: func(string, model.OpenCodeCommunityPluginID) (opencodeplugin.UninstallResult, error) {
			t.Fatal("unexpected plugin uninstall")
			return opencodeplugin.UninstallResult{}, nil
		},
		ReviewMode: func(context.Context, string, string, string) (cli.ReviewModeResult, error) {
			t.Fatal("unexpected review mode call")
			return cli.ReviewModeResult{}, nil
		},
		SurveyReviewStore: func(context.Context, string, reviewtransaction.StoreResetRequest) (reviewtransaction.StoreResetReport, error) {
			t.Fatal("unexpected review store survey")
			return reviewtransaction.StoreResetReport{}, nil
		},
		ResetReviewStore: func(context.Context, string, reviewtransaction.StoreResetRequest) (reviewtransaction.StoreResetReport, error) {
			t.Fatal("unexpected review store reset")
			return reviewtransaction.StoreResetReport{}, nil
		},
		Doctor: func(context.Context, string) doctor.Report { return doctor.Report{} },
		CheckUpdates: func(context.Context, string, system.PlatformProfile, []string) []update.UpdateResult {
			t.Fatal("unexpected update check")
			return nil
		},
		Sync: func(string, *model.SyncOverrides) (service.SyncResult, error) {
			t.Fatal("unexpected sync")
			return service.SyncResult{}, nil
		},
		CodexModels: func(context.Context) []string { return nil },
		OpenCodeCatalog: func(context.Context, string) (map[string]opencode.Provider, error) {
			return nil, nil
		},
		Install: func(string, service.InstallRequest, pipeline.ProgressFunc) pipeline.ExecutionResult {
			t.Fatal("unexpected install")
			return pipeline.ExecutionResult{}
		},
		SetGlobalReviewMode: func(context.Context, string, bool) (reviewtransaction.RDDModeStatus, error) {
			t.Fatal("unexpected review mode change")
			return reviewtransaction.RDDModeStatus{}, nil
		},
		Upgrade: func(context.Context, []update.UpdateResult, system.PlatformProfile, string, upgrade.ExecuteOptions) upgrade.UpgradeReport {
			t.Fatal("unexpected upgrade")
			return upgrade.UpgradeReport{}
		},
		CommunityToolStatus: func(id model.CommunityToolID, _ string) communitytool.Status {
			return communitytool.Status{Tool: id, CLI: communitytool.AvailabilityMissing}
		},
		CommunityToolRunner: newCommandRunner,
		InstallCommunityTool: func(model.CommunityToolID, string, string, communitytool.Runner) (communitytool.Result, error) {
			t.Fatal("unexpected community tool install")
			return communitytool.Result{}, nil
		},
		NewEngine: func(id model.AgentID) agentbuilder.GenerationEngine {
			return &agentbuilder.MockEngine{AgentIDVal: id}
		},
		Uninstall: func(string, string, []model.AgentID, []model.ComponentID, model.EngramUninstallScope) (componentuninstall.Result, error) {
			t.Fatal("unexpected uninstall")
			return componentuninstall.Result{}, nil
		},
		Executable: func() (string, error) { return "", errors.New("no executable in tests") },
		RemoveFile: func(path string) error {
			t.Fatalf("unexpected removal of %s", path)
			return nil
		},
	}
}

type line map[string]any

// call runs one method and returns every NDJSON line plus the run error.
func callAPI(t *testing.T, deps Deps, args []string, params string) ([]line, error) {
	t.Helper()
	var out bytes.Buffer
	err := Run(context.Background(), args, strings.NewReader(params), &out, deps)
	var lines []line
	scanner := bufio.NewScanner(&out)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for scanner.Scan() {
		var decoded line
		if jsonErr := json.Unmarshal(scanner.Bytes(), &decoded); jsonErr != nil {
			t.Fatalf("output line is not JSON: %q: %v", scanner.Text(), jsonErr)
		}
		lines = append(lines, decoded)
	}
	if len(lines) == 0 {
		t.Fatalf("no output lines")
	}
	return lines, err
}

// result asserts a successful call and returns its data, re-decoded into T.
func result[T any](t *testing.T, deps Deps, method, params string) T {
	t.Helper()
	lines, err := callAPI(t, deps, []string{method}, params)
	if err != nil {
		t.Fatalf("%s error = %v, lines = %v", method, err, lines)
	}
	final := lines[len(lines)-1]
	if final["type"] != "result" || final["schema"] != Schema {
		t.Fatalf("final line = %v, want result envelope", final)
	}
	raw, _ := json.Marshal(final["data"])
	var data T
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("decode data %s: %v", raw, err)
	}
	return data
}

// failure asserts an error envelope with the given code.
func failure(t *testing.T, deps Deps, args []string, params string, code ErrorCode) string {
	t.Helper()
	lines, err := callAPI(t, deps, args, params)
	if err == nil {
		t.Fatalf("%v succeeded, want %s: %v", args, code, lines)
	}
	final := lines[len(lines)-1]
	payload, _ := final["error"].(map[string]any)
	if final["type"] != "error" || final["schema"] != Schema || payload["code"] != string(code) {
		t.Fatalf("final line = %v, want %s error envelope", final, code)
	}
	return payload["message"].(string)
}

func TestDescribeListsMethodsAndVersions(t *testing.T) {
	deps := testDeps(t)
	got := result[describeResult](t, deps, "describe", "")
	if got.Version != deps.Version || got.APIVersion != Version {
		t.Fatalf("describe = %+v", got)
	}
	for _, name := range []string{"describe", "status", "plan"} {
		if !slices.Contains(got.Methods, name) {
			t.Errorf("methods %v missing %q", got.Methods, name)
		}
	}
	if !slices.IsSorted(got.Methods) {
		t.Errorf("methods are not sorted: %v", got.Methods)
	}
}

func TestEnvelopeRejectsBadCalls(t *testing.T) {
	deps := testDeps(t)
	failure(t, deps, nil, "", CodeInvalidParams)
	failure(t, deps, []string{"describe", "extra"}, "", CodeInvalidParams)
	failure(t, deps, []string{"no-such-method"}, "", CodeUnsupported)
	failure(t, deps, []string{"describe"}, "[]", CodeInvalidParams)
	failure(t, deps, []string{"describe"}, `{"unknown":true}`, CodeInvalidParams)
	failure(t, deps, []string{"describe"}, `{} {}`, CodeInvalidParams)
	failure(t, deps, []string{"plan"}, `{}`, CodeInvalidParams)
	failure(t, deps, []string{"plan"}, `{"selection":{"agents":[]}}`, CodeInvalidParams)
	failure(t, deps, []string{"plan"}, `{"selection":{"agents":["nope"]}}`, CodeInvalidParams)
	// Retired SDD options are not part of the API.
	if msg := failure(t, deps, []string{"plan"}, `{"selection":{"agents":["claude-code"],"strictTdd":true}}`, CodeInvalidParams); !strings.Contains(msg, "unknown field") {
		t.Errorf("strictTdd message = %q", msg)
	}
}

func TestStatusReportsCatalogAndPersistedState(t *testing.T) {
	deps := testDeps(t)
	if err := state.Write(deps.HomeDir, state.InstallState{
		InstalledAgents:     []string{string(model.AgentClaudeCode)},
		SelectionConfigured: true,
		Components:          []model.ComponentID{model.ComponentEngram, model.ComponentSkills},
		Preset:              model.PresetMinimal,
		Persona:             string(model.PersonaNeutral),
		RDDMode:             "off",
		PendingSync:         true,
		BackgroundIntent:    model.OpenCodeBackgroundOn,
	}); err != nil {
		t.Fatal(err)
	}

	lines, err := callAPI(t, deps, []string{"status"}, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(lines[len(lines)-1]["data"])
	if bytes.Contains(raw, []byte("null")) {
		t.Errorf("status data contains null: %s", raw)
	}

	got := result[statusResult](t, deps, "status", "")
	if got.Version != deps.Version || got.System.OS != "linux" || !got.System.Supported {
		t.Fatalf("status system = %+v", got)
	}
	claude := findByID(t, got.Agents, "claude-code")
	if !claude.Detected || !claude.Installed || !claude.Supported || claude.ConfigPath != deps.HomeDir+"/.claude" {
		t.Errorf("claude status = %+v", claude)
	}
	if opencode := findByID(t, got.Agents, "opencode"); opencode.Detected || opencode.Installed {
		t.Errorf("opencode status = %+v", opencode)
	}
	if engram := findByID(t, got.Components, "engram"); !engram.Installed {
		t.Errorf("engram status = %+v", engram)
	}
	if context7 := findByID(t, got.Components, "context7"); context7.Installed {
		t.Errorf("context7 status = %+v", context7)
	}
	if judgment := findByID(t, got.Skills, "judgment-day"); !judgment.Installed {
		t.Errorf("minimal preset should install judgment-day: %+v", judgment)
	}
	if goTesting := findByID(t, got.Skills, "go-testing"); goTesting.Installed {
		t.Errorf("minimal preset should not install go-testing: %+v", goTesting)
	}
	if got.State.Preset != "minimal" || got.State.Persona != "neutral" || got.State.RDDMode != "off" || !got.State.PendingSync || got.State.Background.OpenCode != "on" {
		t.Errorf("state = %+v", got.State)
	}
	if got.OpenCodeDetected || !slices.Equal(got.BuilderEngines, []string{"claude-code"}) {
		t.Errorf("openCodeDetected = %v, builderEngines = %v", got.OpenCodeDetected, got.BuilderEngines)
	}
	if len(got.Presets) != 4 || got.Presets[0].ID != string(model.PresetFullGentleman) || len(got.Presets[0].Components) == 0 {
		t.Errorf("presets = %+v", got.Presets)
	}
}

func TestStatusFailsOnUnreadableState(t *testing.T) {
	deps := testDeps(t)
	if err := writeFile(state.Path(deps.HomeDir), "{not json"); err != nil {
		t.Fatal(err)
	}
	failure(t, deps, []string{"status"}, "", CodeFailed)
}

func TestPlanAsksTheInstallerQuestionsForTheSelection(t *testing.T) {
	deps := testDeps(t)

	got := result[planResult](t, deps, "plan", `{"selection":{"agents":["claude-code","opencode"],"preset":"custom","components":["skills","engram"]}}`)
	if !slices.Equal(got.Questions, []string{"communityTools", "openCodePlugins", "skills", "rdd", "openCodeBackground"}) {
		t.Errorf("custom questions = %v", got.Questions)
	}
	if !slices.Equal(got.Agents, []string{"claude-code", "opencode"}) || !slices.Contains(got.Components, "engram") {
		t.Errorf("plan = %+v", got)
	}
	if got.Steps[0].ID != "prepare:check-dependencies" || !slices.ContainsFunc(got.Steps, func(s planStep) bool { return s.ID == "agent:opencode" }) {
		t.Errorf("steps = %+v", got.Steps)
	}

	// Pi alone skips optional community setup and installs only memory.
	pi := result[planResult](t, deps, "plan", `{"selection":{"agents":["pi"]}}`)
	if !slices.Equal(pi.Questions, []string{"piPlugins", "rdd", "piBackground"}) || !slices.Equal(pi.Components, []string{"engram"}) {
		t.Errorf("pi plan = %+v", pi)
	}

	// A managed prior choice resolves the background question without asking.
	if err := state.Write(deps.HomeDir, state.InstallState{BackgroundIntent: model.OpenCodeBackgroundOff}); err != nil {
		t.Fatal(err)
	}
	resolved := result[planResult](t, deps, "plan", `{"selection":{"agents":["opencode"]}}`)
	if !slices.Equal(resolved.Questions, []string{"communityTools", "openCodePlugins", "rdd"}) {
		t.Errorf("resolved background questions = %v", resolved.Questions)
	}
}

type identified interface {
	agentStatus | componentStatus | skillStatus
}

func findByID[T identified](t *testing.T, items []T, id string) T {
	t.Helper()
	for _, item := range items {
		raw, _ := json.Marshal(item)
		var probe struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(raw, &probe)
		if probe.ID == id {
			return item
		}
	}
	t.Fatalf("no item %q", id)
	var zero T
	return zero
}
