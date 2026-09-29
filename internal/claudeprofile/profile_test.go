package claudeprofile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readEnv(t *testing.T, home string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(ClaudeSettingsPath(home))
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	env, _ := settings["env"].(map[string]any)
	return env
}

// Applying profiles points the slots at their models and keeps every other setting;
// applying none puts back exactly what the user had.
func TestApplyEnvSwitchesSlotsAndRestoresTheUsersOwn(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home := t.TempDir()
	path := ClaudeSettingsPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{"theme":"dark","env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:8317","ANTHROPIC_DEFAULT_HAIKU_MODEL":"my-haiku"}}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	store := Store{}
	dynamic := Profile{Name: "Dynamic", Slots: map[string]Slot{
		"opus":  {Model: "claude-opus-5-5"},
		"haiku": {Model: "gpt-6-luna", Label: "GPT-6 Luna", UseFor: "bounded tasks"},
	}}
	if err := ApplyEnv(home, &store, &dynamic); err != nil {
		t.Fatal(err)
	}
	env := readEnv(t, home)
	if env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "gpt-6-luna" || env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "claude-opus-5-5" || env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:8317" {
		t.Fatalf("env after apply = %v", env)
	}
	if _, set := env["ANTHROPIC_DEFAULT_SONNET_MODEL"]; set {
		t.Fatal("a slot the profile leaves out keeps Claude Code's own model")
	}

	// A second profile replaces the first; the originals are still the user's.
	saver := Profile{Name: "Saver", Slots: map[string]Slot{"sonnet": {Model: "gpt-6-sol"}}}
	if err := ApplyEnv(home, &store, &saver); err != nil {
		t.Fatal(err)
	}
	env = readEnv(t, home)
	if env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "my-haiku" || env["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "gpt-6-sol" {
		t.Fatalf("env after switching = %v", env)
	}
	if _, set := env["ANTHROPIC_DEFAULT_OPUS_MODEL"]; set {
		t.Fatal("the previous profile's slot must not linger")
	}

	if err := ApplyEnv(home, &store, nil); err != nil {
		t.Fatal(err)
	}
	env = readEnv(t, home)
	if len(env) != 2 || env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "my-haiku" || store.OriginalEnv != nil {
		t.Fatalf("env after applying none = %v, originals %v", env, store.OriginalEnv)
	}
}

func TestGuideDescribesTheSlotsTheProfileExplains(t *testing.T) {
	guide := Guide(Profile{Slots: map[string]Slot{
		"opus":  {Model: "claude-opus-5-5", Label: "Opus 5.5", UseFor: "ODD design and hard reasoning"},
		"haiku": {Model: "gpt-6-luna", UseFor: "bounded tasks"},
		"fable": {Model: "claude-fable-5-1"},
	}})
	for _, want := range []string{"| `opus` | Opus 5.5 | ODD design and hard reasoning |", "| `haiku` | gpt-6-luna | bounded tasks |"} {
		if !strings.Contains(guide, want) {
			t.Errorf("guide missing %q:\n%s", want, guide)
		}
	}
	if strings.Contains(guide, "`fable`") {
		t.Error("a slot with nothing to say is left out")
	}
	if Guide(Profile{}) != "" {
		t.Error("no guide without slot descriptions")
	}
}

func TestValidateRejectsWhatClaudeCodeCannotUse(t *testing.T) {
	good := Profile{Name: "Pinned", Slots: map[string]Slot{"haiku": {Model: "gpt-6-luna"}}, Phases: map[string]PhaseModel{"odd-worker": {Model: "custom:gpt-6-luna", Effort: "high"}}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Profile{
		{Name: "", Slots: nil},
		{Name: "x", Slots: map[string]Slot{"gpt": {Model: "a"}}},
		{Name: "x", Slots: map[string]Slot{"opus": {Model: "a b"}}},
		{Name: "x", Slots: map[string]Slot{"opus": {Model: "a", UseFor: "x | y"}}},
		{Name: "x", Phases: map[string]PhaseModel{"odd-worker": {Model: "gpt-6-luna"}}},
	} {
		if bad.Validate() == nil {
			t.Errorf("%+v should be rejected", bad)
		}
	}
}
