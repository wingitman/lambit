package app

import (
	"fmt"
	"strconv"
	"os"
	"os/exec"
	"path/filepath"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/wingitman/lambit/internal/bench"
	"github.com/wingitman/lambit/internal/config"
	"github.com/wingitman/lambit/internal/invoke"
	"github.com/wingitman/lambit/internal/project"
	"github.com/wingitman/lambit/internal/runtime"
	"github.com/atotto/clipboard"
	"github.com/wingitman/lambit/internal/server"
)

// ─── API shared state ─────────────────────────────────────────────────────────

// apiSharedState holds the data that the HTTP server goroutine reads when
// handling incoming API requests. It is written by the BubbleTea goroutine
// (under mu) and read by the HTTP goroutine (under mu.RLock).
// prog is set once from main.go after tea.NewProgram is called.
type apiSharedState struct {
	mu   sync.RWMutex
	proj *project.Project
	rt   runtime.Runtime
	prog atomic.Pointer[tea.Program]
}

func (s *apiSharedState) set(proj *project.Project, rt runtime.Runtime) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.proj = proj
	s.rt = rt
}

// ─── Mode ─────────────────────────────────────────────────────────────────────

type Mode int

const (
	ModeNormal      Mode = iota
	ModeInvoking         // subprocess running
	ModeEdit             // context-sensitive text input
	ModeNewTest          // two-step text input: name then payload
	ModeFilter           // live filter / search
	ModeHelp             // keybind overlay
	ModeError            // error panel
	ModeNoProject        // no .lambit.toml found
	ModeBuildRunning     // build subprocess running
)

type InputStep int

const (
	StepName    InputStep = iota
	StepPayload InputStep = iota
)

type PanelSection int

const (
	SectionFunctions PanelSection = iota
	SectionTests
	SectionModels
)

// ─── Filter result ────────────────────────────────────────────────────────────

// filterResult is one row in the live-filtered list.
type filterResult struct {
	section  PanelSection
	fnIdx    int  // index into proj.Functions
	testIdx  int  // index into fn.Tests; -1 when not a test row
	modelIdx int  // index into proj.Models; -1 when not a model row
	label    string
	isHeader bool // non-selectable function-name header row above its tests
	isXUnit  bool // display the ⊕ marker
}

// ─── Model ────────────────────────────────────────────────────────────────────

type Model struct {
	cfg  *config.Config
	keys resolvedKeys

	width  int
	height int

	proj    *project.Project
	runtime runtime.Runtime

	// Navigation cursor.
	section     PanelSection
	fnCursor    int
	testCursor  int
	modelCursor int

	// selectedFnIdx is the confirmed/locked function index.
	// fnLocked is true only after an explicit lock action (Enter / Tab / down
	// past last function).  While fnLocked is false, selectedFunction() follows
	// fnCursor so the Tests section previews the hovered function live.
	// Moving fnCursor to a different function while in SectionFunctions releases
	// the lock automatically.
	selectedFnIdx int
	fnLocked      bool

	// Left-panel scroll offset.
	listOffset int

	results []invocationRecord
	bench   *bench.Bench

	mode     Mode
	errorMsg string
	buildLog string

	// Text input for edit / new-test overlays.
	textInput   textinput.Model
	inputStep   InputStep
	pendingName string

	// Filter / search state.
	filterInput   textinput.Model
	filterText    string
	filterResults []filterResult // computed on every keystroke
	filterCursor  int            // index into filterResults (skips headers)

	benchVisible bool
	apiServer    *server.Server
	statusMsg    string
	apiCallCount int // number of requests handled by the API server this session

	// Shared state read by the HTTP server goroutine.
	shared *apiSharedState

	// Last invocation output.
	lastStdout      string
	lastStderr      string
	lastOutputIsErr bool
	outputMode      bool
	outputScroll    int
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
	tab       string
	shiftTab  string
	filter      string
	copy        string
	copyCurl    string
	gotoSource  string
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
		tab:       k.Tab,
		shiftTab:  k.ShiftTab,
		filter:     k.Filter,
		copy:       k.Copy,
		copyCurl:   k.CopyCurl,
		gotoSource: k.GotoSource,
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

	fi := textinput.New()
	fi.Placeholder = "type to search…"
	fi.CharLimit = 80

	m := &Model{
		shared: &apiSharedState{},
		cfg:         cfg,
		keys:        resolveKeys(cfg.Keybinds),
		bench:       bench.New(),
		textInput:   ti,
		filterInput: fi,
	}

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

	m.mergeDiscoveredTests()

	// Initialise the shared state used by the HTTP server goroutine.
	m.shared = &apiSharedState{}
	m.shared.set(m.proj, m.runtime)

	port := proj.APIPort
	if port == 0 {
		port = project.DefaultAPIPort
	}
	m.apiServer = server.New(port, m.apiInvoke)

	return m, nil
}

// SetProgram stores the tea.Program reference so the HTTP server goroutine can
// send results into the BubbleTea event loop. Call this from main.go immediately
// after tea.NewProgram and before p.Run().
func (m *Model) SetProgram(p *tea.Program) {
	m.shared.prog.Store(p)
}

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
		existing := m.proj.Functions[i].Tests
		m.proj.Functions[i].Tests = append(discovered, existing...)
	}
}

// apiInvoke is the server.InvokeFn callback.  It runs in the HTTP server
// goroutine, reads from shared state (safe under RLock), invokes the lambda
// subprocess, and sends a silent apiResultMsg into the BubbleTea event loop.
func (m *Model) apiInvoke(functionName, payload string) (string, bool) {
	m.shared.mu.RLock()
	proj := m.shared.proj
	rt := m.shared.rt
	m.shared.mu.RUnlock()

	if rt == nil || proj == nil {
		return `{"error":"no runtime detected"}`, false
	}

	// Two-segment path: POST /FunctionName/TestCaseName
	// Routes to xUnit test runner or payload-test invocation.
	if idx := strings.Index(functionName, "/"); idx >= 0 {
		fnName := functionName[:idx]
		tcName, _ := url.PathUnescape(functionName[idx+1:])
		fn := functionByNameIn(proj, fnName)
		if fn == nil {
			return fmt.Sprintf(`{"error":"function %q not found"}`, fnName), false
		}
		tc := testCaseByNameIn(fn, tcName)
		if tc == nil {
			return fmt.Sprintf(`{"error":"test case %q not found in %s"}`, tcName, fnName), false
		}
		return m.apiInvokeTestCase(rt, proj, fn, tc)
	}

	fn := functionByNameIn(proj, functionName)
	if fn == nil {
		return fmt.Sprintf(`{"error":"function %q not found"}`, functionName), false
	}

	// Inject --no-build so dotnet run does not print build diagnostics to
	// stdout, which would contaminate the JSON response body.
	args := rt.InvokeArgs(proj.Path, *fn, payload)
	args = injectNoBuild(args)

	res := invoke.Run(invoke.Request{Args: args, ProjectRoot: proj.Path})
	result := rt.ParseResult(res.Stdout, res.Stderr, res.Duration)

	// Send the result into BubbleTea silently — updates results bar and
	// benchmark without opening the output pane or interrupting the user.
	m.apiSendResult("[API] "+fn.Name, result)

	if !result.Success {
		return result.Error, false
	}
	return result.Stdout, true
}

// testCaseByNameIn finds a test case by name (case-insensitive) in fn.Tests.
func testCaseByNameIn(fn *project.Function, name string) *project.TestCase {
	for i := range fn.Tests {
		if strings.EqualFold(fn.Tests[i].Name, name) {
			return &fn.Tests[i]
		}
	}
	return nil
}

// apiInvokeTestCase invokes a single test case via the API server goroutine.
// For xUnit tests it runs dotnet test --filter; for payload tests it invokes
// the lambda shim with the test's own payload (ignoring the HTTP body).
func (m *Model) apiInvokeTestCase(rt runtime.Runtime, proj *project.Project, fn *project.Function, tc *project.TestCase) (string, bool) {
	var stdout, stderr string
	var dur time.Duration
	var success bool

	if tc.Kind == project.TestCaseXUnit {
		ts, ok := rt.(runtime.TestScanner)
		if !ok {
			return `{"error":"runtime does not support xUnit test invocation"}`, false
		}
		args := ts.InvokeTestArgs(proj.Path, *tc)
		if len(args) == 0 {
			return fmt.Sprintf(`{"error":"test project not found for %q"}`, tc.Filter), false
		}
		res := invoke.Run(invoke.Request{Args: args, ProjectRoot: proj.Path})
		result := ts.ParseTestResult(res.Stdout, res.Stderr, res.Duration)
		stdout, stderr, dur, success = result.Stdout, result.Stderr, result.Duration, result.Success
	} else {
		// Payload test — invoke the lambda with the test's payload.
		payload := tc.Payload
		if payload == "" {
			payload = "{}"
		}
		args := rt.InvokeArgs(proj.Path, *fn, payload)
		args = injectNoBuild(args)
		res := invoke.Run(invoke.Request{Args: args, ProjectRoot: proj.Path})
		result := rt.ParseResult(res.Stdout, res.Stderr, res.Duration)
		stdout, stderr, dur, success = result.Stdout, result.Stderr, result.Duration, result.Success
	}

	fullResult := runtime.InvokeResult{
		Stdout: stdout, Stderr: stderr, Duration: dur, Success: success,
	}
	if !success {
		fullResult.Error = stderr
		if fullResult.Error == "" {
			fullResult.Error = stdout
		}
	}
	m.apiSendResult("[API] "+fn.Name+"/"+tc.Name, fullResult)

	if !success {
		return fullResult.Error, false
	}
	return stdout, true
}

// apiSendResult fires a silent apiResultMsg into the BubbleTea event loop.
func (m *Model) apiSendResult(label string, result runtime.InvokeResult) {
	if prog := m.shared.prog.Load(); prog != nil {
		record := invocationRecord{label: label, result: result, at: time.Now()}
		prog.Send(apiResultMsg{record: record})
	}
}

// functionByNameIn looks up a function by name (case-insensitive) or by the
// method-name segment of its handler string, so both "/Greet" and
// "/GreetFunction" resolve correctly regardless of how the function is named
// in .lambit.toml.
func functionByNameIn(proj *project.Project, name string) *project.Function {
	for i := range proj.Functions {
		fn := &proj.Functions[i]
		if strings.EqualFold(fn.Name, name) {
			return fn
		}
		// Also match against the method segment of the handler string
		// (e.g. "GreetingFunction::GreetingFunction.Function::Greet" → "Greet").
		parts := strings.Split(fn.Handler, "::")
		if len(parts) == 3 && strings.EqualFold(parts[2], name) {
			return fn
		}
		// Match against the Node.js export name (e.g. "index.transform" → "transform").
		if idx := strings.LastIndex(fn.Handler, "."); idx >= 0 {
			if strings.EqualFold(fn.Handler[idx+1:], name) {
				return fn
			}
		}
	}
	return nil
}

// injectNoBuild inserts "--no-build" before the "--" argument separator in
// dotnet-run commands, preventing build output from polluting stdout.
// It is a no-op for any other command.
func injectNoBuild(args []string) []string {
	if len(args) < 2 || args[0] != "dotnet" || args[1] != "run" {
		return args
	}
	out := make([]string, 0, len(args)+1)
	for _, a := range args {
		if a == "--" {
			out = append(out, "--no-build")
		}
		out = append(out, a)
	}
	return out
}

// ─── Tea interface ────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd { return nil }

// ─── Message types ────────────────────────────────────────────────────────────

type invokeResultMsg struct{ record invocationRecord }
type apiResultMsg struct{ record invocationRecord } // sent from HTTP server goroutine
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
			errMsg := "Build failed:\n" + msg.log
			// Add a helpful hint for the common "no project file" MSBuild error.
			if strings.Contains(msg.log, "MSB1003") || strings.Contains(msg.log, "Specify a project or solution file") {
				errMsg += "\n\nHint: lambit could not find a .csproj for this function.\n" +
					"Check that the handler string (Assembly::Namespace.Class::Method)\n" +
					"matches the assembly name in your .csproj, then press [" + m.keys.invoke + "] again."
			}
			m.mode = ModeError
			m.errorMsg = errMsg
			return m, nil
		}
		return m, m.runInvoke()

	case invokeResultMsg:
		m.results = append(m.results, msg.record)
		if len(m.results) > 50 {
			m.results = m.results[1:]
		}
		m.bench.Add(msg.record.label, msg.record.result.Duration, msg.record.result.Success)
		m.lastStdout = strings.TrimSpace(msg.record.result.Stdout)
		m.lastStderr = strings.TrimSpace(msg.record.result.Stderr)
		m.lastOutputIsErr = !msg.record.result.Success
		m.outputMode = true
		m.outputScroll = 0
		m.mode = ModeNormal
		return m, nil

	case apiResultMsg:
		// Silent result from the HTTP API server — update results bar and
		// benchmark but do NOT open the output pane or interrupt the user.
		m.results = append(m.results, msg.record)
		if len(m.results) > 50 {
			m.results = m.results[1:]
		}
		m.bench.Add(msg.record.label, msg.record.result.Duration, msg.record.result.Success)
		m.apiCallCount++
		return m, nil

	case scaffoldReloadMsg:
		return m.handleScaffoldReload(msg.dir)

	case tea.KeyMsg:
		return m.handleKey(msg.String())
	}

	switch m.mode {
	case ModeEdit, ModeNewTest:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	case ModeFilter:
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

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

	// Update shared state so the API server goroutine sees the new project.
	m.shared.set(m.proj, m.runtime)

	if m.apiServer != nil {
		m.apiServer.Stop()
	}
	m.apiServer = server.New(proj.APIPort, m.apiInvoke)

	m.section = SectionFunctions
	m.fnCursor = 0
	m.selectedFnIdx = 0
	m.fnLocked = false
	m.testCursor = 0
	m.modelCursor = 0
	m.listOffset = 0
	m.outputMode = false
	m.filterText = ""
	m.filterResults = nil
	m.apiCallCount = 0

	m.mode = ModeNormal
	m.statusMsg = "Project loaded"
	return m, tea.Tick(2*time.Second, func(_ time.Time) tea.Msg { return clearStatusMsg{} })
}

// ─── Key handling ─────────────────────────────────────────────────────────────

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
			m.textInput, cmd = m.textInput.Update(
				tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
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
			m.textInput, cmd = m.textInput.Update(
				tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
			return m, cmd
		}
		return m, nil
	}

	// ── Filter mode ───────────────────────────────────────────────────────
	if m.mode == ModeFilter {
		switch key {
		case "enter":
			m.applyFilterSelection()
			m.clearFilter()
			m.mode = ModeNormal
		case "esc":
			m.clearFilter()
			m.mode = ModeNormal
		case "up", m.keys.up:
			m.moveCursorInFilterResults(-1)
		case "down", m.keys.down:
			m.moveCursorInFilterResults(+1)
		default:
			var cmd tea.Cmd
			m.filterInput, cmd = m.filterInput.Update(
				tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
			m.filterText = m.filterInput.Value()
			m.computeFilterResults()
			m.filterCursor = m.firstSelectableIdx()
			return m, cmd
		}
		return m, nil
	}

	// ── Output pane scroll ────────────────────────────────────────────────
	if m.outputMode {
		panelH := m.outputPaneHeight()
		outLines := m.outputLineCount()
		maxScroll := outLines - panelH
		if maxScroll < 0 {
			maxScroll = 0
		}
		switch {
		case matchKey(key, m.keys.up):
			if m.outputScroll > 0 {
				m.outputScroll--
			}
			return m, nil
		case matchKey(key, m.keys.down):
			if m.outputScroll < maxScroll {
				m.outputScroll++
			}
			return m, nil
		case matchKey(key, m.keys.pageUp):
			m.outputScroll -= panelH
			if m.outputScroll < 0 {
				m.outputScroll = 0
			}
			return m, nil
		case matchKey(key, m.keys.pageDown):
			m.outputScroll += panelH
			if m.outputScroll > maxScroll {
				m.outputScroll = maxScroll
			}
			return m, nil
		default:
			m.outputMode = false
		}
	}

	// ── Normal mode ───────────────────────────────────────────────────────
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

	case matchKey(key, m.keys.tab):
		m.jumpSection(+1)
		return m, nil

	case matchKey(key, m.keys.shiftTab):
		m.jumpSection(-1)
		return m, nil

	case matchKey(key, m.keys.confirm):
		if m.section == SectionFunctions {
			m.lockFunction(m.fnCursor)
			m.section = SectionTests
			m.testCursor = 0
			m.outputMode = false
			m.adjustListScroll()
		}
		return m, nil

	case matchKey(key, m.keys.filter):
		m.filterText = ""
		m.filterInput.Reset()
		m.filterInput.SetValue("")
		m.filterInput.Focus()
		m.filterResults = nil
		m.filterCursor = 0
		m.mode = ModeFilter
		return m, textinput.Blink

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

	case matchKey(key, m.keys.copy):
		return m.doCopy()

	case matchKey(key, m.keys.copyCurl):
		return m.doCopyCurl()

	case matchKey(key, m.keys.gotoSource):
		return m.doGotoSource()
	}

	return m, nil
}

// ─── Cursor movement ──────────────────────────────────────────────────────────

func (m *Model) moveCursorUp() {
	m.outputMode = false
	switch m.section {
	case SectionFunctions:
		if m.fnCursor > 0 {
			m.fnCursor--
		} else if len(m.proj.Models) > 0 {
			m.section = SectionModels
			m.modelCursor = len(m.proj.Models) - 1
		}
		// Release lock if cursor moved away from the locked function.
		if m.fnLocked && m.fnCursor != m.selectedFnIdx {
			m.fnLocked = false
		}
	case SectionTests:
		fn := m.selectedFunction()
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
			// Jump to the LAST test item, not the first.
			if fn := m.selectedFunction(); fn != nil && len(fn.Tests) > 0 {
				m.testCursor = len(fn.Tests) - 1
			} else {
				m.testCursor = 0
			}
		}
	}
	m.adjustListScroll()
}

func (m *Model) moveCursorDown() {
	m.outputMode = false
	switch m.section {
	case SectionFunctions:
		if m.fnCursor < len(m.proj.Functions)-1 {
			m.fnCursor++
			// Release lock if cursor moved away from the locked function.
			if m.fnLocked && m.fnCursor != m.selectedFnIdx {
				m.fnLocked = false
			}
		} else {
			// Transitioning into Tests — lock the current function.
			m.lockFunction(m.fnCursor)
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
		fn := m.selectedFunction()
		if fn != nil && m.testCursor < len(fn.Tests)-1 {
			m.testCursor++
		} else if len(m.proj.Models) > 0 {
			m.section = SectionModels
			m.modelCursor = 0
		}
	case SectionModels:
		if m.modelCursor < len(m.proj.Models)-1 {
			m.modelCursor++
		} else {
			// Wrap around to the top of the Functions list.
			m.section = SectionFunctions
			m.fnCursor = 0
			m.fnLocked = false
		}
	}
	m.adjustListScroll()
}

func (m *Model) jumpSection(dir int) {
	m.outputMode = false
	sections := []PanelSection{SectionFunctions, SectionTests, SectionModels}

	cur := 0
	for i, s := range sections {
		if s == m.section {
			cur = i
			break
		}
	}

	for attempt := 0; attempt < len(sections); attempt++ {
		cur = (cur + dir + len(sections)) % len(sections)
		next := sections[cur]

		if next == SectionModels && len(m.proj.Models) == 0 {
			continue
		}
		if next == SectionTests {
			fn := m.selectedFunction()
			if fn == nil || len(fn.Tests) == 0 {
				continue
			}
			// Jumping into Tests locks the current function.
			m.lockFunction(m.fnCursor)
			m.testCursor = 0
		}

		m.section = next
		break
	}
	m.adjustListScroll()
}

// lockFunction sets selectedFnIdx and marks fnLocked = true.
func (m *Model) lockFunction(idx int) {
	m.selectedFnIdx = idx
	m.fnLocked = true
}

// currentFunction returns the function at fnCursor (navigation position only).
func (m *Model) currentFunction() *project.Function {
	if m.proj == nil || len(m.proj.Functions) == 0 {
		return nil
	}
	if m.fnCursor >= len(m.proj.Functions) {
		return nil
	}
	return &m.proj.Functions[m.fnCursor]
}

// selectedFunction returns the function whose tests and right-panel info are shown.
// When fnLocked is true this is selectedFnIdx; otherwise it follows fnCursor
// (live preview while browsing the Functions list).
func (m *Model) selectedFunction() *project.Function {
	if m.proj == nil || len(m.proj.Functions) == 0 {
		return nil
	}
	idx := m.fnCursor
	if m.fnLocked {
		idx = m.selectedFnIdx
	}
	if idx >= len(m.proj.Functions) {
		idx = 0
	}
	return &m.proj.Functions[idx]
}

func (m *Model) currentPayload() string {
	fn := m.selectedFunction()
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

func (m *Model) currentTestCase() *project.TestCase {
	fn := m.selectedFunction()
	if fn == nil || m.section != SectionTests {
		return nil
	}
	if m.testCursor >= len(fn.Tests) {
		return nil
	}
	return &fn.Tests[m.testCursor]
}

// ─── List scroll ──────────────────────────────────────────────────────────────

func (m *Model) leftPanelHeight() int {
	reserved := 5
	if m.benchVisible {
		reserved += 12
	}
	h := m.height - reserved
	if h < 4 {
		h = 4
	}
	return h
}

func (m *Model) adjustListScroll() {
	height := m.leftPanelHeight()
	cursorLine := m.cursorLineIndex()
	if cursorLine < 0 {
		return
	}
	if cursorLine >= m.listOffset+height {
		m.listOffset = cursorLine - height + 1
	}
	if cursorLine < m.listOffset {
		m.listOffset = cursorLine
	}
	if m.listOffset < 0 {
		m.listOffset = 0
	}
}

func (m *Model) cursorLineIndex() int {
	if m.proj == nil {
		return 0
	}
	line := 0
	line++ // Functions header
	nFuncs := len(m.proj.Functions)
	if nFuncs == 0 {
		line++
	}

	switch m.section {
	case SectionFunctions:
		return line + m.fnCursor
	}

	line += nFuncs
	line += 2 // blank + Tests header

	fn := m.selectedFunction()
	nTests := 0
	if fn != nil {
		nTests = len(fn.Tests)
	}
	if nTests == 0 {
		line++
	}

	if m.section == SectionTests {
		return line + m.testCursor
	}

	line += nTests
	if len(m.proj.Models) > 0 {
		line += 2 // blank + Models header
		return line + m.modelCursor
	}
	return line
}

// ─── Filter / search ──────────────────────────────────────────────────────────

// computeFilterResults rebuilds the filterResults slice from the current filterText.
// Results are grouped: matching functions first, then tests (with function headers),
// then models.
func (m *Model) computeFilterResults() {
	m.filterResults = nil
	q := strings.ToLower(strings.TrimSpace(m.filterText))
	if q == "" {
		return
	}

	// ── Functions ──────────────────────────────────────────────────────────
	for i, fn := range m.proj.Functions {
		if strings.Contains(strings.ToLower(fn.Name), q) {
			m.filterResults = append(m.filterResults, filterResult{
				section: SectionFunctions,
				fnIdx:   i,
				testIdx: -1,
				modelIdx: -1,
				label:   fn.Name,
			})
		}
	}

	// ── Tests (all functions) ──────────────────────────────────────────────
	for fi, fn := range m.proj.Functions {
		var matchingTests []int
		for ti, t := range fn.Tests {
			if strings.Contains(strings.ToLower(t.Name), q) {
				matchingTests = append(matchingTests, ti)
			}
		}
		if len(matchingTests) == 0 {
			continue
		}
		// Add function-name header only if the function itself didn't already
		// appear in the Functions results above.
		fnAlreadyShown := strings.Contains(strings.ToLower(fn.Name), q)
		if !fnAlreadyShown {
			m.filterResults = append(m.filterResults, filterResult{
				section:  SectionTests,
				fnIdx:    fi,
				testIdx:  -1,
				modelIdx: -1,
				label:    fn.Name,
				isHeader: true,
			})
		}
		for _, ti := range matchingTests {
			t := fn.Tests[ti]
			m.filterResults = append(m.filterResults, filterResult{
				section:  SectionTests,
				fnIdx:    fi,
				testIdx:  ti,
				modelIdx: -1,
				label:    t.Name,
				isXUnit:  t.Kind == project.TestCaseXUnit,
			})
		}
	}

	// ── Models ────────────────────────────────────────────────────────────
	for i, mdl := range m.proj.Models {
		if strings.Contains(strings.ToLower(mdl.Name), q) {
			m.filterResults = append(m.filterResults, filterResult{
				section:  SectionModels,
				fnIdx:    -1,
				testIdx:  -1,
				modelIdx: i,
				label:    mdl.Name,
			})
		}
	}
}

// firstSelectableIdx returns the index of the first non-header row in filterResults.
func (m *Model) firstSelectableIdx() int {
	for i, r := range m.filterResults {
		if !r.isHeader {
			return i
		}
	}
	return 0
}

// selectableCount returns the number of non-header rows.
func (m *Model) selectableCount() int {
	n := 0
	for _, r := range m.filterResults {
		if !r.isHeader {
			n++
		}
	}
	return n
}

// moveCursorInFilterResults moves the filter cursor by dir (+1 or -1), skipping headers.
func (m *Model) moveCursorInFilterResults(dir int) {
	next := m.filterCursor + dir
	for next >= 0 && next < len(m.filterResults) && m.filterResults[next].isHeader {
		next += dir
	}
	if next >= 0 && next < len(m.filterResults) {
		m.filterCursor = next
	}
}

// applyFilterSelection commits the highlighted filter result to the real cursors.
func (m *Model) applyFilterSelection() {
	if len(m.filterResults) == 0 {
		return
	}
	idx := m.filterCursor
	if idx >= len(m.filterResults) {
		idx = 0
	}
	r := m.filterResults[idx]
	if r.isHeader {
		// Shouldn't happen (filterCursor skips headers), but be safe.
		return
	}
	m.section = r.section
	switch r.section {
	case SectionFunctions:
		m.fnCursor = r.fnIdx
		// Don't auto-lock — just position cursor; user can press Enter to lock.
		m.fnLocked = false
	case SectionTests:
		m.fnCursor = r.fnIdx
		m.lockFunction(r.fnIdx)
		m.testCursor = r.testIdx
	case SectionModels:
		m.modelCursor = r.modelIdx
	}
	m.adjustListScroll()
}

// clearFilter resets all filter state.
func (m *Model) clearFilter() {
	m.filterText = ""
	m.filterInput.Reset()
	m.filterInput.Blur()
	m.filterResults = nil
	m.filterCursor = 0
}

// ─── Output pane helpers ──────────────────────────────────────────────────────

func (m *Model) outputPaneHeight() int {
	h := m.leftPanelHeight()
	if h > 6 {
		return h - 6
	}
	return 4
}

func (m *Model) outputLineCount() int {
	count := 0
	if m.lastStdout != "" {
		count += len(strings.Split(m.lastStdout, "\n"))
	}
	if m.lastStderr != "" {
		count += 2
		count += len(strings.Split(m.lastStderr, "\n"))
	}
	return count
}

// ─── Actions ──────────────────────────────────────────────────────────────────

func (m Model) doInvoke() (tea.Model, tea.Cmd) {
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
	fn := m.selectedFunction()
	if fn == nil {
		m.mode = ModeError
		m.errorMsg = "No function selected."
		return m, nil
	}

	// Use function-specific build args if the runtime supports them.
	// This avoids MSB1003 "Specify a project or solution file" when the
	// project root has no .csproj/.sln at its top level.
	type funcBuilder interface {
		BuildFunctionArgs(projectRoot string, fn project.Function) []string
	}
	var buildArgs []string
	if fb, ok := m.runtime.(funcBuilder); ok {
		buildArgs = fb.BuildFunctionArgs(m.proj.Path, *fn)
	} else {
		buildArgs = m.runtime.BuildArgs(m.proj.Path)
	}
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
	fn := m.selectedFunction()
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
			return m, nil
		}
		fn := m.selectedFunction()
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
		oldParts := strings.Split(oldHandler, "::")
		newParts := strings.Split(val, "::")
		if len(oldParts) == 3 && len(newParts) == 3 {
			if m.proj.Functions[m.fnCursor].Name == oldParts[2] {
				m.proj.Functions[m.fnCursor].Name = newParts[2]
			}
		}
		_ = project.Save(m.proj)

	case SectionTests:
		fn := m.selectedFunction()
		if fn == nil {
			return m, nil
		}
		selIdx := m.selectedFnIdx
		if !m.fnLocked {
			selIdx = m.fnCursor
		}
		if m.testCursor < len(fn.Tests) && fn.Tests[m.testCursor].Kind != project.TestCaseXUnit {
			m.proj.Functions[selIdx].Tests[m.testCursor].Payload = val
			_ = project.Save(m.proj)
		}

	case SectionModels:
		if m.modelCursor < len(m.proj.Models) {
			m.proj.Models[m.modelCursor].JSON = val
			_ = project.Save(m.proj)
		}
	}
	// Keep shared state in sync so the API server sees any handler changes.
	m.shared.set(m.proj, m.runtime)
	return m, nil
}

func (m Model) openNewTestForm() (tea.Model, tea.Cmd) {
	if m.selectedFunction() == nil {
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
	fn := m.selectedFunction()
	if fn == nil {
		return m, nil
	}
	selIdx := m.selectedFnIdx
	if !m.fnLocked {
		selIdx = m.fnCursor
	}
	m.proj.Functions[selIdx].Tests = append(fn.Tests,
		project.TestCase{Name: m.pendingName, Payload: val})
	_ = project.Save(m.proj)
	m.section = SectionTests
	m.testCursor = len(m.proj.Functions[selIdx].Tests) - 1
	m.lockFunction(selIdx)
	m.adjustListScroll()
	return m, nil
}

func (m Model) doDelete() (tea.Model, tea.Cmd) {
	switch m.section {
	case SectionTests:
		fn := m.selectedFunction()
		if fn == nil || len(fn.Tests) == 0 {
			return m, nil
		}
		if m.testCursor >= len(fn.Tests) {
			return m, nil
		}
		tc := fn.Tests[m.testCursor]
		if tc.Kind == project.TestCaseXUnit {
			m.statusMsg = "Cannot delete auto-discovered tests"
			return m, tea.Tick(2*time.Second, func(_ time.Time) tea.Msg { return clearStatusMsg{} })
		}
		selIdx := m.selectedFnIdx
		if !m.fnLocked {
			selIdx = m.fnCursor
		}
		tests := fn.Tests
		m.proj.Functions[selIdx].Tests = append(tests[:m.testCursor], tests[m.testCursor+1:]...)
		if m.testCursor >= len(m.proj.Functions[selIdx].Tests) && m.testCursor > 0 {
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
		// Brief confirmation — the [API: OFF] in the status bar takes over immediately.
		m.statusMsg = "API server stopped"
		return m, tea.Tick(1*time.Second, func(_ time.Time) tea.Msg { return clearStatusMsg{} })
	}
	if err := m.apiServer.Start(); err != nil {
		m.mode = ModeError
		m.errorMsg = "Could not start API server: " + err.Error()
		return m, nil
	}
	// No transient statusMsg — the permanent [API: addr] in the status bar is enough.
	return m, nil
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

// ─── Left panel width ─────────────────────────────────────────────────────────

// computeLeftPanelWidth returns a dynamic left panel width based on the longest
// item name currently visible, clamped to a sensible range.
func (m *Model) computeLeftPanelWidth() int {
	maxRunes := 0
	check := func(s string) {
		n := len([]rune(s))
		if n > maxRunes {
			maxRunes = n
		}
	}
	for _, fn := range m.proj.Functions {
		check(fn.Name)
	}
	if fn := m.selectedFunction(); fn != nil {
		for _, t := range fn.Tests {
			check(t.Name)
		}
	}
	for _, mdl := range m.proj.Models {
		check(mdl.Name)
	}
	// Add room for cursor ("▶ "), trailing marker (" ⊕"), and side padding.
	w := maxRunes + 6
	min := 36
	max := m.width / 2
	if max > 60 {
		max = 60
	}
	if w < min {
		w = min
	}
	if w > max {
		w = max
	}
	return w
}

// ─── Copy ─────────────────────────────────────────────────────────────────────

func (m Model) doCopy() (tea.Model, tea.Cmd) {
	text := m.buildCopyText()
	if text == "" {
		return m, nil
	}
	if clipboard.Unsupported {
		m.statusMsg = "Clipboard not available — install xclip or wl-copy"
		return m, tea.Tick(3*time.Second, func(_ time.Time) tea.Msg { return clearStatusMsg{} })
	}
	if err := clipboard.WriteAll(text); err != nil {
		m.statusMsg = "Copy failed: " + err.Error()
	} else {
		m.statusMsg = "Copied!"
	}
	return m, tea.Tick(2*time.Second, func(_ time.Time) tea.Msg { return clearStatusMsg{} })
}

// buildCopyText returns the context-sensitive text for [y]:
//   - Functions:            function name
//   - xUnit tests:          dotnet test filter command
//   - Payload tests:        curl command (against API if running, else configured port)
//   - Models:               model JSON
func (m *Model) buildCopyText() string {
	switch m.section {
	case SectionFunctions:
		fn := m.currentFunction()
		if fn == nil {
			return ""
		}
		return fn.Name

	case SectionTests:
		fn := m.selectedFunction()
		tc := m.currentTestCase()
		if fn == nil || tc == nil {
			return ""
		}
		if tc.Kind == project.TestCaseXUnit {
			// xUnit: copy the dotnet test filter command.
			ts, ok := m.runtime.(runtime.TestScanner)
			if !ok {
				return tc.Filter
			}
			args := ts.InvokeTestArgs(m.proj.Path, *tc)
			if len(args) == 0 {
				return tc.Filter
			}
			return strings.Join(args, " ")
		}
		// Payload test: copy a curl command.
		return m.buildCurlCommand(fn, tc.Payload)

	case SectionModels:
		if m.modelCursor < len(m.proj.Models) {
			return m.proj.Models[m.modelCursor].JSON
		}
	}
	return ""
}

// doCopyCurl copies a curl command for the selected item regardless of type.
// For xUnit tests it uses the test's Payload variant value (or "{}" if none).
// When the API server is running the command targets the live endpoint.
func (m Model) doCopyCurl() (tea.Model, tea.Cmd) {
	text := m.buildCopyCurlText()
	if text == "" {
		return m, nil
	}
	if clipboard.Unsupported {
		m.statusMsg = "Clipboard not available — install xclip or wl-copy"
		return m, tea.Tick(3*time.Second, func(_ time.Time) tea.Msg { return clearStatusMsg{} })
	}
	if err := clipboard.WriteAll(text); err != nil {
		m.statusMsg = "Copy failed: " + err.Error()
	} else {
		m.statusMsg = "Copied curl!"
	}
	return m, tea.Tick(2*time.Second, func(_ time.Time) tea.Msg { return clearStatusMsg{} })
}

func (m *Model) buildCopyCurlText() string {
	fn := m.selectedFunction()
	if fn == nil {
		fn = m.currentFunction()
	}
	if fn == nil {
		return ""
	}
	tc := m.currentTestCase()

	// xUnit tests are now invocable via the two-segment API path /Fn/Test.
	if tc != nil && tc.Kind == project.TestCaseXUnit {
		return m.buildTestCurlCommand(fn, tc)
	}

	payload := "{}"
	if tc != nil && tc.Payload != "" {
		payload = tc.Payload
	} else if m.section == SectionModels && m.modelCursor < len(m.proj.Models) {
		payload = m.proj.Models[m.modelCursor].JSON
	}
	return m.buildCurlCommand(fn, payload)
}

// buildCurlCommand builds a curl invocation for fn with the given JSON payload.
// When the API server is running it targets the live endpoint; otherwise it
// uses the project's configured port so the command is ready to paste.
func (m *Model) buildCurlCommand(fn *project.Function, payload string) string {
	if payload == "" {
		payload = "{}"
	}
	port := m.proj.APIPort
	if port == 0 {
		port = project.DefaultAPIPort
	}
	scheme := "http"
	host := fmt.Sprintf("localhost:%d", port)
	// If the API server is running, use its actual address.
	if m.apiServer != nil && m.apiServer.Running() {
		addr := m.apiServer.Addr() // "http://localhost:8080"
		// Strip the scheme for display clarity, keep full URL for the command.
		_ = scheme
		_ = host
		return fmt.Sprintf(
			`curl -X POST %s/%s -H "Content-Type: application/json" -d '%s'`,
			addr, fn.Name, payload)
	}
	return fmt.Sprintf(
		`curl -X POST %s://%s/%s -H "Content-Type: application/json" -d '%s'`,
		scheme, host, fn.Name, payload)
}

// ─── Goto source ──────────────────────────────────────────────────────────────

func (m Model) doGotoSource() (tea.Model, tea.Cmd) {
	if m.proj == nil {
		return m, nil
	}

	file, line, found := m.resolveSourceLocation()
	if !found || file == "" {
		m.statusMsg = "Source location not found"
		return m, tea.Tick(2*time.Second, func(_ time.Time) tea.Msg { return clearStatusMsg{} })
	}
	return m, m.openEditorAt(file, line)
}

func (m *Model) resolveSourceLocation() (file string, line int, found bool) {
	sl, hasLocator := m.runtime.(runtime.SourceLocator)

	switch m.section {
	case SectionFunctions:
		fn := m.currentFunction()
		if fn == nil {
			return "", 0, false
		}
		if hasLocator {
			return sl.FindFunctionSource(m.proj.Path, *fn)
		}
		// Fallback: open .lambit.toml at the handler line.
		f, l, ok := findInTOML(m.proj.Path, fn.Handler)
		return f, l, ok

	case SectionTests:
		tc := m.currentTestCase()
		if tc == nil {
			return "", 0, false
		}
		if hasLocator {
			return sl.FindTestSource(m.proj.Path, *tc)
		}
		// Fallback: open .lambit.toml at the test name line.
		f, l, ok := findInTOML(m.proj.Path, tc.Name)
		return f, l, ok

	case SectionModels:
		if m.modelCursor >= len(m.proj.Models) {
			return "", 0, false
		}
		mdl := m.proj.Models[m.modelCursor]
		if hasLocator {
			return sl.FindModelSource(m.proj.Path, mdl)
		}
		f, l, ok := findInTOML(m.proj.Path, mdl.Name)
		return f, l, ok
	}
	return "", 0, false
}

// findInTOML is a package-level helper (mirrors the one in dotnet.go but
// accessible from model.go for runtimes that don't implement SourceLocator).
func findInTOML(projectRoot, search string) (string, int, bool) {
	tomlPath := filepath.Join(projectRoot, project.ProjectFile)
	data, err := os.ReadFile(tomlPath)
	if err != nil {
		return "", 0, false
	}
	escaped := strings.ReplaceAll(search, `"`, `\"`)
	for i, raw := range strings.Split(string(data), "\n") {
		l := strings.TrimRight(raw, "\r")
		if strings.Contains(l, `"`+escaped+`"`) ||
			strings.Contains(l, "'"+search+"'") {
			return tomlPath, i + 1, true
		}
	}
	return tomlPath, 1, false
}

// openEditorAt opens path in $EDITOR at the given 1-based line number.
// Line-jump syntax varies by editor — we handle the most common cases.
// Falls back to just opening the file for editors that don't support +N.
func (m Model) openEditorAt(path string, line int) tea.Cmd {
	editor := m.cfg.Apps.Editor
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		for _, e := range []string{"nvim", "vim", "vi", "nano", "emacs", "code", "code.cmd", "notepad++", "notepad.exe"} {
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

	var c *exec.Cmd
	base := filepath.Base(editor)
	// Strip .exe / .cmd suffixes for matching.
	baseLower := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(base, ".cmd"), ".exe"))

	switch baseLower {
	case "code":
		// VS Code: code --goto file:line  (works on Linux, macOS, Windows)
		if line > 0 {
			c = exec.Command(editor, "--goto", path+":"+strconv.Itoa(line))
		} else {
			c = exec.Command(editor, path)
		}
	case "notepad++":
		// Notepad++: notepad++ -nLINE file
		if line > 0 {
			c = exec.Command(editor, "-n"+strconv.Itoa(line), path)
		} else {
			c = exec.Command(editor, path)
		}
	case "notepad":
		// Notepad.exe has no line support — just open the file.
		c = exec.Command(editor, path)
	default:
		// vim, nvim, vi, nano, emacs, and most Unix editors: +LINE file
		if line > 0 {
			c = exec.Command(editor, "+"+strconv.Itoa(line), path)
		} else {
			c = exec.Command(editor, path)
		}
	}

	return tea.ExecProcess(c, func(err error) tea.Msg { return nil })
}

// buildTestCurlCommand builds a curl command that invokes a test case via the
// two-segment API path: POST /FunctionName/TestCaseName
// The test name is URL-path-escaped so spaces and parentheses are safe.
func (m *Model) buildTestCurlCommand(fn *project.Function, tc *project.TestCase) string {
	port := m.proj.APIPort
	if port == 0 {
		port = project.DefaultAPIPort
	}
	base := fmt.Sprintf("http://localhost:%d", port)
	if m.apiServer != nil && m.apiServer.Running() {
		base = m.apiServer.Addr()
	}
	testPath := url.PathEscape(tc.Name)
	return fmt.Sprintf(`curl -X POST %s/%s/%s`, base, fn.Name, testPath)
}
