package diff

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/arnarg/nilla-utils/internal/util"
	"github.com/charmbracelet/lipgloss"
)

const (
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
	nameWidth := 0
	for _, c := range r.Changes {
		if len(c.Name) > nameWidth {
			nameWidth = len(c.Name)
		}
	}

	// Render sections
	if len(changed) > 0 {
		fmt.Fprintln(w, "Version changes:")
		for i, c := range changed {
			t.renderChanged(w, i+1, c, numWidth, nameWidth)
		}
	}
	if len(added) > 0 {
		fmt.Fprintln(w, "Added packages:")
		for i, c := range added {
			t.renderAdded(w, i+1+len(changed), c, numWidth, nameWidth)
		}
	}
	if len(removed) > 0 {
		fmt.Fprintln(w, "Removed packages:")
		for i, c := range removed {
			t.renderRemoved(w, i+1+len(changed)+len(added), c, numWidth, nameWidth)
		}
	}

	t.renderStats(w, r)
	return nil
}

func (t *terminalRenderer) renderChanged(w io.Writer, num int, c Change, numWidth, nameWidth int) {
	paddedNum := fmt.Sprintf("%0*d", numWidth, num)
	styledName := lipgloss.NewStyle().Width(nameWidth).Foreground(colorPackage).Render(string(c.Name))

	prefixLen := longestCommonPrefix(c.Before, c.After)
	beforeStr := t.formatVersions(c.Before, prefixLen, colorRemoved)
	afterStr := t.formatVersions(c.After, prefixLen, colorAdded)

	fmt.Fprintf(w, "#%s  %s  %s -> %s\n", paddedNum, styledName, beforeStr, afterStr)
}

func (t *terminalRenderer) renderAdded(w io.Writer, num int, c Change, numWidth, nameWidth int) {
	paddedNum := fmt.Sprintf("%0*d", numWidth, num)
	styledName := lipgloss.NewStyle().Width(nameWidth).Foreground(colorPackage).Render(string(c.Name))
	versions := lipgloss.NewStyle().Foreground(colorPrefix).Render(versionsToString(c.After))
	fmt.Fprintf(w, "#%s  %s  %s\n", paddedNum, styledName, versions)
}

func (t *terminalRenderer) renderRemoved(w io.Writer, num int, c Change, numWidth, nameWidth int) {
	paddedNum := fmt.Sprintf("%0*d", numWidth, num)
	styledName := lipgloss.NewStyle().Width(nameWidth).Foreground(colorPackage).Render(string(c.Name))
	versions := lipgloss.NewStyle().Foreground(colorMuted).Render(versionsToString(c.Before))
	fmt.Fprintf(w, "#%s  %s  %s\n", paddedNum, styledName, versions)
}

func (t *terminalRenderer) renderStats(w io.Writer, r Report) {
	diffVal, negative, unit := util.DiffBytes(r.BytesBefore, r.BytesAfter)
	prefix := "+"
	if negative {
		prefix = "-"
	}
	fmt.Fprintf(w, "Closure size: %d -> %d (disk usage %s%.2f%s)\n",
		r.NumBefore, r.NumAfter, prefix, diffVal, unit)
}

func (t *terminalRenderer) formatVersions(vers []Version, highlight int, color lipgloss.Color) string {
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
			suf := lipgloss.NewStyle().Foreground(color).Render(s[highlight:])
			parts[i] = pre + suf
		} else {
			parts[i] = lipgloss.NewStyle().Foreground(color).Render(s)
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
