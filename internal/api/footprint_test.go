package api

import (
	"strings"
	"testing"

	componentuninstall "github.com/gentleman-programming/gentle-ai/v3/internal/components/uninstall"
	"github.com/gentleman-programming/gentle-ai/v3/internal/model"
)

func TestFootprintReportsOneAgentAndRejectsUnknownOnes(t *testing.T) {
	deps := testDeps(t)
	var asked model.AgentID
	deps.Footprint = func(homeDir string, agent model.AgentID) (componentuninstall.Footprint, error) {
		asked = agent
		return componentuninstall.Footprint{Agent: string(agent), Removed: []string{homeDir + "/.claude/skills/judgment-day"}, Rewritten: []componentuninstall.RewrittenFile{}, Unsimulated: []string{}}, nil
	}
	got := result[componentuninstall.Footprint](t, deps, "footprint", `{"agent":"claude-code"}`)
	if asked != model.AgentClaudeCode || len(got.Removed) != 1 || !strings.HasSuffix(got.Removed[0], "judgment-day") {
		t.Fatalf("footprint = %+v for %q", got, asked)
	}

	lines, err := callAPI(t, deps, []string{"footprint"}, `{"agent":"nope"}`)
	if err == nil || lines[len(lines)-1]["type"] != "error" {
		t.Fatalf("unknown agent should fail: %v %v", lines, err)
	}
}
