package api

import (
	"context"

	"github.com/gentleman-programming/gentle-ai/v3/internal/cli"
	"github.com/gentleman-programming/gentle-ai/v3/internal/doctor"
	"github.com/gentleman-programming/gentle-ai/v3/internal/system"
	"github.com/gentleman-programming/gentle-ai/v3/internal/update"
)

type doctorCheck struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Remedy  string `json:"remedy,omitempty"`
}

type doctorResult struct {
	OK     bool          `json:"ok"`
	Checks []doctorCheck `json:"checks"`
}

// runDoctor reports the doctor's structured checks; ok is false when any fails.
func runDoctor(ctx context.Context, env *env, _ noParams) (any, error) {
	report := env.deps.Doctor(ctx, env.deps.HomeDir)
	result := doctorResult{OK: true, Checks: make([]doctorCheck, 0, len(report.Checks))}
	for _, check := range report.Checks {
		entry := doctorCheck{ID: string(check.Name), Status: string(check.Status), Message: check.Detail}
		if check.Remedy != nil {
			entry.Remedy = check.Remedy.Description
		}
		if check.Status == doctor.StatusFail {
			result.OK = false
		}
		result.Checks = append(result.Checks, entry)
	}
	return result, nil
}

type updatesParams struct {
	Force bool `json:"force"`
}

type toolUpdate struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Status          string `json:"status"`
	Installed       string `json:"installed,omitempty"`
	Latest          string `json:"latest,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	ReleaseURL      string `json:"releaseUrl,omitempty"`
	ManualAction    string `json:"manualAction,omitempty"`
	Error           string `json:"error,omitempty"`
}

type updatesResult struct {
	// Checked is false when the six-hour cooldown the TUI applies skipped the
	// remote check; pass force to check anyway.
	Checked bool         `json:"checked"`
	Tools   []toolUpdate `json:"tools"`
}

func checkUpdates(ctx context.Context, env *env, params updatesParams) (any, error) {
	detection, err := env.deps.detect(ctx)
	if err != nil {
		return nil, err
	}
	profile := cli.ResolveInstallProfile(detection)
	checkAll := func(ctx context.Context, version string, profile system.PlatformProfile) []update.UpdateResult {
		return env.deps.CheckUpdates(ctx, version, profile, nil)
	}
	var results []update.UpdateResult
	if params.Force {
		results = checkAll(ctx, env.deps.Version, profile)
	} else {
		results = update.CheckAllWithCooldown(ctx, env.deps.Version, profile, env.deps.HomeDir, update.UpdateCheckTTL, env.deps.Now, checkAll)
	}
	return updatesResult{Checked: params.Force || results != nil, Tools: toolUpdates(results)}, nil
}

func toolUpdates(results []update.UpdateResult) []toolUpdate {
	out := make([]toolUpdate, 0, len(results))
	for _, result := range results {
		entry := toolUpdate{
			ID:              result.Tool.Name,
			Name:            result.Tool.Name,
			Status:          string(result.Status),
			Installed:       result.InstalledVersion,
			Latest:          result.LatestVersion,
			UpdateAvailable: result.Status == update.UpdateAvailable,
			ReleaseURL:      result.ReleaseURL,
			ManualAction:    result.UpdateHint,
		}
		if result.Err != nil {
			entry.Error = result.Err.Error()
		}
		out = append(out, entry)
	}
	return out
}
