package api

import (
	"context"
	"errors"
	"os"

	"github.com/gentleman-programming/gentle-ai/v3/internal/state"
)

// methods is the registry, in the order describe lists related methods. It is
// a function rather than a variable so describe can refer to it.
func methods() []method {
	return []method{
		{name: "describe", run: typed(describe)},
		{name: "status", run: typed(status)},
		{name: "plan", run: typed(plan)},
		{name: "sync", run: typed(runSync)},
		{name: "models.get", run: typed(getModels)},
		{name: "models.set", run: typed(setModels)},
		{name: "install", run: typed(install)},
		{name: "upgrade", run: typed(runUpgrade)},
		{name: "updates", run: typed(checkUpdates)},
		{name: "backups.list", run: typed(listBackups)},
		{name: "backups.restore", run: typed(restoreBackup)},
		{name: "backups.delete", run: typed(deleteBackup)},
		{name: "backups.rename", run: typed(renameBackup)},
		{name: "backups.pin", run: typed(pinBackup)},
		{name: "plugins.list", run: typed(listPlugins)},
		{name: "plugins.install", run: typed(installPlugins)},
		{name: "plugins.uninstall", run: typed(uninstallPlugin)},
		{name: "review.status", run: typed(reviewStatus)},
		{name: "review.set", run: typed(reviewSet)},
		{name: "reviewStore.survey", run: typed(reviewStoreSurvey)},
		{name: "reviewStore.reset", run: typed(reviewStoreReset)},
		{name: "tools.list", run: typed(listTools)},
		{name: "tools.install", run: typed(installTools)},
		{name: "builder.engines", run: typed(builderEngines)},
		{name: "builder.generate", run: typed(builderGenerate)},
		{name: "builder.install", run: typed(builderInstall)},
		{name: "uninstall.plan", run: typed(uninstallPlanMethod)},
		{name: "uninstall.run", run: typed(runUninstall)},
		{name: "doctor", run: typed(runDoctor)},
		{name: "footprint", run: typed(footprint)},
	}
}

type describeResult struct {
	Version    string   `json:"version"`
	APIVersion int      `json:"apiVersion"`
	Methods    []string `json:"methods"`
	// Features names the workflows this build offers, so hosts need not probe
	// its commands: "odd" since SDD was retired.
	Features []string `json:"features"`
}

// buildFeatures are the workflows this build of gentle-ai offers.
var buildFeatures = []string{"odd"}

func describe(_ context.Context, env *env, _ noParams) (any, error) {
	return describeResult{Version: env.deps.Version, APIVersion: Version, Methods: MethodNames(), Features: buildFeatures}, nil
}

// readState returns the persisted install state; a missing file is a fresh home.
func readState(homeDir string) (state.InstallState, error) {
	current, err := state.Read(homeDir)
	if errors.Is(err, os.ErrNotExist) {
		return state.InstallState{}, nil
	}
	if err != nil {
		return state.InstallState{}, errorf(CodeFailed, "read install state: %v", err)
	}
	return current, nil
}

// strs converts typed ids to strings, never returning nil so JSON shows [].
func strs[T ~string](values []T) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, string(value))
	}
	return out
}

// orEmpty keeps nil slices from encoding as JSON null.
func orEmpty[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}
