package runtime

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wingitman/lambit/internal/project"
)

func init() {
	Register(&dotnetRuntime{})
}

type dotnetRuntime struct{}

func (d *dotnetRuntime) Name() string { return "dotnet" }

// Detect returns true when any .csproj file within 4 subdirectory levels
// references the Lambda SDK (Amazon.Lambda) or declares AWSProjectType.
func (d *dotnetRuntime) Detect(projectRoot string) bool {
	found := false
	walkFiles(projectRoot, 4, func(path string, _ int) {
		if found {
			return
		}
		if !strings.HasSuffix(path, ".csproj") {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		content := string(data)
		if strings.Contains(content, "Amazon.Lambda") || strings.Contains(content, "AWSProjectType") {
			found = true
		}
	})
	return found
}

// Scan discovers lambda handlers using a three-tier cascade:
//  1. template.yaml  — most accurate, covers multi-function SAM projects
//  2. aws-lambda-tools-defaults.json — per-function tool config
//  3. .csproj inference — best-guess fallback, flagged as needing review
func (d *dotnetRuntime) Scan(projectRoot string) ([]project.Function, error) {
	// Tier 1: template.yaml
	if fns := d.scanTemplateYAML(projectRoot); len(fns) > 0 {
		return fns, nil
	}
	// Tier 2: aws-lambda-tools-defaults.json
	if fns := d.scanToolsDefaults(projectRoot); len(fns) > 0 {
		return fns, nil
	}
	// Tier 3: .csproj inference
	return d.scanCSProjInference(projectRoot), nil
}

// ─── Tier 1: template.yaml ────────────────────────────────────────────────────

// scanTemplateYAML walks the project tree looking for template.yaml files and
// extracts dotnet Handler: strings (identified by the Assembly::Namespace::Method
// double-colon pattern). Each resource becomes a Function.
func (d *dotnetRuntime) scanTemplateYAML(projectRoot string) []project.Function {
	var fns []project.Function
	var yamlPaths []string

	walkFiles(projectRoot, 4, func(path string, _ int) {
		if filepath.Base(path) == "template.yaml" || filepath.Base(path) == "template.yml" {
			yamlPaths = append(yamlPaths, path)
		}
	})

	for _, yamlPath := range yamlPaths {
		fns = append(fns, d.parseTemplateYAML(yamlPath)...)
	}
	return fns
}

// parseTemplateYAML does a line-by-line parse of a SAM/CloudFormation
// template.yaml, collecting resources whose Handler: value contains "::"
// (the dotnet handler convention). It extracts the SAM resource key as the
// function name.
//
// The parser is intentionally simple — it does not handle anchors, multi-doc,
// etc. It works correctly on all standard SAM templates.
func (d *dotnetRuntime) parseTemplateYAML(path string) []project.Function {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")

	var fns []project.Function
	// Track the current top-level (2-space indent) resource key.
	currentResource := ""
	// Track whether we're inside a Properties block of a Lambda resource.
	inLambdaProperties := false

	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")

		// A line at indent 2 ending in ":" is a SAM resource key.
		// e.g. "  GetStockFunction:"
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") {
			trimmed := strings.TrimSpace(line)
			if strings.HasSuffix(trimmed, ":") && !strings.Contains(trimmed, " ") {
				currentResource = strings.TrimSuffix(trimmed, ":")
				inLambdaProperties = false
			}
		}

		// Detect "Type: AWS::Serverless::Function" or "Type: AWS::Lambda::Function"
		if strings.Contains(line, "Type:") &&
			(strings.Contains(line, "AWS::Serverless::Function") ||
				strings.Contains(line, "AWS::Lambda::Function")) {
			inLambdaProperties = true
		}

		// Extract Handler: value when we're in a lambda resource.
		if inLambdaProperties && strings.Contains(line, "Handler:") {
			parts := strings.SplitN(line, "Handler:", 2)
			if len(parts) == 2 {
				handler := strings.TrimSpace(parts[1])
				// Remove any inline YAML comment.
				if ci := strings.Index(handler, " #"); ci >= 0 {
					handler = strings.TrimSpace(handler[:ci])
				}
				// Only accept dotnet-style handlers (contain "::")
				if strings.Contains(handler, "::") {
					name := currentResource
					if name == "" {
						name = methodFromHandler(handler)
					}
					desc := "Discovered from template.yaml"
					// Peek a few lines ahead for a Description: field.
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
		}
	}
	return fns
}

// ─── Tier 2: aws-lambda-tools-defaults.json ───────────────────────────────────

func (d *dotnetRuntime) scanToolsDefaults(projectRoot string) []project.Function {
	var fns []project.Function

	walkFiles(projectRoot, 4, func(path string, _ int) {
		if filepath.Base(path) != "aws-lambda-tools-defaults.json" {
			return
		}
		dir := filepath.Dir(path)
		handler := d.readToolsDefaultsHandler(dir)
		if handler == "" || !strings.Contains(handler, "::") {
			return
		}
		name := filepath.Base(dir)
		if name == "" || name == "." {
			name = methodFromHandler(handler)
		}
		fns = append(fns, project.Function{
			Name:        name,
			Handler:     handler,
			Description: "Discovered from aws-lambda-tools-defaults.json",
		})
	})
	return fns
}

func (d *dotnetRuntime) readToolsDefaultsHandler(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "aws-lambda-tools-defaults.json"))
	if err != nil {
		return ""
	}
	content := string(data)
	key := `"function-handler"`
	idx := strings.Index(content, key)
	if idx < 0 {
		return ""
	}
	rest := content[idx+len(key):]
	colon := strings.Index(rest, ":")
	if colon < 0 {
		return ""
	}
	rest = strings.TrimSpace(rest[colon+1:])
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	end := strings.Index(rest[1:], `"`)
	if end < 0 {
		return ""
	}
	return rest[1 : end+1]
}

// ─── Tier 3: .csproj inference ────────────────────────────────────────────────

// csprojXML is used to extract AssemblyName / RootNamespace from a .csproj.
type csprojXML struct {
	PropertyGroup struct {
		AssemblyName  string `xml:"AssemblyName"`
		RootNamespace string `xml:"RootNamespace"`
	} `xml:"PropertyGroup"`
}

func (d *dotnetRuntime) scanCSProjInference(projectRoot string) []project.Function {
	var fns []project.Function

	walkFiles(projectRoot, 4, func(path string, _ int) {
		if !strings.HasSuffix(path, ".csproj") {
			return
		}
		// Skip test projects — names ending in Test, Tests, UnitTest, etc.
		base := strings.TrimSuffix(filepath.Base(path), ".csproj")
		lower := strings.ToLower(base)
		if strings.Contains(lower, "test") || strings.Contains(lower, "spec") {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		content := string(data)
		// Only include lambda projects.
		if !strings.Contains(content, "Amazon.Lambda") && !strings.Contains(content, "AWSProjectType") {
			return
		}
		var proj csprojXML
		if err := xml.Unmarshal(data, &proj); err != nil {
			return
		}
		assembly := proj.PropertyGroup.AssemblyName
		ns := proj.PropertyGroup.RootNamespace
		if assembly == "" {
			assembly = base
		}
		if ns == "" {
			ns = assembly
		}
		handler := assembly + "::" + ns + ".Function::FunctionHandler"
		fns = append(fns, project.Function{
			Name:        assembly,
			Handler:     handler,
			Description: "Inferred — verify the handler string in .lambit.toml",
		})
	})
	return fns
}

// ─── Shared helpers ───────────────────────────────────────────────────────────

// walkFiles calls fn for every regular file under root, up to maxDepth
// subdirectory levels deep (root itself is depth 0).
func walkFiles(root string, maxDepth int, fn func(path string, depth int)) {
	walkDir(root, 0, maxDepth, fn)
}

func walkDir(dir string, depth, maxDepth int, fn func(path string, depth int)) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		// Skip hidden directories and common noise directories.
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() {
			switch name {
			case "node_modules", "bin", "obj", "dist", ".git", "vendor", "testdata":
				continue
			}
			if depth < maxDepth {
				walkDir(filepath.Join(dir, name), depth+1, maxDepth, fn)
			}
		} else {
			fn(filepath.Join(dir, name), depth)
		}
	}
}

// methodFromHandler returns the method-name segment of a dotnet handler string
// (the part after the last "::"), or the full string if no "::" is present.
func methodFromHandler(handler string) string {
	parts := strings.Split(handler, "::")
	if len(parts) == 3 {
		return parts[2]
	}
	return handler
}

// ─── Runtime interface methods ────────────────────────────────────────────────

func (d *dotnetRuntime) BuildArgs(projectRoot string) []string {
	return []string{"dotnet", "build", projectRoot, "--nologo", "-v", "q"}
}

func (d *dotnetRuntime) ShimDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".lambit", "dotnet-runner")
}

func (d *dotnetRuntime) InvokeArgs(projectRoot string, fn project.Function, payload string) []string {
	shimDir := d.ShimDir(projectRoot)
	return []string{
		"dotnet", "run",
		"--project", shimDir,
		"--",
		fn.Handler,
		payload,
	}
}

func (d *dotnetRuntime) ParseResult(stdout, stderr string, dur time.Duration) InvokeResult {
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
