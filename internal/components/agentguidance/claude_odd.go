package agentguidance

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
)

// claudeODDAgentMarker identifies an ODD worker agent file Gentle AI wrote, so a user's
// own agent under the same name is never overwritten or removed.
const claudeODDAgentMarker = "<!-- gentle-ai:odd-worker-agent -->"

// claudeODDClasses are the ODD worker classes that get a named Claude Code agent when a
// model is chosen for them. Claude Code's Agent tool only takes a tier alias per call, so
// an agent file is the one place a class's model, any model a proxy serves included, and
// its effort can be set.
var claudeODDClasses = []struct{ role, description, body string }{
	{
		"odd-explorer",
		"Gentle AI ODD explorer: maps code the orchestrator needs to understand and returns a short handoff. Read-only.",
		"Explore only what the handoff asks about. Read and search; never edit files. Return a short, factual handoff: the files and symbols that matter, how they connect, and anything uncertain.",
	},
	{
		"odd-worker",
		"Gentle AI ODD worker: makes one bounded, already-understood change the orchestrator hands over.",
		"Make exactly the change the handoff describes, following the project's conventions. Do not widen the scope. Report what changed, what you verified, and anything that blocked you.",
	},
	{
		"odd-verify",
		"Gentle AI ODD verifier: runs the checks the orchestrator names and reports the evidence.",
		"Run the checks the handoff names and report their real results: commands, pass or fail, and the relevant output. Do not fix anything; report what failed and why.",
	},
}

// claudeODDChoices resolves the chosen assignment per ODD class from phase and legacy
// assignments; classes without a choice are absent.
func claudeODDChoices(phases map[string]model.ClaudePhaseAssignment, legacy map[string]model.ClaudeModelAlias) map[string]model.ClaudePhaseAssignment {
	chosen := model.ClaudePhaseAssignmentsFromLegacy(legacy)
	if chosen == nil {
		chosen = map[string]model.ClaudePhaseAssignment{}
	}
	for role, assignment := range phases {
		if assignment.Valid() {
			chosen[role] = assignment
		}
	}
	return chosen
}

// renderClaudeODDAssignments tells Claude Code's orchestrator to delegate each ODD worker
// class that has a chosen model to its named agent, and which tier to pass for any other
// delegation. Nothing is rendered when no class has a choice, so Claude Code keeps running
// workers on the main conversation's model.
func renderClaudeODDAssignments(phases map[string]model.ClaudePhaseAssignment, legacy map[string]model.ClaudeModelAlias) string {
	chosen := claudeODDChoices(phases, legacy)
	var rows strings.Builder
	for _, class := range claudeODDClasses {
		if assignment, ok := chosen[class.role]; ok {
			fmt.Fprintf(&rows, "| `%s` | `%s` | `%s` |\n", class.role, class.role, assignment.Model.ModelID())
		}
	}
	var general string
	// Only a tier can be passed per call; a proxied model has no general agent to carry it.
	if assignment, ok := chosen["default"]; ok && !strings.HasPrefix(string(assignment.Model), model.ClaudeCustomModelPrefix) {
		general = fmt.Sprintf("\n\nFor any other delegated task, pass `model: %s` to the Agent tool.", assignment.Model.ModelID())
	}
	if rows.Len() == 0 && general == "" {
		return ""
	}
	section := "\n\n### Claude Code ODD worker agents"
	if rows.Len() > 0 {
		section += "\n\nDelegate these ODD worker classes to their named agent, as the Agent tool's `subagent_type`. Each agent carries the model chosen for it; classes not listed use the general-purpose agent on the main conversation's model. Native RDD review agents carry their own models and are not affected.\n\n| ODD worker class | agent | model |\n|---|---|---|\n" + rows.String()
	}
	return section + general
}

// ClaudeODDAgentPaths are the agent files Gentle AI may own in a Claude Code agents directory.
func ClaudeODDAgentPaths(agentsDir string) []string {
	paths := make([]string, 0, len(claudeODDClasses))
	for _, class := range claudeODDClasses {
		paths = append(paths, filepath.Join(agentsDir, class.role+".md"))
	}
	return paths
}

// ClaudeODDAgentOwned reports whether path holds an ODD worker agent Gentle AI wrote.
func ClaudeODDAgentOwned(path string) (bool, error) {
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return bytes.Contains(content, []byte(claudeODDAgentMarker)), nil
}

// SyncClaudeODDAgents writes the named agent of every ODD worker class with a chosen model
// into agentsDir and removes the ones Gentle AI wrote for classes that no longer have one.
// A user's own agent under the same name is left alone. It returns the files it changed.
func SyncClaudeODDAgents(agentsDir string, phases map[string]model.ClaudePhaseAssignment, legacy map[string]model.ClaudeModelAlias) ([]string, error) {
	chosen := claudeODDChoices(phases, legacy)
	changed := []string{}
	for _, class := range claudeODDClasses {
		path := filepath.Join(agentsDir, class.role+".md")
		owned, err := ClaudeODDAgentOwned(path)
		if err != nil {
			return changed, err
		}
		_, statErr := os.Stat(path)
		if statErr == nil && !owned {
			continue
		}
		assignment, ok := chosen[class.role]
		if !ok {
			if owned {
				if err := os.Remove(path); err != nil {
					return changed, err
				}
				changed = append(changed, path)
			}
			continue
		}
		effort := ""
		if assignment.Effort != model.ClaudeEffortDefault {
			effort = "effort: " + string(assignment.Effort) + "\n"
		}
		content := fmt.Sprintf("---\nname: %s\ndescription: %s\nmodel: %s\n%s---\n%s\n\n%s\n", class.role, class.description, assignment.Model.ModelID(), effort, claudeODDAgentMarker, class.body)
		if existing, err := os.ReadFile(path); err == nil && string(existing) == content {
			continue
		}
		if err := os.MkdirAll(agentsDir, 0o755); err != nil {
			return changed, err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return changed, err
		}
		changed = append(changed, path)
	}
	return changed, nil
}
