package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/arnarg/nilla-utils/internal/diff"
	"github.com/arnarg/nilla-utils/internal/util"
)

var (
	mutedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	activeTabStyle = lipgloss.NewStyle().Bold(true).Underline(true)
	sectionStyle   = lipgloss.NewStyle().Bold(true)
	changedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	addedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	removedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

// diffTab is a category of changes shown by the browser. tabAll shows
// every category under a section header.
type diffTab int

const (
	tabAll diffTab = iota
	tabChanged
	tabAdded
	tabRemoved
)

var tabSections = []struct {
	tab   diffTab
	kind  diff.ChangeType
	title string
}{
	{tabChanged, diff.Changed, "Version changes"},
	{tabAdded, diff.Added, "Added packages"},
	{tabRemoved, diff.Removed, "Removed packages"},
}

// diffBrowserChrome is the number of lines the browser draws around the
// change list: tabs, two rules, the closure line and the key help.
const diffBrowserChrome = 5

// diffBrowser is a scrollable, filterable view of a diff report.
type diffBrowser struct {
	report    diff.Report
	nameWidth int

	tabs []diffTab
	tab  int

	filter    string
	filtering bool

	offset int
	// height is the number of change rows that fit on screen.
	height int
	width  int
}

func newDiffBrowser(r diff.Report) diffBrowser {
	b := diffBrowser{report: r, nameWidth: r.NameWidth(), tabs: []diffTab{tabAll}}
	changed, added, removed := r.Counts()
	for _, t := range []struct {
		tab   diffTab
		count int
	}{{tabChanged, changed}, {tabAdded, added}, {tabRemoved, removed}} {
		if t.count > 0 {
			b.tabs = append(b.tabs, t.tab)
		}
	}
	return b
}

func (b diffBrowser) currentTab() diffTab {
	return b.tabs[b.tab]
}

// matches returns the changes of kind that pass the filter.
func (b diffBrowser) matches(kind diff.ChangeType) []diff.Change {
	var out []diff.Change
	filter := strings.ToLower(b.filter)
	for _, c := range b.report.Changes {
		if c.Type != kind {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(string(c.Name)), filter) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// rows returns the lines of the change list for the current tab and filter.
func (b diffBrowser) rows() []string {
	nameWidth := b.nameWidth
	if b.width > 0 {
		nameWidth = min(nameWidth, max(b.width/2, 10))
	}

	var rows []string
	for _, s := range tabSections {
		if t := b.currentTab(); t != tabAll && t != s.tab {
			continue
		}
		changes := b.matches(s.kind)
		if len(changes) == 0 {
			continue
		}
		if b.currentTab() == tabAll {
			rows = append(rows, sectionStyle.Render(s.title+":"))
		}
		for _, c := range changes {
			rows = append(rows, diff.FormatChange(c, nameWidth))
		}
	}
	return rows
}

func (b diffBrowser) maxOffset() int {
	return max(len(b.rows())-b.height, 0)
}

func (b diffBrowser) clamp() diffBrowser {
	b.offset = max(min(b.offset, b.maxOffset()), 0)
	return b
}

// setSize sets the width and the number of lines available to the whole
// browser, chrome included.
func (b diffBrowser) setSize(width, lines int) diffBrowser {
	b.width = width
	b.height = max(lines-diffBrowserChrome, 1)
	return b.clamp()
}

// update handles navigation keys. It reports whether the key was consumed,
// so that the caller can treat the rest as answers to the prompt.
func (b diffBrowser) update(msg tea.KeyPressMsg) (diffBrowser, bool) {
	if b.filtering {
		switch msg.String() {
		case "enter":
			b.filtering = false
		case "esc":
			b.filtering = false
			b.filter = ""
		case "backspace":
			if r := []rune(b.filter); len(r) > 0 {
				b.filter = string(r[:len(r)-1])
			}
		default:
			if msg.Text == "" {
				return b, msg.String() != "ctrl+c"
			}
			b.filter += msg.Text
		}
		b.offset = 0
		return b.clamp(), true
	}

	switch msg.String() {
	case "/":
		b.filtering = true
	case "esc":
		b.filter = ""
		b.offset = 0
	case "right", "l", "tab":
		b.tab = (b.tab + 1) % len(b.tabs)
		b.offset = 0
	case "left", "h", "shift+tab":
		b.tab = (b.tab - 1 + len(b.tabs)) % len(b.tabs)
		b.offset = 0
	case "down", "j":
		b.offset++
	case "up", "k":
		b.offset--
	case "pgdown", "f", "space":
		b.offset += b.height
	case "pgup", "b":
		b.offset -= b.height
	case "home", "g":
		b.offset = 0
	case "end", "G":
		b.offset = b.maxOffset()
	default:
		return b, false
	}
	return b.clamp(), true
}

func (b diffBrowser) tabsView() string {
	changed, added, removed := b.report.Counts()
	labels := map[diffTab]struct {
		text  string
		style lipgloss.Style
	}{
		tabAll:     {fmt.Sprintf("All (%d)", len(b.report.Changes)), lipgloss.NewStyle()},
		tabChanged: {fmt.Sprintf("Changed (%d)", changed), changedStyle},
		tabAdded:   {fmt.Sprintf("Added (%d)", added), addedStyle},
		tabRemoved: {fmt.Sprintf("Removed (%d)", removed), removedStyle},
	}
	parts := make([]string, len(b.tabs))
	for i, t := range b.tabs {
		l := labels[t]
		if i == b.tab {
			parts[i] = l.style.Inherit(activeTabStyle).Render(l.text)
		} else {
			parts[i] = mutedStyle.Render(l.text)
		}
	}
	line := strings.Join(parts, "  ")
	if b.filtering || b.filter != "" {
		cursor := ""
		if b.filtering {
			cursor = "█"
		}
		line += "   / " + b.filter + cursor
	}
	return line
}

func (b diffBrowser) view() string {
	var s strings.Builder
	rule := mutedStyle.Render(strings.Repeat("─", max(min(b.width, 72), 10)))

	s.WriteString(b.tabsView() + "\n")
	s.WriteString(rule + "\n")

	rows := b.rows()
	switch {
	case len(b.report.Changes) == 0:
		s.WriteString("No version changes.\n")
	case len(rows) == 0:
		s.WriteString(mutedStyle.Render("No packages match the filter.") + "\n")
	default:
		end := min(b.offset+b.height, len(rows))
		for _, r := range rows[b.offset:end] {
			s.WriteString(r + "\n")
		}
	}

	scroll := ""
	if len(rows) > b.height {
		end := min(b.offset+b.height, len(rows))
		scroll = fmt.Sprintf("  %d–%d of %d", b.offset+1, end, len(rows))
	}
	s.WriteString(rule + mutedStyle.Render(scroll) + "\n")
	s.WriteString(diff.FormatClosure(b.report) + "\n")

	help := "←/→ category · ↑/↓ scroll · / filter"
	if b.filtering {
		help = "type to filter · enter done · esc clear"
	}
	s.WriteString(mutedStyle.Render(help))
	return s.String()
}

// DiffSummary describes a diff report on one line.
func DiffSummary(r diff.Report) string {
	changed, added, removed := r.Counts()

	var counts string
	if len(r.Changes) == 0 {
		counts = "no version changes"
	} else {
		counts = strings.Join([]string{
			changedStyle.Render(fmt.Sprintf("~%d changed", changed)),
			addedStyle.Render(fmt.Sprintf("+%d added", added)),
			removedStyle.Render(fmt.Sprintf("-%d removed", removed)),
		}, "  ")
	}

	diffVal, negative, unit := util.DiffBytes(r.BytesBefore, r.BytesAfter)
	sign := "+"
	if negative {
		sign = "-"
	}
	return fmt.Sprintf("%s %s %d → %d paths, %s%.2f %s",
		counts, mutedStyle.Render("│"), r.NumBefore, r.NumAfter, sign, diffVal, unit)
}
