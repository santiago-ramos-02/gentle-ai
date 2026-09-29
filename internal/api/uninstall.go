package api

import (
	"context"
	"os"
	"slices"

	"github.com/gentleman-programming/gentle-ai/v4/internal/catalog"
	componentuninstall "github.com/gentleman-programming/gentle-ai/v4/internal/components/uninstall"
	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
	"github.com/gentleman-programming/gentle-ai/v4/internal/service"
)

type uninstallParams struct {
	Mode        string   `json:"mode"`
	Agents      []string `json:"agents"`
	Components  []string `json:"components"`
	EngramScope string   `json:"engramScope"`
	Cwd         string   `json:"cwd"`
}

type uninstallPlan struct {
	Mode                 string   `json:"mode"`
	Agents               []string `json:"agents"`
	Components           []string `json:"components"`
	EngramScopeAvailable bool     `json:"engramScopeAvailable"`
	EngramScope          string   `json:"engramScope"`

	agentIDs     []model.AgentID
	componentIDs []model.ComponentID
	scope        model.EngramUninstallScope
}

// planUninstall validates params and expands the mode the way the TUI's
// uninstall screens do.
//
// cwd is optional: it only scopes project cleanup, such as Engram data kept in
// the project, which is then unavailable.
func planUninstall(params uninstallParams) (uninstallPlan, error) {
	if params.Cwd != "" {
		if err := requireCwd(params.Cwd); err != nil {
			return uninstallPlan{}, err
		}
	}
	mode := model.UninstallMode(params.Mode)
	switch mode {
	case model.UninstallModePartial:
		if len(params.Agents) == 0 || len(params.Components) == 0 {
			return uninstallPlan{}, invalidParams("partial uninstall needs at least one agent and one component")
		}
	case model.UninstallModeFull, model.UninstallModeFullRemove, model.UninstallModeCleanInstall:
		if params.Agents != nil || params.Components != nil {
			return uninstallPlan{}, invalidParams("%s uninstall covers every agent and component; omit agents and components", mode)
		}
	default:
		return uninstallPlan{}, invalidParams("mode must be partial, full, full-remove, or clean-install; got %q", params.Mode)
	}
	agents, err := agentIDs(params.Agents, "agents")
	if err != nil {
		return uninstallPlan{}, err
	}
	components := make([]model.ComponentID, 0, len(params.Components))
	for _, raw := range params.Components {
		id := model.ComponentID(raw)
		if !slices.ContainsFunc(catalog.MVPComponents(), func(c catalog.Component) bool { return c.ID == id }) {
			return uninstallPlan{}, invalidParams("unknown component %q", raw)
		}
		components = append(components, id)
	}
	agents, components = service.UninstallTargets(mode, agents, components)
	plan := uninstallPlan{
		Mode:                 string(mode),
		Agents:               strs(agents),
		Components:           strs(components),
		EngramScopeAvailable: params.Cwd != "" && service.ProjectEngramDataAvailable(components, params.Cwd, os.Stat),
		agentIDs:             agents,
		componentIDs:         components,
		scope:                model.EngramUninstallScopeGlobal,
	}
	switch model.EngramUninstallScope(params.EngramScope) {
	case "", model.EngramUninstallScopeGlobal:
	case model.EngramUninstallScopeProject:
		if !plan.EngramScopeAvailable {
			return uninstallPlan{}, invalidParams("engramScope project needs Engram selected and .engram data in cwd")
		}
		plan.scope = model.EngramUninstallScopeProject
	default:
		return uninstallPlan{}, invalidParams("engramScope must be global or project, got %q", params.EngramScope)
	}
	plan.EngramScope = string(plan.scope)
	return plan, nil
}

func uninstallPlanMethod(_ context.Context, _ *env, params uninstallParams) (any, error) {
	return planUninstall(params)
}

type uninstallSync struct {
	Files []string `json:"files"`
	Error string   `json:"error,omitempty"`
}

type uninstallResult struct {
	BackupID                         string         `json:"backupId,omitempty"`
	BackupPath                       string         `json:"backupPath,omitempty"`
	ChangedFiles                     []string       `json:"changedFiles"`
	RemovedFiles                     []string       `json:"removedFiles"`
	RemovedDirectories               []string       `json:"removedDirectories"`
	RetainedPiResources              []string       `json:"retainedPiResources"`
	OptionalPiPackageCleanupCommands []string       `json:"optionalPiPackageCleanupCommands"`
	ManualActions                    []string       `json:"manualActions"`
	AgentsRemovedFromState           []string       `json:"agentsRemovedFromState"`
	FailedAgents                     []string       `json:"failedAgents"`
	BinaryRemoved                    bool           `json:"binaryRemoved,omitempty"`
	Synced                           *uninstallSync `json:"synced,omitempty"`
}

// runUninstall removes managed configuration, then applies the mode's
// follow-up: full-remove deletes this gentle-ai binary and clean-install
// re-syncs every managed asset.
func runUninstall(_ context.Context, env *env, params uninstallParams) (any, error) {
	plan, err := planUninstall(params)
	if err != nil {
		return nil, err
	}
	workspace := params.Cwd
	if workspace == "" {
		// No project: an empty directory leaves project cleanup nothing to find,
		// where an empty path would resolve against this process's directory.
		empty, err := os.MkdirTemp("", "gentle-ai-no-project-*")
		if err != nil {
			return nil, errorf(CodeFailed, "uninstall: %v", err)
		}
		defer os.RemoveAll(empty)
		workspace = empty
	}
	result, err := env.deps.Uninstall(env.deps.HomeDir, workspace, plan.agentIDs, plan.componentIDs, plan.scope)
	if err != nil {
		return nil, errorf(CodeFailed, "uninstall: %v", err)
	}
	followUp, err := service.FinishUninstall(model.UninstallMode(plan.Mode), &result, service.UninstallEffects{
		Executable: env.deps.Executable,
		Remove:     env.deps.RemoveFile,
		Sync: func() ([]string, error) {
			synced, err := env.deps.Sync(env.deps.HomeDir, nil)
			return synced.Files, err
		},
	})
	if err != nil {
		return nil, errorf(CodeFailed, "%v", err)
	}
	out := uninstallResultFrom(result)
	out.BinaryRemoved = followUp.BinaryRemoved
	if followUp.Synced {
		out.Synced = &uninstallSync{Files: orEmpty(followUp.SyncFiles)}
		if followUp.SyncErr != nil {
			out.Synced.Error = followUp.SyncErr.Error()
		}
	}
	return out, nil
}

func uninstallResultFrom(result componentuninstall.Result) uninstallResult {
	return uninstallResult{
		BackupID:                         result.Manifest.ID,
		BackupPath:                       result.BackupPath,
		ChangedFiles:                     orEmpty(result.ChangedFiles),
		RemovedFiles:                     orEmpty(result.RemovedFiles),
		RemovedDirectories:               orEmpty(result.RemovedDirectories),
		RetainedPiResources:              orEmpty(result.RetainedPiResources),
		OptionalPiPackageCleanupCommands: orEmpty(result.OptionalPiPackageCleanupCommands),
		ManualActions:                    orEmpty(result.ManualActions),
		AgentsRemovedFromState:           strs(result.AgentsRemovedFromState),
		FailedAgents:                     strs(result.FailedAgents),
	}
}
