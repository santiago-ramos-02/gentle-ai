package service

import (
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/gentleman-programming/gentle-ai/v3/internal/components/filemerge"
	"github.com/gentleman-programming/gentle-ai/v3/internal/model"
	"github.com/gentleman-programming/gentle-ai/v3/internal/opencode"
)

// The functions below turn a confirmed model picker choice into the sync
// overrides that apply and persist it. The TUI's Configure Models screens and
// the headless API both call them, so a choice means the same thing on either
// surface.

// ClaudeModelOverrides syncs Claude with the given role assignments.
func ClaudeModelOverrides(assignments map[string]model.ClaudePhaseAssignment) *model.SyncOverrides {
	return &model.SyncOverrides{
		TargetAgents:           []model.AgentID{model.AgentClaudeCode},
		ClaudeModelAssignments: model.ClaudeLegacyAssignments(assignments),
		ClaudePhaseAssignments: assignments,
	}
}

// KiroModelOverrides syncs Kiro with the given role assignments.
func KiroModelOverrides(assignments map[string]model.KiroModelAlias) *model.SyncOverrides {
	return &model.SyncOverrides{
		TargetAgents:         []model.AgentID{model.AgentKiroIDE},
		KiroModelAssignments: assignments,
	}
}

// OpenCodeModelOverrides syncs OpenCode with the given agent assignments.
// Efforts the discovered catalog no longer offers are cleared first.
func OpenCodeModelOverrides(assignments map[string]model.ModelAssignment, catalog map[string][]opencode.Model) *model.SyncOverrides {
	return &model.SyncOverrides{
		TargetAgents:     []model.AgentID{model.AgentOpenCode},
		ModelAssignments: SanitizeModelEfforts(assignments, catalog),
		SDDMode:          model.SDDModeMulti,
	}
}

// CodexChoice is a confirmed Codex picker choice. Efforts are the per-role
// reasoning efforts the picker produced: the preset's own, or the preset's
// with custom efforts layered on top. CustomModels holds the per-role model
// ids of a confirmed Custom choice and is nil for a plain preset.
type CodexChoice struct {
	Preset       string
	Efforts      map[string]model.CodexEffort
	Custom       bool
	CustomModels map[string]string
}

// ApplyCodexChoice records choice in selection the way the Codex picker does
// and returns the overrides that sync and persist it. The preset decides the
// lane models; a preset also sets its curated orchestrator, while Custom
// clears it. Retired SDD keys already in selection are kept until a separate
// migration owns their removal.
func ApplyCodexChoice(selection *model.Selection, choice CodexChoice) *model.SyncOverrides {
	efforts := maps.Clone(choice.Efforts)
	if efforts == nil {
		efforts = map[string]model.CodexEffort{}
	}
	for role, effort := range selection.CodexModelAssignments {
		if strings.HasPrefix(role, "sdd-") {
			efforts[role] = effort
		}
	}
	selection.CodexModelAssignments = efforts
	carrilModels := model.CodexCarrilModelsForPreset(choice.Preset)
	selection.CodexCarrilModelAssignments = carrilModels
	if choice.Custom {
		selection.CodexOrchestratorAssignment = nil
		selection.ClearCodexOrchestratorAssignment = true
	} else {
		selection.CodexOrchestratorAssignment = model.CodexPresetOrchestratorAssignment(choice.Preset)
		selection.ClearCodexOrchestratorAssignment = false
	}

	legacyModels := make(map[string]string)
	for role, modelID := range selection.CodexPhaseModelAssignments {
		if strings.HasPrefix(role, "sdd-") {
			legacyModels[role] = modelID
		}
	}
	// Custom persists the per-role model map so the inject layer renders a
	// per-role table instead of the lane table; a preset clears it.
	selection.CodexPhaseModelAssignments = nil
	if choice.Custom {
		selection.CodexPhaseModelAssignments = nonEmptyModels(choice.CustomModels)
	}
	if len(legacyModels) > 0 {
		if selection.CodexPhaseModelAssignments == nil {
			selection.CodexPhaseModelAssignments = legacyModels
		} else {
			maps.Copy(selection.CodexPhaseModelAssignments, legacyModels)
		}
	}

	// An empty map explicitly clears stale active role models on the preset path.
	phaseOverride := selection.CodexPhaseModelAssignments
	if phaseOverride == nil {
		phaseOverride = map[string]string{}
	}
	return &model.SyncOverrides{
		TargetAgents:                     []model.AgentID{model.AgentCodex},
		CodexModelAssignments:            efforts,
		CodexOrchestratorAssignment:      selection.CodexOrchestratorAssignment,
		ClearCodexOrchestratorAssignment: selection.ClearCodexOrchestratorAssignment,
		CodexCarrilModelAssignments:      carrilModels,
		CodexPhaseModelAssignments:       phaseOverride,
	}
}

func nonEmptyModels(models map[string]string) map[string]string {
	out := make(map[string]string, len(models))
	for role, modelID := range models {
		if modelID != "" {
			out[role] = modelID
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SanitizeModelEfforts clears efforts the catalog says a model no longer
// supports, so a stale effort is not re-injected on the next sync. Models the
// catalog does not list keep their effort.
func SanitizeModelEfforts(assignments map[string]model.ModelAssignment, catalog map[string][]opencode.Model) map[string]model.ModelAssignment {
	if assignments == nil {
		return nil
	}
	sanitized := make(map[string]model.ModelAssignment, len(assignments))
	for phase, assignment := range assignments {
		sanitized[phase] = sanitizeModelEffort(assignment, catalog)
	}
	return sanitized
}

func sanitizeModelEffort(assignment model.ModelAssignment, catalog map[string][]opencode.Model) model.ModelAssignment {
	if assignment.Effort == "" {
		return assignment
	}
	modelsForProvider, ok := catalog[assignment.ProviderID]
	if !ok {
		return assignment
	}
	for _, available := range modelsForProvider {
		if available.ID != assignment.ModelID {
			continue
		}
		levels := available.EffortLevels()
		if len(levels) == 0 {
			if available.Reasoning {
				return assignment
			}
			assignment.Effort = ""
			return assignment
		}
		if slices.Contains(levels, assignment.Effort) {
			return assignment
		}
		assignment.Effort = ""
		return assignment
	}
	return assignment
}

// ReadOpenCodeAssignments reads the agent model assignments an OpenCode
// settings file configures. A missing or unparsable file has none; legacy
// sdd-orchestrator keys are read as gentle-orchestrator without modifying them.
func ReadOpenCodeAssignments(settingsPath string) (map[string]model.ModelAssignment, error) {
	assignments := make(map[string]model.ModelAssignment)
	data, err := os.ReadFile(settingsPath)
	if os.IsNotExist(err) {
		return assignments, nil
	}
	if err != nil {
		return nil, err
	}
	settings, err := filemerge.UnmarshalJSONObject(data)
	if err != nil {
		return assignments, nil
	}
	agents, ok := settings["agent"].(map[string]any)
	if !ok {
		return assignments, nil
	}
	for name, raw := range agents {
		agent, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		spec, ok := agent["model"].(string)
		if !ok {
			continue
		}
		providerID, modelID, ok := model.SplitModelSpec(spec)
		if !ok {
			continue
		}
		if name == "sdd-orchestrator" {
			name = "gentle-orchestrator" // Read legacy settings without modifying them.
			if _, exists := agents[name]; exists {
				continue
			}
		}
		effort, _ := agent["variant"].(string)
		assignments[name] = model.ModelAssignment{ProviderID: providerID, ModelID: modelID, Effort: effort}
	}
	return assignments, nil
}
