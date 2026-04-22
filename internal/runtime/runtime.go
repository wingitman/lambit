// Package runtime defines the Runtime interface, the TestScanner optional
// interface, the registry, and shared helpers used by all runtime implementations.
package runtime

import (
	"os"
	"strings"
	"time"

	"github.com/wingitman/lambit/internal/project"
)

// ─── Core result type ─────────────────────────────────────────────────────────

// InvokeResult holds the captured output of a single lambda or test invocation.
type InvokeResult struct {
	Stdout   string
	Stderr   string
	Duration time.Duration
	Success  bool
	Error    string // non-empty when Success == false
}

// ─── Runtime interface ────────────────────────────────────────────────────────

// Runtime is the interface every lambda runtime implementation must satisfy.
type Runtime interface {
	// Name returns the short identifier for this runtime (e.g. "dotnet", "nodejs").
	Name() string

	// Detect returns true if this runtime recognises the project at projectRoot.
	Detect(projectRoot string) bool

	// Scan discovers handler functions in the project.
	Scan(projectRoot string) ([]project.Function, error)

	// BuildArgs returns the command to build the project, or nil to skip.
	BuildArgs(projectRoot string) []string

	// ShimDir returns the path where the invocation shim lives.
	ShimDir(projectRoot string) string

	// InvokeArgs returns the command to invoke fn with the given JSON payload.
	InvokeArgs(projectRoot string, fn project.Function, payload string) []string

	// ParseResult converts raw subprocess output into an InvokeResult.
	ParseResult(stdout, stderr string, dur time.Duration) InvokeResult
}

// ─── TestScanner optional interface ──────────────────────────────────────────

// TestScanner is an optional capability a Runtime may implement.
// Lambit discovers it via type assertion: rt.(TestScanner).
//
// Runtimes that support test discovery (e.g. dotnet with xUnit) implement
// this interface. Results are never persisted to .lambit.toml — they are
// re-discovered on every startup.
type TestScanner interface {
	// ScanTests scans the project's test source files for unit test methods
	// associated with fn and returns them as TestCase entries with
	// Kind == project.TestCaseXUnit.
	ScanTests(projectRoot string, fn project.Function) []project.TestCase

	// InvokeTestArgs returns the OS/exec command to run a single xUnit test.
	InvokeTestArgs(projectRoot string, tc project.TestCase) []string

	// ParseTestResult converts raw dotnet-test output into an InvokeResult.
	ParseTestResult(stdout, stderr string, dur time.Duration) InvokeResult
}

// ─── SourceLocator optional interface ────────────────────────────────────────

// SourceLocator is an optional capability a Runtime may implement.
// Lambit discovers it via type assertion: rt.(SourceLocator).
// It finds the source file and line number for a function handler or test case
// so lambit can open the editor at that exact location.
type SourceLocator interface {
	// FindTestSource returns the source file and 1-based line number of the
	// [Fact]/[Theory] attribute (or method signature) for an xUnit test case,
	// or the .lambit.toml file and line for a payload test / function / model.
	FindTestSource(projectRoot string, tc project.TestCase) (file string, line int, found bool)

	// FindFunctionSource returns the .lambit.toml path and line number for a
	// function handler entry.
	FindFunctionSource(projectRoot string, fn project.Function) (file string, line int, found bool)

	// FindModelSource returns the .lambit.toml path and line number for a model.
	FindModelSource(projectRoot string, mdl project.Model) (file string, line int, found bool)
}

// ─── Registry ─────────────────────────────────────────────────────────────────

var registry []Runtime

// Register adds a Runtime to the registry (call from init()).
func Register(r Runtime) { registry = append(registry, r) }

// All returns a copy of the registry.
func All() []Runtime {
	out := make([]Runtime, len(registry))
	copy(out, registry)
	return out
}

// Detect returns the first registered Runtime that recognises projectRoot.
func Detect(projectRoot string) Runtime {
	for _, r := range registry {
		if r.Detect(projectRoot) {
			return r
		}
	}
	return nil
}

// ByName returns a registered Runtime by its Name(), or nil.
func ByName(name string) Runtime {
	for _, r := range registry {
		if r.Name() == name {
			return r
		}
	}
	return nil
}

// ─── Shared YAML helper ───────────────────────────────────────────────────────

// parseTemplateYAMLHandlers performs a line-by-line parse of a SAM/
// CloudFormation template.yaml file, collecting Lambda function resources
// whose Handler: value is accepted by the accept predicate.
//
// The parser tracks the top-level YAML section so that resource keys from
// Globals:, Parameters:, and Outputs: are never mistaken for Resources:.
// It intentionally does not handle YAML anchors or multi-document streams —
// all real SAM templates follow the simple structure this parser covers.
func parseTemplateYAMLHandlers(path string, accept func(handler string) bool) []project.Function {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")

	var fns []project.Function
	inResourcesBlock := false
	currentResource := ""
	inLambdaResource := false

	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")

		// ── Top-level section key (0-indent, non-blank, non-comment) ──────
		// e.g. "Resources:", "Globals:", "Parameters:"
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' && line[0] != '#' {
			trimmed := strings.TrimSpace(line)
			if strings.HasSuffix(trimmed, ":") && !strings.Contains(trimmed, " ") {
				key := strings.TrimSuffix(trimmed, ":")
				inResourcesBlock = (key == "Resources")
				currentResource = ""
				inLambdaResource = false
			}
			continue
		}

		if !inResourcesBlock {
			continue
		}

		// ── 2-space-indented key = SAM resource name ───────────────────────
		// e.g. "  GetStockFunction:"
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") {
			trimmed := strings.TrimSpace(line)
			if strings.HasSuffix(trimmed, ":") && !strings.Contains(trimmed, " ") {
				currentResource = strings.TrimSuffix(trimmed, ":")
				inLambdaResource = false
			}
		}

		// ── Detect Lambda resource type ────────────────────────────────────
		if strings.Contains(line, "Type:") &&
			(strings.Contains(line, "AWS::Serverless::Function") ||
				strings.Contains(line, "AWS::Lambda::Function")) {
			inLambdaResource = true
		}

		// ── Extract Handler: within a Lambda resource ──────────────────────
		if inLambdaResource && strings.Contains(line, "Handler:") {
			parts := strings.SplitN(line, "Handler:", 2)
			if len(parts) != 2 {
				continue
			}
			handler := strings.TrimSpace(parts[1])
			// Strip inline YAML comment.
			if ci := strings.Index(handler, " #"); ci >= 0 {
				handler = strings.TrimSpace(handler[:ci])
			}
			if !accept(handler) {
				continue
			}
			name := currentResource
			if name == "" {
				name = handler
			}
			desc := "Discovered from template.yaml"
			// Peek ahead for a Description: field within the same resource block.
			for j := i + 1; j < i+15 && j < len(lines); j++ {
				l := lines[j]
				// Stop if we hit the next resource key.
				if strings.HasPrefix(l, "  ") && !strings.HasPrefix(l, "   ") {
					break
				}
				if strings.Contains(l, "Description:") {
					dp := strings.SplitN(l, "Description:", 2)
					if len(dp) == 2 {
						d := strings.Trim(strings.TrimSpace(dp[1]), `'"`)
						if d != "" {
							desc = d
						}
					}
					break
				}
			}
			fns = append(fns, project.Function{
				Name:        name,
				Handler:     handler,
				Description: desc,
			})
		}
	}
	return fns
}
