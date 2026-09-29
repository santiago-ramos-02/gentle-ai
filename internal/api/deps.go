package api

import (
	"context"
	"io"
	"os"
	"os/exec"
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
	"github.com/gentleman-programming/gentle-ai/v4/internal/system"
	"github.com/gentleman-programming/gentle-ai/v4/internal/update"
	"github.com/gentleman-programming/gentle-ai/v4/internal/update/upgrade"
)

// Deps are the effects a method may perform. Production wiring lives in
// DefaultDeps; tests replace individual fields with fakes over a temp home.
type Deps struct {
	Version string
	HomeDir string
	Now     func() time.Time

	Detect          func(context.Context) (system.DetectionResult, error)
	EngineAvailable service.EngineAvailable

	RestoreBackup func(backup.Manifest) error

	InstallPlugins  func(homeDir string, ids []model.OpenCodeCommunityPluginID) ([]opencodeplugin.Result, error)
	UninstallPlugin func(homeDir string, id model.OpenCodeCommunityPluginID) (opencodeplugin.UninstallResult, error)

	ReviewMode        func(ctx context.Context, cwd, operation, scope string) (cli.ReviewModeResult, error)
	SurveyReviewStore func(ctx context.Context, cwd string, request reviewtransaction.StoreResetRequest) (reviewtransaction.StoreResetReport, error)
	ResetReviewStore  func(ctx context.Context, cwd string, request reviewtransaction.StoreResetRequest) (reviewtransaction.StoreResetReport, error)

	Doctor       func(ctx context.Context, homeDir string) doctor.Report
	CheckUpdates func(ctx context.Context, version string, profile system.PlatformProfile, tools []string) []update.UpdateResult

	Sync            func(homeDir string, overrides *model.SyncOverrides) (service.SyncResult, error)
	CodexModels     func(ctx context.Context) []string
	OpenCodeCatalog func(ctx context.Context, projectDir string) (map[string]opencode.Provider, error)

	Install             func(homeDir string, request service.InstallRequest, onProgress pipeline.ProgressFunc) pipeline.ExecutionResult
	SetGlobalReviewMode func(ctx context.Context, repo string, enabled bool) (reviewtransaction.RDDModeStatus, error)
	Upgrade             func(ctx context.Context, checks []update.UpdateResult, profile system.PlatformProfile, homeDir string, options upgrade.ExecuteOptions) upgrade.UpgradeReport

	CommunityToolStatus  func(id model.CommunityToolID, homeDir string) communitytool.Status
	CommunityToolRunner  func(dir string, output io.Writer) communitytool.Runner
	InstallCommunityTool func(id model.CommunityToolID, cwd, homeDir string, runner communitytool.Runner) (communitytool.Result, error)

	NewEngine func(model.AgentID) agentbuilder.GenerationEngine

	Uninstall  func(homeDir, cwd string, agents []model.AgentID, components []model.ComponentID, engramScope model.EngramUninstallScope) (componentuninstall.Result, error)
	Executable func() (string, error)
	RemoveFile func(string) error

	Footprint func(homeDir string, agents []model.AgentID) (componentuninstall.Footprint, error)
}

// DefaultDeps wires the real services for homeDir.
func DefaultDeps(version, homeDir string) Deps {
	return Deps{
		Version:             version,
		HomeDir:             homeDir,
		Now:                 time.Now,
		Detect:              system.Detect,
		EngineAvailable:     service.DefaultEngineAvailable,
		RestoreBackup:       backup.RestoreService{}.Restore,
		InstallPlugins:      opencodeplugin.InstallAll,
		UninstallPlugin:     opencodeplugin.Uninstall,
		ReviewMode:          cli.ReviewMode,
		SurveyReviewStore:   reviewtransaction.SurveyReviewStore,
		ResetReviewStore:    reviewtransaction.ResetReviewStore,
		Doctor:              cli.DoctorChecks,
		CheckUpdates:        update.CheckFiltered,
		Sync:                service.Sync,
		CodexModels:         model.DiscoverCodexModels,
		OpenCodeCatalog:     opencode.DiscoverCatalog,
		Install:             service.Install,
		SetGlobalReviewMode: cli.SetGlobalReviewMode,
		Upgrade: func(ctx context.Context, checks []update.UpdateResult, profile system.PlatformProfile, homeDir string, options upgrade.ExecuteOptions) upgrade.UpgradeReport {
			return upgrade.ExecuteWithOptions(ctx, checks, profile, homeDir, false, options)
		},
		CommunityToolStatus: func(id model.CommunityToolID, homeDir string) communitytool.Status {
			return communitytool.DetectStatus(id, homeDir, communitytool.DetectorFunc(exec.LookPath))
		},
		CommunityToolRunner: newCommandRunner,
		InstallCommunityTool: func(id model.CommunityToolID, cwd, homeDir string, runner communitytool.Runner) (communitytool.Result, error) {
			return communitytool.InstallWithHome(id, cwd, homeDir, runner, communitytool.DetectorFunc(exec.LookPath))
		},
		NewEngine: agentbuilder.NewEngine,
		Uninstall: func(homeDir, cwd string, agents []model.AgentID, components []model.ComponentID, engramScope model.EngramUninstallScope) (componentuninstall.Result, error) {
			return cli.RunUninstallWithSelectionAndProfiles(homeDir, cwd, agents, components, nil, engramScope)
		},
		Executable: os.Executable,
		RemoveFile: os.Remove,
		Footprint:  componentuninstall.AgentFootprint,
	}
}

func (d Deps) detect(ctx context.Context) (system.DetectionResult, error) {
	detection, err := d.Detect(ctx)
	if err != nil {
		return system.DetectionResult{}, errorf(CodeFailed, "detect system: %v", err)
	}
	return detection, nil
}
