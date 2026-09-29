// Package service holds the installer flow rules and operations that both the
// interactive TUI and the headless API apply, so neither surface owns a private
// copy of a rule the other one needs.
package service

import (
	"strings"

	"github.com/gentleman-programming/gentle-ai/v4/internal/agentbuilder"
	"github.com/gentleman-programming/gentle-ai/v4/internal/cli"
	"github.com/gentleman-programming/gentle-ai/v4/internal/components/skills"
	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
	"github.com/gentleman-programming/gentle-ai/v4/internal/planner"
	"github.com/gentleman-programming/gentle-ai/v4/internal/system"
)

// InstallQuestion names an optional choice the installer asks for a selection.
type InstallQuestion string

const (
	QuestionCommunityTools     InstallQuestion = "communityTools"
	QuestionOpenCodePlugins    InstallQuestion = "openCodePlugins"
	QuestionPiPlugins          InstallQuestion = "piPlugins"
	QuestionSkills             InstallQuestion = "skills"
	QuestionRDD                InstallQuestion = "rdd"
	QuestionOpenCodeBackground InstallQuestion = "openCodeBackground"
	QuestionPiBackground       InstallQuestion = "piBackground"
)

// InstallQuestions lists, in installer order, the optional choices the
// installer asks for selection. The background questions appear only when the
// environment and prior state leave the choice unresolved, exactly when the
// TUI shows its prompt.
func InstallQuestions(selection model.Selection, priorOpenCode model.OpenCodeBackgroundIntent, priorPi model.PiBackgroundIntent) ([]InstallQuestion, error) {
	questions := []InstallQuestion{}
	if AsksCommunityTools(selection) {
		questions = append(questions, QuestionCommunityTools)
	}
	if AsksOpenCodePlugins(selection) {
		questions = append(questions, QuestionOpenCodePlugins)
	}
	if AsksPiPlugins(selection) {
		questions = append(questions, QuestionPiPlugins)
	}
	if AsksSkills(selection) {
		questions = append(questions, QuestionSkills)
	}
	questions = append(questions, QuestionRDD)
	if OffersOpenCodeBackground(selection) {
		choice, err := ResolveOpenCodeBackgroundChoice(priorOpenCode)
		if err != nil {
			return nil, err
		}
		if choice.NeedsPrompt {
			questions = append(questions, QuestionOpenCodeBackground)
		}
	}
	if OffersPiBackground(selection) {
		choice, err := ResolvePiBackgroundChoice(priorPi)
		if err != nil {
			return nil, err
		}
		if choice.NeedsPrompt {
			questions = append(questions, QuestionPiBackground)
		}
	}
	return questions, nil
}

// IsPiOnlyAgents reports the Pi-only installer path, which skips persona,
// preset, and optional community setup.
func IsPiOnlyAgents(agents []model.AgentID) bool {
	return len(agents) == 1 && agents[0] == model.AgentPi
}

// PiOnlyComponents is the component set the Pi-only path installs: Pi's own
// stack comes from gentle-pi, so only memory is managed here.
func PiOnlyComponents() []model.ComponentID {
	return []model.ComponentID{model.ComponentEngram}
}

// AsksCommunityTools reports whether the installer offers community tools.
func AsksCommunityTools(selection model.Selection) bool {
	return !IsPiOnlyAgents(selection.Agents)
}

// AsksOpenCodePlugins reports whether the installer offers OpenCode plugins.
func AsksOpenCodePlugins(selection model.Selection) bool {
	return selection.HasAgent(model.AgentOpenCode)
}

// AsksPiPlugins reports whether the installer offers optional Pi packages.
func AsksPiPlugins(selection model.Selection) bool {
	return selection.HasAgent(model.AgentPi)
}

// AsksSkills reports whether the installer offers the per-skill picker: only
// the custom preset with the Skills component lets the user pick skills.
func AsksSkills(selection model.Selection) bool {
	return selection.Preset == model.PresetCustom && selection.HasComponent(model.ComponentSkills)
}

// PreselectedSkills is what the custom preset's skill picker starts with:
// every selectable skill.
func PreselectedSkills() []model.SkillID {
	return skills.AllSkillIDs()
}

// OffersOpenCodeBackground reports whether the OpenCode background preference
// must be resolved before installing.
func OffersOpenCodeBackground(selection model.Selection) bool {
	return selection.HasAgent(model.AgentOpenCode)
}

// OffersPiBackground gates only on the Pi agent: Pi's stack is provided by
// gentle-pi itself, so the Pi-only flow must still resolve the preference.
func OffersPiBackground(selection model.Selection) bool {
	return selection.HasAgent(model.AgentPi)
}

// OpenCodeBackgroundChoice is the installer's resolved OpenCode background
// preference. When NeedsPrompt is true the user must choose on or off.
type OpenCodeBackgroundChoice struct {
	Effective   model.OpenCodeBackgroundIntent
	Persist     model.OpenCodeBackgroundIntent
	NeedsPrompt bool
}

// ResolveOpenCodeBackgroundChoice resolves the environment and prior managed
// state the way the installer does. An unresolved auto installs in the
// foreground.
func ResolveOpenCodeBackgroundChoice(prior model.OpenCodeBackgroundIntent) (OpenCodeBackgroundChoice, error) {
	resolution, err := cli.ResolveOpenCodeBackgroundInteractive(prior)
	if err != nil {
		return OpenCodeBackgroundChoice{}, err
	}
	if resolution.NeedsPrompt {
		return OpenCodeBackgroundChoice{NeedsPrompt: true}, nil
	}
	effective := resolution.Effective
	if effective == model.OpenCodeBackgroundAuto {
		effective = model.OpenCodeBackgroundOff
	}
	return OpenCodeBackgroundChoice{Effective: effective, Persist: resolution.Persist}, nil
}

// PiBackgroundChoice is the installer's resolved Pi background preference.
type PiBackgroundChoice struct {
	Effective   model.PiBackgroundIntent
	Persist     model.PiBackgroundIntent
	NeedsPrompt bool
}

// ResolvePiBackgroundChoice resolves the environment and prior managed state
// the way the installer does. Unmanaged auto stays auto: only managed on/off
// decisions are projected for gentle-pi.
func ResolvePiBackgroundChoice(prior model.PiBackgroundIntent) (PiBackgroundChoice, error) {
	resolution, err := cli.ResolvePiBackgroundInteractive(prior)
	if err != nil {
		return PiBackgroundChoice{}, err
	}
	if resolution.NeedsPrompt {
		return PiBackgroundChoice{NeedsPrompt: true}, nil
	}
	return PiBackgroundChoice{Effective: resolution.Effective, Persist: resolution.Persist}, nil
}

// InstallStepIDs lists the pipeline step ids an install of resolved produces,
// in execution order, so progress can be shown before the steps run.
func InstallStepIDs(resolved planner.ResolvedPlan, communityTools []model.CommunityToolID) []string {
	ids := make([]string, 0, 3+len(resolved.Agents)+len(communityTools)+len(resolved.OrderedComponents))
	ids = append(ids, "prepare:check-dependencies", "prepare:backup-snapshot", "apply:rollback-restore")
	for _, agent := range resolved.Agents {
		ids = append(ids, "agent:"+string(agent))
	}
	for _, tool := range communityTools {
		ids = append(ids, "community-tool:"+string(tool))
	}
	for _, component := range resolved.OrderedComponents {
		ids = append(ids, "component:"+string(component))
	}
	return ids
}

// AgentDetected reports whether detection found the agent's config directory.
func AgentDetected(detection system.DetectionResult, agent model.AgentID) bool {
	for _, config := range detection.Configs {
		if config.Exists && strings.TrimSpace(config.Agent) == string(agent) {
			return true
		}
	}
	return false
}

// builderEngineIDs are the agents that can generate a custom agent, in the
// order the agent builder offers them.
var builderEngineIDs = []model.AgentID{
	model.AgentClaudeCode,
	model.AgentOpenCode,
	model.AgentGeminiCLI,
	model.AgentCodex,
}

// BuilderEngineIDs returns every agent the agent builder can use as an engine.
func BuilderEngineIDs() []model.AgentID {
	return append([]model.AgentID(nil), builderEngineIDs...)
}

// EngineAvailable reports whether an agent builder engine's binary is usable.
type EngineAvailable func(model.AgentID) bool

// DefaultEngineAvailable probes PATH for the engine's binary.
func DefaultEngineAvailable(id model.AgentID) bool {
	engine := agentbuilder.NewEngine(id)
	return engine != nil && engine.Available()
}

// AvailableBuilderEngines lists the builder engines whose binaries are usable.
func AvailableBuilderEngines(available EngineAvailable) []model.AgentID {
	engines := []model.AgentID{}
	for _, id := range builderEngineIDs {
		if available(id) {
			engines = append(engines, id)
		}
	}
	return engines
}
