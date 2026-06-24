package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

var (
	checkStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	overflowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// listState holds the done buffer and provides sweep/render logic.
type listState struct {
	done []doneEntry
}

func (l listState) add(item item) listState {
	l.done = append(l.done, doneEntry{item: item})
	return l
}

// sweep stamps freshly-finished entries, immediately drops invisible
// overflow beyond the visible window, then prunes one expired visible
// entry per tick so the list shrinks smoothly toward the active-item
// height without a plateau or sudden collapse.
func (l listState) sweep(activeCount, cap int, now time.Time) listState {
	if len(l.done) == 0 {
		return l
	}

	for i := range l.done {
		if l.done[i].finishedAt.IsZero() {
			l.done[i].finishedAt = now
		}
	}

	room := max(cap-activeCount, 0)
	if len(l.done) > room {
		l.done = l.done[len(l.done)-room:]
	}

	if len(l.done) > 0 && now.Sub(l.done[0].finishedAt) >= doneGrace {
		l.done = l.done[1:]
	}

	return l
}

// view renders the done buffer (top) followed by active items (bottom),
// capped to cap lines. Oldest done entries are culled first; if active
// items alone fill the cap only the oldest are shown, with an overflow
// indicator.
func (l listState) view(spinner string, active []item, cap int) string {
	var b strings.Builder
	done := l.done

	if len(active)+len(done) <= cap {
		writeRegion(&b, spinner, done, active)
		return b.String()
	}

	if len(active) >= cap {
		// Only show overflow indicator if there's room for at least
		// one item alongside it. With cap == 1 (compact mode) showing
		// only "+N active" is useless, so prefer the single item.
		if len(active) > cap && cap > 1 {
			// Reserve one line for the overflow indicator so
			// total output never exceeds cap.
			hidden := len(active) - cap + 1
			fmt.Fprintf(&b, "%s\n", overflowStyle.Render(fmt.Sprintf("  +%d active", hidden)))
			active = active[:cap-1]
		} else {
			active = active[:cap]
		}
		writeRegion(&b, spinner, nil, active)
		return b.String()
	}

	room := cap - len(active)
	if len(done) > room {
		done = done[len(done)-room:]
	}

	writeRegion(&b, spinner, done, active)
	return b.String()
}

func writeRegion(b *strings.Builder, spinner string, done []doneEntry, active []item) {
	check := checkStyle.SetString("✓").String()
	for _, d := range done {
		fmt.Fprintf(b, "%s %s\n", check, d.String())
	}
	for _, a := range active {
		fmt.Fprintf(b, "%s%s\n", spinner, a.String())
	}
}
