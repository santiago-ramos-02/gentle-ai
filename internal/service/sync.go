package service

import (
	"errors"
	"fmt"
	"os"

	"github.com/gentleman-programming/gentle-ai/v4/internal/cli"
	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
	"github.com/gentleman-programming/gentle-ai/v4/internal/state"
	"github.com/gentleman-programming/gentle-ai/v4/internal/statecoord"
)

// SyncResult is what a managed-asset sync changed and what it left for the user.
type SyncResult struct {
	Files         []string
	ManualActions []string
}

// Sync performs a full managed-asset sync for homeDir, the way the TUI's sync
// and model configuration screens do. It discovers the installed agents from
// persisted state (or the filesystem fallback), restores persisted model
// assignments, applies overrides, and persists the assignments it used so the
// next sync preserves them. A nil overrides is a plain sync.
func Sync(homeDir string, overrides *model.SyncOverrides) (SyncResult, error) {
	agentIDs := SyncAgentIDs(homeDir, overrides)
	syncFlags := cli.SyncFlags{IncludePermissions: SyncIncludesPermissions(agentIDs)}
	selection := cli.BuildSyncSelection(syncFlags, agentIDs)

	// Load persisted model assignments so a plain sync (no overrides)
	// preserves the user's previous choices instead of falling back
	// to the "balanced" preset.
	LoadPersistedAssignments(homeDir, &selection)

	ApplyOverrides(&selection, overrides)

	result, err := cli.RunSyncWithSelection(homeDir, selection)
	if err != nil {
		return SyncResult{}, err
	}

	// Persist model assignments that were actually used (from overrides
	// or loaded from state) so the next sync preserves them too.
	if err := PersistAssignments(homeDir, selection); err != nil {
		return SyncResult{}, fmt.Errorf("persist model assignments: %w", err)
	}

	return SyncResult{Files: result.ChangedFiles, ManualActions: result.ManualActions}, nil
}

// SyncAgentIDs returns the agents a sync targets: the override's target agents
// when given (deduplicated, in order), otherwise the discovered agents.
func SyncAgentIDs(homeDir string, overrides *model.SyncOverrides) []model.AgentID {
	if overrides == nil || len(overrides.TargetAgents) == 0 {
		return cli.DiscoverAgents(homeDir)
	}

	seen := make(map[model.AgentID]bool, len(overrides.TargetAgents))
	ids := make([]model.AgentID, 0, len(overrides.TargetAgents))
	for _, id := range overrides.TargetAgents {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

// SyncIncludesPermissions reports whether a sync of agentIDs includes the
// permissions component, which Codex requires.
func SyncIncludesPermissions(agentIDs []model.AgentID) bool {
	for _, id := range agentIDs {
		if id == model.AgentCodex {
			return true
		}
	}
	return false
}

// ApplyOverrides merges non-nil fields from overrides into selection.
// A nil overrides pointer is a no-op.
func ApplyOverrides(selection *model.Selection, overrides *model.SyncOverrides) {
	if overrides == nil {
		return
	}
	ApplyModelOverrides(selection, overrides)
	if overrides.SDDMode != "" {
		selection.SDDMode = overrides.SDDMode
	}
	if overrides.StrictTDD != nil {
		selection.StrictTDD = *overrides.StrictTDD
	}
	// Persisted components may omit SDD. Explicit model assignment work must
	// still run even when an older installation did not select it.
	if model.CarriesSDDWork(overrides.ModelAssignments) {
		selection.EnsureComponent(model.ComponentSDD)
	}
}

// ApplyModelOverrides merges only the non-nil model assignment fields of
// overrides into selection. An install uses it to take picker choices
// without the sync-only SDD rules.
func ApplyModelOverrides(selection *model.Selection, overrides *model.SyncOverrides) {
	if overrides == nil {
		return
	}
	if overrides.ModelAssignments != nil {
		selection.ModelAssignments = overrides.ModelAssignments
	}
	if overrides.ClaudeModelAssignments != nil {
		selection.ClaudeModelAssignments = overrides.ClaudeModelAssignments
	}
	if overrides.ClaudePhaseAssignments != nil {
		selection.ClaudePhaseAssignments = overrides.ClaudePhaseAssignments
		selection.ClaudeModelAssignments = nil
	}
	if overrides.KiroModelAssignments != nil {
		selection.KiroModelAssignments = overrides.KiroModelAssignments
	}
	if overrides.ClearCodexOrchestratorAssignment {
		selection.CodexOrchestratorAssignment = nil
		selection.ClearCodexOrchestratorAssignment = true
	} else if overrides.CodexOrchestratorAssignment != nil {
		selection.CodexOrchestratorAssignment = overrides.CodexOrchestratorAssignment
		selection.ClearCodexOrchestratorAssignment = false
	}
	if overrides.CodexModelAssignments != nil {
		selection.CodexModelAssignments = overrides.CodexModelAssignments
	}
	if overrides.CodexCarrilModelAssignments != nil {
		selection.CodexCarrilModelAssignments = overrides.CodexCarrilModelAssignments
	}
	if overrides.CodexPhaseModelAssignments != nil {
		selection.CodexPhaseModelAssignments = overrides.CodexPhaseModelAssignments
	}
}

// LoadPersistedAssignments reads previously-saved model assignments from
// state.json and populates the selection when the corresponding maps are empty.
// This ensures a plain `sync` (no TUI overrides, no CLI flags) preserves the
// user's last-known model choices.
func LoadPersistedAssignments(homeDir string, selection *model.Selection) {
	s, err := state.Read(homeDir)
	if err != nil {
		return
	}
	cli.RestorePersistedSelection(selection, s, cli.SyncFlags{})
	RestoreModelAssignments(selection, s)
}

// RestoreModelAssignments fills every model assignment selection leaves empty
// from persisted state, so a run that does not choose models keeps the last
// choices for every agent.
func RestoreModelAssignments(selection *model.Selection, s state.InstallState) {
	if len(selection.ClaudePhaseAssignments) == 0 && len(s.ClaudePhaseAssignments) > 0 {
		m := make(map[string]model.ClaudePhaseAssignment, len(s.ClaudePhaseAssignments))
		for k, v := range s.ClaudePhaseAssignments {
			if k == "orchestrator" {
				continue
			}
			a := model.ClaudePhaseAssignment{Model: model.ClaudeModelAlias(v.Model), Effort: model.ClaudeEffort(v.Effort)}
			if a.Valid() {
				m[k] = a
			}
		}
		selection.ClaudePhaseAssignments = m
	}
	if len(selection.ClaudeModelAssignments) == 0 && len(selection.ClaudePhaseAssignments) == 0 && len(s.ClaudeModelAssignments) > 0 {
		m := make(map[string]model.ClaudeModelAlias, len(s.ClaudeModelAssignments))
		for k, v := range s.ClaudeModelAssignments {
			// Claude Code controls the main session/orchestrator model itself.
			// Keep persisted assignments scoped to Agent tool calls only.
			if k == "orchestrator" {
				continue
			}
			m[k] = model.ClaudeModelAlias(v)
		}
		selection.ClaudeModelAssignments = m
	}
	if len(selection.KiroModelAssignments) == 0 && len(s.KiroModelAssignments) > 0 {
		m := make(map[string]model.KiroModelAlias, len(s.KiroModelAssignments))
		for k, v := range s.KiroModelAssignments {
			m[k] = model.KiroModelAlias(v)
		}
		selection.KiroModelAssignments = m
	}
	if len(selection.CodexModelAssignments) == 0 && len(s.CodexModelAssignments) > 0 {
		m := make(map[string]model.CodexEffort, len(s.CodexModelAssignments))
		for k, v := range s.CodexModelAssignments {
			m[k] = model.CodexEffort(v)
		}
		selection.CodexModelAssignments = m
	}
	if len(selection.CodexCarrilModelAssignments) == 0 && len(s.CodexCarrilModelAssignments) > 0 {
		selection.CodexCarrilModelAssignments = model.MigrateLegacyCodexCarrilDefaults(s.CodexCarrilModelAssignments)
	}
	if len(selection.CodexPhaseModelAssignments) == 0 && len(s.CodexPhaseModelAssignments) > 0 {
		m := make(map[string]string, len(s.CodexPhaseModelAssignments))
		for k, v := range s.CodexPhaseModelAssignments {
			m[k] = v
		}
		selection.CodexPhaseModelAssignments = m
	}
	if !selection.ClearCodexOrchestratorAssignment && selection.CodexOrchestratorAssignment == nil && s.CodexOrchestratorAssignment != nil {
		selection.CodexOrchestratorAssignment = codexOrchestratorFromState(s.CodexOrchestratorAssignment)
	}
	if len(selection.ModelAssignments) == 0 && len(s.ModelAssignments) > 0 {
		m := make(map[string]model.ModelAssignment, len(s.ModelAssignments))
		for k, v := range s.ModelAssignments {
			m[k] = model.ModelAssignment{ProviderID: v.ProviderID, ModelID: v.ModelID, Effort: v.Effort}
		}
		selection.ModelAssignments = m
	}
}

// PersistAssignments writes the model assignments from selection back to
// state.json using a read-merge-write pattern so that other fields
// (InstalledAgents) are not lost.
//
// For CodexPhaseModelAssignments the function distinguishes three states:
//   - nil: not provided (partial sync) — leave the existing state value untouched.
//   - non-nil, len > 0: new per-phase assignments — write them.
//   - non-nil, len == 0: explicit clear signal (preset selected) — delete the key.
func PersistAssignments(homeDir string, selection model.Selection) error {
	hasAssignmentSignal := selection.ClaudeModelAssignments != nil ||
		selection.ClaudePhaseAssignments != nil ||
		selection.KiroModelAssignments != nil ||
		selection.ModelAssignments != nil ||
		selection.CodexModelAssignments != nil ||
		selection.CodexOrchestratorAssignment != nil ||
		selection.ClearCodexOrchestratorAssignment ||
		selection.CodexCarrilModelAssignments != nil ||
		selection.CodexPhaseModelAssignments != nil
	if len(selection.ClaudeModelAssignments) == 0 && len(selection.ClaudePhaseAssignments) == 0 && len(selection.KiroModelAssignments) == 0 && len(selection.ModelAssignments) == 0 && len(selection.CodexModelAssignments) == 0 && len(selection.CodexCarrilModelAssignments) == 0 && len(selection.CodexPhaseModelAssignments) == 0 && !hasAssignmentSignal {
		return nil
	}
	// The whole read-modify-write runs under the canonical install-state lock:
	// the written value derives from the read below, so the read must happen
	// inside the lock or a concurrent writer's changes would be lost.
	return statecoord.WithLock(homeDir, func() error {
		current, err := state.Read(homeDir)
		if err != nil {
			// State file may not exist yet (e.g. pre-state users). Other read
			// failures, such as invalid JSON, must not overwrite existing state.
			if !errors.Is(err, os.ErrNotExist) {
				return nil
			}
			current = state.InstallState{}
		}
		if selection.ClaudeModelAssignments != nil {
			if len(selection.ClaudeModelAssignments) > 0 {
				current.ClaudeModelAssignments = claudeAliasesToStrings(selection.ClaudeModelAssignments)
			} else {
				current.ClaudeModelAssignments = nil
			}
		}
		if selection.ClaudePhaseAssignments != nil {
			if len(selection.ClaudePhaseAssignments) > 0 {
				current.ClaudePhaseAssignments = claudePhaseAssignmentsToState(selection.ClaudePhaseAssignments)
			} else {
				current.ClaudePhaseAssignments = nil
			}
			current.ClaudeModelAssignments = nil
		}
		if selection.KiroModelAssignments != nil {
			if len(selection.KiroModelAssignments) > 0 {
				current.KiroModelAssignments = kiroAliasesToStrings(selection.KiroModelAssignments)
			} else {
				current.KiroModelAssignments = nil
			}
		}
		if selection.ClearCodexOrchestratorAssignment {
			current.CodexOrchestratorAssignment = nil
		} else if selection.CodexOrchestratorAssignment != nil {
			current.CodexOrchestratorAssignment = codexOrchestratorToState(selection.CodexOrchestratorAssignment)
		}
		if selection.CodexModelAssignments != nil {
			if len(selection.CodexModelAssignments) > 0 {
				current.CodexModelAssignments = codexEffortsToStrings(selection.CodexModelAssignments)
			} else {
				current.CodexModelAssignments = nil
			}
		}
		if selection.CodexCarrilModelAssignments != nil {
			if len(selection.CodexCarrilModelAssignments) > 0 {
				current.CodexCarrilModelAssignments = selection.CodexCarrilModelAssignments
			} else {
				current.CodexCarrilModelAssignments = nil
			}
		}
		// non-nil, len > 0 → write; non-nil, len == 0 → clear (explicit preset signal); nil → leave untouched.
		if selection.CodexPhaseModelAssignments != nil {
			if len(selection.CodexPhaseModelAssignments) > 0 {
				current.CodexPhaseModelAssignments = selection.CodexPhaseModelAssignments
			} else {
				current.CodexPhaseModelAssignments = nil
			}
		}
		if selection.ModelAssignments != nil {
			if len(selection.ModelAssignments) > 0 {
				current.ModelAssignments = modelAssignmentsToState(selection.ModelAssignments)
			} else {
				current.ModelAssignments = nil
			}
		}
		return state.Write(homeDir, current)
	})
}

// claudeAliasesToStrings converts a typed ClaudeModelAlias map to plain strings
// for JSON serialisation in state.json.
func claudeAliasesToStrings(m map[string]model.ClaudeModelAlias) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		// Claude Code owns the main session/orchestrator model; do not persist it
		// as a Gentle AI model assignment.
		if k == "orchestrator" {
			continue
		}
		out[k] = string(v)
	}
	return out
}

func claudeLegacyAssignmentsForState(
	legacy map[string]model.ClaudeModelAlias,
	phase map[string]state.ClaudePhaseAssignmentState,
) map[string]string {
	if len(phase) > 0 {
		return nil
	}
	return claudeAliasesToStrings(legacy)
}

func claudePhaseAssignmentsToState(m map[string]model.ClaudePhaseAssignment) map[string]state.ClaudePhaseAssignmentState {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]state.ClaudePhaseAssignmentState, len(m))
	for k, v := range m {
		if k == "orchestrator" || !v.Valid() {
			continue
		}
		out[k] = state.ClaudePhaseAssignmentState{Model: string(v.Model), Effort: string(v.Effort)}
	}
	return out
}

func kiroAliasesToStrings(m map[string]model.KiroModelAlias) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = string(v)
	}
	return out
}

// codexEffortsToStrings converts a typed CodexEffort map to plain strings
// for JSON serialisation in state.json.
func codexEffortsToStrings(m map[string]model.CodexEffort) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = string(v)
	}
	return out
}

// modelAssignmentsToState converts model.ModelAssignment maps to the
// state-serialisable form.
func modelAssignmentsToState(m map[string]model.ModelAssignment) map[string]state.ModelAssignmentState {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]state.ModelAssignmentState, len(m))
	for k, v := range m {
		out[k] = state.ModelAssignmentState{ProviderID: v.ProviderID, ModelID: v.ModelID, Effort: v.Effort}
	}
	return out
}

func codexOrchestratorToState(a *model.CodexOrchestratorAssignment) *state.CodexOrchestratorAssignmentState {
	if a == nil {
		return nil
	}
	return &state.CodexOrchestratorAssignmentState{Model: a.Model, Effort: string(a.Effort)}
}

func codexOrchestratorFromState(a *state.CodexOrchestratorAssignmentState) *model.CodexOrchestratorAssignment {
	if a == nil {
		return nil
	}
	return &model.CodexOrchestratorAssignment{Model: a.Model, Effort: model.CodexEffort(a.Effort)}
}
