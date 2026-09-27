package api

import (
	"context"
	"strings"

	"github.com/gentleman-programming/gentle-ai/v3/internal/cli"
	"github.com/gentleman-programming/gentle-ai/v3/internal/service"
	"github.com/gentleman-programming/gentle-ai/v3/internal/update"
	"github.com/gentleman-programming/gentle-ai/v3/internal/update/upgrade"
)

type upgradeParams struct {
	// Tools limits the upgrade to these managed tools; empty means all.
	Tools []string `json:"tools"`
	// Backup snapshots agent configs first; it defaults to true.
	Backup *bool `json:"backup"`
	// Sync runs a sync after the upgrade, like the TUI's "Upgrade + Sync";
	// without it this is the TUI's "Upgrade tools".
	Sync bool `json:"sync"`
}

type upgradedTool struct {
	Name       string `json:"name"`
	OldVersion string `json:"oldVersion,omitempty"`
	NewVersion string `json:"newVersion,omitempty"`
}

type unfinishedTool struct {
	Name         string `json:"name"`
	Error        string `json:"error,omitempty"`
	ManualAction string `json:"manualAction,omitempty"`
}

type upgradeResult struct {
	Upgraded      []upgradedTool   `json:"upgraded"`
	Failed        []unfinishedTool `json:"failed"`
	Skipped       []unfinishedTool `json:"skipped"`
	BackupID      string           `json:"backupId,omitempty"`
	BackupWarning string           `json:"backupWarning,omitempty"`
	// RestartRequired is true when gentle-ai replaced itself: the running
	// binary is stale until the host starts a new process.
	RestartRequired bool `json:"restartRequired"`
	// SyncPending is true when the sync was deferred to the next launch
	// because gentle-ai itself was upgraded.
	SyncPending bool     `json:"syncPending"`
	Files       []string `json:"files"`
}

// runUpgrade upgrades the managed tools and, when asked, then syncs, like the
// TUI's "Upgrade tools" and "Upgrade + Sync". When gentle-ai upgrades itself
// the stale binary must not rewrite configs, so a requested sync is deferred
// to the next launch.
func runUpgrade(ctx context.Context, env *env, params upgradeParams) (any, error) {
	detection, err := env.deps.detect(ctx)
	if err != nil {
		return nil, err
	}
	profile := cli.ResolveInstallProfile(detection)
	checks := env.deps.CheckUpdates(ctx, env.deps.Version, profile, params.Tools)
	if failed := update.CheckFailures(checks); len(failed) > 0 {
		return nil, errorf(CodeFailed, "update check failed for: %s", strings.Join(failed, ", "))
	}

	progress := env.events.logWriter()
	report := env.deps.Upgrade(ctx, checks, profile, env.deps.HomeDir, upgrade.ExecuteOptions{
		Progress:          progress,
		BackupDiagnostics: progress,
		SkipBackup:        params.Backup != nil && !*params.Backup,
	})
	progress.Flush()

	result := upgradeResult{
		Upgraded:      []upgradedTool{},
		Failed:        []unfinishedTool{},
		Skipped:       []unfinishedTool{},
		Files:         []string{},
		BackupID:      report.BackupID,
		BackupWarning: report.BackupWarning,
	}
	for _, tool := range report.Results {
		switch tool.Status {
		case upgrade.UpgradeSucceeded:
			result.Upgraded = append(result.Upgraded, upgradedTool{Name: tool.ToolName, OldVersion: tool.OldVersion, NewVersion: tool.NewVersion})
		case upgrade.UpgradeFailed:
			result.Failed = append(result.Failed, unfinishedTool{Name: tool.ToolName, Error: errorText(tool.Err), ManualAction: tool.ManualHint})
		default:
			result.Skipped = append(result.Skipped, unfinishedTool{Name: tool.ToolName, Error: errorText(tool.Err), ManualAction: tool.ManualHint})
		}
	}

	if _, selfUpgraded := report.GentleAIVersion(); selfUpgraded {
		result.RestartRequired = true
		if !params.Sync {
			return result, nil
		}
		if err := service.MarkPendingSync(env.deps.HomeDir); err != nil {
			return nil, errorf(CodeFailed, "gentle-ai was upgraded but the deferred sync could not be recorded: %v", err)
		}
		result.SyncPending = true
		return result, nil
	}
	if report.ExitRequested {
		// An upgrade strategy needs this process gone before anything else runs.
		result.RestartRequired = true
		return result, nil
	}
	if !params.Sync {
		return result, nil
	}
	synced, err := syncWith(env, nil)
	if err != nil {
		return nil, err
	}
	result.Files = synced.Files
	return result, nil
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
