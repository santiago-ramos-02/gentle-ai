package catalog

import "github.com/gentleman-programming/gentle-ai/v4/internal/model"

// Preset is an installer preset in the order the installer offers it.
type Preset struct {
	ID          model.PresetID
	Label       string
	Description string
}

var presets = []Preset{
	{ID: model.PresetFullGentleman, Label: "Dev Stack + Polish", Description: "Dev Stack plus managed themes and logo polish"},
	{ID: model.PresetEcosystemOnly, Label: "Dev Stack", Description: "Memory + SDD + skills + docs + GGA"},
	{ID: model.PresetMinimal, Label: "Memory Only", Description: "Just Engram persistent memory across sessions"},
	{ID: model.PresetCustom, Label: "Custom", Description: "Choose each component manually: memory, persona, themes, logo, and more"},
}

// Presets returns the installer presets in display order.
func Presets() []Preset {
	out := make([]Preset, len(presets))
	copy(out, presets)
	return out
}

// Persona is a selectable persona in the order the installer offers it.
type Persona struct {
	ID          model.PersonaID
	Description string
}

var personas = []Persona{
	{ID: model.PersonaGentleman, Description: "Voseo conversation; English technical artifacts"},
	{ID: model.PersonaNeutral, Description: "No regional conversation tone; English technical artifacts"},
	{ID: model.PersonaCustom, Description: "Do not install a managed persona; choose themes/logo on the next screens"},
}

// legacyPersonaDescription labels persisted state that still carries the
// legacy alias. The alias is remapped at normalization time and never offered.
const legacyPersonaDescription = "No regional conversation tone; English technical artifacts (legacy alias, remapped)"

// Personas returns the selectable personas in display order.
func Personas() []Persona {
	out := make([]Persona, len(personas))
	copy(out, personas)
	return out
}

// PersonaDescription describes any persona id the installer may encounter,
// including the legacy alias found in unmigrated state.
func PersonaDescription(id model.PersonaID) (string, bool) {
	if id == model.PersonaGentlemanNeutralArtifacts {
		return legacyPersonaDescription, true
	}
	for _, persona := range personas {
		if persona.ID == id {
			return persona.Description, true
		}
	}
	return "", false
}
