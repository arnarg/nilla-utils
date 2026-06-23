package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type summaryCell struct {
	icon  string
	value int
	color string
}

func summaryRow(cells []summaryCell, suffix string) string {
	parts := make([]string, len(cells))
	for i, c := range cells {
		parts[i] = lipgloss.NewStyle().
			Foreground(lipgloss.Color(c.color)).
			SetString(fmt.Sprintf("%s %d", c.icon, c.value)).
			String()
	}
	result := strings.Join(parts, " | ")
	if suffix != "" {
		result += " " + suffix
	}
	return result
}

func summaryHeader(label, value string) string {
	return lipgloss.NewStyle().
		Bold(true).
		Width(lipgloss.Width(value)).
		SetString(label).
		String()
}
