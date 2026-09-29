package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gentleman-programming/gentle-ai/v4/internal/agentbuilder"
	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
	"github.com/gentleman-programming/gentle-ai/v4/internal/system"
)

// BuilderSkillsDir returns the skills directory a generated agent is
// installed into for agentID, and whether the agent builder supports it.
func BuilderSkillsDir(homeDir string, agentID model.AgentID) (string, bool) {
	switch agentID {
	case model.AgentClaudeCode:
		return filepath.Join(homeDir, ".claude", "skills"), true
	case model.AgentOpenCode:
		return filepath.Join(homeDir, ".config", "opencode", "skills"), true
	case model.AgentGeminiCLI:
		return filepath.Join(homeDir, ".gemini", "skills"), true
	case model.AgentCodex:
		return filepath.Join(homeDir, ".codex", "skills"), true
	default:
		return "", false
	}
}

// BuilderAdapters lists where a generated agent is installed: every detected
// agent the builder supports, or, when none is detected, every available
// generation engine.
func BuilderAdapters(homeDir string, detection system.DetectionResult, engines []model.AgentID) []agentbuilder.AdapterInfo {
	var adapters []agentbuilder.AdapterInfo
	for _, cfg := range detection.Configs {
		if !cfg.Exists {
			continue
		}
		agentID := model.AgentID(strings.TrimSpace(cfg.Agent))
		if skillsDir, ok := BuilderSkillsDir(homeDir, agentID); ok {
			adapters = append(adapters, agentbuilder.AdapterInfo{AgentID: agentID, SkillsDir: skillsDir})
		}
	}
	if len(adapters) == 0 {
		for _, id := range engines {
			if skillsDir, ok := BuilderSkillsDir(homeDir, id); ok {
				adapters = append(adapters, agentbuilder.AdapterInfo{AgentID: id, SkillsDir: skillsDir})
			}
		}
	}
	return adapters
}

// BuilderTargets lists the SKILL.md paths installing agent writes, or the
// skills directories while no agent has been generated yet.
func BuilderTargets(adapters []agentbuilder.AdapterInfo, agent *agentbuilder.GeneratedAgent) []string {
	targets := make([]string, 0, len(adapters))
	for _, adapter := range adapters {
		if agent != nil {
			targets = append(targets, filepath.Join(adapter.SkillsDir, agent.Name, "SKILL.md"))
		} else {
			targets = append(targets, adapter.SkillsDir)
		}
	}
	return targets
}

// GenerateAgent asks engine to write an agent for request, telling it which
// agents the result will be installed into, and parses its answer.
func GenerateAgent(ctx context.Context, engine agentbuilder.GenerationEngine, request string, adapters []agentbuilder.AdapterInfo) (*agentbuilder.GeneratedAgent, error) {
	if engine == nil {
		return nil, errors.New("no generation engine")
	}
	installed := make([]model.AgentID, 0, len(adapters))
	for _, adapter := range adapters {
		installed = append(installed, adapter.AgentID)
	}
	raw, err := engine.Generate(ctx, agentbuilder.ComposePrompt(request, installed))
	if err != nil {
		return nil, err
	}
	return agentbuilder.Parse(raw)
}

// BuiltAgentInstall is the outcome of installing a generated agent.
type BuiltAgentInstall struct {
	Results []agentbuilder.InstallResult
	// Name is the installed name: a name that collides with a built-in skill
	// is installed with a -custom suffix.
	Name string
	// RegistryErr reports a failed custom-agent registry update. The skill
	// files are installed either way.
	RegistryErr error
}

// InstallBuiltAgent writes agent's SKILL.md into every adapter (all or
// nothing) and records it in the custom-agent registry.
func InstallBuiltAgent(homeDir string, agent *agentbuilder.GeneratedAgent, adapters []agentbuilder.AdapterInfo, engineID model.AgentID, now time.Time) (BuiltAgentInstall, error) {
	installAgent := agent
	if agentbuilder.HasConflictWithBuiltin(agent.Name) {
		// Shallow copy so the caller's generated agent keeps its name.
		renamed := *agent
		renamed.Name = agent.Name + "-custom"
		installAgent = &renamed
	}
	outcome := BuiltAgentInstall{Name: installAgent.Name}
	results, err := agentbuilder.Install(installAgent, adapters, "")
	outcome.Results = results
	if err != nil {
		return outcome, err
	}
	outcome.RegistryErr = recordBuiltAgent(homeDir, installAgent, results, engineID, now)
	return outcome, nil
}

func recordBuiltAgent(homeDir string, agent *agentbuilder.GeneratedAgent, results []agentbuilder.InstallResult, engineID model.AgentID, now time.Time) error {
	registryPath := filepath.Join(homeDir, ".config", "gentle-ai", "custom-agents.json")
	if err := os.MkdirAll(filepath.Dir(registryPath), 0o755); err != nil {
		return fmt.Errorf("create custom agent registry directory: %w", err)
	}
	registry, err := agentbuilder.LoadRegistry(registryPath)
	if err != nil {
		return fmt.Errorf("load custom agent registry: %w", err)
	}
	var installedIDs []model.AgentID
	for _, result := range results {
		if result.Success {
			installedIDs = append(installedIDs, result.AgentID)
		}
	}
	entry := agentbuilder.RegistryEntry{
		Name:             agent.Name,
		Title:            agent.Title,
		Description:      agent.Description,
		CreatedAt:        now,
		GenerationEngine: engineID,
		InstalledAgents:  installedIDs,
	}
	// Update an existing entry in place; otherwise append.
	if existing := registry.FindByName(agent.Name); existing != nil {
		existing.Title = entry.Title
		existing.Description = entry.Description
		existing.CreatedAt = entry.CreatedAt
		existing.GenerationEngine = entry.GenerationEngine
		existing.InstalledAgents = entry.InstalledAgents
	} else {
		registry.Add(entry)
	}
	if err := agentbuilder.SaveRegistry(registryPath, registry); err != nil {
		return fmt.Errorf("save custom agent registry: %w", err)
	}
	return nil
}
