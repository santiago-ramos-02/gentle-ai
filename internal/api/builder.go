package api

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/gentleman-programming/gentle-ai/v3/internal/agentbuilder"
	"github.com/gentleman-programming/gentle-ai/v3/internal/catalog"
	"github.com/gentleman-programming/gentle-ai/v3/internal/model"
	"github.com/gentleman-programming/gentle-ai/v3/internal/service"
)

type builderEngine struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
}

type builderEnginesResult struct {
	Engines []builderEngine `json:"engines"`
}

func agentName(id model.AgentID) string {
	for _, agent := range catalog.AllAgents() {
		if agent.ID == id {
			return agent.Name
		}
	}
	return string(id)
}

func builderEngines(_ context.Context, env *env, _ noParams) (any, error) {
	ids := service.BuilderEngineIDs()
	engines := make([]builderEngine, 0, len(ids))
	for _, id := range ids {
		engines = append(engines, builderEngine{ID: string(id), Name: agentName(id), Available: env.deps.EngineAvailable(id)})
	}
	return builderEnginesResult{Engines: engines}, nil
}

type builtAgent struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description"`
	Trigger     string `json:"trigger,omitempty"`
	Content     string `json:"content"`
}

type builderGenerateParams struct {
	Engine string `json:"engine"`
	Prompt string `json:"prompt"`
}

type builderGenerateResult struct {
	Agent     builtAgent `json:"agent"`
	Targets   []string   `json:"targets"`
	Conflicts []string   `json:"conflicts"`
}

// builderAdapters resolves where a built agent installs, as the TUI does.
func builderAdapters(ctx context.Context, env *env) ([]agentbuilder.AdapterInfo, error) {
	detection, err := env.deps.detect(ctx)
	if err != nil {
		return nil, err
	}
	engines := service.AvailableBuilderEngines(env.deps.EngineAvailable)
	return service.BuilderAdapters(env.deps.HomeDir, detection, engines), nil
}

// conflicts explains a name that collides with a built-in skill.
func conflicts(agent *agentbuilder.GeneratedAgent) []string {
	if agentbuilder.HasConflictWithBuiltin(agent.Name) {
		return []string{"'" + agent.Name + "' conflicts with a built-in skill. It will be installed as '" + agent.Name + "-custom'."}
	}
	return []string{}
}

// generationTimeout bounds one engine run, like the TUI's agent builder.
const generationTimeout = 5 * time.Minute

func builderGenerate(ctx context.Context, env *env, params builderGenerateParams) (any, error) {
	engineID := model.AgentID(params.Engine)
	if !slices.Contains(service.BuilderEngineIDs(), engineID) {
		return nil, invalidParams("engine must be one of the agent builder engines, got %q", params.Engine)
	}
	if strings.TrimSpace(params.Prompt) == "" {
		return nil, invalidParams("prompt is required")
	}
	engine := env.deps.NewEngine(engineID)
	if engine == nil || !engine.Available() {
		return nil, errorf(CodeUnsupported, "%s is not available on this machine", agentName(engineID))
	}
	adapters, err := builderAdapters(ctx, env)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, generationTimeout)
	defer cancel()
	agent, err := service.GenerateAgent(ctx, engine, params.Prompt, adapters)
	if err != nil {
		return nil, errorf(CodeFailed, "generate agent: %v", err)
	}
	return builderGenerateResult{
		Agent:     builtAgent{Name: agent.Name, Title: agent.Title, Description: agent.Description, Trigger: agent.Trigger, Content: agent.Content},
		Targets:   service.BuilderTargets(adapters, agent),
		Conflicts: conflicts(agent),
	}, nil
}

type builderInstallParams struct {
	Agent *builtAgent `json:"agent"`
	// Engine records which engine generated the agent in the registry.
	Engine string `json:"engine"`
}

type builderInstallResult struct {
	Files     []string `json:"files"`
	RenamedTo string   `json:"renamedTo,omitempty"`
	Warnings  []string `json:"warnings"`
}

// builderInstall installs an agent from builder.generate, possibly edited.
// The content is parsed again so the installed name and sections always come
// from the content itself; a name that disagrees with it is rejected.
func builderInstall(ctx context.Context, env *env, params builderInstallParams) (any, error) {
	if params.Agent == nil || strings.TrimSpace(params.Agent.Content) == "" {
		return nil, invalidParams("agent.content is required")
	}
	agent, err := agentbuilder.Parse(params.Agent.Content)
	if err != nil {
		return nil, invalidParams("agent.content: %v", err)
	}
	if params.Agent.Name != "" && params.Agent.Name != agent.Name {
		return nil, invalidParams("agent.name %q does not match the content's title (%q)", params.Agent.Name, agent.Name)
	}
	engineID := model.AgentID(params.Engine)
	if params.Engine != "" && !slices.Contains(service.BuilderEngineIDs(), engineID) {
		return nil, invalidParams("unknown engine %q", params.Engine)
	}
	adapters, err := builderAdapters(ctx, env)
	if err != nil {
		return nil, err
	}
	if len(adapters) == 0 {
		return nil, errorf(CodeUnsupported, "no agent that can load custom skills was found")
	}
	outcome, err := service.InstallBuiltAgent(env.deps.HomeDir, agent, adapters, engineID, env.deps.Now())
	if err != nil {
		return nil, errorf(CodeFailed, "install agent: %v", err)
	}
	result := builderInstallResult{Files: []string{}, Warnings: []string{}}
	for _, installed := range outcome.Results {
		result.Files = append(result.Files, installed.Path)
	}
	if outcome.Name != agent.Name {
		result.RenamedTo = outcome.Name
	}
	if outcome.RegistryErr != nil {
		result.Warnings = append(result.Warnings, outcome.RegistryErr.Error())
	}
	return result, nil
}
