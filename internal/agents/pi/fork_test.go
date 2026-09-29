package pi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gentleman-programming/gentle-ai/v4/internal/system"
)

func useGentlePiSource(t *testing.T, source string) {
	t.Helper()
	original := gentlePiSource
	originalManaged := managedPackageSources
	t.Cleanup(func() { gentlePiSource, managedPackageSources = original, originalManaged })
	gentlePiSource = source
	managedPackageSources = append([]string{source}, originalManaged[1:]...)
}

func writePiSettings(t *testing.T, homeDir string, settings map[string]any) string {
	t.Helper()
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Setenv("GENTLE_PI_AGENT_HOME", "")
	path := NewAdapter().SettingsPath(homeDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(settings)
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readPiPackages(t *testing.T, path string) []any {
	t.Helper()
	settings, err := readPiJSONObject(path)
	if err != nil {
		t.Fatal(err)
	}
	return piPackagesAsSlice(settings["packages"])
}

// A fork build sets Pi up with the fork's gentle-pi and drops any other gentle-pi,
// which Pi would otherwise load alongside it.
func TestForkGentlePiReplacesOtherGentlePiDeclarations(t *testing.T) {
	fork := "git:github.com/friend/gentle-shell"
	useGentlePiSource(t, fork)
	home := t.TempDir()
	path := writePiSettings(t, home, map[string]any{
		"theme": "dark",
		"packages": []any{
			"npm:gentle-pi@3.7.0",
			"npm:pi-btw",
			map[string]any{"source": "npm:gentle-pi"},
			"git:github.com/someone-else/gentle-shell@v3.6.0",
			fork,
		},
	})

	commands, err := NewAdapter().InstallCommand(system.PlatformProfile{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(commands[0], []string{"pi", "install", fork}) {
		t.Fatalf("first install command = %v, want the fork's gentle-pi", commands[0])
	}
	if err := PrepareGentlePiSource(home); err != nil {
		t.Fatal(err)
	}
	if got := readPiPackages(t, path); !reflect.DeepEqual(got, []any{"npm:pi-btw", fork}) {
		t.Fatalf("packages = %v, want only pi-btw and the fork's gentle-pi", got)
	}
	settings, _ := readPiJSONObject(path)
	if settings["theme"] != "dark" {
		t.Fatalf("other settings were not kept: %v", settings)
	}
}

// Upstream builds keep installing npm:gentle-pi, and replace a fork's gentle-pi with it.
func TestUpstreamGentlePiReplacesAForksGentlePi(t *testing.T) {
	home := t.TempDir()
	path := writePiSettings(t, home, map[string]any{
		"packages": []any{"https://github.com/friend/gentle-shell", "npm:gentle-pi", "npm:pi-btw"},
	})
	if err := PrepareGentlePiSource(home); err != nil {
		t.Fatal(err)
	}
	if got := readPiPackages(t, path); !reflect.DeepEqual(got, []any{"npm:gentle-pi", "npm:pi-btw"}) {
		t.Fatalf("packages = %v", got)
	}
	if isGentlePiDeclaration("git:github.com/friend/gentle-shell-extras") || isGentlePiDeclaration("npm:gentle-pi-tools") {
		t.Fatal("other packages must not count as gentle-pi")
	}
}
