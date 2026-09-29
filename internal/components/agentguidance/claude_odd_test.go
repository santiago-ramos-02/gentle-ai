package agentguidance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
)

// Claude Code runs each ODD worker class that has a chosen model through a named agent
// carrying that model and effort, including a model a proxy routes; without choices
// nothing is added and workers stay on the main conversation's model.
func TestClaudeODDWorkersRunOnTheirChosenModels(t *testing.T) {
	home := t.TempDir()
	paths, err := RoutingPaths(home, model.AgentClaudeCode)
	if err != nil {
		t.Fatal(err)
	}
	agentsDir := filepath.Join(home, ".claude", "agents")
	read := func(path string) string {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}

	if _, err := InjectRoutingWithOptions(home, model.AgentClaudeCode, RoutingOptions{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(paths[0]), "Claude Code ODD worker agents") {
		t.Fatal("with no models chosen, the guidance stays as upstream's")
	}
	if files, err := SyncClaudeODDAgents(agentsDir, nil, nil); err != nil || len(files) != 0 {
		t.Fatalf("with no models chosen no agent is written: %v %v", files, err)
	}

	// The user's own verifier agent is theirs, whatever Gentle AI would put there.
	userVerify := filepath.Join(agentsDir, "odd-verify.md")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userVerify, []byte("my own verifier"), 0o644); err != nil {
		t.Fatal(err)
	}
	phases := map[string]model.ClaudePhaseAssignment{
		"odd-explorer": {Model: model.ClaudeCustomModel("claude-fable-5-dd-anul-6-tpg"), Effort: model.ClaudeEffortHigh},
		"odd-worker":   {Model: model.ClaudeModelSonnet},
		"odd-verify":   {Model: model.ClaudeModelHaiku},
		"jd-judge-a":   {Model: model.ClaudeModelOpus},
	}
	legacy := map[string]model.ClaudeModelAlias{"default": model.ClaudeModelHaiku}
	if _, err := InjectRoutingWithOptions(home, model.AgentClaudeCode, RoutingOptions{ClaudePhaseAssignments: phases, ClaudeModelAssignments: legacy}); err != nil {
		t.Fatal(err)
	}
	guidance := read(paths[0])
	for _, want := range []string{
		"| `odd-explorer` | `odd-explorer` | `claude-fable-5-dd-anul-6-tpg` |",
		"| `odd-worker` | `odd-worker` | `sonnet` |",
		"For any other delegated task, pass `model: haiku` to the Agent tool.",
	} {
		if !strings.Contains(guidance, want) {
			t.Errorf("Claude guidance missing %q", want)
		}
	}
	if strings.Contains(guidance, "| `jd-judge-a`") {
		t.Error("review agents carry their own models and are not listed")
	}

	files, err := SyncClaudeODDAgents(agentsDir, phases, legacy)
	if err != nil || len(files) != 2 {
		t.Fatalf("written agents = %v, %v; want explorer and worker only", files, err)
	}
	explorer := read(filepath.Join(agentsDir, "odd-explorer.md"))
	if !strings.Contains(explorer, "\nmodel: claude-fable-5-dd-anul-6-tpg\neffort: high\n---\n") || !strings.HasPrefix(explorer, "---\nname: odd-explorer\n") {
		t.Fatalf("explorer agent = %q", explorer)
	}
	if read(userVerify) != "my own verifier" {
		t.Fatal("a user's own agent must never be overwritten")
	}
	if again, err := SyncClaudeODDAgents(agentsDir, phases, legacy); err != nil || len(again) != 0 {
		t.Fatalf("an unchanged sync rewrites nothing: %v %v", again, err)
	}

	// Clearing a class's model takes its agent away again; the user's file stays.
	delete(phases, "odd-explorer")
	if _, err := SyncClaudeODDAgents(agentsDir, phases, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(agentsDir, "odd-explorer.md")); !os.IsNotExist(err) {
		t.Fatalf("explorer agent should be removed: %v", err)
	}
	if owned, _ := ClaudeODDAgentOwned(userVerify); owned {
		t.Fatal("the user's verifier is not Gentle AI's")
	}
}

// A proxied model cannot be passed per delegation, so it never becomes the general tier.
func TestClaudeProxiedGeneralModelIsNotPassedPerCall(t *testing.T) {
	legacy := map[string]model.ClaudePhaseAssignment{"default": {Model: model.ClaudeCustomModel("gpt-5.6-sol")}}
	if section := renderClaudeODDAssignments(legacy, nil); section != "" {
		t.Fatalf("section = %q", section)
	}
}
