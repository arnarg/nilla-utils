package diff

import (
	"fmt"
	"image/color"
	"io"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/arnarg/nilla-utils/internal/util"
)

var (
	colorMuted   = lipgloss.Color("8")
	colorPrefix  = lipgloss.Color("3")
	colorRemoved = lipgloss.Color("1")
	colorAdded   = lipgloss.Color("2")
	colorPackage = lipgloss.Color("10")
)

// Renderer handles output formatting.
type ReportRenderer interface {
	Render(w io.Writer, report Report) error
}

type terminalRenderer struct{}

func NewTerminalRenderer() ReportRenderer {
	return &terminalRenderer{}
}

func (t *terminalRenderer) Render(w io.Writer, r Report) error {
	if len(r.Changes) == 0 {
		fmt.Fprintln(w, "No version changes.")
		t.renderStats(w, r)
		return nil
	}

	// Split into categories
	var changed, added, removed []Change
	for _, c := range r.Changes {
		switch c.Type {
		case Changed:
			changed = append(changed, c)
		case Added:
			added = append(added, c)
		case Removed:
			removed = append(removed, c)
		}
	}

	// Calculate column widths
	total := len(r.Changes)
	numWidth := len(strconv.Itoa(total))
	nameWidth := r.NameWidth()

	// Render sections
	if len(changed) > 0 {
		fmt.Fprintln(w, "Version changes:")
		for i, c := range changed {
			t.renderRow(w, i+1, c, numWidth, nameWidth)
		}
	}
	if len(added) > 0 {
		fmt.Fprintln(w, "Added packages:")
		for i, c := range added {
			t.renderRow(w, i+1+len(changed), c, numWidth, nameWidth)
		}
	}
	if len(removed) > 0 {
		fmt.Fprintln(w, "Removed packages:")
		for i, c := range removed {
			t.renderRow(w, i+1+len(changed)+len(added), c, numWidth, nameWidth)
		}
	}

	t.renderStats(w, r)
	return nil
}

func (t *terminalRenderer) renderRow(w io.Writer, num int, c Change, numWidth, nameWidth int) {
	fmt.Fprintf(w, "#%0*d  %s\n", numWidth, num, FormatChange(c, nameWidth))
}

func (t *terminalRenderer) renderStats(w io.Writer, r Report) {
	fmt.Fprintln(w, FormatClosure(r))
}

// FormatChange renders a change as its name, padded to nameWidth, followed by
// its versions: "before -> after" for changes, the versions alone otherwise.
func FormatChange(c Change, nameWidth int) string {
	styledName := lipgloss.NewStyle().Width(nameWidth).Foreground(colorPackage).Render(string(c.Name))

	switch c.Type {
	case Added:
		return styledName + "  " + lipgloss.NewStyle().Foreground(colorPrefix).Render(versionsToString(c.After))
	case Removed:
		return styledName + "  " + lipgloss.NewStyle().Foreground(colorMuted).Render(versionsToString(c.Before))
	}

	prefixLen := longestCommonPrefix(c.Before, c.After)
	beforeStr := formatVersions(c.Before, prefixLen, colorRemoved)
	afterStr := formatVersions(c.After, prefixLen, colorAdded)

	return fmt.Sprintf("%s  %s -> %s", styledName, beforeStr, afterStr)
}

// FormatClosure renders the closure path count and disk usage change.
func FormatClosure(r Report) string {
	diffVal, negative, unit := util.DiffBytes(r.BytesBefore, r.BytesAfter)
	prefix := "+"
	if negative {
		prefix = "-"
	}
	return fmt.Sprintf("Closure size: %d -> %d (disk usage %s%.2f%s)",
		r.NumBefore, r.NumAfter, prefix, diffVal, unit)
}

// Counts returns the number of changed, added and removed packages.
func (r Report) Counts() (changed, added, removed int) {
	for _, c := range r.Changes {
		switch c.Type {
		case Changed:
			changed++
		case Added:
			added++
		case Removed:
			removed++
		}
	}
	return changed, added, removed
}

// NameWidth returns the length of the longest package name in the report.
func (r Report) NameWidth() int {
	w := 0
	for _, c := range r.Changes {
		w = max(w, len(c.Name))
	}
	return w
}

func formatVersions(vers []Version, highlight int, clr color.Color) string {
	if len(vers) == 0 {
		return lipgloss.NewStyle().Foreground(colorMuted).Render("<none>")
	}
	parts := make([]string, len(vers))
	for i, v := range vers {
		s := string(v)
		if s == "" {
			parts[i] = lipgloss.NewStyle().Foreground(colorMuted).Render("<none>")
			continue
		}
		if highlight > 0 && highlight < len(s) {
			pre := lipgloss.NewStyle().Foreground(colorPrefix).Render(s[:highlight])
			suf := lipgloss.NewStyle().Foreground(clr).Render(s[highlight:])
			parts[i] = pre + suf
		} else {
			parts[i] = lipgloss.NewStyle().Foreground(clr).Render(s)
		}
	}
	return strings.Join(parts, ", ")
}

func longestCommonPrefix(before, after []Version) int {
	if len(before) == 0 || len(after) == 0 {
		return 0
	}
	all := append(before, after...)
	shortest := 256
	for _, v := range all {
		if s := string(v); len(s) > 0 && len(s) < shortest {
			shortest = len(s)
		}
	}
	maxLen := 0
	for _, a := range before {
		sa := string(a)
		for _, b := range after {
			sb := string(b)
			if sa == "" || sb == "" {
				continue
			}
			i := 0
			for i < shortest && i < len(sa) && i < len(sb) && sa[i] == sb[i] {
				i++
			}
			for i > 0 && sa[i-1] != '.' && sa[i-1] != '-' {
				i--
			}
			if i > maxLen {
				maxLen = i
			}
		}
	}
	if maxLen > shortest {
		return shortest
	}
	return maxLen
}

func versionsToString(v []Version) string {
	if len(v) == 0 {
		return ""
	}
	s := make([]string, len(v))
	for i, ver := range v {
		s[i] = string(ver)
	}
	return strings.Join(s, ", ")
}
