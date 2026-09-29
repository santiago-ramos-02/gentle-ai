package api

import (
	"strings"

	"github.com/gentleman-programming/gentle-ai/v4/internal/catalog"
	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
)

// stepLabel names a pipeline step for people, such as "Set up Claude Code"
// for agent:claude-code, so hosts show progress without parsing step ids.
// Steps it does not know read as their id in words.
func stepLabel(id string) string {
	kind, rest, _ := strings.Cut(id, ":")
	switch kind {
	case "agent":
		if agent, ok := strings.CutPrefix(rest, "native-review:"); ok {
			return "Add review agents to " + agentName(model.AgentID(agent))
		}
		return "Set up " + agentName(model.AgentID(rest))
	case "agent-guidance":
		return "Add workflow guidance to " + agentName(model.AgentID(rest))
	case "managed-opencode-plugins":
		return "Add plugins to " + agentName(model.AgentID(rest))
	case "component":
		if rest == "compatibility-skills-refresh" {
			return "Refresh skills"
		}
		return "Install " + componentName(rest)
	case "prepare", "apply":
		switch rest {
		case "check-dependencies":
			return "Check dependencies"
		case "backup-snapshot":
			return "Back up agent files"
		case "rollback-restore":
			return "Prepare rollback"
		}
	}
	return words(id)
}

func componentName(id string) string {
	for _, component := range catalog.MVPComponents() {
		if component.ID == model.ComponentID(id) {
			return component.Name
		}
	}
	return words(id)
}

// words turns an id such as prepare:opencode-telemetry into "Opencode telemetry".
func words(id string) string {
	if _, rest, found := strings.Cut(id, ":"); found && (strings.HasPrefix(id, "prepare:") || strings.HasPrefix(id, "apply:")) {
		id = rest
	}
	text := strings.NewReplacer(":", " ", "-", " ", "_", " ").Replace(id)
	if text == "" {
		return id
	}
	return strings.ToUpper(text[:1]) + text[1:]
}
