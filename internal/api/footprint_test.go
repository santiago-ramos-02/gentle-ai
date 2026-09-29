package api

import (
	"slices"
	"testing"

	componentuninstall "github.com/gentleman-programming/gentle-ai/v4/internal/components/uninstall"
	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
	"github.com/gentleman-programming/gentle-ai/v4/internal/state"
)

func TestFootprintCoversOnlyAgentsGentleAiSetUp(t *testing.T) {
	deps := testDeps(t)
	if err := state.Write(deps.HomeDir, state.InstallState{InstalledAgents: []string{"claude-code", "opencode"}}); err != nil {
		t.Fatal(err)
	}
	var asked []model.AgentID
	deps.Footprint = func(_ string, agents []model.AgentID) (componentuninstall.Footprint, error) {
		asked = agents
		names := make([]string, 0, len(agents))
		for _, agent := range agents {
			names = append(names, string(agent))
		}
		return componentuninstall.Footprint{Agents: names, Removed: []string{}, Rewritten: []componentuninstall.RewrittenFile{}, Unsimulated: []string{}}, nil
	}

	// Codex was not set up by Gentle AI, so its configuration is left alone.
	got := result[componentuninstall.Footprint](t, deps, "footprint", `{"agents":["claude-code","codex"]}`)
	if !slices.Equal(asked, []model.AgentID{model.AgentClaudeCode}) || !slices.Equal(got.Agents, []string{"claude-code"}) {
		t.Fatalf("asked %v, got %+v", asked, got)
	}
	// Without agents, every agent Gentle AI set up.
	result[componentuninstall.Footprint](t, deps, "footprint", `{}`)
	if !slices.Equal(asked, []model.AgentID{model.AgentClaudeCode, model.AgentOpenCode}) {
		t.Fatalf("asked %v for every set-up agent", asked)
	}

	lines, err := callAPI(t, deps, []string{"footprint"}, `{"agents":["nope"]}`)
	if err == nil || lines[len(lines)-1]["type"] != "error" {
		t.Fatalf("unknown agent should fail: %v %v", lines, err)
	}
}

func TestDescribeNamesTheWorkflowsThisBuildOffers(t *testing.T) {
	got := result[describeResult](t, testDeps(t), "describe", "")
	if !slices.Equal(got.Features, []string{"odd"}) || !slices.Contains(got.Methods, "footprint") {
		t.Fatalf("describe = %+v", got)
	}
}

func TestStatusSaysWhenAgentFilesAreBehind(t *testing.T) {
	deps := testDeps(t)
	deps.Version = "3.8.0"
	if err := state.Write(deps.HomeDir, state.InstallState{InstalledAgents: []string{"claude-code"}, InstalledBinaryVersion: "3.7.0"}); err != nil {
		t.Fatal(err)
	}
	if got := result[statusResult](t, deps, "status", ""); !got.State.SyncNeeded || got.State.PendingSync {
		t.Fatalf("files from 3.7.0 under 3.8.0 need a sync: %+v", got.State)
	}
}
