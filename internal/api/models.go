package api

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/gentleman-programming/gentle-ai/v4/internal/catalog"
	"github.com/gentleman-programming/gentle-ai/v4/internal/components/opencodedefault"
	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
	"github.com/gentleman-programming/gentle-ai/v4/internal/opencode"
	"github.com/gentleman-programming/gentle-ai/v4/internal/service"
)

// modelsParams is model.SyncOverrides on the wire. An absent field leaves that
// assignment alone; an empty object clears it.
type modelsParams struct {
	TargetAgents                     []string                      `json:"targetAgents,omitempty"`
	ModelAssignments                 map[string]openCodeAssignment `json:"modelAssignments,omitempty"`
	ClaudePhaseAssignments           map[string]claudeAssignment   `json:"claudePhaseAssignments,omitempty"`
	ClaudeModelAssignments           map[string]string             `json:"claudeModelAssignments,omitempty"`
	KiroModelAssignments             map[string]string             `json:"kiroModelAssignments,omitempty"`
	CodexModelAssignments            map[string]string             `json:"codexModelAssignments,omitempty"`
	CodexOrchestratorAssignment      *codexOrchestratorAssignment  `json:"codexOrchestratorAssignment,omitempty"`
	ClearCodexOrchestratorAssignment bool                          `json:"clearCodexOrchestratorAssignment,omitempty"`
	CodexCarrilModelAssignments      map[string]string             `json:"codexCarrilModelAssignments,omitempty"`
	CodexPhaseModelAssignments       map[string]string             `json:"codexPhaseModelAssignments,omitempty"`
}

type openCodeAssignment struct {
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
	Effort     string `json:"effort,omitempty"`
}

type claudeAssignment struct {
	Model  string `json:"model"`
	Effort string `json:"effort,omitempty"`
}

type codexOrchestratorAssignment struct {
	Model  string `json:"model"`
	Effort string `json:"effort,omitempty"`
}

// toOverrides validates p and converts it, keeping nil and empty distinct.
func (p *modelsParams) toOverrides() (*model.SyncOverrides, error) {
	if p == nil {
		return nil, nil
	}
	overrides := &model.SyncOverrides{
		ClearCodexOrchestratorAssignment: p.ClearCodexOrchestratorAssignment,
		CodexCarrilModelAssignments:      p.CodexCarrilModelAssignments,
		CodexPhaseModelAssignments:       p.CodexPhaseModelAssignments,
	}
	agents, err := agentIDs(p.TargetAgents, "models.targetAgents")
	if err != nil {
		return nil, err
	}
	overrides.TargetAgents = agents
	if p.ModelAssignments != nil {
		overrides.ModelAssignments = make(map[string]model.ModelAssignment, len(p.ModelAssignments))
		for phase, assignment := range p.ModelAssignments {
			if assignment.ProviderID == "" || assignment.ModelID == "" {
				return nil, invalidParams("models.modelAssignments.%s needs providerId and modelId", phase)
			}
			overrides.ModelAssignments[phase] = model.ModelAssignment{ProviderID: assignment.ProviderID, ModelID: assignment.ModelID, Effort: assignment.Effort}
		}
	}
	if p.ClaudePhaseAssignments != nil {
		overrides.ClaudePhaseAssignments = make(map[string]model.ClaudePhaseAssignment, len(p.ClaudePhaseAssignments))
		for phase, raw := range p.ClaudePhaseAssignments {
			assignment := model.ClaudePhaseAssignment{Model: model.ClaudeModelAlias(raw.Model), Effort: model.ClaudeEffort(raw.Effort)}
			if !assignment.Valid() {
				return nil, invalidParams("models.claudePhaseAssignments.%s: model %q with effort %q is not a valid Claude choice", phase, raw.Model, raw.Effort)
			}
			overrides.ClaudePhaseAssignments[phase] = assignment
		}
	}
	if p.ClaudeModelAssignments != nil {
		overrides.ClaudeModelAssignments = make(map[string]model.ClaudeModelAlias, len(p.ClaudeModelAssignments))
		for phase, raw := range p.ClaudeModelAssignments {
			if alias := model.ClaudeModelAlias(raw); alias.Valid() {
				overrides.ClaudeModelAssignments[phase] = alias
				continue
			}
			return nil, invalidParams("models.claudeModelAssignments.%s: unknown Claude model %q", phase, raw)
		}
	}
	if p.KiroModelAssignments != nil {
		overrides.KiroModelAssignments = make(map[string]model.KiroModelAlias, len(p.KiroModelAssignments))
		for phase, raw := range p.KiroModelAssignments {
			if alias := model.KiroModelAlias(raw); alias.Valid() {
				overrides.KiroModelAssignments[phase] = alias
				continue
			}
			return nil, invalidParams("models.kiroModelAssignments.%s: unknown Kiro model %q", phase, raw)
		}
	}
	if p.CodexModelAssignments != nil {
		overrides.CodexModelAssignments = make(map[string]model.CodexEffort, len(p.CodexModelAssignments))
		for phase, raw := range p.CodexModelAssignments {
			if effort := model.CodexEffort(raw); effort.Valid() {
				overrides.CodexModelAssignments[phase] = effort
				continue
			}
			return nil, invalidParams("models.codexModelAssignments.%s: unknown Codex effort %q", phase, raw)
		}
	}
	if orchestrator := p.CodexOrchestratorAssignment; orchestrator != nil {
		effort := model.CodexEffort(orchestrator.Effort)
		if orchestrator.Model == "" || (orchestrator.Effort != "" && !effort.Valid()) {
			return nil, invalidParams("models.codexOrchestratorAssignment needs a model and a known effort")
		}
		overrides.CodexOrchestratorAssignment = &model.CodexOrchestratorAssignment{Model: orchestrator.Model, Effort: effort}
	}
	return overrides, nil
}

// agentIDs validates agent ids against the catalog.
func agentIDs(raw []string, field string) ([]model.AgentID, error) {
	if raw == nil {
		return nil, nil
	}
	ids := make([]model.AgentID, 0, len(raw))
	for _, value := range raw {
		id := model.AgentID(value)
		if !catalog.IsSupportedAgent(id) {
			return nil, invalidParams("%s: unknown agent %q", field, value)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

type syncParams struct {
	Agents []string      `json:"agents"`
	Models *modelsParams `json:"models"`
}

type syncResult struct {
	Files         []string `json:"files"`
	ManualActions []string `json:"manualActions"`
}

// runSync syncs managed assets exactly like the TUI's sync screen. Without
// agents it syncs the persisted (or discovered) agents.
func runSync(_ context.Context, env *env, params syncParams) (any, error) {
	overrides, err := params.Models.toOverrides()
	if err != nil {
		return nil, err
	}
	agents, err := agentIDs(params.Agents, "agents")
	if err != nil {
		return nil, err
	}
	if len(agents) > 0 {
		if overrides == nil {
			overrides = &model.SyncOverrides{}
		}
		overrides.TargetAgents = agents
	}
	return syncWith(env, overrides)
}

func syncWith(env *env, overrides *model.SyncOverrides) (syncResult, error) {
	result, err := env.deps.Sync(env.deps.HomeDir, overrides)
	if err != nil {
		return syncResult{}, errorf(CodeFailed, "sync: %v", err)
	}
	return syncResult{Files: orEmpty(result.Files), ManualActions: orEmpty(result.ManualActions)}, nil
}

// configurableAgents are the agents the model configuration screen offers.
var configurableAgents = []model.AgentID{model.AgentClaudeCode, model.AgentCodex, model.AgentKiroIDE, model.AgentOpenCode}

func configurableAgent(raw string) (model.AgentID, error) {
	id := model.AgentID(raw)
	if !slices.Contains(configurableAgents, id) {
		return "", invalidParams("agent must be one of claude-code, codex, kiro-ide, opencode; got %q", raw)
	}
	return id, nil
}

// persistedSelection is the model configuration sync would restore.
func persistedSelection(homeDir string) model.Selection {
	var selection model.Selection
	service.LoadPersistedAssignments(homeDir, &selection)
	return selection
}

// claudeCurrent is what the Claude picker opens with: phase assignments when
// present, otherwise the legacy model-only map.
func claudeCurrent(selection model.Selection) map[string]model.ClaudePhaseAssignment {
	if len(selection.ClaudePhaseAssignments) > 0 {
		return selection.ClaudePhaseAssignments
	}
	return model.ClaudePhaseAssignmentsFromLegacy(selection.ClaudeModelAssignments)
}

// presetOverrides resolves choosing preset for agent exactly as the model
// picker does, starting from the persisted configuration.
func presetOverrides(homeDir string, agent model.AgentID, preset string) (*model.SyncOverrides, error) {
	selection := persistedSelection(homeDir)
	switch agent {
	case model.AgentClaudeCode:
		assignments, ok := model.ClaudePresetAssignments(preset, claudeCurrent(selection))
		if !ok {
			return nil, invalidParams("unknown Claude preset %q", preset)
		}
		return service.ClaudeModelOverrides(assignments), nil
	case model.AgentKiroIDE:
		assignments, ok := model.KiroPresetAssignments(preset, selection.KiroModelAssignments)
		if !ok {
			return nil, invalidParams("unknown Kiro preset %q", preset)
		}
		return service.KiroModelOverrides(assignments), nil
	case model.AgentCodex:
		if !slices.ContainsFunc(model.CodexModelPresets(), func(p model.ModelPreset) bool { return p.ID == preset }) {
			return nil, invalidParams("unknown Codex preset %q", preset)
		}
		return service.ApplyCodexChoice(&selection, service.CodexChoice{Preset: preset, Efforts: model.CodexODDEffortsForPreset(preset)}), nil
	default:
		return nil, invalidParams("%s has no model presets; pass models instead", agent)
	}
}

// agentFieldsOnly rejects model fields that belong to another agent, so a
// custom choice cannot silently rewrite a different agent's configuration.
func agentFieldsOnly(agent model.AgentID, p *modelsParams) error {
	owned := map[model.AgentID]bool{
		model.AgentOpenCode:   p.ModelAssignments != nil,
		model.AgentClaudeCode: p.ClaudePhaseAssignments != nil || p.ClaudeModelAssignments != nil,
		model.AgentKiroIDE:    p.KiroModelAssignments != nil,
		model.AgentCodex: p.CodexModelAssignments != nil || p.CodexOrchestratorAssignment != nil || p.ClearCodexOrchestratorAssignment ||
			p.CodexCarrilModelAssignments != nil || p.CodexPhaseModelAssignments != nil,
	}
	for other, set := range owned {
		if other != agent && set {
			return invalidParams("models sets %s fields, but agent is %s", other, agent)
		}
	}
	return nil
}

type modelsSetParams struct {
	Agent  string        `json:"agent"`
	Preset string        `json:"preset"`
	Models *modelsParams `json:"models"`
}

// setModels applies a preset or a custom configuration for one agent and runs
// the sync that persists it, like confirming a Configure Models picker.
func setModels(_ context.Context, env *env, params modelsSetParams) (any, error) {
	agent, err := configurableAgent(params.Agent)
	if err != nil {
		return nil, err
	}
	if (params.Preset == "") == (params.Models == nil) {
		return nil, invalidParams("pass exactly one of preset or models")
	}
	var overrides *model.SyncOverrides
	if params.Preset != "" {
		if overrides, err = presetOverrides(env.deps.HomeDir, agent, params.Preset); err != nil {
			return nil, err
		}
	} else {
		if err := agentFieldsOnly(agent, params.Models); err != nil {
			return nil, err
		}
		if overrides, err = params.Models.toOverrides(); err != nil {
			return nil, err
		}
		if agent == model.AgentOpenCode {
			overrides.ModelAssignments = service.OpenCodeModelOverrides(overrides.ModelAssignments, nil).ModelAssignments
			overrides.SDDMode = cmp.Or(overrides.SDDMode, model.SDDModeMulti)
		}
		if len(overrides.TargetAgents) == 0 {
			overrides.TargetAgents = []model.AgentID{agent}
		}
	}
	return syncWith(env, overrides)
}

// modelsView is the models object holding one agent's current configuration.
// Every field the agent owns is present, so an unset map reads as {}.
func modelsView(selection model.Selection, agent model.AgentID) map[string]any {
	view := map[string]any{}
	switch agent {
	case model.AgentClaudeCode:
		phases := map[string]claudeAssignment{}
		for phase, assignment := range claudeCurrent(selection) {
			phases[phase] = claudeAssignment{Model: string(assignment.Model), Effort: string(assignment.Effort)}
		}
		view["claudePhaseAssignments"] = phases
	case model.AgentKiroIDE:
		view["kiroModelAssignments"] = stringMap(selection.KiroModelAssignments)
	case model.AgentCodex:
		view["codexModelAssignments"] = stringMap(selection.CodexModelAssignments)
		if orchestrator := selection.CodexOrchestratorAssignment; orchestrator != nil {
			view["codexOrchestratorAssignment"] = codexOrchestratorAssignment{Model: orchestrator.Model, Effort: string(orchestrator.Effort)}
		}
		view["codexCarrilModelAssignments"] = stringMap(selection.CodexCarrilModelAssignments)
		view["codexPhaseModelAssignments"] = stringMap(selection.CodexPhaseModelAssignments)
	case model.AgentOpenCode:
		assignments := map[string]openCodeAssignment{}
		for phase, assignment := range selection.ModelAssignments {
			assignments[phase] = openCodeAssignment{ProviderID: assignment.ProviderID, ModelID: assignment.ModelID, Effort: assignment.Effort}
		}
		view["modelAssignments"] = assignments
	}
	return view
}

func stringMap[V ~string](values map[string]V) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = string(value)
	}
	return out
}

type modelsGetParams struct {
	Agent    string `json:"agent"`
	Discover bool   `json:"discover"`
	// Cwd selects the project whose OpenCode configuration applies; without it
	// the global configuration is read.
	Cwd string `json:"cwd"`
}

type presetOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type phaseOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Group string `json:"group"`
}

type modelsGetResult struct {
	Agent         string         `json:"agent"`
	Presets       []presetOption `json:"presets"`
	CurrentPreset *string        `json:"currentPreset"`
	Phases        []phaseOption  `json:"phases"`
	Current       map[string]any `json:"current"`
	Options       modelOptions   `json:"options"`
}

type modelOptions struct {
	Claude   *claudeOptions   `json:"claude,omitempty"`
	Kiro     *kiroOptions     `json:"kiro,omitempty"`
	Codex    *codexOptions    `json:"codex,omitempty"`
	OpenCode *openCodeOptions `json:"opencode,omitempty"`
}

type claudeOptions struct {
	Models []claudeModelOption `json:"models"`
}

type claudeModelOption struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Efforts []string `json:"efforts"`
}

type kiroOptions struct {
	Models []labeled `json:"models"`
}

type labeled struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type codexOptions struct {
	Models  []labeled `json:"models"`
	Efforts []string  `json:"efforts"`
}

type openCodeOptions struct {
	Providers    []openCodeProvider `json:"providers"`
	CustomAgents []string           `json:"customAgents"`
	Warnings     []string           `json:"warnings"`
}

type openCodeProvider struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Models []openCodeModel `json:"models"`
}

type openCodeModel struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Variants []string `json:"variants"`
}

func presetOptions(presets []model.ModelPreset) []presetOption {
	out := make([]presetOption, 0, len(presets))
	for _, preset := range presets {
		out = append(out, presetOption{ID: preset.ID, Label: preset.ID, Description: preset.Description})
	}
	return out
}

func phaseOptions(roles []model.ModelRole) []phaseOption {
	out := make([]phaseOption, 0, len(roles))
	for _, role := range roles {
		out = append(out, phaseOption{ID: role.ID, Label: role.Label, Group: role.Group})
	}
	return out
}

func optionalPreset(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

// getModels reports one agent's presets, configurable rows, current
// assignments, and the choices each row accepts.
func getModels(ctx context.Context, env *env, params modelsGetParams) (any, error) {
	agent, err := configurableAgent(params.Agent)
	if err != nil {
		return nil, err
	}
	if params.Cwd != "" {
		if err := requireCwd(params.Cwd); err != nil {
			return nil, err
		}
	}
	selection := persistedSelection(env.deps.HomeDir)
	result := modelsGetResult{Agent: string(agent), Presets: []presetOption{}}
	switch agent {
	case model.AgentClaudeCode:
		result.Presets = presetOptions(model.ClaudeModelPresets())
		result.CurrentPreset = optionalPreset(model.ClaudePresetFor(claudeCurrent(selection)))
		result.Phases = phaseOptions(model.ClaudeModelRoles())
		options := &claudeOptions{Models: []claudeModelOption{}}
		for _, alias := range model.ClaudeModelAliases() {
			options.Models = append(options.Models, claudeModelOption{ID: string(alias), Label: string(alias), Efforts: strs(model.ClaudeEffortsForModel(alias))})
		}
		result.Options.Claude = options
	case model.AgentKiroIDE:
		result.Presets = presetOptions(model.KiroModelPresets())
		result.CurrentPreset = optionalPreset(model.KiroPresetFor(selection.KiroModelAssignments))
		result.Phases = phaseOptions(model.KiroModelRoles())
		options := &kiroOptions{Models: []labeled{}}
		for _, alias := range model.KiroModelAliases() {
			options.Models = append(options.Models, labeled{ID: string(alias), Label: model.KiroModelID(alias)})
		}
		result.Options.Kiro = options
	case model.AgentCodex:
		result.Presets = presetOptions(model.CodexModelPresets())
		if !hasActiveCodexCustomModels(selection.CodexPhaseModelAssignments) {
			result.CurrentPreset = optionalPreset(model.CodexPresetFor(selection.CodexModelAssignments))
		}
		result.Phases = phaseOptions(model.CodexModelRoles())
		available := model.CodexAvailableModels()
		if params.Discover {
			if discovered := env.deps.CodexModels(ctx); len(discovered) > 0 {
				available = discovered
			}
		}
		options := &codexOptions{Models: []labeled{}, Efforts: strs(model.CodexEfforts())}
		for _, id := range available {
			options.Models = append(options.Models, labeled{ID: id, Label: id})
		}
		result.Options.Codex = options
	case model.AgentOpenCode:
		// Without cwd only the global configuration applies; the catalog
		// command still needs a directory, and the home is never a project.
		snapshot, configErr := opencode.ResolveRuntimeConfigForHome(env.deps.HomeDir, params.Cwd)
		projectDir := cmp.Or(params.Cwd, env.deps.HomeDir)
		settingsPath := cmp.Or(snapshot.WritePath, opencode.DefaultSettingsPathForHome(env.deps.HomeDir))
		if len(selection.ModelAssignments) == 0 {
			// Like the TUI, fall back to what the OpenCode settings configure.
			if current, err := service.ReadOpenCodeAssignments(settingsPath); err == nil {
				selection.ModelAssignments = current
			}
		}
		options, err := openCodeModelOptions(ctx, env, params.Discover, projectDir, settingsPath, snapshot.Providers)
		if err != nil {
			return nil, err
		}
		if configErr != nil {
			options.Warnings = append(options.Warnings, "Could not read OpenCode config: "+configErr.Error())
		}
		options.Warnings = append(options.Warnings, snapshot.Diagnostics...)
		result.Phases = openCodePhases(options.CustomAgents)
		result.Options.OpenCode = options
	}
	result.Current = modelsView(selection, agent)
	return result, nil
}

// hasActiveCodexCustomModels reports per-role Custom model choices; retired
// SDD keys alone do not make the configuration custom.
func hasActiveCodexCustomModels(models map[string]string) bool {
	for role := range models {
		if !strings.HasPrefix(role, "sdd-") {
			return true
		}
	}
	return false
}

// openCodePhases mirrors the rows of the TUI's OpenCode model picker.
func openCodePhases(customAgents []string) []phaseOption {
	phases := []phaseOption{{ID: "gentle-orchestrator", Label: "gentle-orchestrator", Group: "orchestrator"}}
	add := func(ids []string, group string) {
		for _, id := range ids {
			phases = append(phases, phaseOption{ID: id, Label: id, Group: group})
		}
	}
	add(opencode.GentleAIODDPhases(), "odd")
	add(opencode.JDPhases(), "judgment-day")
	add(opencode.ReviewPhases(), "review")
	add([]string{"general", "explore"}, "native")
	add(customAgents, "custom")
	return phases
}

func openCodeModelOptions(ctx context.Context, env *env, discover bool, projectDir, settingsPath string, configured map[string]opencode.Provider) (*openCodeOptions, error) {
	options := &openCodeOptions{Providers: []openCodeProvider{}, CustomAgents: []string{}, Warnings: []string{}}
	providers := configured
	if discover {
		runtime, err := env.deps.OpenCodeCatalog(ctx, projectDir)
		if err != nil {
			options.Warnings = append(options.Warnings, "OpenCode model discovery failed: "+err.Error())
		}
		providers = opencode.MergeConfiguredCatalog(runtime, configured)
	}
	for id, provider := range providers {
		models := opencode.FilterModelsForSDD(provider)
		if len(models) == 0 {
			continue
		}
		entry := openCodeProvider{ID: id, Name: cmp.Or(provider.Name, id), Models: make([]openCodeModel, 0, len(models))}
		for _, m := range models {
			entry.Models = append(entry.Models, openCodeModel{ID: m.ID, Name: cmp.Or(m.Name, m.ID), Variants: orEmpty(m.EffortLevels())})
		}
		options.Providers = append(options.Providers, entry)
	}
	slices.SortFunc(options.Providers, func(a, b openCodeProvider) int { return cmp.Compare(a.Name, b.Name) })
	agents, err := opencodedefault.DiscoverCustomAgents(settingsPath)
	if err != nil {
		options.Warnings = append(options.Warnings, "Could not discover custom agents from opencode.json: "+err.Error())
	}
	options.CustomAgents = orEmpty(agents)
	return options, nil
}
