// Package invoke runs lambda handlers as local subprocesses, capturing stdout,
// stderr and wall-clock duration.
package invoke

import (
	"bytes"
	"context"
	"os/exec"
	"time"

	"github.com/wingitman/lambit/internal/runtime"
)

// Request describes a single invocation.
type Request struct {
	Args        []string // full command + arguments from Runtime.InvokeArgs
	ProjectRoot string
}

// Run executes the subprocess described by req and returns the result.
// A non-zero exit code is captured in InvokeResult.Success rather than
// returning a Go error, so the TUI can display it cleanly.
func Run(req Request) runtime.InvokeResult {
	if len(req.Args) == 0 {
		return runtime.InvokeResult{
			Success: false,
			Error:   "empty command",
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, req.Args[0], req.Args[1:]...)
	cmd.Dir = req.ProjectRoot

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	dur := time.Since(start)

	if ctx.Err() != nil {
		return runtime.InvokeResult{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			Duration: dur,
			Success:  false,
			Error:    "invocation timed out after 120s",
		}
	}

	stdoutStr := stdout.String()
	stderrStr := stderr.String()

	if err != nil {
		return runtime.InvokeResult{
			Stdout:   stdoutStr,
			Stderr:   stderrStr,
			Duration: dur,
			Success:  false,
			Error:    err.Error(),
		}
	}

	return runtime.InvokeResult{
		Stdout:   stdoutStr,
		Stderr:   stderrStr,
		Duration: dur,
		Success:  true,
	}
}

// Build runs the build command for the given project (e.g. "dotnet build").
// Returns an error string if the build fails, or "" on success.
func Build(projectRoot string, args []string) (string, error) {
	if len(args) == 0 {
		return "", nil // no build needed
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = projectRoot

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return out.String(), err
	}
	return out.String(), nil
}
