package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/wingitman/lambit/internal/bench"
	"github.com/wingitman/lambit/internal/config"
	"github.com/wingitman/lambit/internal/invoke"
	"github.com/wingitman/lambit/internal/project"
	"github.com/wingitman/lambit/internal/runtime"
	"github.com/wingitman/lambit/internal/server"
)

// ─── Mode ─────────────────────────────────────────────────────────────────────

type Mode int

const (
	ModeNormal      Mode = iota
	ModeInvoking         // subprocess running
	ModeEdit             // context-sensitive text input (handler / payload / model)
	ModeNewTest          // two-step text input: name then payload
	ModeHelp             // keybind overlay
	ModeError            // error panel
	ModeNoProject        // no .lambit.toml found
	ModeBuildRunning     // build subprocess running
)

// InputStep tracks which field we're filling in a multi-step input form.
type InputStep int

const (
	StepName    InputStep = iota
	StepPayload InputStep = iota
)

// PanelSection identifies which section of the left list is active.
type PanelSection int

const (
	SectionFunctions PanelSection = iota
	SectionTests
	SectionModels
)

// ─── Model ────────────────────────────────────────────────────────────────────

type Model struct {
	cfg  *config.Config
	keys resolvedKeys

	width  int
	height int

	proj    *project.Project
	runtime runtime.Runtime

	section     PanelSection
	fnCursor    int
	testCursor  int
	modelCursor int

	results []invocationRecord
	bench   *bench.Bench

	mode     Mode
	errorMsg string
	buildLog string

	textInput   textinput.Model
	inputStep   InputStep
	pendingName string

	benchVisible bool
	apiServer    *server.Server
	statusMsg    string
}

type invocationRecord struct {
	label  string
	result runtime.InvokeResult
	at     time.Time
}

// ─── Resolved keybinds ────────────────────────────────────────────────────────

type resolvedKeys struct {
	up        string
	down      string
	confirm   string
	back      string
	options   string
	quit      string
	invoke    string
	newTest   string
	edit      string
	delete    string
	toggleAPI string
	benchmark string
	scaffold  string
	help      string
	pageUp    string
	pageDown  string
}

func resolveKeys(k config.Keybinds) resolvedKeys {
	return resolvedKeys{
		up:        k.Up,
		down:      k.Down,
		confirm:   k.Confirm,
		back:      k.Back,
		options:   k.Options,
		quit:      k.Quit,
		invoke:    k.Invoke,
		newTest:   k.NewTest,
		edit:      k.Edit,
		delete:    k.Delete,
		toggleAPI: k.ToggleAPI,
		benchmark: k.Benchmark,
		scaffold:  k.Scaffold,
		help:      k.Help,
		pageUp:    k.PageUp,
		pageDown:  k.PageDown,
	}
}

// ─── Constructor ─────────────────────────────────────────────────────────────

func New(cfg *config.Config, projectDir string) (*Model, error) {
	if projectDir == "" {
		var err error
		projectDir, err = os.Getwd()
		if err != nil {
			projectDir = "."
		}
	}

	ti := textinput.New()
	ti.CharLimit = 4096

	m := &Model{
		cfg:   cfg,
		keys:  resolveKeys(cfg.Keybinds),
		bench: bench.New(),
	}
	m.textInput = ti

	proj, err := project.Load(projectDir)
	if err != nil {
		m.mode = ModeNoProject
		m.proj = &project.Project{Path: projectDir}
		return m, nil
	}
	m.proj = proj

	var rt runtime.Runtime
	if proj.Runtime != "" {
		rt = runtime.ByName(proj.Runtime)
	}
	if rt == nil {
		rt = runtime.Detect(proj.Path)
	}
	m.runtime = rt

	if len(proj.Functions) == 0 && rt != nil {
		fns, _ := rt.Scan(proj.Path)
		m.proj.Functions = fns
	}

	// Merge discovered xUnit tests (in-memory only, never persisted).
	m.mergeDiscoveredTests()

	port := proj.APIPort
	if port == 0 {
		port = project.DefaultAPIPort
	}
	m.apiServer = server.New(port, m.handleAPIInvoke)

	return m, nil
}

// mergeDiscoveredTests calls ScanTests on the runtime (if it implements
// runtime.TestScanner) and prepends discovered xUnit tests to each function's
// in-memory test list. These are never written to .lambit.toml.
func (m *Model) mergeDiscoveredTests() {
	scanner, ok := m.runtime.(runtime.TestScanner)
	if !ok {
		return
	}
	for i := range m.proj.Functions {
		discovered := scanner.ScanTests(m.proj.Path, m.proj.Functions[i])
		if len(discovered) == 0 {
			continue
		}
		// Prepend discovered tests, keep user-defined tests after them.
		existing := m.proj.Functions[i].Tests
		m.proj.Functions[i].Tests = append(discovered, existing...)
	}
}

// handleAPIInvoke is the callback used by the API server.
func (m *Model) handleAPIInvoke(functionName, payload string) (string, bool) {
	if m.runtime == nil {
		return `{"error":"no runtime detected"}`, false
	}
	fn := m.functionByName(functionName)
	if fn == nil {
		return fmt.Sprintf(`{"error":"function %q not found"}`, functionName), false
	}
	args := m.runtime.InvokeArgs(m.proj.Path, *fn, payload)
	res := invoke.Run(invoke.Request{Args: args, ProjectRoot: m.proj.Path})
	result := m.runtime.ParseResult(res.Stdout, res.Stderr, res.Duration)
	if !result.Success {
		return result.Error, false
	}
	return result.Stdout, true
}

func (m *Model) functionByName(name string) *project.Function {
	for i := range m.proj.Functions {
		if strings.EqualFold(m.proj.Functions[i].Name, name) {
			return &m.proj.Functions[i]
		}
	}
	return nil
}

// ─── Tea interface ────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd { return nil }

// ─── Message types ────────────────────────────────────────────────────────────

type invokeResultMsg struct{ record invocationRecord }
type buildDoneMsg struct {
	log string
	err error
}
type scaffoldReloadMsg struct{ dir string }
type clearStatusMsg struct{}

// ─── Update ──────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case clearStatusMsg:
		m.statusMsg = ""
		return m, nil

	case buildDoneMsg:
		m.buildLog = msg.log
		if msg.err != nil {
			m.mode = ModeError
			m.errorMsg = "Build failed:\n" + msg.log
			return m, nil
		}
		return m, m.runInvoke()

	case invokeResultMsg:
		m.results = append(m.results, msg.record)
		if len(m.results) > 50 {
			m.results = m.results[1:]
		}
		m.bench.Add(msg.record.label, msg.record.result.Duration, msg.record.result.Success)
		m.mode = ModeNormal
		return m, nil

	case scaffoldReloadMsg:
		return m.handleScaffoldReload(msg.dir)

	case tea.KeyMsg:
		return m.handleKey(msg.String())
	}

	if m.mode == ModeEdit || m.mode == ModeNewTest {
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

// handleScaffoldReload is called after the editor exits post-scaffold.
func (m Model) handleScaffoldReload(dir string) (tea.Model, tea.Cmd) {
	proj, err := project.Load(dir)
	if err != nil {
		m.mode = ModeError
		m.errorMsg = "Could not load .lambit.toml after scaffold:\n" + err.Error() +
			"\n\nFix the file and press [" + m.keys.scaffold + "] to try again."
		return m, nil
	}
	m.proj = proj

	var rt runtime.Runtime
	if proj.Runtime != "" {
		rt = runtime.ByName(proj.Runtime)
	}
	if rt == nil {
		rt = runtime.Detect(proj.Path)
	}
	m.runtime = rt
	if len(proj.Functions) == 0 && rt != nil {
		fns, _ := rt.Scan(proj.Path)
		m.proj.Functions = fns
	}

	m.mergeDiscoveredTests()

	if m.apiServer != nil {
		m.apiServer.Stop()
	}
	m.apiServer = server.New(proj.APIPort, m.handleAPIInvoke)

	m.section = SectionFunctions
	m.fnCursor = 0
	m.testCursor = 0
	m.modelCursor = 0

	m.mode = ModeNormal
	m.statusMsg = "Project loaded"
	return m, tea.Tick(2*time.Second, func(_ time.Time) tea.Msg { return clearStatusMsg{} })
}

func (m Model) handleKey(key string) (tea.Model, tea.Cmd) {
	if m.mode == ModeError {
		m.mode = ModeNormal
		m.errorMsg = ""
		return m, nil
	}

	if m.mode == ModeNoProject {
		switch {
		case matchKey(key, m.keys.scaffold):
			return m.doScaffold()
		case matchKey(key, m.keys.quit) || key == "ctrl+c":
			return m, tea.Quit
		}
		return m, nil
	}

	if m.mode == ModeHelp {
		m.mode = ModeNormal
		return m, nil
	}

	if m.mode == ModeBuildRunning || m.mode == ModeInvoking {
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil
	}

	if m.mode == ModeEdit {
		switch key {
		case "enter":
			return m.submitEdit()
		case "esc":
			m.mode = ModeNormal
			m.textInput.Blur()
		default:
			var cmd tea.Cmd
			m.textInput, cmd = m.textInput.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
			return m, cmd
		}
		return m, nil
	}

	if m.mode == ModeNewTest {
		switch key {
		case "enter":
			return m.submitNewTest()
		case "esc":
			m.mode = ModeNormal
			m.textInput.Blur()
		default:
			var cmd tea.Cmd
			m.textInput, cmd = m.textInput.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
			return m, cmd
		}
		return m, nil
	}

	switch {
	case matchKey(key, m.keys.quit) || key == "ctrl+c":
		if m.apiServer != nil {
			m.apiServer.Stop()
		}
		return m, tea.Quit

	case matchKey(key, m.keys.help):
		m.mode = ModeHelp
		return m, nil

	case matchKey(key, m.keys.options):
		return m, m.openEditor(config.ConfigPath())

	case matchKey(key, m.keys.benchmark):
		m.benchVisible = !m.benchVisible
		return m, nil

	case matchKey(key, m.keys.toggleAPI):
		return m.toggleAPI()

	case matchKey(key, m.keys.up):
		m.moveCursorUp()
		return m, nil

	case matchKey(key, m.keys.down):
		m.moveCursorDown()
		return m, nil

	case matchKey(key, m.keys.pageUp):
		for i := 0; i < 5; i++ {
			m.moveCursorUp()
		}
		return m, nil

	case matchKey(key, m.keys.pageDown):
		for i := 0; i < 5; i++ {
			m.moveCursorDown()
		}
		return m, nil

	case matchKey(key, m.keys.invoke):
		return m.doInvoke()

	case matchKey(key, m.keys.edit):
		return m.openEdit()

	case matchKey(key, m.keys.newTest):
		return m.openNewTestForm()

	case matchKey(key, m.keys.delete):
		return m.doDelete()

	case matchKey(key, m.keys.scaffold):
		return m.doScaffold()
	}

	return m, nil
}

// ─── Cursor movement ──────────────────────────────────────────────────────────

func (m *Model) moveCursorUp() {
	switch m.section {
	case SectionFunctions:
		if m.fnCursor > 0 {
			m.fnCursor--
		} else if len(m.proj.Models) > 0 {
			m.section = SectionModels
			m.modelCursor = len(m.proj.Models) - 1
		}
	case SectionTests:
		fn := m.currentFunction()
		if fn != nil && m.testCursor > 0 {
			m.testCursor--
		} else {
			m.section = SectionFunctions
		}
	case SectionModels:
		if m.modelCursor > 0 {
			m.modelCursor--
		} else {
			m.section = SectionTests
		}
	}
}

func (m *Model) moveCursorDown() {
	switch m.section {
	case SectionFunctions:
		if m.fnCursor < len(m.proj.Functions)-1 {
			m.fnCursor++
		} else {
			fn := m.currentFunction()
			if fn != nil && len(fn.Tests) > 0 {
				m.section = SectionTests
				m.testCursor = 0
			} else if len(m.proj.Models) > 0 {
				m.section = SectionModels
				m.modelCursor = 0
			}
		}
	case SectionTests:
		fn := m.currentFunction()
		if fn != nil && m.testCursor < len(fn.Tests)-1 {
			m.testCursor++
		} else if len(m.proj.Models) > 0 {
			m.section = SectionModels
			m.modelCursor = 0
		}
	case SectionModels:
		if m.modelCursor < len(m.proj.Models)-1 {
			m.modelCursor++
		}
	}
}

func (m *Model) currentFunction() *project.Function {
	if m.proj == nil || len(m.proj.Functions) == 0 {
		return nil
	}
	if m.fnCursor >= len(m.proj.Functions) {
		return nil
	}
	return &m.proj.Functions[m.fnCursor]
}

func (m *Model) currentPayload() string {
	fn := m.currentFunction()
	if fn == nil {
		return "{}"
	}
	switch m.section {
	case SectionTests:
		if m.testCursor < len(fn.Tests) {
			return fn.Tests[m.testCursor].Payload
		}
	case SectionModels:
		if m.modelCursor < len(m.proj.Models) {
			return m.proj.Models[m.modelCursor].JSON
		}
	}
	return "{}"
}

// currentTestCase returns the test case at the cursor, or nil.
func (m *Model) currentTestCase() *project.TestCase {
	fn := m.currentFunction()
	if fn == nil || m.section != SectionTests {
		return nil
	}
	if m.testCursor >= len(fn.Tests) {
		return nil
	}
	return &fn.Tests[m.testCursor]
}

// ─── Actions ──────────────────────────────────────────────────────────────────

func (m Model) doInvoke() (tea.Model, tea.Cmd) {
	// Route xUnit test cases through the test runner path.
	if m.section == SectionTests {
		if tc := m.currentTestCase(); tc != nil && tc.Kind == project.TestCaseXUnit {
			return m.doInvokeXUnit(*tc)
		}
	}

	if m.runtime == nil {
		m.mode = ModeError
		m.errorMsg = "No runtime detected.\nPress [" + m.keys.scaffold + "] to scaffold a .lambit.toml."
		return m, nil
	}
	fn := m.currentFunction()
	if fn == nil {
		m.mode = ModeError
		m.errorMsg = "No function selected."
		return m, nil
	}

	buildArgs := m.runtime.BuildArgs(m.proj.Path)
	if len(buildArgs) > 0 {
		m.mode = ModeBuildRunning
		projectPath := m.proj.Path
		return m, func() tea.Msg {
			log, err := invoke.Build(projectPath, buildArgs)
			return buildDoneMsg{log: log, err: err}
		}
	}
	m.mode = ModeInvoking
	return m, m.runInvoke()
}

// doInvokeXUnit runs a single xUnit test case via dotnet test.
func (m Model) doInvokeXUnit(tc project.TestCase) (tea.Model, tea.Cmd) {
	ts, ok := m.runtime.(runtime.TestScanner)
	if !ok {
		m.mode = ModeError
		m.errorMsg = "This runtime does not support test invocation."
		return m, nil
	}
	args := ts.InvokeTestArgs(m.proj.Path, tc)
	if len(args) == 0 {
		m.mode = ModeError
		m.errorMsg = "Could not find test project for:\n" + tc.Filter
		return m, nil
	}
	m.mode = ModeInvoking
	label := tc.Name
	projectPath := m.proj.Path
	return m, func() tea.Msg {
		res := invoke.Run(invoke.Request{Args: args, ProjectRoot: projectPath})
		result := ts.ParseTestResult(res.Stdout, res.Stderr, res.Duration)
		record := invocationRecord{label: label, result: result, at: time.Now()}
		return invokeResultMsg{record: record}
	}
}

func (m *Model) runInvoke() tea.Cmd {
	fn := m.currentFunction()
	if fn == nil {
		return nil
	}
	payload := m.currentPayload()
	args := m.runtime.InvokeArgs(m.proj.Path, *fn, payload)
	label := fn.Name
	if m.section == SectionTests && m.testCursor < len(fn.Tests) {
		label = fn.Tests[m.testCursor].Name
	}
	projectPath := m.proj.Path
	rt := m.runtime
	return func() tea.Msg {
		res := invoke.Run(invoke.Request{Args: args, ProjectRoot: projectPath})
		record := invocationRecord{
			label:  label,
			result: rt.ParseResult(res.Stdout, res.Stderr, res.Duration),
			at:     time.Now(),
		}
		return invokeResultMsg{record: record}
	}
}

// openEdit opens the context-sensitive edit overlay.
// Disabled for xUnit test cases (they are read-only source-derived entries).
func (m Model) openEdit() (tea.Model, tea.Cmd) {
	switch m.section {
	case SectionFunctions:
		fn := m.currentFunction()
		if fn == nil {
			return m, nil
		}
		m.textInput.Reset()
		m.textInput.Placeholder = "Assembly::Namespace.Class::Method"
		m.textInput.SetValue(fn.Handler)
		m.textInput.Focus()
		m.mode = ModeEdit
		return m, textinput.Blink

	case SectionTests:
		tc := m.currentTestCase()
		if tc == nil {
			return m, nil
		}
		if tc.Kind == project.TestCaseXUnit {
			// xUnit tests are read-only — editing is not applicable.
			return m, nil
		}
		fn := m.currentFunction()
		if fn == nil {
			return m, nil
		}
		m.textInput.Reset()
		m.textInput.Placeholder = "JSON payload"
		m.textInput.SetValue(m.currentPayload())
		m.textInput.Focus()
		m.mode = ModeEdit
		return m, textinput.Blink

	case SectionModels:
		if m.modelCursor >= len(m.proj.Models) {
			return m, nil
		}
		m.textInput.Reset()
		m.textInput.Placeholder = "JSON"
		m.textInput.SetValue(m.proj.Models[m.modelCursor].JSON)
		m.textInput.Focus()
		m.mode = ModeEdit
		return m, textinput.Blink
	}
	return m, nil
}

func (m Model) submitEdit() (tea.Model, tea.Cmd) {
	val := strings.TrimSpace(m.textInput.Value())
	m.textInput.Blur()
	m.mode = ModeNormal
	if val == "" {
		return m, nil
	}

	switch m.section {
	case SectionFunctions:
		fn := m.currentFunction()
		if fn == nil {
			return m, nil
		}
		oldHandler := fn.Handler
		m.proj.Functions[m.fnCursor].Handler = val
		// Auto-update display name if it still matches the old method segment.
		oldParts := strings.Split(oldHandler, "::")
		newParts := strings.Split(val, "::")
		if len(oldParts) == 3 && len(newParts) == 3 {
			if m.proj.Functions[m.fnCursor].Name == oldParts[2] {
				m.proj.Functions[m.fnCursor].Name = newParts[2]
			}
		}
		_ = project.Save(m.proj)

	case SectionTests:
		fn := m.currentFunction()
		if fn == nil {
			return m, nil
		}
		if m.testCursor < len(fn.Tests) && fn.Tests[m.testCursor].Kind != project.TestCaseXUnit {
			m.proj.Functions[m.fnCursor].Tests[m.testCursor].Payload = val
			_ = project.Save(m.proj)
		}

	case SectionModels:
		if m.modelCursor < len(m.proj.Models) {
			m.proj.Models[m.modelCursor].JSON = val
			_ = project.Save(m.proj)
		}
	}
	return m, nil
}

func (m Model) openNewTestForm() (tea.Model, tea.Cmd) {
	if m.currentFunction() == nil {
		return m, nil
	}
	m.inputStep = StepName
	m.pendingName = ""
	m.textInput.Reset()
	m.textInput.Placeholder = "test name"
	m.textInput.SetValue("")
	m.textInput.Focus()
	m.mode = ModeNewTest
	return m, textinput.Blink
}

func (m Model) submitNewTest() (tea.Model, tea.Cmd) {
	val := strings.TrimSpace(m.textInput.Value())
	if val == "" {
		m.textInput.Blur()
		m.mode = ModeNormal
		return m, nil
	}
	if m.inputStep == StepName {
		m.pendingName = val
		m.inputStep = StepPayload
		m.textInput.Reset()
		m.textInput.Placeholder = "JSON payload"
		m.textInput.SetValue("{}")
		return m, textinput.Blink
	}
	m.textInput.Blur()
	m.mode = ModeNormal
	fn := m.currentFunction()
	if fn == nil {
		return m, nil
	}
	m.proj.Functions[m.fnCursor].Tests = append(m.proj.Functions[m.fnCursor].Tests,
		project.TestCase{Name: m.pendingName, Payload: val})
	_ = project.Save(m.proj)
	m.section = SectionTests
	m.testCursor = len(m.proj.Functions[m.fnCursor].Tests) - 1
	return m, nil
}

func (m Model) doDelete() (tea.Model, tea.Cmd) {
	switch m.section {
	case SectionTests:
		fn := m.currentFunction()
		if fn == nil || len(fn.Tests) == 0 {
			return m, nil
		}
		tc := fn.Tests[m.testCursor]
		if tc.Kind == project.TestCaseXUnit {
			// Can't delete source-derived tests — they'll re-appear on next scan.
			m.statusMsg = "Cannot delete auto-discovered tests"
			return m, tea.Tick(2*time.Second, func(_ time.Time) tea.Msg { return clearStatusMsg{} })
		}
		tests := fn.Tests
		if m.testCursor >= len(tests) {
			return m, nil
		}
		m.proj.Functions[m.fnCursor].Tests = append(tests[:m.testCursor], tests[m.testCursor+1:]...)
		if m.testCursor >= len(m.proj.Functions[m.fnCursor].Tests) && m.testCursor > 0 {
			m.testCursor--
		}
		_ = project.Save(m.proj)

	case SectionModels:
		if m.modelCursor >= len(m.proj.Models) {
			return m, nil
		}
		m.proj.Models = append(m.proj.Models[:m.modelCursor], m.proj.Models[m.modelCursor+1:]...)
		if m.modelCursor >= len(m.proj.Models) && m.modelCursor > 0 {
			m.modelCursor--
		}
		_ = project.Save(m.proj)
	}
	return m, nil
}

// doScaffold detects runtime, scans for functions, writes .lambit.toml,
// then opens it in $EDITOR. On editor close, scaffoldReloadMsg triggers a
// full reload and transitions to ModeNormal (or ModeError on bad TOML).
func (m Model) doScaffold() (tea.Model, tea.Cmd) {
	dir := m.proj.Path

	var detected []project.Function
	rt := runtime.Detect(dir)
	if rt != nil {
		detected, _ = rt.Scan(dir)
	}

	if err := project.Scaffold(dir, detected); err != nil {
		m.mode = ModeError
		m.errorMsg = err.Error()
		return m, nil
	}

	projFilePath := filepath.Join(dir, project.ProjectFile)
	return m, tea.Sequence(
		m.openEditor(projFilePath),
		func() tea.Msg { return scaffoldReloadMsg{dir: dir} },
	)
}

func (m Model) toggleAPI() (tea.Model, tea.Cmd) {
	if m.apiServer == nil {
		return m, nil
	}
	if m.apiServer.Running() {
		m.apiServer.Stop()
		m.statusMsg = "API server stopped"
	} else {
		if err := m.apiServer.Start(); err != nil {
			m.mode = ModeError
			m.errorMsg = "Could not start API server: " + err.Error()
			return m, nil
		}
		m.statusMsg = "API server started at " + m.apiServer.Addr()
	}
	return m, tea.Tick(2*time.Second, func(_ time.Time) tea.Msg { return clearStatusMsg{} })
}

// ─── Editor ───────────────────────────────────────────────────────────────────

func (m Model) openEditor(path string) tea.Cmd {
	editor := m.cfg.Apps.Editor
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		for _, e := range []string{"nano", "vi", "vim", "nvim", "code", "notepad.exe"} {
			if _, err := exec.LookPath(e); err == nil {
				editor = e
				break
			}
		}
	}
	if editor == "" {
		return func() tea.Msg {
			return fmt.Sprintf("No editor found. Set $EDITOR or apps.editor in %s", config.ConfigPath())
		}
	}
	c := exec.Command(editor, path)
	return tea.ExecProcess(c, func(err error) tea.Msg { return nil })
}

func matchKey(pressed, binding string) bool { return pressed == binding }
