package api

import (
	"context"
	"slices"
	"time"

	"github.com/gentleman-programming/gentle-ai/v3/internal/catalog"
	"github.com/gentleman-programming/gentle-ai/v3/internal/components/skills"
	"github.com/gentleman-programming/gentle-ai/v3/internal/model"
	"github.com/gentleman-programming/gentle-ai/v3/internal/planner"
	"github.com/gentleman-programming/gentle-ai/v3/internal/service"
	"github.com/gentleman-programming/gentle-ai/v3/internal/state"
	"github.com/gentleman-programming/gentle-ai/v3/internal/system"
)

type statusResult struct {
	Version          string            `json:"version"`
	System           systemInfo        `json:"system"`
	Agents           []agentStatus     `json:"agents"`
	Components       []componentStatus `json:"components"`
	Presets          []presetInfo      `json:"presets"`
	Personas         []personaInfo     `json:"personas"`
	Skills           []skillStatus     `json:"skills"`
	State            stateInfo         `json:"state"`
	OpenCodeDetected bool              `json:"openCodeDetected"`
	BuilderEngines   []string          `json:"builderEngines"`
}

type systemInfo struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Shell     string `json:"shell"`
	Supported bool   `json:"supported"`
}

type agentStatus struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Detected   bool   `json:"detected"`
	Installed  bool   `json:"installed"`
	Supported  bool   `json:"supported"`
	ConfigPath string `json:"configPath"`
}

type componentStatus struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Installed   bool     `json:"installed"`
	Requires    []string `json:"requires"`
}

type presetInfo struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Components  []string `json:"components"`
}

type personaInfo struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type skillStatus struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
}

type stateInfo struct {
	Preset      string `json:"preset,omitempty"`
	Persona     string `json:"persona,omitempty"`
	RDDMode     string `json:"rddMode,omitempty"`
	PendingSync bool   `json:"pendingSync"`
	// SyncNeeded says the agents' files are behind this gentle-ai: a sync is
	// pending, or they were written by another version.
	SyncNeeded   bool           `json:"syncNeeded"`
	LastSyncedAt *time.Time     `json:"lastSyncedAt,omitempty"`
	Background   backgroundInfo `json:"background"`
}

type backgroundInfo struct {
	OpenCode string `json:"opencode,omitempty"`
	Pi       string `json:"pi,omitempty"`
}

func status(ctx context.Context, env *env, _ noParams) (any, error) {
	detection, err := env.deps.detect(ctx)
	if err != nil {
		return nil, err
	}
	current, err := readState(env.deps.HomeDir)
	if err != nil {
		return nil, err
	}
	return statusResult{
		Version:          env.deps.Version,
		System:           systemInfo{OS: detection.System.OS, Arch: detection.System.Arch, Shell: detection.System.Shell, Supported: detection.System.Supported},
		Agents:           agentStatuses(detection, current),
		Components:       componentStatuses(current),
		Presets:          presetInfos(),
		Personas:         personaInfos(),
		Skills:           skillStatuses(current),
		State:            stateSummary(current, env.deps.Version),
		OpenCodeDetected: service.AgentDetected(detection, model.AgentOpenCode),
		BuilderEngines:   strs(service.AvailableBuilderEngines(env.deps.EngineAvailable)),
	}, nil
}

func agentStatuses(detection system.DetectionResult, current state.InstallState) []agentStatus {
	agents := catalog.AllAgents()
	out := make([]agentStatus, 0, len(agents))
	for _, agent := range agents {
		configPath := agent.ConfigPath
		for _, config := range detection.Configs {
			if config.Agent == string(agent.ID) && config.Path != "" {
				configPath = config.Path
			}
		}
		out = append(out, agentStatus{
			ID:         string(agent.ID),
			Name:       agent.Name,
			Detected:   service.AgentDetected(detection, agent.ID),
			Installed:  slices.Contains(current.InstalledAgents, string(agent.ID)),
			Supported:  agent.Tier == model.TierFull && detection.System.Supported,
			ConfigPath: configPath,
		})
	}
	return out
}

func componentStatuses(current state.InstallState) []componentStatus {
	graph := planner.MVPGraph()
	components := catalog.MVPComponents()
	out := make([]componentStatus, 0, len(components))
	for _, component := range components {
		out = append(out, componentStatus{
			ID:          string(component.ID),
			Name:        component.Name,
			Description: component.Description,
			Installed:   current.SelectionConfigured && slices.Contains(current.Components, component.ID),
			Requires:    strs(graph.DependenciesOf(component.ID)),
		})
	}
	return out
}

// presetInfos lists each preset's components for the default persona, the
// selection the installer starts from.
func presetInfos() []presetInfo {
	presets := catalog.Presets()
	out := make([]presetInfo, 0, len(presets))
	for _, preset := range presets {
		out = append(out, presetInfo{
			ID:          string(preset.ID),
			Label:       preset.Label,
			Description: preset.Description,
			Components:  strs(model.ComponentsForPreset(preset.ID, model.PersonaGentleman)),
		})
	}
	return out
}

func personaInfos() []personaInfo {
	personas := catalog.Personas()
	out := make([]personaInfo, 0, len(personas))
	for _, persona := range personas {
		out = append(out, personaInfo{ID: string(persona.ID), Label: string(persona.ID), Description: persona.Description})
	}
	return out
}

// skillStatuses marks the skills the persisted selection installs: explicit
// skills win, otherwise the preset's defaults apply, as the installer resolves them.
func skillStatuses(current state.InstallState) []skillStatus {
	installed := []model.SkillID{}
	if current.SelectionConfigured && slices.Contains(current.Components, model.ComponentSkills) {
		installed = current.Skills
		if len(installed) == 0 {
			installed = skills.SkillsForPreset(current.Preset)
		}
	}
	ids := skills.AllSkillIDs()
	out := make([]skillStatus, 0, len(ids))
	for _, id := range ids {
		out = append(out, skillStatus{ID: string(id), Name: string(id), Installed: slices.Contains(installed, id)})
	}
	return out
}

func stateSummary(current state.InstallState, version string) stateInfo {
	return stateInfo{
		Preset:       string(current.Preset),
		Persona:      current.Persona,
		RDDMode:      current.RDDMode,
		PendingSync:  current.PendingSync,
		SyncNeeded:   len(current.InstalledAgents) > 0 && (current.PendingSync || (current.InstalledBinaryVersion != "" && current.InstalledBinaryVersion != version)),
		LastSyncedAt: current.LastSyncedAt,
		Background:   backgroundInfo{OpenCode: string(current.BackgroundIntent), Pi: string(current.PiBackgroundIntent)},
	}
}
