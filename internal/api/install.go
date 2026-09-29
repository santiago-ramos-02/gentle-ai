package api

import (
	"cmp"
	"context"
	"slices"

	"github.com/gentleman-programming/gentle-ai/v4/internal/backup"
	"github.com/gentleman-programming/gentle-ai/v4/internal/components/communitytool"
	"github.com/gentleman-programming/gentle-ai/v4/internal/components/opencodeplugin"
	"github.com/gentleman-programming/gentle-ai/v4/internal/components/piplugin"
	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
	"github.com/gentleman-programming/gentle-ai/v4/internal/pipeline"
	"github.com/gentleman-programming/gentle-ai/v4/internal/service"
	"github.com/gentleman-programming/gentle-ai/v4/internal/state"
)

type installParams struct {
	Selection *selectionParams `json:"selection"`
	// ModelPresets picks a model preset per agent, as the model pickers do.
	ModelPresets map[string]string `json:"modelPresets"`
	// Models sets explicit assignments; its fields win over ModelPresets.
	Models          *modelsParams     `json:"models"`
	CommunityTools  []string          `json:"communityTools"`
	OpenCodePlugins []string          `json:"openCodePlugins"`
	PiPlugins       []string          `json:"piPlugins"`
	RDD             *bool             `json:"rdd"`
	Background      *backgroundParams `json:"background"`
	// Cwd is the repository the RDD status is resolved for; defaults to home.
	Cwd string `json:"cwd"`
}

type backgroundParams struct {
	OpenCode string `json:"opencode"`
	Pi       string `json:"pi"`
}

type installStep struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type installResult struct {
	Steps         []installStep `json:"steps"`
	ManualActions []string      `json:"manualActions"`
	BackupID      string        `json:"backupId,omitempty"`
	RDDMode       string        `json:"rddMode,omitempty"`
}

// install runs the installer non-interactively. Every question the installer
// would ask (see plan) is answered by a param; an unresolved background
// choice is an error rather than a prompt.
func install(ctx context.Context, env *env, params installParams) (any, error) {
	if params.Selection == nil {
		return nil, invalidParams("selection is required")
	}
	if params.Cwd != "" {
		if err := requireCwd(params.Cwd); err != nil {
			return nil, err
		}
	}
	detection, err := env.deps.detect(ctx)
	if err != nil {
		return nil, err
	}
	selection, err := params.Selection.toSelection(detection)
	if err != nil {
		return nil, err
	}
	if params.Selection.Skills == nil && service.AsksSkills(selection) {
		selection.Skills = service.PreselectedSkills()
	}
	current, err := readState(env.deps.HomeDir)
	if err != nil {
		return nil, err
	}
	// Like the TUI, start from every persisted model choice so the install
	// republishes them, then apply the presets and explicit models chosen now.
	service.RestoreModelAssignments(&selection, current)
	if err := applyInstallModels(env.deps.HomeDir, &selection, params); err != nil {
		return nil, err
	}
	if selection.CommunityTools, err = communityToolIDs(params.CommunityTools); err != nil {
		return nil, err
	}
	if len(selection.CommunityTools) > 0 && !service.AsksCommunityTools(selection) {
		return nil, invalidParams("communityTools are not offered for this selection")
	}
	if selection.OpenCodePlugins, err = openCodePluginIDs(params.OpenCodePlugins); err != nil {
		return nil, err
	}
	if len(selection.OpenCodePlugins) > 0 && !service.AsksOpenCodePlugins(selection) {
		return nil, invalidParams("openCodePlugins need the opencode agent in the selection")
	}
	if selection.PiPlugins, err = piPluginIDs(params.PiPlugins); err != nil {
		return nil, err
	}
	if len(selection.PiPlugins) > 0 && !service.AsksPiPlugins(selection) {
		return nil, invalidParams("piPlugins need the pi agent in the selection")
	}
	resolved, err := resolvePlan(selection)
	if err != nil {
		return nil, err
	}
	// Absent communityTools keeps the persisted list: they are managed with
	// tools.install, and an empty list here would forget the installed ones.
	request := service.InstallRequest{Selection: selection, Resolved: resolved, Detection: detection, KeepCommunityTools: params.CommunityTools == nil}
	if err := resolveBackground(&request, current, params.Background); err != nil {
		return nil, err
	}

	before := backupIDs(env.deps.HomeDir)
	execution := env.deps.Install(env.deps.HomeDir, request, env.events.progress)
	if execution.Err != nil {
		return nil, errorf(CodeFailed, "install: %v", execution.Err)
	}
	result := installResult{Steps: installSteps(execution), ManualActions: orEmpty(execution.ManualActions), BackupID: newBackupID(env.deps.HomeDir, before)}
	if params.RDD != nil {
		// The installer saves the RDD choice only after a successful install;
		// a failed save is reported as a manual action, not an install failure.
		status, err := env.deps.SetGlobalReviewMode(ctx, cmp.Or(params.Cwd, env.deps.HomeDir), *params.RDD)
		if err != nil {
			result.ManualActions = append(result.ManualActions,
				"RDD mode was not saved ("+err.Error()+"). Retry with `gentle-ai review mode enable --scope global` or `gentle-ai review mode disable --scope global`.")
		} else {
			result.RDDMode = string(status.Global)
		}
	}
	return result, nil
}

// applyInstallModels merges the chosen presets, then the explicit models, into
// the selection's model assignments.
func applyInstallModels(homeDir string, selection *model.Selection, params installParams) error {
	agents := make([]string, 0, len(params.ModelPresets))
	for agent := range params.ModelPresets {
		agents = append(agents, agent)
	}
	slices.Sort(agents)
	for _, raw := range agents {
		agent, err := configurableAgent(raw)
		if err != nil {
			return invalidParams("modelPresets: %s", asError(err).Message)
		}
		overrides, err := presetOverrides(homeDir, agent, params.ModelPresets[raw])
		if err != nil {
			return err
		}
		service.ApplyModelOverrides(selection, overrides)
	}
	if params.Models == nil {
		return nil
	}
	if params.Models.TargetAgents != nil {
		return invalidParams("install models take assignments only; choose agents in selection")
	}
	overrides, err := params.Models.toOverrides()
	if err != nil {
		return err
	}
	service.ApplyModelOverrides(selection, overrides)
	return nil
}

func communityToolIDs(raw []string) ([]model.CommunityToolID, error) {
	ids := make([]model.CommunityToolID, 0, len(raw))
	for _, value := range raw {
		id := model.CommunityToolID(value)
		if _, ok := communitytool.DefinitionFor(id); !ok {
			return nil, invalidParams("unknown community tool %q", value)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func openCodePluginIDs(raw []string) ([]model.OpenCodeCommunityPluginID, error) {
	ids := make([]model.OpenCodeCommunityPluginID, 0, len(raw))
	for _, value := range raw {
		id := model.OpenCodeCommunityPluginID(value)
		if !slices.ContainsFunc(opencodeplugin.Definitions(), func(def opencodeplugin.Definition) bool { return def.ID == id }) {
			return nil, invalidParams("unknown OpenCode plugin %q", value)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func piPluginIDs(raw []string) ([]model.PiPluginID, error) {
	ids := make([]model.PiPluginID, 0, len(raw))
	for _, value := range raw {
		id := model.PiPluginID(value)
		if _, ok := piplugin.DefinitionFor(id); !ok {
			return nil, invalidParams("unknown Pi plugin %q", value)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// resolveBackground answers the installer's background questions. An explicit
// answer is the user's choice and is persisted; otherwise the environment and
// prior state decide, and a choice they leave open must be answered.
func resolveBackground(request *service.InstallRequest, current state.InstallState, params *backgroundParams) error {
	var answers backgroundParams
	if params != nil {
		answers = *params
	}
	if service.OffersOpenCodeBackground(request.Selection) {
		if answers.OpenCode != "" {
			intent := model.OpenCodeBackgroundIntent(answers.OpenCode)
			if intent != model.OpenCodeBackgroundOn && intent != model.OpenCodeBackgroundOff {
				return invalidParams("background.opencode must be on or off, got %q", answers.OpenCode)
			}
			request.OpenCodeBackground, request.OpenCodeBackgroundPersist = intent, intent
		} else {
			choice, err := service.ResolveOpenCodeBackgroundChoice(current.BackgroundIntent)
			if err != nil {
				return invalidParams("%v", err)
			}
			if choice.NeedsPrompt {
				return invalidParams("background.opencode is required: choose on or off for OpenCode background subagents")
			}
			request.OpenCodeBackground, request.OpenCodeBackgroundPersist = choice.Effective, choice.Persist
		}
	} else if answers.OpenCode != "" {
		return invalidParams("background.opencode needs the opencode agent in the selection")
	}
	if service.OffersPiBackground(request.Selection) {
		if answers.Pi != "" {
			intent := model.PiBackgroundIntent(answers.Pi)
			if intent != model.PiBackgroundOn && intent != model.PiBackgroundOff {
				return invalidParams("background.pi must be on or off, got %q", answers.Pi)
			}
			request.PiBackground, request.PiBackgroundPersist = intent, intent
		} else {
			choice, err := service.ResolvePiBackgroundChoice(current.PiBackgroundIntent)
			if err != nil {
				return invalidParams("%v", err)
			}
			if choice.NeedsPrompt {
				return invalidParams("background.pi is required: choose on or off for Pi background subagents")
			}
			request.PiBackground, request.PiBackgroundPersist = choice.Effective, choice.Persist
		}
	} else if answers.Pi != "" {
		return invalidParams("background.pi needs the pi agent in the selection")
	}
	return nil
}

func installSteps(execution pipeline.ExecutionResult) []installStep {
	steps := []installStep{}
	for _, stage := range []pipeline.StageResult{execution.Prepare, execution.Apply, execution.Rollback} {
		for _, step := range stage.Steps {
			entry := installStep{ID: step.StepID, Status: string(step.Status)}
			if step.Err != nil {
				entry.Error = step.Err.Error()
			}
			steps = append(steps, entry)
		}
	}
	return steps
}

func backupIDs(homeDir string) map[string]bool {
	ids := map[string]bool{}
	for _, manifest := range backup.List(homeDir) {
		ids[manifest.ID] = true
	}
	return ids
}

// newBackupID names the newest backup an operation created, if any.
func newBackupID(homeDir string, before map[string]bool) string {
	for _, manifest := range backup.List(homeDir) {
		if !before[manifest.ID] {
			return manifest.ID
		}
	}
	return ""
}
