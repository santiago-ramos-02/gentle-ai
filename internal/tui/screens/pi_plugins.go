package screens

import (
	"fmt"
	"slices"
	"strings"

	"github.com/gentleman-programming/gentle-ai/v4/internal/components/piplugin"
	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
	"github.com/gentleman-programming/gentle-ai/v4/internal/tui/styles"
)

// RenderPiPlugins offers the optional Pi packages, each with a row that opens its repo.
func RenderPiPlugins(selected []model.PiPluginID, cursor int) string {
	var b strings.Builder
	b.WriteString(styles.TitleStyle.Render("Optional Pi Plugins"))
	b.WriteString("\n\n")
	b.WriteString(styles.SubtextStyle.Render("Add Pi packages alongside Gentle AI's, or open their repos first to review them."))
	b.WriteString("\n\n")

	row := 0
	line := func(text string, subtle bool) {
		switch {
		case cursor == row:
			b.WriteString(styles.SelectedStyle.Render("> "+text) + "\n")
		case subtle:
			b.WriteString(styles.SubtextStyle.Render("  "+text) + "\n")
		default:
			b.WriteString(styles.UnselectedStyle.Render("  "+text) + "\n")
		}
		row++
	}
	for _, def := range piplugin.Definitions() {
		checkbox := "[ ]"
		if slices.Contains(selected, def.ID) {
			checkbox = "[x]"
		}
		line(fmt.Sprintf("%s %s — %s", checkbox, def.Name, def.Description), false)
		line("View repo: "+def.RepoURL, true)
	}
	line("Continue", false)
	line("Back", false)

	b.WriteString("\n")
	b.WriteString(styles.HelpStyle.Render("space/enter: toggle • repo row: open browser • esc: back"))
	return styles.FrameStyle.Render(b.String())
}

// PiPluginsOptionCount is the number of selectable rows on the Pi plugins screen.
func PiPluginsOptionCount() int {
	return len(piplugin.Definitions())*2 + 2
}
