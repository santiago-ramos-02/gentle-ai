package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gentleman-programming/gentle-ai/v4/internal/backup"
	"github.com/gentleman-programming/gentle-ai/v4/internal/cli"
	"github.com/gentleman-programming/gentle-ai/v4/internal/components/opencodeplugin"
	"github.com/gentleman-programming/gentle-ai/v4/internal/doctor"
	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
	"github.com/gentleman-programming/gentle-ai/v4/internal/reviewtransaction"
	"github.com/gentleman-programming/gentle-ai/v4/internal/state"
	"github.com/gentleman-programming/gentle-ai/v4/internal/system"
	"github.com/gentleman-programming/gentle-ai/v4/internal/update"
)

func writeBackup(t *testing.T, home, id string, createdAt time.Time, pinned bool) backup.Manifest {
	t.Helper()
	root := filepath.Join(home, ".gentle-ai", "backups", id)
	manifest := backup.Manifest{
		ID:        id,
		CreatedAt: createdAt,
		RootDir:   root,
		Source:    backup.BackupSourceSync,
		FileCount: 1,
		Pinned:    pinned,
		Entries: []backup.ManifestEntry{
			{OriginalPath: filepath.Join(home, ".claude", "CLAUDE.md"), Existed: true},
			{OriginalPath: filepath.Join(home, ".claude", "new.md"), Existed: false},
		},
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := backup.WriteManifest(filepath.Join(root, backup.ManifestFilename), manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestBackupsListRenamePinRestoreDelete(t *testing.T) {
	deps := testDeps(t)
	older := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	writeBackup(t, deps.HomeDir, "older", older, false)
	writeBackup(t, deps.HomeDir, "newer", older.Add(time.Hour), true)

	listed := result[backupsResult](t, deps, "backups.list", "")
	if len(listed.Backups) != 2 || listed.Backups[0].ID != "newer" || !listed.Backups[0].Pinned || listed.Backups[1].Source != "sync" {
		t.Fatalf("backups = %+v", listed.Backups)
	}

	result[empty](t, deps, "backups.rename", `{"id":"older","description":"before refactor"}`)
	result[empty](t, deps, "backups.pin", `{"id":"older","pinned":true}`)
	result[empty](t, deps, "backups.pin", `{"id":"older","pinned":true}`) // idempotent
	result[empty](t, deps, "backups.pin", `{"id":"newer","pinned":false}`)
	listed = result[backupsResult](t, deps, "backups.list", "")
	if listed.Backups[1].Description != "before refactor" || !listed.Backups[1].Pinned || listed.Backups[0].Pinned {
		t.Fatalf("after rename/pin = %+v", listed.Backups)
	}

	var restored backup.Manifest
	deps.RestoreBackup = func(m backup.Manifest) error { restored = m; return nil }
	got := result[restoreResult](t, deps, "backups.restore", `{"id":"older"}`)
	if restored.ID != "older" || !slices.Equal(got.RestoredFiles, []string{filepath.Join(deps.HomeDir, ".claude", "CLAUDE.md")}) {
		t.Fatalf("restore = %+v, restored %q", got, restored.ID)
	}
	deps.RestoreBackup = func(backup.Manifest) error { return errors.New("disk full") }
	failure(t, deps, []string{"backups.restore"}, `{"id":"older"}`, CodeFailed)

	result[empty](t, deps, "backups.delete", `{"id":"older"}`)
	if listed = result[backupsResult](t, deps, "backups.list", ""); len(listed.Backups) != 1 {
		t.Fatalf("after delete = %+v", listed.Backups)
	}
	failure(t, deps, []string{"backups.delete"}, `{"id":"older"}`, CodeNotFound)
	failure(t, deps, []string{"backups.rename"}, `{"id":"newer"}`, CodeInvalidParams)
	failure(t, deps, []string{"backups.pin"}, `{"id":"newer"}`, CodeInvalidParams)
	failure(t, deps, []string{"backups.restore"}, `{}`, CodeInvalidParams)
}

func detectOpenCode(deps *Deps) {
	deps.Detect = func(context.Context) (system.DetectionResult, error) {
		return system.DetectionResult{Configs: []system.ConfigState{{Agent: string(model.AgentOpenCode), Exists: true}}}, nil
	}
}

func TestRetiredOpenCodePluginsAreListedOnlyForRemoval(t *testing.T) {
	deps := testDeps(t)
	listed := result[pluginsResult](t, deps, "plugins.list", "")
	if listed.Supported || listed.Reason == "" || len(listed.Plugins) != 0 {
		t.Fatalf("plugins without OpenCode = %+v", listed)
	}
	failure(t, deps, []string{"plugins.install"}, `{"ids":["sub-agent-statusline"]}`, CodeUnsupported)
	failure(t, deps, []string{"plugins.install"}, `{"ids":["not-a-plugin"]}`, CodeInvalidParams)
	failure(t, deps, []string{"plugins.install"}, `{"ids":[]}`, CodeInvalidParams)

	detectOpenCode(&deps)
	deps.InstallPlugins = func(_ string, _ []model.OpenCodeCommunityPluginID) ([]opencodeplugin.Result, error) {
		t.Fatal("retired OpenCode plugins reached the installer")
		return nil, nil
	}
	listed = result[pluginsResult](t, deps, "plugins.list", "")
	if !listed.Supported || len(listed.Plugins) != 0 {
		t.Fatalf("retired plugin catalog = %+v", listed)
	}

	// tui.json registers the statusline package and the logo; the retired
	// SDD manager is listed too so it can still be removed.
	configPath := filepath.Join(deps.HomeDir, ".config", "opencode", "tui.json")
	config := `{"plugin":["opencode-subagent-statusline","opencode-sdd-engram-manage","/x/tui-plugins/gentle-logo.tsx"]}`
	if err := writeFile(configPath, config); err != nil {
		t.Fatal(err)
	}
	listed = result[pluginsResult](t, deps, "plugins.list", "")
	if !listed.Supported || !listed.Plugins[0].Installed || len(listed.Plugins) != 3 {
		t.Fatalf("plugins = %+v", listed)
	}
	for _, id := range []model.OpenCodeCommunityPluginID{model.OpenCodePluginSubAgentStatusline, model.OpenCodePluginSDDEngramManage, model.OpenCodePluginGentleLogo} {
		message := failure(t, deps, []string{"plugins.install"}, fmt.Sprintf(`{"ids":[%q]}`, id), CodeUnsupported)
		if !strings.Contains(message, "no longer offered") {
			t.Fatalf("retirement message = %q", message)
		}
	}
	if data, err := os.ReadFile(configPath); err != nil || string(data) != config {
		t.Fatalf("retired plugin operations changed existing configuration: %q, %v", data, err)
	}

	failure(t, deps, []string{"plugins.uninstall"}, `{"id":"not-installed"}`, CodeNotFound)
	deps.UninstallPlugin = func(_ string, id model.OpenCodeCommunityPluginID) (opencodeplugin.UninstallResult, error) {
		return opencodeplugin.UninstallResult{PluginID: id, ChangedTUI: true}, nil
	}
	removed := result[pluginUninstallResult](t, deps, "plugins.uninstall", `{"id":"gentle-logo"}`)
	if removed.PluginID != "gentle-logo" || !removed.ChangedTUI || removed.CleanupPending == nil {
		t.Fatalf("uninstall = %+v", removed)
	}
}

func TestReviewMethodsValidateCwdAndMapRefusals(t *testing.T) {
	deps := testDeps(t)
	cwd := t.TempDir()
	failure(t, deps, []string{"review.status"}, `{"cwd":"relative/dir"}`, CodeInvalidParams)
	// A clone override needs the clone; the global switch does not.
	failure(t, deps, []string{"review.set"}, `{"enabled":true,"scope":"clone"}`, CodeInvalidParams)
	failure(t, deps, []string{"review.set"}, `{"cwd":`+quote(cwd)+`}`, CodeInvalidParams)
	failure(t, deps, []string{"review.set"}, `{"cwd":`+quote(cwd)+`,"enabled":true,"scope":"repo"}`, CodeInvalidParams)

	var calls []string
	deps.ReviewMode = func(_ context.Context, gotCwd, operation, scope string) (cli.ReviewModeResult, error) {
		calls = append(calls, gotCwd+"|"+operation+"|"+scope)
		return cli.ReviewModeResult{Schema: cli.ReviewModeSchema, Operation: operation, Scope: scope}, nil
	}
	status := result[cli.ReviewModeResult](t, deps, "review.status", `{"cwd":`+quote(cwd)+`}`)
	set := result[cli.ReviewModeResult](t, deps, "review.set", `{"cwd":`+quote(cwd)+`,"enabled":false}`)
	result[cli.ReviewModeResult](t, deps, "review.set", `{"cwd":`+quote(cwd)+`,"enabled":true,"scope":"clone"}`)
	result[cli.ReviewModeResult](t, deps, "review.status", `{}`)
	result[cli.ReviewModeResult](t, deps, "review.set", `{"enabled":true}`)
	home := deps.HomeDir
	if status.Schema != cli.ReviewModeSchema || set.Operation != "disable" || !slices.Equal(calls, []string{cwd + "|status|global", cwd + "|disable|global", cwd + "|enable|clone", home + "|status|global", home + "|enable|global"}) {
		t.Fatalf("review calls = %v, status %+v, set %+v", calls, status, set)
	}

	deps.SurveyReviewStore = func(_ context.Context, _ string, request reviewtransaction.StoreResetRequest) (reviewtransaction.StoreResetReport, error) {
		if request.IncludeInFlight || request.IncludeAdapterReviews {
			t.Errorf("survey request = %+v, want the TUI's empty request", request)
		}
		return reviewtransaction.StoreResetReport{Schema: "survey", Complete: true}, nil
	}
	survey := result[cli.ReviewStoreResetResult](t, deps, "reviewStore.survey", `{"cwd":`+quote(cwd)+`}`)
	if survey.Schema != cli.ReviewStoreResetSchema || survey.Report.Schema != "survey" {
		t.Fatalf("survey = %+v", survey)
	}

	deps.ResetReviewStore = func(_ context.Context, _ string, request reviewtransaction.StoreResetRequest) (reviewtransaction.StoreResetReport, error) {
		if !request.IncludeAdapterReviews {
			return reviewtransaction.StoreResetReport{}, &reviewtransaction.StoreResetInFlightError{Repository: cwd}
		}
		return reviewtransaction.StoreResetReport{Complete: true}, nil
	}
	failure(t, deps, []string{"reviewStore.reset"}, `{"cwd":`+quote(cwd)+`}`, CodeConflict)
	if reset := result[cli.ReviewStoreResetResult](t, deps, "reviewStore.reset", `{"cwd":`+quote(cwd)+`,"includeAdapterReviews":true}`); !reset.Report.Complete {
		t.Fatalf("reset = %+v", reset)
	}
}

func TestDoctorReportsStructuredChecks(t *testing.T) {
	deps := testDeps(t)
	deps.Doctor = func(context.Context, string) doctor.Report {
		return doctor.Report{Checks: []doctor.Result{
			{Name: doctor.ToolCheckID("engram"), Status: doctor.StatusPass, Detail: "found"},
			{Name: doctor.CheckStateJSON, Status: doctor.StatusFail, Detail: "corrupt", Remedy: doctor.NewRemedy(doctor.RemedyRepairState, "repair state.json")},
		}}
	}
	got := result[doctorResult](t, deps, "doctor", "")
	if got.OK || len(got.Checks) != 2 || got.Checks[0].ID != "tool:engram" || got.Checks[1].Remedy != "repair state.json" {
		t.Fatalf("doctor = %+v", got)
	}
}

func TestUpdatesHonorCooldownUnlessForced(t *testing.T) {
	deps := testDeps(t)
	checks := 0
	deps.CheckUpdates = func(_ context.Context, version string, _ system.PlatformProfile, tools []string) []update.UpdateResult {
		checks++
		if version != deps.Version || tools != nil {
			t.Errorf("check(%q, %v)", version, tools)
		}
		return []update.UpdateResult{{Tool: update.ToolInfo{Name: "engram"}, InstalledVersion: "1.0.0", LatestVersion: "1.1.0", Status: update.UpdateAvailable, ReleaseURL: "https://example.test/r"}}
	}
	recent := deps.Now().Add(-time.Hour)
	if err := state.Write(deps.HomeDir, state.InstallState{LastUpdateCheck: &recent}); err != nil {
		t.Fatal(err)
	}
	cooled := result[updatesResult](t, deps, "updates", "")
	if cooled.Checked || len(cooled.Tools) != 0 || checks != 0 {
		t.Fatalf("cooldown result = %+v after %d checks", cooled, checks)
	}
	forced := result[updatesResult](t, deps, "updates", `{"force":true}`)
	if !forced.Checked || checks != 1 || len(forced.Tools) != 1 || !forced.Tools[0].UpdateAvailable || forced.Tools[0].Latest != "1.1.0" {
		t.Fatalf("forced result = %+v", forced)
	}
}

func quote(s string) string {
	return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
}

// Pi's plugins are its optional packages: listed from Pi's settings, installed
// and removed through pi itself.
func TestPiPluginsInstallThroughPi(t *testing.T) {
	deps := testDeps(t)
	t.Setenv("PI_CODING_AGENT_DIR", "")
	listed := result[pluginsResult](t, deps, "plugins.list", `{"agent":"pi"}`)
	if listed.Supported || listed.Reason == "" || len(listed.Plugins) != 1 || listed.Plugins[0].ID != "claude-bridge" {
		t.Fatalf("Pi plugins without Pi = %+v", listed)
	}
	failure(t, deps, []string{"plugins.install"}, `{"agent":"pi","ids":["claude-bridge"]}`, CodeUnsupported)
	failure(t, deps, []string{"plugins.list"}, `{"agent":"cursor"}`, CodeInvalidParams)

	deps.Detect = func(context.Context) (system.DetectionResult, error) {
		return system.DetectionResult{Configs: []system.ConfigState{{Agent: string(model.AgentPi), Exists: true}}}, nil
	}
	settings := filepath.Join(deps.HomeDir, ".pi", "agent", "settings.json")
	var ran [][]string
	deps.RunCommand = func(name string, args ...string) error {
		ran = append(ran, append([]string{name}, args...))
		// pi install records the package in Pi's settings, as the real one does.
		if args[0] == "install" {
			return writeFile(settings, `{"packages":["`+args[1]+`"]}`)
		}
		return writeFile(settings, `{"packages":[]}`)
	}
	failure(t, deps, []string{"plugins.install"}, `{"agent":"pi","ids":["sub-agent-statusline"]}`, CodeInvalidParams)
	got := result[pluginsInstallResult](t, deps, "plugins.install", `{"agent":"pi","ids":["claude-bridge"]}`)
	if len(got.Results) != 1 || !got.Results[0].Changed {
		t.Fatalf("install = %+v", got)
	}
	listed = result[pluginsResult](t, deps, "plugins.list", `{"agent":"pi"}`)
	if !listed.Supported || !listed.Plugins[0].Installed {
		t.Fatalf("after install = %+v", listed)
	}
	result[map[string]any](t, deps, "plugins.uninstall", `{"agent":"pi","id":"claude-bridge"}`)
	failure(t, deps, []string{"plugins.uninstall"}, `{"agent":"pi","id":"claude-bridge"}`, CodeNotFound)
	want := [][]string{{"pi", "install", "npm:pi-claude-bridge"}, {"pi", "remove", "npm:pi-claude-bridge"}}
	if !slices.EqualFunc(ran, want, slices.Equal[[]string]) {
		t.Fatalf("ran %v, want %v", ran, want)
	}
}
