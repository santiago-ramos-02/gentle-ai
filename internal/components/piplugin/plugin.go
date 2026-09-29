// Package piplugin offers optional Pi packages, such as the Claude bridge, that
// Gentle AI can install into Pi alongside its own package stack. A Pi plugin is
// a Pi package: installing it runs `pi install <source>`, which adds it to Pi's
// settings, and removing it runs `pi remove <source>`.
package piplugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gentleman-programming/gentle-ai/v4/internal/agents/pi"
	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
)

// Definition is one offered Pi plugin.
type Definition struct {
	ID          model.PiPluginID
	Name        string
	Source      string
	RepoURL     string
	Description string
}

var definitions = []Definition{
	{
		ID:          model.PiPluginClaudeBridge,
		Name:        "Claude Bridge",
		Source:      "npm:pi-claude-bridge",
		RepoURL:     "https://github.com/ShikherVerma/pi-claude-bridge",
		Description: "Claude models in Pi on your Claude Pro/Max subscription, through Claude Code (sign in to Claude Code first)",
	},
}

// Definitions returns the offered Pi plugins.
func Definitions() []Definition {
	return append([]Definition(nil), definitions...)
}

// DefinitionFor returns the definition of id.
func DefinitionFor(id model.PiPluginID) (Definition, bool) {
	for _, def := range definitions {
		if def.ID == id {
			return def, true
		}
	}
	return Definition{}, false
}

// InstalledIDs lists the offered plugins Pi's settings declare.
func InstalledIDs(homeDir string) []model.PiPluginID {
	raw, err := os.ReadFile(filepath.Join(pi.AgentConfigPath(homeDir), "settings.json"))
	if err != nil {
		return nil
	}
	var settings struct {
		Packages []json.RawMessage `json:"packages"`
	}
	if json.Unmarshal(raw, &settings) != nil {
		return nil
	}
	declared := map[string]bool{}
	for _, entry := range settings.Packages {
		var source string
		if json.Unmarshal(entry, &source) != nil {
			var object struct {
				Source string `json:"source"`
			}
			if json.Unmarshal(entry, &object) != nil {
				continue
			}
			source = object.Source
		}
		declared[withoutVersion(source)] = true
	}
	ids := []model.PiPluginID{}
	for _, def := range definitions {
		if declared[def.Source] {
			ids = append(ids, def.ID)
		}
	}
	return ids
}

// withoutVersion drops an npm source's @version, keeping a scope's leading @.
func withoutVersion(source string) string {
	name, ok := strings.CutPrefix(source, "npm:")
	if !ok {
		return source
	}
	if at := strings.LastIndex(name, "@"); at > 0 {
		name = name[:at]
	}
	return "npm:" + name
}

// Runner runs one command to completion.
type Runner func(name string, args ...string) error

// InstallCommands are the commands that install ids into Pi, in order.
func InstallCommands(ids []model.PiPluginID) ([][]string, error) {
	commands := make([][]string, 0, len(ids))
	for _, id := range ids {
		def, ok := DefinitionFor(id)
		if !ok {
			return nil, fmt.Errorf("unknown Pi plugin %q", id)
		}
		commands = append(commands, []string{"pi", "install", def.Source})
	}
	return commands, nil
}

// Install installs ids into Pi with run.
func Install(run Runner, ids []model.PiPluginID) error {
	commands, err := InstallCommands(ids)
	if err != nil {
		return err
	}
	for _, command := range commands {
		if err := run(command[0], command[1:]...); err != nil {
			return fmt.Errorf("%s: %w", strings.Join(command, " "), err)
		}
	}
	return nil
}

// Uninstall removes id from Pi with run.
func Uninstall(run Runner, id model.PiPluginID) error {
	def, ok := DefinitionFor(id)
	if !ok {
		return fmt.Errorf("unknown Pi plugin %q", id)
	}
	if err := run("pi", "remove", def.Source); err != nil {
		return fmt.Errorf("pi remove %s: %w", def.Source, err)
	}
	return nil
}
