package api

import (
	"context"
	"slices"

	"github.com/gentleman-programming/gentle-ai/v4/internal/claudeprofile"
	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
)

type claudeProfilesResult struct {
	Active   *string                 `json:"active"`
	Profiles []claudeprofile.Profile `json:"profiles"`
}

func claudeProfilesView(store claudeprofile.Store) claudeProfilesResult {
	result := claudeProfilesResult{Profiles: store.Profiles}
	if _, ok := store.ActiveProfile(); ok {
		result.Active = &store.Active
	}
	return result
}

// getClaudeProfiles lists the Claude Code profiles and which one is applied.
func getClaudeProfiles(_ context.Context, env *env, _ noParams) (any, error) {
	store, err := claudeprofile.Load(env.deps.HomeDir)
	if err != nil {
		return nil, err
	}
	return claudeProfilesView(store), nil
}

type claudeProfileSaveParams struct {
	Profile claudeprofile.Profile `json:"profile"`
	// Replaces is the profile's previous name when it is renamed.
	Replaces string `json:"replaces"`
	// Host is passed on when saving the applied profile applies it again.
	Host string `json:"host"`
}

// saveClaudeProfile adds or replaces a profile. Saving the applied profile applies it again.
func saveClaudeProfile(ctx context.Context, env *env, params claudeProfileSaveParams) (any, error) {
	if err := params.Profile.Validate(); err != nil {
		return nil, invalidParams("%s", err.Error())
	}
	store, err := claudeprofile.Load(env.deps.HomeDir)
	if err != nil {
		return nil, err
	}
	replaces := params.Replaces
	if replaces == "" {
		replaces = params.Profile.Name
	}
	if params.Profile.Name != replaces {
		if _, taken := store.Find(params.Profile.Name); taken {
			return nil, invalidParams("a profile named %q already exists", params.Profile.Name)
		}
	}
	index := slices.IndexFunc(store.Profiles, func(p claudeprofile.Profile) bool { return p.Name == replaces })
	if index < 0 {
		store.Profiles = append(store.Profiles, params.Profile)
	} else {
		store.Profiles[index] = params.Profile
	}
	wasActive := store.Active == replaces && replaces != ""
	if wasActive {
		store.Active = params.Profile.Name
	}
	if err := claudeprofile.Save(env.deps.HomeDir, store); err != nil {
		return nil, err
	}
	if wasActive {
		return applyClaudeProfile(ctx, env, claudeProfileApplyParams{Name: &params.Profile.Name, Host: params.Host})
	}
	return claudeProfilesView(store), nil
}

type claudeProfileDeleteParams struct {
	Name string `json:"name"`
}

// deleteClaudeProfile removes a profile that is not applied.
func deleteClaudeProfile(_ context.Context, env *env, params claudeProfileDeleteParams) (any, error) {
	store, err := claudeprofile.Load(env.deps.HomeDir)
	if err != nil {
		return nil, err
	}
	if store.Active == params.Name {
		return nil, invalidParams("profile %q is applied; apply another one, or none, first", params.Name)
	}
	store.Profiles = slices.DeleteFunc(store.Profiles, func(p claudeprofile.Profile) bool { return p.Name == params.Name })
	if err := claudeprofile.Save(env.deps.HomeDir, store); err != nil {
		return nil, err
	}
	return claudeProfilesView(store), nil
}

type claudeProfileApplyParams struct {
	// Name is the profile to apply; null applies none, restoring Claude Code's own slots.
	Name *string `json:"name"`
	// Host names the app that applies the profile to the Claude Code it launches through
	// a proxy (see claudeprofile.Store.Host); empty applies it to Claude Code's settings.
	Host string `json:"host"`
}

// applyClaudeProfile points Claude Code's model slots at the profile's models, sets its
// pinned phases (clearing them when it pins none, so the orchestrator decides), and syncs
// Claude Code so the orchestrator learns what each slot is for. With no profile, the slots
// go back to what they were and the phase models stay as they are.
func applyClaudeProfile(ctx context.Context, env *env, params claudeProfileApplyParams) (any, error) {
	store, err := claudeprofile.Load(env.deps.HomeDir)
	if err != nil {
		return nil, err
	}
	var profile *claudeprofile.Profile
	if params.Name != nil {
		found, ok := store.Find(*params.Name)
		if !ok {
			return nil, invalidParams("no profile named %q", *params.Name)
		}
		profile = &found
		store.Active = found.Name
	} else {
		store.Active = ""
	}
	store.Host = params.Host
	switch {
	case store.Host == "":
		if err := claudeprofile.ApplyEnv(env.deps.HomeDir, &store, profile); err != nil {
			return nil, err
		}
	case store.OriginalEnv != nil:
		// A profile applied to Claude Code's settings before its host took over: put them back.
		if err := claudeprofile.ApplyEnv(env.deps.HomeDir, &store, nil); err != nil {
			return nil, err
		}
	}
	hostProfile := profile
	if store.Host == "" {
		hostProfile = nil
	}
	if err := claudeprofile.WriteHostSetup(env.deps.HomeDir, hostProfile); err != nil {
		return nil, err
	}
	if err := claudeprofile.Save(env.deps.HomeDir, store); err != nil {
		return nil, err
	}
	installed, err := readState(env.deps.HomeDir)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(installed.InstalledAgents, string(model.AgentClaudeCode)) {
		// Without Gentle AI in Claude Code there is no guidance or phase to update.
		return claudeProfilesView(store), nil
	}
	phases := modelsParams{}
	if profile != nil {
		phases.ClaudePhaseAssignments = map[string]claudeAssignment{}
		phases.ClaudeModelAssignments = map[string]string{}
		for phase, pinned := range profile.Phases {
			phases.ClaudePhaseAssignments[phase] = claudeAssignment{Model: pinned.Model, Effort: pinned.Effort}
		}
	}
	overrides, err := phases.toOverrides()
	if err != nil {
		return nil, err
	}
	overrides.TargetAgents = []model.AgentID{model.AgentClaudeCode}
	if _, err := syncWith(env, overrides); err != nil {
		return nil, err
	}
	return claudeProfilesView(store), nil
}
