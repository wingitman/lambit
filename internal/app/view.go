package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/wingitman/lambit/internal/project"
	"github.com/wingitman/lambit/internal/ui"
)

// ─── Top-level view ───────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var b strings.Builder
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	switch m.mode {
	case ModeNoProject:
		b.WriteString(m.renderNoProject())
	case ModeHelp:
		b.WriteString(m.renderHelp())
	case ModeError:
		b.WriteString(m.renderErrorOverlay())
	case ModeBuildRunning:
		b.WriteString(m.renderBuildRunning())
	case ModeInvoking:
		b.WriteString(m.renderInvoking())
	default:
		b.WriteString(m.renderMain())
	}

	return b.String()
}

// ─── Header ───────────────────────────────────────────────────────────────────

func (m Model) renderHeader() string {
	runtimeLabel := "no runtime"
	if m.runtime != nil {
		runtimeLabel = m.runtime.Name()
	}
	projectName := "(unknown)"
	if m.proj != nil && m.proj.Name != "" {
		projectName = m.proj.Name
	}

	left := ui.StylePrimary.Render(projectName) +
		ui.StyleMuted.Render("  ("+runtimeLabel+")")

	delby := lipgloss.NewStyle().Foreground(ui.ColorBrand1).Bold(true).Render("delby")
	soft := lipgloss.NewStyle().Foreground(ui.ColorBrand2).Bold(true).Render("soft")
	brand := " " + delby + soft + " "

	leftW := lipgloss.Width(left)
	brandW := lipgloss.Width(brand)
	pad := m.width - leftW - brandW
	if pad < 1 {
		pad = 1
	}
	headerLine := left + strings.Repeat(" ", pad) + brand
	rule := ui.StyleMuted.Render(strings.Repeat("─", clamp(m.width, 1, m.width)))
	return headerLine + "\n" + rule
}

// ─── Main layout ──────────────────────────────────────────────────────────────

func (m Model) renderMain() string {
	leftW := 30
	if m.width < 64 {
		leftW = m.width / 2
	}
	rightW := m.width - leftW - 1

	var b strings.Builder

	reservedLines := 5
	if m.benchVisible {
		reservedLines += 12
	}
	contentH := m.height - reservedLines
	if contentH < 4 {
		contentH = 4
	}

	leftLines := m.renderLeftPanel(leftW, contentH)
	rightLines := m.renderRightPanel(rightW, contentH)

	maxLines := len(leftLines)
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}

	for i := 0; i < maxLines; i++ {
		l := ""
		if i < len(leftLines) {
			l = leftLines[i]
		}
		r := ""
		if i < len(rightLines) {
			r = rightLines[i]
		}
		lw := lipgloss.Width(l)
		if lw < leftW {
			l = l + strings.Repeat(" ", leftW-lw)
		}
		b.WriteString(l)
		b.WriteString(ui.StyleMuted.Render("│"))
		b.WriteString(r)
		b.WriteString("\n")
	}

	b.WriteString(ui.StyleMuted.Render(strings.Repeat("─", clamp(m.width, 1, m.width))) + "\n")
	b.WriteString(m.renderResults(m.width))

	if m.benchVisible {
		b.WriteString(ui.StyleMuted.Render(strings.Repeat("─", clamp(m.width, 1, m.width))) + "\n")
		b.WriteString(ui.StyleSectionTitle.Render("  Benchmark") + "\n")
		b.WriteString(m.bench.Render(m.width))
	}

	b.WriteString(ui.StyleMuted.Render(strings.Repeat("─", clamp(m.width, 1, m.width))) + "\n")
	b.WriteString(m.renderStatusBar())

	if m.mode == ModeEdit || m.mode == ModeNewTest {
		b.WriteString(m.renderInputOverlay())
	}

	return b.String()
}

// ─── Left panel ───────────────────────────────────────────────────────────────

func (m Model) renderLeftPanel(width, height int) []string {
	var lines []string

	// Functions section.
	lines = append(lines, ui.StyleSectionTitle.Render("  Functions"))
	if len(m.proj.Functions) == 0 {
		lines = append(lines, ui.StyleMuted.Render("  (none)"))
	}
	for i, fn := range m.proj.Functions {
		cursor := "  "
		style := ui.StyleNormal
		if m.section == SectionFunctions && m.fnCursor == i {
			cursor = " ▶"
			style = ui.StyleSelected
		}
		name := truncate(fn.Name, width-4)
		lines = append(lines, cursor+" "+style.Render(name))
	}

	// Tests section.
	lines = append(lines, "")
	lines = append(lines, ui.StyleSectionTitle.Render("  Tests"))
	fn := m.currentFunction()
	if fn == nil || len(fn.Tests) == 0 {
		lines = append(lines, ui.StyleMuted.Render("  (none)"))
	} else {
		for i, t := range fn.Tests {
			cursor := "  "
			nameStyle := ui.StyleNormal
			if m.section == SectionTests && m.testCursor == i {
				cursor = " ▶"
				nameStyle = ui.StyleSelected
			}

			if t.Kind == project.TestCaseXUnit {
				// xUnit tests: truncate a bit more to leave room for marker.
				name := truncate(t.Name, width-6)
				marker := ui.StyleMuted.Render(" ⊕")
				lines = append(lines, cursor+" "+nameStyle.Render(name)+marker)
			} else {
				name := truncate(t.Name, width-4)
				lines = append(lines, cursor+" "+nameStyle.Render(name))
			}
		}
	}

	// Models section.
	if len(m.proj.Models) > 0 {
		lines = append(lines, "")
		lines = append(lines, ui.StyleSectionTitle.Render("  Models"))
		for i, mdl := range m.proj.Models {
			cursor := "  "
			style := ui.StyleNormal
			if m.section == SectionModels && m.modelCursor == i {
				cursor = " ▶"
				style = ui.StyleSelected
			}
			name := truncate(mdl.Name, width-4)
			lines = append(lines, cursor+" "+style.Render(name))
		}
	}

	return padLines(lines, height)
}

// ─── Right panel ──────────────────────────────────────────────────────────────

func (m Model) renderRightPanel(width, height int) []string {
	var lines []string

	fn := m.currentFunction()
	if fn == nil {
		lines = append(lines, ui.StyleMuted.Render("  No function selected"))
		return padLines(lines, height)
	}

	lines = append(lines, ui.StyleSectionTitle.Render("  "+fn.Name))
	if fn.Handler != "" {
		lines = append(lines, ui.StyleMuted.Render("  "+truncate(fn.Handler, width-3)))
	}
	if fn.Description != "" {
		lines = append(lines, ui.StyleDetail.Render("  "+truncate(fn.Description, width-3)))
	}
	lines = append(lines, "")

	// Section-specific content.
	switch m.section {
	case SectionFunctions:
		lines = append(lines, ui.StyleAccent.Render("  Handler:"))
		for _, hl := range wrapString(fn.Handler, width-3) {
			lines = append(lines, "  "+ui.StyleResult.Render(hl))
		}

	case SectionTests:
		if m.testCursor < len(fn.Tests) {
			tc := fn.Tests[m.testCursor]
			if tc.Kind == project.TestCaseXUnit {
				// xUnit test: show filter and variant (if any).
				lines = append(lines, ui.StyleAccent.Render("  Filter:"))
				for _, fl := range wrapString(tc.Filter, width-3) {
					lines = append(lines, "  "+ui.StyleResult.Render(fl))
				}
				lines = append(lines, "")
				if tc.Payload != "" {
					lines = append(lines, ui.StyleAccent.Render("  Variant:"))
					lines = append(lines, "  "+ui.StyleResult.Render(truncate(tc.Payload, width-3)))
				} else {
					lines = append(lines, ui.StyleMuted.Render("  (no parameters)"))
				}
			} else {
				// User-defined payload test.
				lines = append(lines, ui.StyleAccent.Render("  Payload:"))
				for _, pl := range wrapString(m.currentPayload(), width-3) {
					lines = append(lines, "  "+ui.StyleResult.Render(pl))
				}
			}
		}

	case SectionModels:
		if m.modelCursor < len(m.proj.Models) {
			mdl := m.proj.Models[m.modelCursor]
			lines = append(lines, ui.StyleAccent.Render("  Model: "+mdl.Name))
			for _, jl := range wrapString(mdl.JSON, width-3) {
				lines = append(lines, "  "+ui.StyleResult.Render(jl))
			}
		}
	}
	lines = append(lines, "")

	// Context-sensitive action hints.
	k := m.keys
	editLabel := m.editHint()
	hints := []string{
		ui.StyleMuted.Render("[" + k.invoke + "]Invoke"),
		ui.StyleMuted.Render("[" + k.edit + "]" + editLabel),
		ui.StyleMuted.Render("[" + k.newTest + "]New Test"),
		ui.StyleMuted.Render("[" + k.delete + "]Delete"),
	}
	lines = append(lines, "  "+strings.Join(hints, "  "))

	return padLines(lines, height)
}

// ─── Results pane ─────────────────────────────────────────────────────────────

func (m Model) renderResults(width int) string {
	if len(m.results) == 0 {
		return ui.StyleMuted.Render("  (no results yet)") + "\n"
	}
	start := len(m.results) - 5
	if start < 0 {
		start = 0
	}
	var b strings.Builder
	for _, r := range m.results[start:] {
		status := ui.StyleSuccess.Render("✓")
		if !r.result.Success {
			status = ui.StyleError.Render("✗")
		}
		output := truncate(r.result.Stdout, width-30)
		if !r.result.Success {
			output = truncate(r.result.Error, width-30)
		}
		label := ui.StyleMuted.Render(truncate(r.label, 16))
		b.WriteString(fmt.Sprintf("  %s  %-*s  %6s  %s\n",
			status, 16, label, formatDur(r.result.Duration), ui.StyleResult.Render(output)))
	}
	return b.String()
}

// ─── Overlays / special screens ───────────────────────────────────────────────

func (m Model) renderNoProject() string {
	k := m.keys
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(ui.StyleError.Render("  No .lambit.toml found in this directory or any parent.") + "\n\n")
	b.WriteString(ui.StyleMuted.Render("  lambit needs a .lambit.toml to know what lambda to work with.") + "\n\n")
	b.WriteString(ui.StyleAccent.Render("  ["+k.scaffold+"]") +
		ui.StyleMuted.Render(" Scaffold a .lambit.toml here (auto-detects handlers + tests)") + "\n")
	b.WriteString(ui.StyleAccent.Render("  ["+k.quit+"]") + ui.StyleMuted.Render(" Quit") + "\n\n")
	b.WriteString(ui.StyleMuted.Render("  See SPEC.md for details on creating a custom runtime interface.") + "\n")
	return b.String()
}

func (m Model) renderBuildRunning() string {
	return "\n" + ui.StyleAccent.Render("  Building...") + "\n\n" +
		ui.StyleMuted.Render("  Please wait while the project is built.") + "\n"
}

func (m Model) renderInvoking() string {
	fn := m.currentFunction()
	name := "function"
	if fn != nil {
		name = fn.Name
	}
	if tc := m.currentTestCase(); tc != nil && tc.Kind == project.TestCaseXUnit {
		name = tc.Name
	}
	return "\n" + ui.StyleAccent.Render("  Invoking "+name+"...") + "\n\n" +
		ui.StyleMuted.Render("  Please wait.") + "\n"
}

func (m Model) renderErrorOverlay() string {
	box := ui.StyleConfirmBox.Render(
		ui.StyleError.Render("Error") + "\n\n" +
			m.errorMsg + "\n\n" +
			ui.StyleMuted.Render("Press any key to continue"),
	)
	return box + "\n"
}

func (m Model) renderInputOverlay() string {
	var title string
	switch m.mode {
	case ModeNewTest:
		if m.inputStep == StepName {
			title = "New Test — Enter name"
		} else {
			title = "New Test — Enter JSON payload"
		}
	default:
		title = m.editTitle()
	}
	box := ui.StyleConfirmBox.Render(
		ui.StyleInputPrompt.Render(title) + "\n\n" +
			m.textInput.View() + "\n\n" +
			ui.StyleMuted.Render("Enter to confirm · Esc to cancel"),
	)
	return "\n" + box + "\n"
}

func (m Model) renderHelp() string {
	k := m.keys
	rows := [][2]string{
		{"[" + k.up + "/" + k.down + "]", "Navigate list"},
		{"[" + k.invoke + "]", "Invoke selected function / test"},
		{"[" + k.edit + "]", "Edit selected item (handler / payload / model)"},
		{"[" + k.newTest + "]", "Create a new test case"},
		{"[" + k.delete + "]", "Delete selected test / model"},
		{"[" + k.toggleAPI + "]", "Start / stop local HTTP API server"},
		{"[" + k.benchmark + "]", "Toggle benchmark pane"},
		{"[" + k.scaffold + "]", "Scaffold .lambit.toml (auto-detects handlers + tests)"},
		{"[" + k.options + "]", "Open config in $EDITOR"},
		{"[" + k.help + "]", "Show this help"},
		{"[" + k.quit + "]", "Quit"},
	}
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(ui.StyleSectionTitle.Render("  Keybinds") + "\n\n")
	for _, r := range rows {
		b.WriteString(ui.StyleAccent.Render("  "+r[0]) + "  " + ui.StyleMuted.Render(r[1]) + "\n")
	}
	b.WriteString("\n" + ui.StyleMuted.Render("  Press any key to close") + "\n")
	return b.String()
}

// ─── Status bar ───────────────────────────────────────────────────────────────

func (m Model) renderStatusBar() string {
	if m.statusMsg != "" {
		return ui.StyleSuccess.Render("  "+m.statusMsg) + "\n"
	}

	k := m.keys

	apiStatus := ui.StyleMuted.Render("[API: OFF]")
	if m.apiServer != nil && m.apiServer.Running() {
		apiStatus = ui.StyleSuccess.Render("[API: " + m.apiServer.Addr() + "]")
	}

	hints := []string{
		apiStatus,
		"[" + k.toggleAPI + "]API",
		"[" + k.invoke + "]Invoke",
		"[" + k.newTest + "]NewTest",
		"[" + k.edit + "]" + m.editHint(),
		"[" + k.delete + "]Del",
		"[" + k.benchmark + "]Bench",
		"[" + k.options + "]Config",
		"[" + k.help + "]Help",
		"[" + k.quit + "]Quit",
	}
	row := strings.Join(hints, "  ")
	if len(row) > m.width {
		row = row[:m.width-1]
	}
	return ui.StyleStatusBar.Render("  "+row) + "\n"
}

// ─── Context helpers ──────────────────────────────────────────────────────────

// editTitle returns the overlay title for the current section/test state.
func (m Model) editTitle() string {
	switch m.section {
	case SectionFunctions:
		return "Edit Handler"
	case SectionTests:
		if tc := m.currentTestCase(); tc != nil && tc.Kind == project.TestCaseXUnit {
			return "Edit — read-only (auto-discovered)"
		}
		return "Edit Payload"
	case SectionModels:
		return "Edit Model JSON"
	}
	return "Edit"
}

// editHint returns the short label shown in the right panel and status bar.
func (m Model) editHint() string {
	switch m.section {
	case SectionFunctions:
		return "Edit Handler"
	case SectionTests:
		if tc := m.currentTestCase(); tc != nil && tc.Kind == project.TestCaseXUnit {
			return "(read-only)"
		}
		return "Edit Payload"
	case SectionModels:
		return "Edit Model"
	}
	return "Edit"
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func padLines(lines []string, height int) []string {
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

func truncate(s string, max int) string {
	if max < 4 {
		max = 4
	}
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func wrapString(s string, width int) []string {
	if width < 4 {
		width = 4
	}
	var out []string
	for len(s) > width {
		out = append(out, s[:width])
		s = s[width:]
	}
	if s != "" {
		out = append(out, s)
	}
	if len(out) == 0 {
		out = append(out, "")
	}
	return out
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

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
