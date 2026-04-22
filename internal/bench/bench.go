// Package bench maintains a rolling window of invocation durations and renders
// a horizontal bar chart using Unicode block characters.
package bench

import (
	"fmt"
	"strings"
	"time"
)

const (
	defaultWindowSize = 20
	barWidth          = 24 // characters for the filled/empty bar
)

// Entry is one recorded invocation result in the benchmark window.
type Entry struct {
	Label    string
	Duration time.Duration
	Success  bool
}

// Bench holds a rolling window of benchmark entries.
type Bench struct {
	entries    []Entry
	windowSize int
}

// New creates a Bench with the default rolling window size.
func New() *Bench {
	return &Bench{windowSize: defaultWindowSize}
}

// Add appends a new entry to the rolling window, evicting the oldest if full.
func (b *Bench) Add(label string, dur time.Duration, success bool) {
	e := Entry{Label: label, Duration: dur, Success: success}
	if len(b.entries) >= b.windowSize {
		b.entries = b.entries[1:]
	}
	b.entries = append(b.entries, e)
}

// Entries returns a copy of the current window.
func (b *Bench) Entries() []Entry {
	out := make([]Entry, len(b.entries))
	copy(out, b.entries)
	return out
}

// Clear empties the window.
func (b *Bench) Clear() { b.entries = nil }

// Render returns the benchmark chart as a string ready to embed in the TUI.
// maxWidth controls the total line width available for padding the label column.
func (b *Bench) Render(maxWidth int) string {
	if len(b.entries) == 0 {
		return "  (no invocations yet)\n"
	}

	// Find the longest duration in the window for scaling.
	var maxDur time.Duration
	for _, e := range b.entries {
		if e.Duration > maxDur {
			maxDur = e.Duration
		}
	}
	if maxDur == 0 {
		maxDur = 1
	}

	// Find the longest label for alignment (capped at 20 runes).
	labelW := 0
	for _, e := range b.entries {
		n := len([]rune(e.Label)) // rune count, not byte count
		if n > labelW {
			labelW = n
		}
	}
	if labelW > 20 {
		labelW = 20
	}

	var sb strings.Builder
	for _, e := range b.entries {
		// Scale the bar width proportionally to the max duration.
		filled := int(float64(barWidth) * float64(e.Duration) / float64(maxDur))
		if filled < 1 && e.Duration > 0 {
			filled = 1
		}
		if filled > barWidth {
			filled = barWidth // clamp — floating point can exceed barWidth
		}
		empty := barWidth - filled // always >= 0

		bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)

		// Truncate by rune count so multi-byte characters don't corrupt padding.
		runes := []rune(e.Label)
		if len(runes) > labelW {
			// Use ASCII "..." (3 bytes = 3 chars) so byte len == rune len.
			if labelW > 3 {
				runes = append(runes[:labelW-3], '.', '.', '.')
			} else {
				runes = runes[:labelW]
			}
		}
		label := string(runes)

		// Pad to labelW runes — now safe because we truncated by rune count.
		padN := labelW - len([]rune(label))
		if padN < 0 {
			padN = 0
		}
		pad := strings.Repeat(" ", padN)

		status := "✓"
		if !e.Success {
			status = "✗"
		}

		sb.WriteString(fmt.Sprintf("  %s%s  %s  %6s  %s\n",
			label, pad, bar, formatDur(e.Duration), status))
	}
	return sb.String()
}

func formatDur(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}
