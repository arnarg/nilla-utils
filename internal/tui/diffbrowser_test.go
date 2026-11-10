package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/arnarg/nilla-utils/internal/diff"
)

func testReport() diff.Report {
	return diff.Report{
		Changes: []diff.Change{
			{Name: "alpha", Before: []diff.Version{"1.0"}, After: []diff.Version{"1.1"}, Type: diff.Changed},
			{Name: "beta", Before: []diff.Version{"2.0"}, After: []diff.Version{"3.0"}, Type: diff.Changed},
			{Name: "gamma", After: []diff.Version{"0.1"}, Type: diff.Added},
			{Name: "delta", Before: []diff.Version{"4.2"}, Type: diff.Removed},
		},
		NumBefore: 10,
		NumAfter:  11,
	}
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func plainRows(b diffBrowser) []string {
	rows := b.rows()
	for i, r := range rows {
		rows[i] = strings.Join(strings.Fields(stripANSI(r)), " ")
	}
	return rows
}

func TestDiffBrowser_TabsOnlyForPresentCategories(t *testing.T) {
	r := testReport()
	r.Changes = r.Changes[:2]
	b := newDiffBrowser(r)
	if len(b.tabs) != 2 || b.tabs[1] != tabChanged {
		t.Fatalf("tabs = %v, want [all changed]", b.tabs)
	}
}

func TestDiffBrowser_AllTabHasSections(t *testing.T) {
	b := newDiffBrowser(testReport()).setSize(80, 40)
	got := plainRows(b)
	want := []string{
		"Version changes:",
		"alpha 1.0 -> 1.1",
		"beta 2.0 -> 3.0",
		"Added packages:",
		"gamma 0.1",
		"Removed packages:",
		"delta 4.2",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("rows =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestDiffBrowser_SwitchTabs(t *testing.T) {
	b := newDiffBrowser(testReport()).setSize(80, 40)

	b, _ = b.update(key("right"))
	b, _ = b.update(key("right"))
	if got := plainRows(b); len(got) != 1 || got[0] != "gamma 0.1" {
		t.Errorf("added tab rows = %v", got)
	}

	// Wraps around backwards from the first tab.
	b, _ = b.update(key("left"))
	b, _ = b.update(key("left"))
	b, _ = b.update(key("left"))
	if b.currentTab() != tabRemoved {
		t.Errorf("tab = %v, want removed", b.currentTab())
	}
}

func TestDiffBrowser_Filter(t *testing.T) {
	b := newDiffBrowser(testReport()).setSize(80, 40)

	b, _ = b.update(key("/"))
	for _, k := range []string{"a", "l", "x", "backspace"} {
		var consumed bool
		b, consumed = b.update(key(k))
		if !consumed {
			t.Fatalf("key %q not consumed while filtering", k)
		}
	}
	b, _ = b.update(key("enter"))

	if b.filtering || b.filter != "al" {
		t.Fatalf("filtering=%v filter=%q, want done with \"al\"", b.filtering, b.filter)
	}
	if got := plainRows(b); len(got) != 2 || got[1] != "alpha 1.0 -> 1.1" {
		t.Errorf("filtered rows = %v", got)
	}

	b, _ = b.update(key("esc"))
	if b.filter != "" {
		t.Errorf("esc kept filter %q", b.filter)
	}
}

func TestDiffBrowser_PromptKeysPassThrough(t *testing.T) {
	b := newDiffBrowser(testReport()).setSize(80, 40)
	for _, k := range []string{"y", "n", "enter", "ctrl+c"} {
		if _, consumed := b.update(key(k)); consumed {
			t.Errorf("key %q consumed, want it left for the prompt", k)
		}
	}

	// While filtering, typed letters go to the filter but ctrl+c does not.
	b, _ = b.update(key("/"))
	if _, consumed := b.update(key("y")); !consumed {
		t.Error("y not consumed while filtering")
	}
	if _, consumed := b.update(key("ctrl+c")); consumed {
		t.Error("ctrl+c consumed while filtering")
	}
}

func TestDiffBrowser_ScrollClamps(t *testing.T) {
	// 7 rows, 2 visible.
	b := newDiffBrowser(testReport()).setSize(80, diffBrowserChrome+2)
	for range 10 {
		b, _ = b.update(key("down"))
	}
	if b.offset != 5 {
		t.Errorf("offset = %d, want 5", b.offset)
	}
	if v := stripANSI(b.view()); !strings.Contains(v, "6–7 of 7") {
		t.Errorf("view lacks scroll position:\n%s", v)
	}
	b, _ = b.update(key("g"))
	if b.offset != 0 {
		t.Errorf("offset after g = %d, want 0", b.offset)
	}
}

func TestDiffSummary(t *testing.T) {
	got := stripANSI(DiffSummary(testReport()))
	for _, want := range []string{"~2 changed", "+1 added", "-1 removed", "10 → 11 paths"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}

	if got := stripANSI(DiffSummary(diff.Report{})); !strings.Contains(got, "no version changes") {
		t.Errorf("empty summary = %q", got)
	}
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;:?]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}
