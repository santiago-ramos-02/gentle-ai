package opencodeplugin

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/gentleman-programming/gentle-ai/v3/internal/model"
)

// InstalledIDs reads ~/.config/opencode/tui.json's plugin[] list and maps
// package names back to plugin ids, so callers can offer to uninstall what is
// actually installed. Unknown entries (third-party packages) are ignored, and
// the GentleLogo plugin is recognized by its .tsx path. A missing, malformed,
// or plugin-less tui.json yields an empty list.
func InstalledIDs(home string) []model.OpenCodeCommunityPluginID {
	if home == "" {
		return nil
	}
	tuiPath := filepath.Join(home, ".config", "opencode", "tui.json")
	data, err := os.ReadFile(tuiPath)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	root := map[string]any{}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil
	}
	raw, ok := root["plugin"].([]any)
	if !ok {
		return nil
	}
	knownByPackage := map[string]model.OpenCodeCommunityPluginID{}
	for _, id := range []model.OpenCodeCommunityPluginID{model.OpenCodePluginSubAgentStatusline, model.OpenCodePluginSDDEngramManage} {
		if def, ok := DefinitionFor(id); ok {
			knownByPackage[def.PackageName] = id
		}
	}
	// Match the GentleLogo plugin by either separator form because the
	// Install path uses filepath.Join (native separator for the host).
	gentleLogoSuffixes := []string{
		filepath.Join("tui-plugins", "gentle-logo.tsx"),
		"tui-plugins/gentle-logo.tsx",
	}
	seen := map[model.OpenCodeCommunityPluginID]bool{}
	out := make([]model.OpenCodeCommunityPluginID, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(string)
		if !ok {
			continue
		}
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if id, ok := knownByPackage[entry]; ok && !seen[id] {
			seen[id] = true
			out = append(out, id)
			continue
		}
		if !seen[model.OpenCodePluginGentleLogo] {
			for _, suffix := range gentleLogoSuffixes {
				if strings.HasSuffix(entry, suffix) {
					seen[model.OpenCodePluginGentleLogo] = true
					out = append(out, model.OpenCodePluginGentleLogo)
					break
				}
			}
		}
	}
	return out
}

// InstallAll registers each plugin in order and stops at the first failure,
// returning the results of the plugins registered before it.
func InstallAll(home string, ids []model.OpenCodeCommunityPluginID) ([]Result, error) {
	results := make([]Result, 0, len(ids))
	for _, id := range ids {
		result, err := Install(home, id)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}
