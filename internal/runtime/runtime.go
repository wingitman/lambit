// Package runtime defines the Runtime interface and the runtime registry.
// Each runtime implementation knows how to detect its lambda type in a project,
// scan for handler functions, and produce the OS/exec arguments to build and
// invoke those handlers.
package runtime

import (
	"time"

	"github.com/wingitman/lambit/internal/project"
)

// InvokeResult holds the captured output of a single lambda invocation.
type InvokeResult struct {
	Stdout   string
	Stderr   string
	Duration time.Duration
	Success  bool
	Error    string // non-empty when Success == false
}

// Runtime is the interface every lambda runtime implementation must satisfy.
// Users can add custom implementations by placing a Go file in a separate
// package and registering it with Register().
type Runtime interface {
	// Name returns the identifier for this runtime (e.g. "dotnet8", "nodejs20").
	Name() string

	// Detect returns true if this runtime recognises the project at projectRoot.
	// Detection is done by inspecting files (e.g. *.csproj, package.json).
	Detect(projectRoot string) bool

	// Scan inspects the project and returns a slice of discovered Function
	// descriptors. It should be non-destructive (read-only filesystem access).
	Scan(projectRoot string) ([]project.Function, error)

	// BuildArgs returns the OS/exec command + arguments needed to build the
	// project before invocation (e.g. ["dotnet", "build"]). Return nil to skip
	// the build step.
	BuildArgs(projectRoot string) []string

	// ShimDir returns the relative path (inside projectRoot) where the runtime
	// shim will be extracted. Typically ".lambit/<runtime-name>".
	ShimDir(projectRoot string) string

	// InvokeArgs returns the OS/exec command + arguments to invoke fn with the
	// given JSON payload. The shim must already exist at ShimDir().
	InvokeArgs(projectRoot string, fn project.Function, payload string) []string

	// ParseResult converts the raw subprocess output into an InvokeResult.
	ParseResult(stdout, stderr string, dur time.Duration) InvokeResult
}

// registry holds all registered runtimes, in detection-priority order.
var registry []Runtime

// Register adds a Runtime to the registry. Call this from init() in each
// runtime implementation file.
func Register(r Runtime) {
	registry = append(registry, r)
}

// All returns a copy of the registry slice.
func All() []Runtime {
	out := make([]Runtime, len(registry))
	copy(out, registry)
	return out
}

// Detect returns the first registered Runtime that recognises the project at
// projectRoot. Returns nil if none matches.
func Detect(projectRoot string) Runtime {
	for _, r := range registry {
		if r.Detect(projectRoot) {
			return r
		}
	}
	return nil
}

// ByName returns a registered Runtime by its Name() identifier, or nil.
func ByName(name string) Runtime {
	for _, r := range registry {
		if r.Name() == name {
			return r
		}
	}
	return nil
}
