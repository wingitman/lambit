package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wingitman/lambit/internal/project"
)

func init() {
	Register(&nodeRuntime{})
}

type nodeRuntime struct{}

func (n *nodeRuntime) Name() string { return "nodejs" }

var nodeRuntimeNames = []string{
	"nodejs", "nodejs14.x", "nodejs16.x", "nodejs18.x", "nodejs20.x", "nodejs22.x",
}

func isNodeRuntime(s string) bool {
	lower := strings.ToLower(s)
	for _, name := range nodeRuntimeNames {
		if strings.Contains(lower, name) {
			return true
		}
	}
	return false
}

// Detect returns true when any package.json within 4 subdirectory levels
// references aws-lambda/@aws-sdk, or a template.yaml declares a Node runtime.
func (n *nodeRuntime) Detect(projectRoot string) bool {
	found := false
	walkFiles(projectRoot, 4, func(path string, _ int) {
		if found {
			return
		}
		switch filepath.Base(path) {
		case "package.json":
			data, err := os.ReadFile(path)
			if err != nil {
				return
			}
			c := string(data)
			if strings.Contains(c, "aws-lambda") || strings.Contains(c, "@aws-sdk") {
				found = true
			}
		case "template.yaml", "template.yml":
			data, err := os.ReadFile(path)
			if err != nil {
				return
			}
			for _, line := range strings.Split(string(data), "\n") {
				if strings.Contains(line, "Runtime:") {
					parts := strings.SplitN(line, "Runtime:", 2)
					if len(parts) == 2 && isNodeRuntime(strings.TrimSpace(parts[1])) {
						found = true
						return
					}
				}
			}
		}
	})
	return found
}

// Scan discovers Node lambda handlers via a two-tier cascade:
//  1. template.yaml — most accurate, covers multi-function SAM projects
//  2. package.json  — single-function fallback
func (n *nodeRuntime) Scan(projectRoot string) ([]project.Function, error) {
	if fns := n.scanTemplateYAML(projectRoot); len(fns) > 0 {
		return fns, nil
	}
	return n.scanPackageJSON(projectRoot), nil
}

// ─── Tier 1: template.yaml ────────────────────────────────────────────────────

func (n *nodeRuntime) scanTemplateYAML(projectRoot string) []project.Function {
	var fns []project.Function
	walkFiles(projectRoot, 4, func(path string, _ int) {
		b := filepath.Base(path)
		if b == "template.yaml" || b == "template.yml" {
			// Node handlers: no "::", contains "." separator (e.g. "index.handler")
			fns = append(fns, parseTemplateYAMLHandlers(path, func(h string) bool {
				return !strings.Contains(h, "::") && strings.Contains(h, ".")
			})...)
		}
	})
	return fns
}

// ─── Tier 2: package.json ─────────────────────────────────────────────────────

func (n *nodeRuntime) scanPackageJSON(projectRoot string) []project.Function {
	// Prefer root-level package.json.
	var pkgPaths []string
	walkFiles(projectRoot, 3, func(path string, _ int) {
		if filepath.Base(path) == "package.json" {
			pkgPaths = append(pkgPaths, path)
		}
	})
	rootPkg := filepath.Join(projectRoot, "package.json")
	for _, p := range pkgPaths {
		if p == rootPkg {
			pkgPaths = []string{p}
			break
		}
	}
	if len(pkgPaths) == 0 {
		return nil
	}

	handler := n.readPackageJSONHandler(pkgPaths[0])
	if handler == "" {
		for _, candidate := range []string{"index.js", "index.mjs", "handler.js", "handler.mjs"} {
			if _, err := os.Stat(filepath.Join(projectRoot, candidate)); err == nil {
				base := strings.TrimSuffix(candidate, filepath.Ext(candidate))
				handler = base + ".handler"
				break
			}
		}
	}
	if handler == "" {
		handler = "index.handler"
	}
	return []project.Function{
		{
			Name:        handlerExport(handler),
			Handler:     handler,
			Description: "Discovered from package.json",
		},
	}
}

func (n *nodeRuntime) readPackageJSONHandler(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := string(data)
	key := `"handler"`
	idx := strings.Index(content, key)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(content[idx+len(key):])
	if len(rest) == 0 || rest[0] != ':' {
		return ""
	}
	rest = strings.TrimSpace(rest[1:])
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	end := strings.Index(rest[1:], `"`)
	if end < 0 {
		return ""
	}
	return rest[1 : end+1]
}

func handlerExport(handler string) string {
	parts := strings.SplitN(handler, ".", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return handler
}

// ─── Runtime interface methods ────────────────────────────────────────────────

func (n *nodeRuntime) BuildArgs(projectRoot string) []string {
	data, err := os.ReadFile(filepath.Join(projectRoot, "package.json"))
	if err != nil {
		return nil
	}
	if strings.Contains(string(data), `"build"`) {
		return []string{"npm", "run", "build", "--prefix", projectRoot}
	}
	return nil
}

func (n *nodeRuntime) ShimDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".lambit", "node-runner")
}

func (n *nodeRuntime) InvokeArgs(projectRoot string, fn project.Function, payload string) []string {
	return []string{
		"node", filepath.Join(n.ShimDir(projectRoot), "runner.mjs"),
		fn.Handler, payload,
	}
}

func (n *nodeRuntime) ParseResult(stdout, stderr string, dur time.Duration) InvokeResult {
	stdout = strings.TrimSpace(stdout)
	stderr = strings.TrimSpace(stderr)
	success := stderr == "" || !strings.Contains(strings.ToLower(stderr), "error")
	errMsg := ""
	if !success {
		errMsg = stderr
	}
	return InvokeResult{
		Stdout:   stdout,
		Stderr:   stderr,
		Duration: dur,
		Success:  success,
		Error:    errMsg,
	}
}
