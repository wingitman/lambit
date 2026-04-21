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

// nodeRuntimes is the set of SAM/CloudFormation runtime names that map to Node.
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
// references aws-lambda / @aws-sdk, or when a template.yaml declares a
// Node runtime.
func (n *nodeRuntime) Detect(projectRoot string) bool {
	found := false
	walkFiles(projectRoot, 4, func(path string, _ int) {
		if found {
			return
		}
		base := filepath.Base(path)
		switch base {
		case "package.json":
			data, err := os.ReadFile(path)
			if err != nil {
				return
			}
			content := string(data)
			if strings.Contains(content, "aws-lambda") ||
				strings.Contains(content, "@aws-sdk") {
				found = true
			}
		case "template.yaml", "template.yml":
			data, err := os.ReadFile(path)
			if err != nil {
				return
			}
			// Quick check: does the template declare a Node runtime?
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

// Scan discovers Node lambda handlers using a two-tier cascade:
//  1. template.yaml  — most accurate, covers multi-function SAM projects
//  2. package.json   — single-function fallback
func (n *nodeRuntime) Scan(projectRoot string) ([]project.Function, error) {
	// Tier 1: template.yaml
	if fns := n.scanTemplateYAML(projectRoot); len(fns) > 0 {
		return fns, nil
	}
	// Tier 2: package.json handler field
	return n.scanPackageJSON(projectRoot), nil
}

// ─── Tier 1: template.yaml ────────────────────────────────────────────────────

func (n *nodeRuntime) scanTemplateYAML(projectRoot string) []project.Function {
	var fns []project.Function
	var yamlPaths []string

	walkFiles(projectRoot, 4, func(path string, _ int) {
		base := filepath.Base(path)
		if base == "template.yaml" || base == "template.yml" {
			yamlPaths = append(yamlPaths, path)
		}
	})

	for _, yamlPath := range yamlPaths {
		fns = append(fns, n.parseTemplateYAML(yamlPath)...)
	}
	return fns
}

// parseTemplateYAML extracts Node handler entries from a SAM template.
// Node handlers follow the pattern "file.export" (no "::" double-colon).
func (n *nodeRuntime) parseTemplateYAML(path string) []project.Function {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")

	var fns []project.Function
	currentResource := ""
	inLambdaResource := false
	resourceRuntime := ""

	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")

		// SAM resource key at 2-space indent.
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") {
			trimmed := strings.TrimSpace(line)
			if strings.HasSuffix(trimmed, ":") && !strings.Contains(trimmed, " ") {
				currentResource = strings.TrimSuffix(trimmed, ":")
				inLambdaResource = false
				resourceRuntime = ""
			}
		}

		if strings.Contains(line, "Type:") &&
			(strings.Contains(line, "AWS::Serverless::Function") ||
				strings.Contains(line, "AWS::Lambda::Function")) {
			inLambdaResource = true
		}

		if inLambdaResource && strings.Contains(line, "Runtime:") {
			parts := strings.SplitN(line, "Runtime:", 2)
			if len(parts) == 2 {
				resourceRuntime = strings.TrimSpace(parts[1])
			}
		}

		if inLambdaResource && strings.Contains(line, "Handler:") {
			parts := strings.SplitN(line, "Handler:", 2)
			if len(parts) != 2 {
				continue
			}
			handler := strings.TrimSpace(parts[1])
			if ci := strings.Index(handler, " #"); ci >= 0 {
				handler = strings.TrimSpace(handler[:ci])
			}
			// Only accept node-style handlers (no "::", contains ".").
			if strings.Contains(handler, "::") {
				continue
			}
			if !strings.Contains(handler, ".") {
				continue
			}
			// Check that the runtime for this resource is Node (or unset,
			// in which case we accept it only if the overall template has a
			// Node runtime hint).
			if resourceRuntime != "" && !isNodeRuntime(resourceRuntime) {
				continue
			}

			name := currentResource
			if name == "" {
				name = handlerExport(handler)
			}
			desc := "Discovered from template.yaml"
			// Peek ahead for a Description: field.
			for j := i + 1; j < i+10 && j < len(lines); j++ {
				if strings.Contains(lines[j], "Description:") {
					dp := strings.SplitN(lines[j], "Description:", 2)
					if len(dp) == 2 {
						desc = strings.Trim(strings.TrimSpace(dp[1]), `'"`)
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

// ─── Tier 2: package.json ─────────────────────────────────────────────────────

func (n *nodeRuntime) scanPackageJSON(projectRoot string) []project.Function {
	var pkgPaths []string
	walkFiles(projectRoot, 3, func(path string, _ int) {
		if filepath.Base(path) == "package.json" {
			pkgPaths = append(pkgPaths, path)
		}
	})

	// Prefer the root-level package.json if there are multiple.
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
		// Check for common handler files.
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

// handlerExport returns the export name from a "file.export" handler string.
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
	shimDir := n.ShimDir(projectRoot)
	return []string{
		"node",
		filepath.Join(shimDir, "runner.mjs"),
		fn.Handler,
		payload,
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
