package piplugin

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
)

// Pi's settings declare packages as plain sources, versioned sources, or objects.
func TestInstalledIDsReadsEveryPackageForm(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "")
	home := t.TempDir()
	settings := filepath.Join(home, ".pi", "agent", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, packages := range []string{`["npm:pi-claude-bridge"]`, `["npm:pi-claude-bridge@0.9.0"]`, `[{"source":"npm:pi-claude-bridge"}]`} {
		if err := os.WriteFile(settings, []byte(`{"packages":`+packages+`}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := InstalledIDs(home); !slices.Equal(got, []model.PiPluginID{model.PiPluginClaudeBridge}) {
			t.Fatalf("packages %s: installed = %v", packages, got)
		}
	}
	if err := os.WriteFile(settings, []byte(`{"packages":["npm:pi-web-access"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := InstalledIDs(home); len(got) != 0 {
		t.Fatalf("installed without the bridge = %v", got)
	}
}

func TestInstallAndUninstallRunPi(t *testing.T) {
	var ran [][]string
	run := func(name string, args ...string) error {
		ran = append(ran, append([]string{name}, args...))
		return nil
	}
	if err := Install(run, []model.PiPluginID{model.PiPluginClaudeBridge}); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(run, model.PiPluginClaudeBridge); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"pi", "install", "npm:pi-claude-bridge"}, {"pi", "remove", "npm:pi-claude-bridge"}}
	if !slices.EqualFunc(ran, want, slices.Equal[[]string]) {
		t.Fatalf("ran %v, want %v", ran, want)
	}
	if err := Install(run, []model.PiPluginID{"nope"}); err == nil {
		t.Fatal("an unknown plugin must not install")
	}
}
