package agentguidance

import (
	"os"
	"strings"
	"testing"

	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
)

// Claude Code's orchestrator learns which model to pass for each ODD worker class it was
// given one for, including a model a proxy routes; without choices nothing is added.
func TestClaudeODDWorkerModelsReachTheOrchestrator(t *testing.T) {
	home := t.TempDir()
	paths, err := RoutingPaths(home, model.AgentClaudeCode)
	if err != nil {
		t.Fatal(err)
	}
	read := func() string {
		body, err := os.ReadFile(paths[0])
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}

	if _, err := InjectRoutingWithOptions(home, model.AgentClaudeCode, RoutingOptions{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(), "Claude Code ODD worker models") {
		t.Fatal("with no models chosen, Claude Code keeps running workers on the main model")
	}

	options := RoutingOptions{
		ClaudePhaseAssignments: map[string]model.ClaudePhaseAssignment{
			"odd-explorer": {Model: model.ClaudeCustomModel("gpt-5.6-sol"), Effort: model.ClaudeEffortHigh},
			"odd-worker":   {Model: model.ClaudeModelSonnet},
			"jd-judge-a":   {Model: model.ClaudeModelOpus},
		},
		ClaudeModelAssignments: map[string]model.ClaudeModelAlias{"default": model.ClaudeModelHaiku},
	}
	if _, err := InjectRoutingWithOptions(home, model.AgentClaudeCode, options); err != nil {
		t.Fatal(err)
	}
	body := read()
	for _, want := range []string{
		"| `odd-explorer` | `gpt-5.6-sol` |",
		"| `odd-worker` | `sonnet` |",
		"| any other delegated task | `haiku` |",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Claude guidance missing %q", want)
		}
	}
	if strings.Contains(body, "| `odd-verify`") || strings.Contains(body, "| `jd-judge-a`") {
		t.Error("only ODD classes with a chosen model are listed")
	}
}
