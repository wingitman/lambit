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

	contentH := m.leftPanelHeight()

	leftLines := m.renderLeftPanel(leftW, contentH)
	var rightLines []string
	if m.outputMode {
		rightLines = m.renderOutputPane(rightW, contentH)
	} else {
		rightLines = m.renderRightPanel(rightW, contentH)
	}

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
	// While filter is active with a non-empty query, show the filtered list.
	if m.mode == ModeFilter && m.filterText != "" {
		return m.renderFilteredPanel(width, height)
	}

	var lines []string

	// ── Functions ─────────────────────────────────────────────────────────
	lines = append(lines, ui.StyleSectionTitle.Render("  Functions"))
	if len(m.proj.Functions) == 0 {
		lines = append(lines, ui.StyleMuted.Render("  (none)"))
	}
	for i, fn := range m.proj.Functions {
		navCursor := m.section == SectionFunctions && m.fnCursor == i
		isLocked := m.fnLocked && i == m.selectedFnIdx

		cursor := "  "
		nameStyle := ui.StyleNormal

		switch {
		case navCursor:
			cursor = " ▶"
			nameStyle = ui.StyleSelected
		case isLocked && m.section != SectionFunctions:
			// Show lock marker when cursor has moved to another section.
			cursor = " ●"
			nameStyle = ui.StyleAccent
		}

		name := truncate(fn.Name, width-4)
		lines = append(lines, cursor+" "+nameStyle.Render(name))
	}

	// ── Tests ─────────────────────────────────────────────────────────────
	lines = append(lines, "")

	fn := m.selectedFunction()
	nTests := 0
	if fn != nil {
		nTests = len(fn.Tests)
	}

	testsHeader := "  Tests"
	if nTests > 0 {
		testsHeader = fmt.Sprintf("  Tests (%d)", nTests)
	}
	// When cursor is in Functions, show a subtle lock prompt on the header.
	if m.section == SectionFunctions {
		testsHeader += ui.StyleMuted.Render("  ← " + m.keys.confirm + " to lock")
	}
	lines = append(lines, ui.StyleSectionTitle.Render(testsHeader))

	if fn == nil || nTests == 0 {
		if m.section == SectionFunctions {
			lines = append(lines, ui.StyleMuted.Render("  (navigate to a function, press "+m.keys.confirm+")"))
		} else {
			lines = append(lines, ui.StyleMuted.Render("  (none)"))
		}
	} else {
		for i, t := range fn.Tests {
			navCursor := m.section == SectionTests && m.testCursor == i

			cursor := "  "
			nameStyle := ui.StyleNormal
			if navCursor {
				cursor = " ▶"
				nameStyle = ui.StyleSelected
			}

			if t.Kind == project.TestCaseXUnit {
				name := truncate(t.Name, width-6)
				marker := ui.StyleMuted.Render(" ⊕")
				lines = append(lines, cursor+" "+nameStyle.Render(name)+marker)
			} else {
				name := truncate(t.Name, width-4)
				lines = append(lines, cursor+" "+nameStyle.Render(name))
			}
		}
	}

	// ── Models ────────────────────────────────────────────────────────────
	if len(m.proj.Models) > 0 {
		lines = append(lines, "")
		lines = append(lines, ui.StyleSectionTitle.Render("  Models"))
		for i, mdl := range m.proj.Models {
			navCursor := m.section == SectionModels && m.modelCursor == i
			cursor := "  "
			style := ui.StyleNormal
			if navCursor {
				cursor = " ▶"
				style = ui.StyleSelected
			}
			name := truncate(mdl.Name, width-4)
			lines = append(lines, cursor+" "+style.Render(name))
		}
	}

	// Filter input line — shown at the bottom when filter is open but empty.
	if m.mode == ModeFilter {
		offset := m.listOffset
		if offset > len(lines) {
			offset = len(lines)
		}
		padded := padLines(lines[offset:], height-1)
		return append(padded, m.renderFilterLine())
	}

	// Normal scroll + pad.
	offset := m.listOffset
	if offset > len(lines) {
		offset = len(lines)
	}
	return padLines(lines[offset:], height)
}

// renderFilteredPanel renders the narrowed list when a filter query is active.
// Items are grouped: matching Functions, then Tests (with function headers),
// then Models. The filterCursor points to the highlighted selectable row.
func (m Model) renderFilteredPanel(width, height int) []string {
	var lines []string

	if len(m.filterResults) == 0 {
		lines = append(lines, ui.StyleError.Render("  no results for: "+m.filterText))
	} else {
		for i, r := range m.filterResults {
			if r.isHeader {
				// Non-selectable function name above its matching tests.
				lines = append(lines, ui.StyleMuted.Render("    "+truncate(r.label, width-5)))
				continue
			}
			cursor := "  "
			nameStyle := ui.StyleNormal
			if i == m.filterCursor {
				cursor = " ▶"
				nameStyle = ui.StyleSelected
			}
			switch r.section {
			case SectionFunctions:
				name := truncate(r.label, width-4)
				lines = append(lines, cursor+" "+nameStyle.Render(name))
			case SectionTests:
				name := truncate(r.label, width-6)
				suffix := ""
				if r.isXUnit {
					suffix = ui.StyleMuted.Render(" ⊕")
				}
				lines = append(lines, cursor+" "+nameStyle.Render(name)+suffix)
			case SectionModels:
				name := truncate(r.label, width-4)
				lines = append(lines, cursor+" "+nameStyle.Render(name))
			}
		}
	}

	// Pad to height-1 and add the filter input line at the bottom.
	padded := padLines(lines, height-1)
	return append(padded, m.renderFilterLine())
}

// renderFilterLine builds the / filter input shown at the bottom of the left panel.
func (m Model) renderFilterLine() string {
	prefix := ui.StyleAccent.Render("/")
	return prefix + " " + m.filterInput.View()
}

// ─── Right panel (info view) ──────────────────────────────────────────────────

func (m Model) renderRightPanel(width, height int) []string {
	var lines []string

	fn := m.selectedFunction()
	if fn == nil {
		lines = append(lines, ui.StyleMuted.Render("  No function selected"))
		lines = append(lines, "")
		k := m.keys
		lines = append(lines, ui.StyleMuted.Render("  Navigate to a function and press"))
		lines = append(lines, ui.StyleAccent.Render("  ["+k.confirm+"] to lock it and view its tests"))
		lines = append(lines, ui.StyleMuted.Render("  or press ["+k.tab+"] to jump directly to Tests"))
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

	// When browsing Functions, show a clear lock prompt instead of just handler info.
	if m.section == SectionFunctions {
		lines = append(lines, ui.StyleAccent.Render("  Handler:"))
		for _, hl := range wrapString(fn.Handler, width-3) {
			lines = append(lines, "  "+ui.StyleResult.Render(hl))
		}
		lines = append(lines, "")
		k := m.keys
		lines = append(lines, ui.StyleAccent.Render("  ["+k.confirm+"]") +
			ui.StyleMuted.Render(" lock this function → view tests"))
		lines = append(lines, ui.StyleAccent.Render("  ["+k.tab+"]") +
			ui.StyleMuted.Render(" jump to Tests section"))
		return padLines(lines, height)
	}

	switch m.section {
	case SectionTests:
		if m.testCursor < len(fn.Tests) {
			tc := fn.Tests[m.testCursor]
			if tc.Kind == project.TestCaseXUnit {
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

// ─── Output pane ─────────────────────────────────────────────────────────────

func (m Model) renderOutputPane(width, height int) []string {
	var lines []string

	statusIcon := ui.StyleSuccess.Render("✓")
	if m.lastOutputIsErr {
		statusIcon = ui.StyleError.Render("✗")
	}
	lines = append(lines, statusIcon+"  "+ui.StyleSectionTitle.Render("Output")+
		"  "+ui.StyleMuted.Render("[↑↓ scroll · any key dismiss]"))
	lines = append(lines, ui.StyleMuted.Render("  "+strings.Repeat("─", clamp(width-3, 1, width-3))))

	var allLines []string
	if m.lastStdout != "" {
		for _, l := range strings.Split(m.lastStdout, "\n") {
			allLines = append(allLines, ui.StyleResult.Render("  "+l))
		}
	}
	if m.lastStderr != "" {
		if m.lastStdout != "" {
			allLines = append(allLines, "")
		}
		allLines = append(allLines, ui.StyleError.Render("  ── stderr ──"))
		for _, l := range strings.Split(m.lastStderr, "\n") {
			allLines = append(allLines, ui.StyleError.Render("  "+l))
		}
	}
	if len(allLines) == 0 {
		allLines = append(allLines, ui.StyleMuted.Render("  (no output)"))
	}

	panelLines := height - len(lines)
	if panelLines < 1 {
		panelLines = 1
	}
	start := m.outputScroll
	if start >= len(allLines) {
		start = len(allLines) - 1
	}
	if start < 0 {
		start = 0
	}
	end := start + panelLines
	if end > len(allLines) {
		end = len(allLines)
	}
	lines = append(lines, allLines[start:end]...)

	if len(allLines) > panelLines {
		pct := 0
		if len(allLines)-panelLines > 0 {
			pct = (m.outputScroll * 100) / (len(allLines) - panelLines)
		}
		lines = append(lines, ui.StyleMuted.Render(fmt.Sprintf("  %d%%", pct)))
	}

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
		output := r.result.Stdout
		if !r.result.Success && r.result.Error != "" {
			output = r.result.Error
		}
		// Show first non-empty line as the summary.
		for _, line := range strings.Split(output, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				output = line
				break
			}
		}
		output = truncate(output, width-32)
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
	fn := m.selectedFunction()
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
		{"[" + k.confirm + "]", "Lock function and jump to its tests"},
		{"[" + k.tab + "]", "Jump to next section (Functions → Tests → Models)"},
		{"[" + k.shiftTab + "]", "Jump to previous section"},
		{"[" + k.filter + "]", "Filter / search across all sections"},
		{"[" + k.invoke + "]", "Invoke selected function / test"},
		{"[" + k.edit + "]", "Edit selected item (handler / payload / model)"},
		{"[" + k.newTest + "]", "Create a new test case"},
		{"[" + k.delete + "]", "Delete selected test / model"},
		{"[" + k.toggleAPI + "]", "Start / stop local HTTP API server"},
		{"[" + k.benchmark + "]", "Toggle benchmark pane"},
		{"[" + k.scaffold + "]", "Scaffold .lambit.toml"},
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

	// Filter active.
	if m.mode == ModeFilter {
		if m.filterText == "" {
			return ui.StyleStatusBar.Render(
				"  / type to search across all sections  [Esc]Cancel",
			) + "\n"
		}
		n := m.selectableCount()
		var matchInfo string
		if n == 0 {
			matchInfo = ui.StyleError.Render("no results")
		} else {
			matchInfo = ui.StyleSuccess.Render(fmt.Sprintf("%d result", n))
			if n != 1 {
				matchInfo = ui.StyleSuccess.Render(fmt.Sprintf("%d results", n))
			}
		}
		return ui.StyleStatusBar.Render("  Filter: "+m.filterText+"  ") +
			matchInfo +
			ui.StyleStatusBar.Render("  [↑↓]Navigate  [Enter]Go  [Esc]Clear") + "\n"
	}

	// Output pane active.
	if m.outputMode {
		return ui.StyleMuted.Render(
			"  ["+k.up+"/"+k.down+"]Scroll output  [any key]Dismiss  ["+k.invoke+"]Re-invoke",
		) + "\n"
	}

	apiStatus := ui.StyleMuted.Render("[API: OFF]")
	if m.apiServer != nil && m.apiServer.Running() {
		addr := m.apiServer.Addr()
		if m.apiCallCount > 0 {
			noun := "call"
			if m.apiCallCount != 1 {
				noun = "calls"
			}
			apiStatus = ui.StyleSuccess.Render(
				fmt.Sprintf("[API: %s · %d %s]", addr, m.apiCallCount, noun))
		} else {
			apiStatus = ui.StyleSuccess.Render("[API: " + addr + "]")
		}
	}

	// Put the most important navigation hints first so they survive truncation.
	hints := []string{
		// Selection/navigation — critical, shown first.
		"[" + k.confirm + "]Lock",
		"[" + k.tab + "]Tab",
		"[" + k.filter + "]Filter",
		// Actions.
		"[" + k.invoke + "]Invoke",
		"[" + k.edit + "]" + m.editHint(),
		"[" + k.newTest + "]New",
		"[" + k.delete + "]Del",
		// Toggles.
		apiStatus,
		"[" + k.toggleAPI + "]API",
		"[" + k.benchmark + "]Bench",
		"[" + k.options + "]Config",
		"[" + k.help + "]?",
		"[" + k.quit + "]Quit",
	}
	row := strings.Join(hints, "  ")
	if len(row) > m.width {
		row = row[:m.width-1]
	}
	return ui.StyleStatusBar.Render("  "+row) + "\n"
}

// ─── Context helpers ──────────────────────────────────────────────────────────

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
