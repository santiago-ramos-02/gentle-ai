package api

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gentleman-programming/gentle-ai/v4/internal/claudeprofile"
)

// With a host, applying a profile leaves Claude Code's settings alone and hands the
// host the slot models and guide; a profile applied to the settings earlier is undone.
func TestApplyClaudeProfileForAHostLeavesClaudeCodeSettingsAlone(t *testing.T) {
	deps := testDeps(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	settingsPath := filepath.Join(deps.HomeDir, ".claude", "settings.json")
	if err := writeFile(settingsPath, `{"env":{"ANTHROPIC_DEFAULT_HAIKU_MODEL":"my-haiku"}}`); err != nil {
		t.Fatal(err)
	}
	profile := `{"name":"Dynamic","slots":{"haiku":{"model":"luna-then-muse","label":"GPT-6 Luna","useFor":"bounded tasks"},"opus":{"model":"claude-opus-5-5"}}}`
	result[claudeProfilesResult](t, deps, "claude.profiles.save", `{"profile":`+profile+`}`)

	// Applied the old way first, so the settings hold the profile's slots.
	result[claudeProfilesResult](t, deps, "claude.profiles.apply", `{"name":"Dynamic"}`)
	if env := settingsEnv(t, settingsPath); env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "luna-then-muse" {
		t.Fatalf("settings env after a plain apply = %v", env)
	}

	applied := result[claudeProfilesResult](t, deps, "claude.profiles.apply", `{"name":"Dynamic","host":"t3"}`)
	if applied.Active == nil || *applied.Active != "Dynamic" {
		t.Fatalf("active = %v", applied.Active)
	}
	if env := settingsEnv(t, settingsPath); len(env) != 1 || env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "my-haiku" {
		t.Fatalf("settings env with a host = %v, want the user's own back", env)
	}
	raw, err := os.ReadFile(claudeprofile.HostPath(deps.HomeDir))
	if err != nil {
		t.Fatal(err)
	}
	var setup claudeprofile.HostSetup
	if err := json.Unmarshal(raw, &setup); err != nil {
		t.Fatal(err)
	}
	if setup.Profile != "Dynamic" || setup.Env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "luna-then-muse" || setup.Env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "claude-opus-5-5" || len(setup.Env) != 2 || setup.Guide == "" {
		t.Fatalf("host setup = %+v", setup)
	}

	// Saving the applied profile keeps its host.
	edited := `{"name":"Dynamic","slots":{"haiku":{"model":"muse"}}}`
	result[claudeProfilesResult](t, deps, "claude.profiles.save", `{"profile":`+edited+`,"host":"t3"}`)
	setup = claudeprofile.HostSetup{}
	if err := json.Unmarshal(mustRead(t, claudeprofile.HostPath(deps.HomeDir)), &setup); err != nil {
		t.Fatal(err)
	}
	if setup.Env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "muse" || len(setup.Env) != 1 || setup.Guide != "" {
		t.Fatalf("host setup after saving = %+v", setup)
	}

	result[claudeProfilesResult](t, deps, "claude.profiles.apply", `{"name":null,"host":"t3"}`)
	if _, err := os.Stat(claudeprofile.HostPath(deps.HomeDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("applying none must remove the host setup: %v", err)
	}
}

func settingsEnv(t *testing.T, path string) map[string]any {
	t.Helper()
	var settings map[string]any
	if err := json.Unmarshal(mustRead(t, path), &settings); err != nil {
		t.Fatal(err)
	}
	env, _ := settings["env"].(map[string]any)
	return env
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
