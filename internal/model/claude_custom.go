package model

import (
	"regexp"
	"strings"
)

// ClaudeCustomModelPrefix marks a Claude Code subagent model outside the four tiers: any
// model ID Claude Code's --model accepts, typically one a proxy such as CLIProxyAPI routes
// to another provider, as in custom:gpt-5.6-sol. The prefix keeps it apart from the tiers
// and is dropped wherever the model is handed to Claude Code.
const ClaudeCustomModelPrefix = "custom:"

// Model IDs as providers and proxies name them; nothing that could break out of the
// subagent frontmatter line it is written to.
var claudeCustomModelID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+\-\[\]]{0,127}$`)

// A custom model accepts every effort; whether its provider honors one is the proxy's concern.
var claudeCustomEfforts = []ClaudeEffort{
	ClaudeEffortDefault, ClaudeEffortLow, ClaudeEffortMedium, ClaudeEffortHigh, ClaudeEffortXHigh, ClaudeEffortMax,
}

// ClaudeCustomModel is the choice for a model Claude Code reaches by ID.
func ClaudeCustomModel(id string) ClaudeModelAlias {
	return ClaudeModelAlias(ClaudeCustomModelPrefix + id)
}

func (a ClaudeModelAlias) customModelID() (string, bool) {
	id, ok := strings.CutPrefix(string(a), ClaudeCustomModelPrefix)
	return id, ok && claudeCustomModelID.MatchString(id)
}

// ModelID is what Claude Code receives for the choice: the tier alias, or a custom model's ID.
func (a ClaudeModelAlias) ModelID() string {
	if id, ok := a.customModelID(); ok {
		return id
	}
	return string(a)
}
