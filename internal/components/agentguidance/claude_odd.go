package agentguidance

import (
	"fmt"
	"strings"

	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
)

// claudeODDClasses are the ODD worker classes Claude Code delegates through its Agent
// tool, in the order the table lists them, with "default" as every other delegation.
var claudeODDClasses = []struct{ role, label string }{
	{"odd-explorer", "`odd-explorer`"},
	{"odd-worker", "`odd-worker`"},
	{"odd-verify", "`odd-verify`"},
	{"default", "any other delegated task"},
}

// renderClaudeODDAssignments tells Claude Code's orchestrator which model to pass when it
// delegates each ODD worker class, since those workers are not agent files that carry
// their own model the way the native review agents do. Only classes a model was chosen
// for are listed; the rest keep running on the main conversation's model, so nothing is
// rendered when no class has one. Effort cannot be set per delegation in Claude Code.
func renderClaudeODDAssignments(phases map[string]model.ClaudePhaseAssignment, legacy map[string]model.ClaudeModelAlias) string {
	chosen := model.ClaudePhaseAssignmentsFromLegacy(legacy)
	if chosen == nil {
		chosen = map[string]model.ClaudePhaseAssignment{}
	}
	for role, assignment := range phases {
		if assignment.Valid() {
			chosen[role] = assignment
		}
	}
	var rows strings.Builder
	for _, class := range claudeODDClasses {
		if assignment, ok := chosen[class.role]; ok && assignment.Model.Valid() {
			fmt.Fprintf(&rows, "| %s | `%s` |\n", class.label, assignment.Model.ModelID())
		}
	}
	if rows.Len() == 0 {
		return ""
	}
	return "\n\n### Claude Code ODD worker models\n\nWhen you delegate one of these ODD worker classes with the Agent tool, pass the listed model as the Agent tool's `model` parameter. Classes not listed run on the main conversation's model. Native RDD review agents carry their own models and are not affected.\n\n| ODD worker class | model |\n|---|---|\n" + rows.String()
}
