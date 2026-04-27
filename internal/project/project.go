// Package project handles reading and writing the .lambit.toml project file.
package project

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const ProjectFile = ".lambit.toml"

// TestCaseKind distinguishes user-defined payload tests from auto-discovered
// unit tests. The zero value is TestCasePayload so existing TOML files that
// omit the field decode correctly.
type TestCaseKind string

const (
	TestCasePayload TestCaseKind = ""      // user-defined JSON payload test (default)
	TestCaseXUnit   TestCaseKind = "xunit" // discovered xUnit [Fact]/[Theory] method
)

// TestCase is a named invocable test entry.
//
// For Kind == TestCasePayload: Payload holds the JSON to send to the lambda.
// For Kind == TestCaseXUnit:   Filter holds the dotnet test --filter string;
//
//	Payload holds the [InlineData] variant value (display only).
type TestCase struct {
	Name    string       `toml:"name"`
	Payload string       `toml:"payload,omitempty"`
	Kind    TestCaseKind `toml:"kind,omitempty"`
	Filter  string       `toml:"filter,omitempty"`
}

// Function describes a discoverable lambda handler entry point.
type Function struct {
	Name        string     `toml:"name"`
	Handler     string     `toml:"handler"`
	Description string     `toml:"description,omitempty"`
	Root        string     `toml:"root,omitempty"` // subdirectory relative to proj.Path; overrides project root for shim/build/invoke
	Tests       []TestCase `toml:"tests"`
}

// Model is a named JSON blob used as a payload template.
type Model struct {
	Name string `toml:"name"`
	JSON string `toml:"json"`
}

// Project is the root struct for .lambit.toml.
type Project struct {
	Path string `toml:"-"` // directory containing .lambit.toml (not persisted)

	Name       string     `toml:"name"`
	Runtime    string     `toml:"runtime"`
	APIPort    int        `toml:"api_port"`
	BenchRuns  int        `toml:"bench_runs"`
	Functions  []Function `toml:"functions"`
	Models     []Model    `toml:"models"`
}

// DefaultAPIPort is used when the project file omits api_port.
const DefaultAPIPort = 8080

// DefaultBenchRuns is the default number of iterations for quick-bench.
const DefaultBenchRuns = 10

// Load reads .lambit.toml from dir or any ancestor directory.
func Load(dir string) (*Project, error) {
	path, err := findProjectFile(dir)
	if err != nil {
		return nil, err
	}
	p := &Project{}
	if _, err := toml.DecodeFile(path, p); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	p.Path = filepath.Dir(path)

	// Migration: handle old [project] table format written by earlier lambit versions.
	// If p.Name is empty the file likely uses the old [project] section header,
	// which the flat decoder silently discards. Re-decode using a wrapper struct.
	if p.Name == "" {
		type oldProjectSection struct {
			Project struct {
				Name    string `toml:"name"`
				Runtime string `toml:"runtime"`
				APIPort int    `toml:"api_port"`
			} `toml:"project"`
			Functions []Function `toml:"functions"`
			Models    []Model    `toml:"models"`
		}
		var old oldProjectSection
		if _, err2 := toml.DecodeFile(path, &old); err2 == nil && old.Project.Name != "" {
			p.Name = old.Project.Name
			p.Runtime = old.Project.Runtime
			if old.Project.APIPort > 0 {
				p.APIPort = old.Project.APIPort
			}
			if len(p.Functions) == 0 {
				p.Functions = old.Functions
			}
			if len(p.Models) == 0 {
				p.Models = old.Models
			}
		}
	}

	if p.APIPort == 0 {
		p.APIPort = DefaultAPIPort
	}
	if p.BenchRuns == 0 {
		p.BenchRuns = DefaultBenchRuns
	}
	return p, nil
}

// Save writes the project back to its .lambit.toml file.
// xUnit test cases (Kind == TestCaseXUnit) are never written — they are
// re-discovered on every startup.
func Save(p *Project) error {
	stripped := *p
	stripped.Functions = make([]Function, len(p.Functions))
	for i, fn := range p.Functions {
		fn2 := fn
		fn2.Tests = nil
		for _, tc := range fn.Tests {
			if tc.Kind != TestCaseXUnit {
				fn2.Tests = append(fn2.Tests, tc)
			}
		}
		stripped.Functions[i] = fn2
	}
	path := filepath.Join(p.Path, ProjectFile)
	return os.WriteFile(path, []byte(buildProjectTOML(&stripped)), 0644)
}

// Scaffold writes a .lambit.toml to dir (does not overwrite).
func Scaffold(dir string, detected []Function) error {
	path := filepath.Join(dir, ProjectFile)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	var functions []Function
	if len(detected) > 0 {
		for _, fn := range detected {
			fn2 := fn
			fn2.Tests = nil
			for _, tc := range fn.Tests {
				if tc.Kind != TestCaseXUnit {
					fn2.Tests = append(fn2.Tests, tc)
				}
			}
			if len(fn2.Tests) == 0 {
				fn2.Tests = []TestCase{
					{Name: "Hello world", Payload: `{"input": "hello world"}`},
				}
			}
			functions = append(functions, fn2)
		}
	} else {
		functions = []Function{
			{
				Name:        "FunctionHandler",
				Handler:     "MyFunction::MyFunction.Function::FunctionHandler",
				Description: "Main lambda entry point — update the handler string above",
				Tests: []TestCase{
					{Name: "Hello world", Payload: `{"input": "hello world"}`},
				},
			},
		}
	}
	p := &Project{
		Path:      dir,
		Name:      filepath.Base(dir),
		APIPort:   DefaultAPIPort,
		Functions: functions,
		Models:    []Model{{Name: "SamplePayload", JSON: `{"key": "value", "count": 1}`}},
	}
	return os.WriteFile(path, []byte(buildProjectTOML(p)), 0644)
}

// ErrNotFound is returned when no project file is found in the directory tree.
var ErrNotFound = fmt.Errorf("no %s found in this directory or any parent", ProjectFile)

func findProjectFile(dir string) (string, error) {
	cur := dir
	for {
		candidate := filepath.Join(cur, ProjectFile)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return "", ErrNotFound
}

// buildProjectTOML serialises a Project to TOML text.
// Fields are written as top-level keys (no [project] section header) so that
// toml.DecodeFile into a flat *Project correctly reads them back.
func buildProjectTOML(p *Project) string {
	benchRuns := p.BenchRuns
	if benchRuns == 0 {
		benchRuns = DefaultBenchRuns
	}
	out := "# lambit project file\n# Generated by lambit — edit freely.\n\n" +
		"name       = " + quote(p.Name) + "\n" +
		"runtime    = " + quote(p.Runtime) + "  # leave empty for auto-detect\n" +
		"api_port   = " + itoa(p.APIPort) + "\n" +
		"bench_runs = " + itoa(benchRuns) + "  # iterations for quick-bench (r)\n"

	for _, fn := range p.Functions {
		out += "\n[[functions]]\n"
		out += "name        = " + quote(fn.Name) + "\n"
		out += "handler     = " + quote(fn.Handler) + "\n"
		out += "description = " + quote(fn.Description) + "\n"
		if fn.Root != "" {
			out += "root        = " + quote(fn.Root) + "\n"
		}
		for _, t := range fn.Tests {
			out += "\n[[functions.tests]]\n"
			out += "name    = " + quote(t.Name) + "\n"
			out += "payload = " + singleQuote(t.Payload) + "\n"
		}
	}
	for _, m := range p.Models {
		out += "\n[[models]]\n"
		out += "name = " + quote(m.Name) + "\n"
		out += "json = " + singleQuote(m.JSON) + "\n"
	}
	return out
}

func quote(s string) string       { return `"` + s + `"` }
func singleQuote(s string) string { return "'" + s + "'" }
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
