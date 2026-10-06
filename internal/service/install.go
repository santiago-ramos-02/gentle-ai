package service

import (
	"errors"
	"fmt"
	"os"

	"github.com/gentleman-programming/gentle-ai/v4/internal/cli"
	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
	"github.com/gentleman-programming/gentle-ai/v4/internal/pipeline"
	"github.com/gentleman-programming/gentle-ai/v4/internal/planner"
	"github.com/gentleman-programming/gentle-ai/v4/internal/state"
	"github.com/gentleman-programming/gentle-ai/v4/internal/statecoord"
	"github.com/gentleman-programming/gentle-ai/v4/internal/system"
)

// InstallRequest is one installer run. The background fields carry the
// effective choice for this run and the choice to publish after success
// (empty Persist leaves the persisted preference untouched).
type InstallRequest struct {
	Selection                 model.Selection
	Resolved                  planner.ResolvedPlan
	Detection                 system.DetectionResult
	OpenCodeBackground        model.OpenCodeBackgroundIntent
	OpenCodeBackgroundPersist model.OpenCodeBackgroundIntent
	PiBackground              model.PiBackgroundIntent
	PiBackgroundPersist       model.PiBackgroundIntent
	// KeepCommunityTools leaves the persisted community tools as they are,
	// for a run that did not ask about them.
	KeepCommunityTools bool
}

// writeInstallState publishes the installed selection; tests replace it to
// hold the publication open.
var writeInstallState = state.WriteReconciled

// Install runs the installer pipeline into homeDir, reporting every step to
// onProgress, and on success persists the selection, model assignments, and
// background choices under the install-state lock. A persistence failure rolls
// the installed assets back so disk and state never disagree.
func Install(homeDir string, request InstallRequest, onProgress pipeline.ProgressFunc) pipeline.ExecutionResult {
	restoreCommandOutput := cli.SetCommandOutputStreaming(false)
	defer restoreCommandOutput()

	selection := request.Selection
	resolved := request.Resolved
	profile := cli.ResolveInstallProfile(request.Detection)
	resolved.PlatformDecision = planner.PlatformDecisionFromProfile(profile)

	execResult, orchestrator, codexServiceTier := cli.ExecuteTUIInstallRecordingCodexServiceTier(homeDir, selection, resolved, profile, request.OpenCodeBackground, request.PiBackground, onProgress)
	// The caller settles asynchronously: keep the deduplicated rollback
	// snapshot until state persistence succeeds or the failure has been
	// compensated.
	defer orchestrator.Finish()
	if execResult.Err == nil {
		// Persist the user's agent selection and model assignments so that future
		// `sync` runs target only the installed agents and preserve model choices.
		persistErr := statecoord.WithLock(homeDir, func() error {
			agentIDs := make([]string, 0, len(selection.Agents))
			for _, a := range selection.Agents {
				agentIDs = append(agentIDs, string(a))
			}
			claudePhaseState := claudePhaseAssignmentsToState(selection.ClaudePhaseAssignments)
			installState, readErr := state.Read(homeDir)
			if errors.Is(readErr, os.ErrNotExist) {
				installState = state.InstallState{}
			} else if readErr != nil {
				return fmt.Errorf("read persisted install state: %w", readErr)
			}
			installState.InstalledAgents = agentIDs
			if !request.KeepCommunityTools {
				installState.CommunityTools = communityToolIDsToStrings(selection.CommunityTools)
				installState.CommunityToolsConfigured = true
			}
			installState.ClaudeModelAssignments = claudeLegacyAssignmentsForState(selection.ClaudeModelAssignments, claudePhaseState)
			installState.ClaudePhaseAssignments = claudePhaseState
			installState.KiroModelAssignments = kiroAliasesToStrings(selection.KiroModelAssignments)
			installState.CodexModelAssignments = codexEffortsToStrings(selection.CodexModelAssignments)
			installState.CodexOrchestratorAssignment = codexOrchestratorToState(selection.CodexOrchestratorAssignment)
			if codexServiceTier != nil { // only what engram actually left in config.toml
				installState.CodexServiceTier = *codexServiceTier
			}
			installState.CodexCarrilModelAssignments = selection.CodexCarrilModelAssignments
			installState.CodexPhaseModelAssignments = selection.CodexPhaseModelAssignments
			installState.ModelAssignments = modelAssignmentsToState(selection.ModelAssignments)
			installState.Persona = string(selection.Persona)
			installState.SetSelection(selection)
			if request.OpenCodeBackgroundPersist != "" {
				installState.BackgroundIntent = request.OpenCodeBackgroundPersist
			}
			if request.PiBackgroundPersist != "" {
				installState.PiBackgroundIntent = request.PiBackgroundPersist
			}
			if writeErr := writeInstallState(homeDir, installState); writeErr != nil {
				return fmt.Errorf("persist install state: %w", writeErr)
			}
			return nil
		})
		if persistErr != nil {
			execResult.Err = persistErr
			if orchestrator != nil {
				rollback := orchestrator.Rollback(execResult)
				if rollback.Err != nil {
					execResult.Err = errors.Join(execResult.Err, rollback.Err)
				}
			}
		}
	}

	return execResult
}

func communityToolIDsToStrings(tools []model.CommunityToolID) []string {
	if tools == nil {
		return nil
	}
	result := make([]string, 0, len(tools))
	for _, tool := range tools {
		result = append(result, string(tool))
	}
	return result
}
