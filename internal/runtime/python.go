package runtime

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/wingitman/lambit/internal/project"
)

func init() {
	Register(&pythonRuntime{})
}

type pythonRuntime struct{}

func (p *pythonRuntime) Name() string { return "python" }

func isPythonRuntime(s string) bool {
	return strings.Contains(strings.ToLower(s), "python")
}

// Detect returns true when a SAM template declares a Python runtime, or when a
// project has common Python Lambda files/configuration.
func (p *pythonRuntime) Detect(projectRoot string) bool {
	found := false
	walkFiles(projectRoot, 4, func(path string, _ int) {
		if found {
			return
		}
		switch filepath.Base(path) {
		case "template.yaml", "template.yml":
			data, err := os.ReadFile(path)
			if err == nil && strings.Contains(strings.ToLower(string(data)), "runtime:") && strings.Contains(strings.ToLower(string(data)), "python") {
				found = true
			}
		case "requirements.txt", "pyproject.toml", "lambda_function.py", "app.py", "handler.py":
			found = true
		}
	})
	return found
}

// Scan discovers Python Lambda handlers via SAM template first, then common
// Python handler files as a fallback.
func (p *pythonRuntime) Scan(projectRoot string) ([]project.Function, error) {
	if fns := p.scanTemplateYAML(projectRoot); len(fns) > 0 {
		return fns, nil
	}
	return p.scanPythonFiles(projectRoot), nil
}

func (p *pythonRuntime) scanTemplateYAML(projectRoot string) []project.Function {
	var fns []project.Function
	walkFiles(projectRoot, 4, func(path string, _ int) {
		b := filepath.Base(path)
		if b == "template.yaml" || b == "template.yml" {
			fns = append(fns, parsePythonTemplateYAML(path, projectRoot)...)
		}
	})
	return fns
}

type pythonTemplateFn struct {
	resourceName string
	handler      string
	runtime      string
	codeURI      string
	description  string
}

func parsePythonTemplateYAML(path, projectRoot string) []project.Function {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	inResources := false
	globalRuntime := ""
	globalCodeURI := ""
	inLambda := false
	cur := pythonTemplateFn{}
	var parsed []pythonTemplateFn

	flush := func() {
		if !inLambda {
			return
		}
		if cur.runtime == "" {
			cur.runtime = globalRuntime
		}
		if cur.codeURI == "" {
			cur.codeURI = globalCodeURI
		}
		if cur.handler != "" && isPythonRuntime(cur.runtime) {
			parsed = append(parsed, cur)
		}
	}

	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			flush()
			inResources = strings.HasSuffix(trimmed, ":") && strings.TrimSuffix(trimmed, ":") == "Resources"
			inLambda = false
			cur = pythonTemplateFn{}
			continue
		}

		if !inResources {
			if strings.Contains(line, "Runtime:") && globalRuntime == "" {
				globalRuntime = yamlValue(line, "Runtime:")
			}
			if strings.Contains(line, "CodeUri:") && globalCodeURI == "" {
				globalCodeURI = yamlValue(line, "CodeUri:")
			}
			continue
		}

		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") && strings.HasSuffix(trimmed, ":") {
			flush()
			cur = pythonTemplateFn{resourceName: strings.TrimSuffix(trimmed, ":")}
			inLambda = false
			continue
		}
		if strings.Contains(line, "Type:") && (strings.Contains(line, "AWS::Serverless::Function") || strings.Contains(line, "AWS::Lambda::Function")) {
			inLambda = true
			continue
		}
		if !inLambda {
			continue
		}
		switch {
		case strings.Contains(line, "Handler:"):
			cur.handler = yamlValue(line, "Handler:")
		case strings.Contains(line, "Runtime:"):
			cur.runtime = yamlValue(line, "Runtime:")
		case strings.Contains(line, "CodeUri:"):
			cur.codeURI = yamlValue(line, "CodeUri:")
		case strings.Contains(line, "Description:") && cur.description == "":
			cur.description = yamlValue(line, "Description:")
		}
	}
	flush()

	fns := make([]project.Function, 0, len(parsed))
	for _, item := range parsed {
		name := handlerExport(item.handler)
		if item.resourceName != "" {
			name = item.resourceName
		}
		desc := item.description
		if desc == "" {
			desc = "Discovered from template.yaml"
		}
		fn := project.Function{Name: name, Handler: item.handler, Description: desc}
		if root := cleanCodeURI(projectRoot, path, item.codeURI); root != "" {
			fn.Root = root
		}
		fns = append(fns, fn)
	}
	return fns
}

func yamlValue(line, key string) string {
	idx := strings.Index(line, key)
	if idx < 0 {
		return ""
	}
	val := strings.TrimSpace(line[idx+len(key):])
	if ci := strings.Index(val, " #"); ci >= 0 {
		val = strings.TrimSpace(val[:ci])
	}
	return strings.Trim(val, `"'`)
}

func cleanCodeURI(projectRoot, templatePath, codeURI string) string {
	codeURI = strings.TrimSpace(codeURI)
	if codeURI == "" || strings.HasPrefix(codeURI, "s3://") {
		return ""
	}
	abs := codeURI
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(filepath.Dir(templatePath), codeURI)
	}
	rel, err := filepath.Rel(projectRoot, filepath.Clean(abs))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return filepath.ToSlash(rel)
}

func (p *pythonRuntime) scanPythonFiles(projectRoot string) []project.Function {
	preferred := []string{"lambda_function.py", "app.py", "handler.py", "index.py"}
	for _, name := range preferred {
		path := filepath.Join(projectRoot, name)
		if handlers := scanPythonHandlerFile(projectRoot, path); len(handlers) > 0 {
			return handlers
		}
	}
	var fns []project.Function
	walkFiles(projectRoot, 3, func(path string, _ int) {
		if len(fns) > 0 || !strings.HasSuffix(path, ".py") || strings.HasSuffix(path, "_test.py") || strings.HasPrefix(filepath.Base(path), "test_") {
			return
		}
		fns = append(fns, scanPythonHandlerFile(projectRoot, path)...)
	})
	return fns
}

func scanPythonHandlerFile(projectRoot, path string) []project.Function {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	module := pythonModuleName(projectRoot, path)
	if module == "" {
		return nil
	}
	var fns []project.Function
	re := regexp.MustCompile(`^def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		m := re.FindStringSubmatch(line)
		if len(m) != 2 {
			continue
		}
		name := m[1]
		if name != "lambda_handler" && name != "handler" {
			continue
		}
		handler := module + "." + name
		fns = append(fns, project.Function{
			Name:        name,
			Handler:     handler,
			Description: "Discovered from Python source",
		})
	}
	return fns
}

func pythonModuleName(projectRoot, path string) string {
	rel, err := filepath.Rel(projectRoot, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	rel = strings.TrimSuffix(filepath.ToSlash(rel), ".py")
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		if part == "__init__" {
			parts = parts[:i]
			break
		}
	}
	return strings.Join(parts, ".")
}

func (p *pythonRuntime) BuildArgs(projectRoot string) []string { return nil }

func (p *pythonRuntime) ShimDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".lambit", "python-runner")
}

func (p *pythonRuntime) InvokeArgs(projectRoot string, fn project.Function, payload string) []string {
	return []string{"python3", "-c", pythonRunnerCode, fn.Handler, payload}
}

const pythonRunnerCode = `import contextlib, importlib, inspect, json, sys, traceback
handler = sys.argv[1]
payload = sys.argv[2]
module_name, func_name = handler.rsplit('.', 1)
try:
    event = json.loads(payload) if payload else None
    module = importlib.import_module(module_name)
    fn = getattr(module, func_name)
    params = inspect.signature(fn).parameters
    with contextlib.redirect_stdout(sys.stderr):
        if len(params) >= 2:
            result = fn(event, None)
        else:
            result = fn(event)
    sys.stdout.write(json.dumps(result, default=str))
except Exception:
    traceback.print_exc(file=sys.stderr)
    sys.exit(1)
`

func (p *pythonRuntime) ParseResult(stdout, stderr string, dur time.Duration) InvokeResult {
	stdout = strings.TrimSpace(stdout)
	stderr = strings.TrimSpace(stderr)
	success := !strings.Contains(stderr, "Traceback (most recent call last):")
	errMsg := ""
	if !success {
		errMsg = stderr
	}
	return InvokeResult{Stdout: stdout, Stderr: stderr, Duration: dur, Success: success, Error: errMsg}
}

func (p *pythonRuntime) FindFunctionSource(projectRoot string, fn project.Function) (string, int, bool) {
	lastDot := strings.LastIndex(fn.Handler, ".")
	if lastDot < 0 {
		return findInTOML(projectRoot, fn.Handler)
	}
	module := fn.Handler[:lastDot]
	funcName := fn.Handler[lastDot+1:]
	path := filepath.Join(projectRoot, filepath.FromSlash(strings.ReplaceAll(module, ".", "/")+".py"))
	if line, ok := findPythonFunction(path, funcName); ok {
		return path, line, true
	}
	initPath := filepath.Join(projectRoot, filepath.FromSlash(strings.ReplaceAll(module, ".", "/")), "__init__.py")
	if line, ok := findPythonFunction(initPath, funcName); ok {
		return initPath, line, true
	}
	return findInTOML(projectRoot, fn.Handler)
}

func findPythonFunction(path, funcName string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	re := regexp.MustCompile(`^\s*def\s+` + regexp.QuoteMeta(funcName) + `\s*\(`)
	for i, raw := range strings.Split(string(data), "\n") {
		if re.MatchString(raw) {
			return i + 1, true
		}
	}
	return 0, false
}

func (p *pythonRuntime) FindTestSource(_ string, _ project.TestCase) (string, int, bool) {
	return "", 0, false
}

func (p *pythonRuntime) FindModelSource(_ string, _ project.Model) (string, int, bool) {
	return "", 0, false
}
