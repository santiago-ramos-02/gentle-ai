// Package claudeprofile keeps named model setups for Claude Code and applies them.
//
// Claude Code's orchestrator can only pick one of four model slots per delegation:
// fable, opus, sonnet, and haiku. A profile decides what each slot really runs
// (ANTHROPIC_DEFAULT_<SLOT>_MODEL, which a proxy can point at any provider's model)
// and tells the orchestrator what each slot is good for, so it chooses per task.
// A profile may also pin Gentle AI's phases to fixed models; with none pinned the
// orchestrator decides everything itself. Switching profiles is how a user moves
// work off an account whose usage limit is running out.
package claudeprofile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/gentleman-programming/gentle-ai/v4/internal/components/filemerge"
	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
)

// Slots are the model names Claude Code's orchestrator can choose between, strongest first.
var Slots = []string{"fable", "opus", "sonnet", "haiku"}

// Slot is what one slot runs and when to use it.
type Slot struct {
	// Model is the ID Claude Code sends, such as claude-opus-5-5 or a proxy's ID.
	Model string `json:"model"`
	// Label names the model for people and for the orchestrator, such as "GPT-6 Luna".
	Label string `json:"label,omitempty"`
	// UseFor tells the orchestrator which work belongs on this slot.
	UseFor string `json:"useFor,omitempty"`
}

// PhaseModel pins one Gentle AI phase: a slot name, or custom:<model id>.
type PhaseModel struct {
	Model  string `json:"model"`
	Effort string `json:"effort,omitempty"`
}

// Profile is one named setup.
type Profile struct {
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	Slots       map[string]Slot       `json:"slots"`
	Phases      map[string]PhaseModel `json:"phases,omitempty"`
}

// Store is every profile and which one is applied.
type Store struct {
	Active   string    `json:"active,omitempty"`
	Profiles []Profile `json:"profiles"`
	// OriginalEnv is what Claude Code's settings held for the slot variables before a
	// profile was first applied, restored when no profile is applied; a null value
	// means the variable was absent.
	OriginalEnv map[string]*string `json:"originalEnv,omitempty"`
}

var (
	profileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,63}$`)
	modelID     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+\-\[\]]{0,127}$`)
)

// EnvVar is the Claude Code setting that decides what slot runs.
func EnvVar(slot string) string {
	return "ANTHROPIC_DEFAULT_" + strings.ToUpper(slot) + "_MODEL"
}

// Validate reports why p cannot be saved, or nil.
func (p Profile) Validate() error {
	if !profileName.MatchString(p.Name) {
		return fmt.Errorf("profile name %q: use 1-64 letters, numbers, spaces, dots, underscores or hyphens", p.Name)
	}
	for slot, value := range p.Slots {
		if !slices.Contains(Slots, slot) {
			return fmt.Errorf("unknown slot %q: use fable, opus, sonnet, or haiku", slot)
		}
		if !modelID.MatchString(value.Model) {
			return fmt.Errorf("slot %s: model %q is not a model ID", slot, value.Model)
		}
		if strings.ContainsAny(value.Label+value.UseFor, "\n\r|") {
			return fmt.Errorf("slot %s: keep the label and use-for text on one line, without |", slot)
		}
	}
	for phase, pinned := range p.Phases {
		assignment := model.ClaudePhaseAssignment{Model: model.ClaudeModelAlias(pinned.Model), Effort: model.ClaudeEffort(pinned.Effort)}
		if !assignment.Valid() {
			return fmt.Errorf("phase %s: %q with effort %q is not a Claude Code choice", phase, pinned.Model, pinned.Effort)
		}
	}
	return nil
}

// Find returns the profile named name.
func (s Store) Find(name string) (Profile, bool) {
	index := slices.IndexFunc(s.Profiles, func(p Profile) bool { return p.Name == name })
	if index < 0 {
		return Profile{}, false
	}
	return s.Profiles[index], true
}

// ActiveProfile returns the applied profile, if any.
func (s Store) ActiveProfile() (Profile, bool) {
	if s.Active == "" {
		return Profile{}, false
	}
	return s.Find(s.Active)
}

// Path is where gentle-ai keeps the profiles.
func Path(homeDir string) string {
	return filepath.Join(homeDir, ".gentle-ai", "claude-profiles.json")
}

// Load reads the profiles; no file is an empty store.
func Load(homeDir string) (Store, error) {
	raw, err := os.ReadFile(Path(homeDir))
	if errors.Is(err, os.ErrNotExist) {
		return Store{Profiles: []Profile{}}, nil
	}
	if err != nil {
		return Store{}, err
	}
	var store Store
	if err := json.Unmarshal(raw, &store); err != nil {
		return Store{}, fmt.Errorf("read %s: %w", Path(homeDir), err)
	}
	if store.Profiles == nil {
		store.Profiles = []Profile{}
	}
	return store, nil
}

// Save writes the profiles.
func Save(homeDir string, store Store) error {
	encoded, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(Path(homeDir)), 0o755); err != nil {
		return err
	}
	_, err = filemerge.WriteFileAtomic(Path(homeDir), append(encoded, '\n'), 0o644)
	return err
}

// ClaudeSettingsPath is the settings file Claude Code reads its environment from.
func ClaudeSettingsPath(homeDir string) string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "settings.json")
	}
	return filepath.Join(homeDir, ".claude", "settings.json")
}

// ApplyEnv makes Claude Code's slots run what profile says, or, with no profile, what
// they ran before any profile was applied. It records that original state in store the
// first time, so the caller must save store afterwards.
func ApplyEnv(homeDir string, store *Store, profile *Profile) error {
	path := ClaudeSettingsPath(homeDir)
	settings := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &settings); err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	env, _ := settings["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	if store.OriginalEnv == nil {
		store.OriginalEnv = map[string]*string{}
		for _, slot := range Slots {
			if value, ok := env[EnvVar(slot)].(string); ok {
				store.OriginalEnv[EnvVar(slot)] = &value
			} else {
				store.OriginalEnv[EnvVar(slot)] = nil
			}
		}
	}
	for _, slot := range Slots {
		key := EnvVar(slot)
		switch {
		case profile != nil && profile.Slots[slot].Model != "":
			env[key] = profile.Slots[slot].Model
		case store.OriginalEnv[key] != nil:
			env[key] = *store.OriginalEnv[key]
		default:
			delete(env, key)
		}
	}
	if profile == nil {
		store.OriginalEnv = nil
	}
	if len(env) == 0 {
		delete(settings, "env")
	} else {
		settings["env"] = env
	}
	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	_, err = filemerge.WriteFileAtomic(path, append(encoded, '\n'), 0o644)
	return err
}

// Guide is the orchestrator guidance for the applied profile: what each slot runs and
// when to use it. It is empty without a profile or when no slot says what it is for.
func Guide(profile Profile) string {
	var rows strings.Builder
	for _, slot := range Slots {
		value, ok := profile.Slots[slot]
		if !ok || value.UseFor == "" {
			continue
		}
		label := value.Label
		if label == "" {
			label = value.Model
		}
		fmt.Fprintf(&rows, "| `%s` | %s | %s |\n", slot, label, value.UseFor)
	}
	if rows.Len() == 0 {
		return ""
	}
	return "\n\n### Claude Code model slots\n\n" +
		"Choose a model for every delegation by passing one of these slots as the Agent tool's `model` parameter. " +
		"Each slot runs the model listed, whatever its name suggests. Pick the cheapest slot that will do the task well; " +
		"keep the strongest slot for work that needs its judgement.\n\n" +
		"| slot | runs | use for |\n|---|---|---|\n" + rows.String()
}
