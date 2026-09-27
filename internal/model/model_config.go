package model

import (
	"maps"
	"strings"
)

// ModelPreset is a named model assignment preset an agent's model picker offers.
type ModelPreset struct {
	ID          string
	Description string
}

// ModelRole is one configurable model assignment row. Group names the family
// the row belongs to: odd, judgment-day, review, or general.
type ModelRole struct {
	ID    string
	Label string
	Group string
}

// claudeRoles are the Claude and Kiro model assignment rows, in picker order.
var claudeRoles = []ModelRole{
	{ID: "odd-explorer", Label: "ODD Explorer", Group: "odd"},
	{ID: "odd-worker", Label: "ODD Worker", Group: "odd"},
	{ID: "odd-verify", Label: "ODD Verify", Group: "odd"},
	{ID: "jd-judge-a", Label: "JD Judge A", Group: "judgment-day"},
	{ID: "jd-judge-b", Label: "JD Judge B", Group: "judgment-day"},
	{ID: "jd-fix-agent", Label: "JD Fix Agent", Group: "judgment-day"},
	{ID: "risk", Label: "RDD Risk", Group: "review"},
	{ID: "readability", Label: "RDD Readability", Group: "review"},
	{ID: "reliability", Label: "RDD Reliability", Group: "review"},
	{ID: "resilience", Label: "RDD Resilience", Group: "review"},
	{ID: "refuter", Label: "RDD Refuter", Group: "review"},
	{ID: "validator", Label: "RDD Validator", Group: "review"},
	{ID: "default", Label: "General delegation", Group: "general"},
}

// ClaudeModelRoles returns the Claude model assignment rows in picker order.
func ClaudeModelRoles() []ModelRole { return append([]ModelRole(nil), claudeRoles...) }

// KiroModelRoles returns the Kiro model assignment rows; Kiro configures the
// same roles as Claude.
func KiroModelRoles() []ModelRole { return append([]ModelRole(nil), claudeRoles...) }

var claudePresets = []ModelPreset{
	{ID: "balanced", Description: "Smart defaults for ODD delegation and review roles"},
	{ID: "performance", Description: "Maximum quality for delegation and review roles"},
	{ID: "economy", Description: "Cost-optimised delegation and review roles"},
	{ID: "diversity", Description: "Diversity: Opus for Judge A, Haiku for Judge B, Sonnet for fixes"},
}

var claudePresetTables = map[string]func() map[string]ClaudeModelAlias{
	"balanced":    ClaudeModelPresetBalanced,
	"performance": ClaudeModelPresetPerformance,
	"economy":     ClaudeModelPresetEconomy,
	"diversity":   ClaudeModelPresetDiversity,
}

// ClaudeModelPresets returns the Claude presets in picker order.
func ClaudeModelPresets() []ModelPreset { return append([]ModelPreset(nil), claudePresets...) }

// ClaudeModelAliases returns the Claude model choices in picker order.
func ClaudeModelAliases() []ClaudeModelAlias {
	return []ClaudeModelAlias{ClaudeModelFable, ClaudeModelOpus, ClaudeModelSonnet, ClaudeModelHaiku}
}

// claudePresetBase is a preset as the picker shows it: retired SDD phase rows
// are dropped and the ODD rows use sonnet with the default effort.
func claudePresetBase(preset string) (map[string]ClaudePhaseAssignment, bool) {
	table, ok := claudePresetTables[preset]
	if !ok {
		return nil, false
	}
	assignments := ClaudePhaseAssignmentsFromModelPreset(table())
	for key := range assignments {
		if strings.HasPrefix(key, "sdd-") {
			delete(assignments, key)
		}
	}
	assignments["odd-explorer"] = ClaudePhaseAssignment{Model: ClaudeModelSonnet}
	assignments["odd-worker"] = ClaudePhaseAssignment{Model: ClaudeModelSonnet}
	assignments["odd-verify"] = ClaudePhaseAssignment{Model: ClaudeModelSonnet}
	return assignments, true
}

// ClaudePresetAssignments returns what choosing preset writes. Legacy SDD
// phase values in current are preserved without being offered as rows.
func ClaudePresetAssignments(preset string, current map[string]ClaudePhaseAssignment) (map[string]ClaudePhaseAssignment, bool) {
	assignments, ok := claudePresetBase(preset)
	if !ok {
		return nil, false
	}
	for key, value := range current {
		if strings.HasPrefix(key, "sdd-") {
			assignments[key] = value
		}
	}
	return assignments, true
}

// ClaudePresetFor names the preset assignments match, or "" when they are custom.
func ClaudePresetFor(assignments map[string]ClaudePhaseAssignment) string {
	if len(assignments) == 0 {
		return ""
	}
	// Only the editable role rows decide the preset. The orchestrator row a
	// preset table carries is never persisted (Claude Code owns the main
	// session model), so comparing it would make every saved preset read as
	// custom after a round trip through state.json.
	visible := make(map[string]ClaudePhaseAssignment)
	for _, role := range claudeRoles {
		if value, ok := assignments[role.ID]; ok {
			visible[role.ID] = value
		}
	}
	for _, preset := range claudePresets {
		base, _ := claudePresetBase(preset.ID)
		delete(base, "orchestrator")
		if maps.Equal(base, visible) || maps.Equal(ClaudePhaseAssignmentsFromModelPreset(claudePresetTables[preset.ID]()), assignments) {
			return preset.ID
		}
	}
	return ""
}

// ClaudeLegacyAssignments projects model+effort assignments onto the
// historical model-only map, dropping invalid models.
func ClaudeLegacyAssignments(assignments map[string]ClaudePhaseAssignment) map[string]ClaudeModelAlias {
	if len(assignments) == 0 {
		return nil
	}
	out := make(map[string]ClaudeModelAlias, len(assignments))
	for phase, assignment := range assignments {
		if assignment.Model.Valid() {
			out[phase] = assignment.Model
		}
	}
	return out
}

var kiroPresets = []ModelPreset{
	{ID: "balanced", Description: "Kiro Auto for ODD delegation and review roles"},
	{ID: "performance", Description: "Frontier Claude-family models for delegation and review roles"},
	{ID: "economy", Description: "Low-credit Kiro options: Qwen, DeepSeek, and MiniMax for budget-conscious runs"},
	{ID: "open-weight", Description: "Kiro open-weight families: MiniMax, GLM, DeepSeek, and Qwen"},
}

var kiroPresetTables = map[string]func() map[string]KiroModelAlias{
	"balanced":    KiroModelPresetBalanced,
	"performance": KiroModelPresetPerformance,
	"economy":     KiroModelPresetEconomy,
	"open-weight": KiroModelPresetOpenWeight,
}

// KiroModelPresets returns the Kiro presets in picker order.
func KiroModelPresets() []ModelPreset { return append([]ModelPreset(nil), kiroPresets...) }

// KiroModelAliases returns the Kiro model choices in picker order.
func KiroModelAliases() []KiroModelAlias {
	return []KiroModelAlias{KiroModelAuto, KiroModelOpus, KiroModelSonnet, KiroModelHaiku, KiroModelMiniMax, KiroModelGLM, KiroModelDeepSeek, KiroModelQwen}
}

// KiroPresetAssignments returns what choosing preset writes. Keys the preset
// does not cover (retired roles) keep their current values.
func KiroPresetAssignments(preset string, current map[string]KiroModelAlias) (map[string]KiroModelAlias, bool) {
	table, ok := kiroPresetTables[preset]
	if !ok {
		return nil, false
	}
	assignments := table()
	for key, alias := range current {
		if _, active := assignments[key]; !active {
			assignments[key] = alias
		}
	}
	return assignments, true
}

// KiroPresetFor names the preset assignments match, or "" when they are custom.
func KiroPresetFor(assignments map[string]KiroModelAlias) string {
	if len(assignments) == 0 {
		return ""
	}
	for _, preset := range kiroPresets {
		if maps.Equal(kiroPresetTables[preset.ID](), assignments) {
			return preset.ID
		}
	}
	return ""
}

// codexRoles are the Codex custom assignment rows, in picker order.
var codexRoles = []ModelRole{
	{ID: "jd-judge-a", Label: "JD Judge A", Group: "judgment-day"},
	{ID: "jd-judge-b", Label: "JD Judge B", Group: "judgment-day"},
	{ID: "jd-fix-agent", Label: "JD Fix Agent", Group: "judgment-day"},
	{ID: "default", Label: "General delegation", Group: "general"},
	{ID: "odd-explorer", Label: "ODD Explorer", Group: "odd"},
	{ID: "odd-worker", Label: "ODD Worker", Group: "odd"},
	{ID: "odd-verify", Label: "ODD Verify", Group: "odd"},
	{ID: "rdd-risk", Label: "RDD Risk", Group: "review"},
	{ID: "rdd-readability", Label: "RDD Readability", Group: "review"},
	{ID: "rdd-reliability", Label: "RDD Reliability", Group: "review"},
	{ID: "rdd-resilience", Label: "RDD Resilience", Group: "review"},
	{ID: "rdd-refuter", Label: "RDD Refuter", Group: "review"},
	{ID: "rdd-validator", Label: "RDD Validator", Group: "review"},
}

// CodexModelRoles returns the Codex custom assignment rows in picker order.
func CodexModelRoles() []ModelRole { return append([]ModelRole(nil), codexRoles...) }

var codexPresets = []ModelPreset{
	{ID: string(CodexPresetLowCost), Description: "Lowest-cost GPT-6 mix — Sol for reasoning, Luna for code and light work"},
	{ID: string(CodexPresetRecommended), Description: "Balanced GPT-6 mix — Sol for reasoning, Luna for code and light work"},
	{ID: string(CodexPresetPowerful), Description: "High-effort GPT-6 mix — Astra for reasoning, Sol for code, Luna for light work"},
}

// CodexModelPresets returns the Codex presets in picker order.
func CodexModelPresets() []ModelPreset { return append([]ModelPreset(nil), codexPresets...) }

// CodexEfforts returns the reasoning efforts the Codex picker offers.
func CodexEfforts() []CodexEffort {
	return []CodexEffort{CodexEffortLow, CodexEffortMedium, CodexEffortHigh, CodexEffortXHigh}
}

// legacyCodexPresetEfforts is a preset's historical per-phase effort table.
func legacyCodexPresetEfforts(preset string) map[string]CodexEffort {
	switch CodexPresetKey(preset) {
	case CodexPresetLowCost:
		return CodexModelPresetLowCost()
	case CodexPresetPowerful:
		return CodexModelPresetPowerful()
	default:
		return CodexModelPresetRecommended()
	}
}

// CodexPresetFor names the preset efforts match, or "" when none does. A
// preset matches its ODD efforts exactly, a superset of them (custom efforts
// layered on the preset), or its historical per-phase table.
func CodexPresetFor(efforts map[string]CodexEffort) string {
	if len(efforts) == 0 {
		return ""
	}
	for _, preset := range codexPresets {
		defaults := CodexODDEffortsForPreset(preset.ID)
		if maps.Equal(defaults, efforts) || codexEffortsCover(efforts, defaults) || maps.Equal(legacyCodexPresetEfforts(preset.ID), efforts) {
			return preset.ID
		}
	}
	return ""
}

func codexEffortsCover(efforts, defaults map[string]CodexEffort) bool {
	if len(efforts) < len(defaults) {
		return false
	}
	for phase, effort := range defaults {
		if efforts[phase] != effort {
			return false
		}
	}
	return true
}
