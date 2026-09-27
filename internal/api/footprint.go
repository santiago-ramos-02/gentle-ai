package api

import "context"

type footprintParams struct {
	Agent string `json:"agent"`
}

// footprint reports what Gentle AI added to one agent, as removing it would
// undo it, so a host can run that agent without Gentle AI and without
// changing anything on disk. It is read-only: the uninstall runs on copies.
func footprint(_ context.Context, env *env, params footprintParams) (any, error) {
	agents, err := agentIDs([]string{params.Agent}, "agent")
	if err != nil {
		return nil, err
	}
	result, err := env.deps.Footprint(env.deps.HomeDir, agents[0])
	if err != nil {
		return nil, errorf(CodeFailed, "footprint: %v", err)
	}
	return result, nil
}
