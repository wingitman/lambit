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

	// Find the longest label for alignment.
	labelW := 0
	for _, e := range b.entries {
		if len(e.Label) > labelW {
			labelW = len(e.Label)
		}
	}
	if labelW > 20 {
		labelW = 20
	}

	var sb strings.Builder
	for _, e := range b.entries {
		filled := int(float64(barWidth) * float64(e.Duration) / float64(maxDur))
		if filled < 1 && e.Duration > 0 {
			filled = 1
		}
		empty := barWidth - filled

		bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)

		label := e.Label
		if len(label) > labelW {
			label = label[:labelW-1] + "…"
		}
		pad := strings.Repeat(" ", labelW-len(label))

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
