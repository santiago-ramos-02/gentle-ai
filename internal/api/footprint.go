package api

import (
	"context"
	"slices"

	"github.com/gentleman-programming/gentle-ai/v3/internal/model"
)

type footprintParams struct {
	// Agents to report on; absent means every agent Gentle AI set up.
	Agents []string `json:"agents"`
}

// footprint reports what Gentle AI added to some agents, as removing it from
// them would undo it, so a host can run them without Gentle AI and without
// changing anything on disk. Agents Gentle AI did not set up are skipped:
// their configuration is the user's own. It is read-only: the uninstall runs
// on copies.
func footprint(_ context.Context, env *env, params footprintParams) (any, error) {
	requested, err := agentIDs(params.Agents, "agents")
	if err != nil {
		return nil, err
	}
	current, err := readState(env.deps.HomeDir)
	if err != nil {
		return nil, err
	}
	setUp := make([]model.AgentID, 0, len(current.InstalledAgents))
	for _, installed := range current.InstalledAgents {
		agent := model.AgentID(installed)
		if requested == nil || slices.Contains(requested, agent) {
			setUp = append(setUp, agent)
		}
	}
	result, err := env.deps.Footprint(env.deps.HomeDir, setUp)
	if err != nil {
		return nil, errorf(CodeFailed, "footprint: %v", err)
	}
	return result, nil
}
