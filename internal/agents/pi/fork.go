package pi

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gentleman-programming/gentle-ai/v3/internal/components/filemerge"
)

// gentlePiSource is the gentle-pi package Pi setup installs. The T3 fork's release
// workflow points it at the fork's own gentle-pi, which carries the API T3 Code uses:
//
//	-X github.com/gentleman-programming/gentle-ai/v3/internal/agents/pi.gentlePiSource=git:github.com/<owner>/gentle-shell
var gentlePiSource = "npm:gentle-pi"

// isGentlePiDeclaration reports whether a Pi package source is some gentle-pi:
// the npm package at any version, or a git install of gentle-shell, its repository.
func isGentlePiDeclaration(source string) bool {
	if source == "npm:gentle-pi" || strings.HasPrefix(source, "npm:gentle-pi@") {
		return true
	}
	location, isGit := strings.CutPrefix(source, "git:")
	if !isGit {
		location, isGit = strings.CutPrefix(source, "https://")
	}
	location, _, _ = strings.Cut(location, "@")
	return isGit && strings.HasSuffix(strings.TrimSuffix(location, ".git"), "/gentle-shell")
}

// PrepareGentlePiSource drops every gentle-pi declaration other than the one Pi
// setup installs from Pi's settings, before that install runs. Pi loads each
// declared package, so two copies of gentle-pi would register the same tools and
// Pi would refuse to start. Pi uninstalls the dropped copy on its next package sync.
func PrepareGentlePiSource(homeDir string) error {
	path := NewAdapter().SettingsPath(homeDir)
	settings, err := readPiJSONObject(path)
	if err != nil {
		return err
	}
	packages := piPackagesAsSlice(settings["packages"])
	kept := make([]any, 0, len(packages))
	for _, pkg := range packages {
		source := piPackageIdentity(pkg)
		if isGentlePiDeclaration(source) && source != gentlePiSource {
			continue
		}
		kept = append(kept, pkg)
	}
	if len(kept) == len(packages) {
		return nil
	}
	settings["packages"] = kept
	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pi settings %q: %w", path, err)
	}
	if _, err := filemerge.WriteFileAtomic(path, append(encoded, '\n'), 0o644); err != nil {
		return err
	}
	return nil
}
