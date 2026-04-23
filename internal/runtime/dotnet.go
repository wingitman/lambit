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

// Detect returns true when any .csproj within 4 subdirectory levels references
// the Lambda SDK or declares AWSProjectType.
func (d *dotnetRuntime) Detect(projectRoot string) bool {
	found := false
	walkFiles(projectRoot, 4, func(path string, _ int) {
		if found || !strings.HasSuffix(path, ".csproj") {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		c := string(data)
		if strings.Contains(c, "Amazon.Lambda") || strings.Contains(c, "AWSProjectType") {
			found = true
		}
	})
	return found
}

// Scan discovers lambda handlers via a three-tier cascade:
//  1. template.yaml  — most accurate, handles multi-function SAM projects
//  2. aws-lambda-tools-defaults.json — per-function tool config
//  3. .csproj inference — best-guess fallback
func (d *dotnetRuntime) Scan(projectRoot string) ([]project.Function, error) {
	if fns := d.scanTemplateYAML(projectRoot); len(fns) > 0 {
		return fns, nil
	}
	if fns := d.scanToolsDefaults(projectRoot); len(fns) > 0 {
		return fns, nil
	}
	return d.scanCSProjInference(projectRoot), nil
}

// ─── Tier 1: template.yaml ────────────────────────────────────────────────────

func (d *dotnetRuntime) scanTemplateYAML(projectRoot string) []project.Function {
	var fns []project.Function
	walkFiles(projectRoot, 4, func(path string, _ int) {
		b := filepath.Base(path)
		if b == "template.yaml" || b == "template.yml" {
			fns = append(fns, parseTemplateYAMLHandlers(path, func(h string) bool {
				return strings.Contains(h, "::") // dotnet: Assembly::NS.Class::Method
			})...)
		}
	})
	return fns
}

// ─── Tier 2: aws-lambda-tools-defaults.json ───────────────────────────────────

func (d *dotnetRuntime) scanToolsDefaults(projectRoot string) []project.Function {
	var fns []project.Function
	walkFiles(projectRoot, 4, func(path string, _ int) {
		if filepath.Base(path) != "aws-lambda-tools-defaults.json" {
			return
		}
		handler := d.readToolsDefaultsHandler(filepath.Dir(path))
		if handler == "" || !strings.Contains(handler, "::") {
			return
		}
		name := filepath.Base(filepath.Dir(path))
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

// ─── Tier 3: .csproj inference ────────────────────────────────────────────────

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
		base := strings.TrimSuffix(filepath.Base(path), ".csproj")
		lower := strings.ToLower(base)
		if strings.Contains(lower, "test") || strings.Contains(lower, "spec") {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		c := string(data)
		if !strings.Contains(c, "Amazon.Lambda") && !strings.Contains(c, "AWSProjectType") {
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
		fns = append(fns, project.Function{
			Name:        assembly,
			Handler:     assembly + "::" + ns + ".Function::FunctionHandler",
			Description: "Inferred — verify the handler string in .lambit.toml",
		})
	})
	return fns
}

// ─── TestScanner implementation ───────────────────────────────────────────────

// ScanTests implements runtime.TestScanner.
// It walks test project directories for .cs files, finds [Fact]/[Theory]
// methods whose class name relates to the handler class of fn, and returns
// them as TestCase entries with Kind == TestCaseXUnit.
func (d *dotnetRuntime) ScanTests(projectRoot string, fn project.Function) []project.TestCase {
	handlerClass := handlerClassName(fn.Handler)
	if handlerClass == "" {
		return nil
	}
	assemblyName := handlerAssemblyName(fn.Handler)

	testDirs := d.findTestProjectDirs(projectRoot)

	// Build a set of test assembly names (from test .csproj filenames) that
	// share a prefix with this lambda's assembly. When a test project is named
	// e.g. "ServerlessTestSamples.UnitTest" and the lambda assembly is
	// "ServerlessTestSamples", all test classes in that project are fair game
	// even if their names don't directly mention the handler class.
	sharedPrefixDirs := map[string]bool{}
	for _, dir := range testDirs {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".csproj") {
				continue
			}
			projBase := strings.TrimSuffix(e.Name(), ".csproj")
			// Test project shares a prefix with the lambda assembly?
			if assemblyName != "" && strings.HasPrefix(
				strings.ToLower(projBase),
				strings.ToLower(assemblyName),
			) {
				sharedPrefixDirs[dir] = true
			}
		}
	}

	var results []project.TestCase
	for _, dir := range testDirs {
		broadMatch := sharedPrefixDirs[dir]
		walkFiles(dir, 3, func(path string, _ int) {
			if !strings.HasSuffix(path, ".cs") {
				return
			}
			results = append(results,
				d.scanCSFileForTests(path, handlerClass, assemblyName, broadMatch)...)
		})
	}
	return results
}

// InvokeTestArgs implements runtime.TestScanner.
func (d *dotnetRuntime) InvokeTestArgs(projectRoot string, tc project.TestCase) []string {
	dirs := d.findTestProjectDirs(projectRoot)
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".csproj") {
				return []string{
					"dotnet", "test", filepath.Join(dir, e.Name()),
					"--filter", tc.Filter,
					"--logger", "console;verbosity=detailed",
				}
			}
		}
	}
	return nil
}

// ParseTestResult implements runtime.TestScanner.
func (d *dotnetRuntime) ParseTestResult(stdout, stderr string, dur time.Duration) InvokeResult {
	stdout = strings.TrimSpace(stdout)
	stderr = strings.TrimSpace(stderr)
	combined := strings.ToLower(stdout + " " + stderr)
	// dotnet test outputs "Passed!" or "X passed" on success.
	success := strings.Contains(combined, "passed") && !strings.Contains(combined, "failed")
	// Also treat zero failures as success even if "passed" isn't shown.
	if strings.Contains(combined, "0 failed") {
		success = true
	}
	if strings.Contains(combined, "failed!") || strings.Contains(combined, ": failed") {
		success = false
	}
	errMsg := ""
	if !success {
		errMsg = stderr
		if errMsg == "" {
			errMsg = stdout
		}
	}
	return InvokeResult{
		Stdout:   stdout,
		Stderr:   stderr,
		Duration: dur,
		Success:  success,
		Error:    errMsg,
	}
}

// ─── Test project discovery ───────────────────────────────────────────────────

// findTestProjectDirs returns the directories of test .csproj files
// (those whose name contains "test"/"spec" OR whose content references xunit).
func (d *dotnetRuntime) findTestProjectDirs(projectRoot string) []string {
	var dirs []string
	seen := map[string]bool{}
	walkFiles(projectRoot, 4, func(path string, _ int) {
		if !strings.HasSuffix(path, ".csproj") {
			return
		}
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(path), ".csproj"))
		isTestByName := strings.Contains(base, "test") || strings.Contains(base, "spec")
		if !isTestByName {
			data, err := os.ReadFile(path)
			if err != nil || !strings.Contains(strings.ToLower(string(data)), "xunit") {
				return
			}
		}
		dir := filepath.Dir(path)
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	})
	return dirs
}

// ─── C# source scanner ────────────────────────────────────────────────────────

// scanCSFileForTests parses a .cs file for [Fact] and [Theory] methods whose
// class relates to handlerClass or assemblyName.
// If broadMatch is true, ALL test classes in the file are included (used when
// the test project shares a name prefix with the lambda assembly).
func (d *dotnetRuntime) scanCSFileForTests(
	path, handlerClass, assemblyName string, broadMatch bool,
) []project.TestCase {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")

	var results []project.TestCase
	currentNamespace := ""
	currentClass := ""
	classMatches := false

	// Pending attribute state.
	pendingFact := false
	pendingTheory := false
	var pendingInlineData []string

	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))

		// ── Namespace ──────────────────────────────────────────────────────
		if strings.HasPrefix(line, "namespace ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				currentNamespace = strings.TrimSuffix(parts[1], ";")
			}
			continue
		}

		// ── Class declaration ──────────────────────────────────────────────
		if isClassDeclaration(line) {
			currentClass = extractClassName(line)
			classMatches = broadMatch || testClassMatchesHandler(currentClass, handlerClass, assemblyName)
			// Reset pending state on new class.
			pendingFact = false
			pendingTheory = false
			pendingInlineData = nil
			continue
		}

		if !classMatches {
			continue
		}

		// ── [Fact] ─────────────────────────────────────────────────────────
		if line == "[Fact]" || strings.HasPrefix(line, "[Fact(") {
			pendingFact = true
			pendingTheory = false
			pendingInlineData = nil
			continue
		}

		// ── [Theory] ──────────────────────────────────────────────────────
		if line == "[Theory]" || strings.HasPrefix(line, "[Theory(") {
			pendingTheory = true
			pendingFact = false
			pendingInlineData = nil
			continue
		}

		// ── [InlineData(...)] ──────────────────────────────────────────────
		if pendingTheory && strings.HasPrefix(line, "[InlineData(") {
			val := extractInlineData(line)
			pendingInlineData = append(pendingInlineData, val)
			continue
		}

		// ── Other attributes — skip ────────────────────────────────────────
		if strings.HasPrefix(line, "[") && !isMethodSignature(line) {
			continue
		}

		// ── Method signature ───────────────────────────────────────────────
		if (pendingFact || pendingTheory) && isMethodSignature(line) {
			methodName := extractMethodName(line)
			if methodName == "" {
				pendingFact = false
				pendingTheory = false
				pendingInlineData = nil
				continue
			}
			filter := "FullyQualifiedName~" + currentNamespace + "." + currentClass + "." + methodName

			if pendingFact {
				results = append(results, project.TestCase{
					Name:   methodName,
					Kind:   project.TestCaseXUnit,
					Filter: filter,
				})
			} else {
				// Theory — one entry per InlineData variant (or one with no payload).
				if len(pendingInlineData) == 0 {
					results = append(results, project.TestCase{
						Name:   methodName,
						Kind:   project.TestCaseXUnit,
						Filter: filter,
					})
				} else {
					for _, val := range pendingInlineData {
						results = append(results, project.TestCase{
							Name:    methodName + "(" + val + ")",
							Kind:    project.TestCaseXUnit,
							Filter:  filter,
							Payload: val,
						})
					}
				}
			}
			pendingFact = false
			pendingTheory = false
			pendingInlineData = nil
		}
	}
	return results
}

// ─── C# parsing helpers ───────────────────────────────────────────────────────

func isClassDeclaration(line string) bool {
	// Matches: "public class Foo", "internal class Foo", "public sealed class Foo", etc.
	// Does not match interface or struct declarations.
	if !strings.Contains(line, "class ") {
		return false
	}
	// Must not be a variable declaration or comment.
	if strings.HasPrefix(line, "//") || strings.HasPrefix(line, "*") {
		return false
	}
	return true
}

func extractClassName(line string) string {
	// Find the word immediately after "class ".
	idx := strings.Index(line, "class ")
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(line[idx+6:])
	// Class name ends at whitespace, '(', ':', '<', '{'.
	for i, c := range rest {
		if c == ' ' || c == '(' || c == ':' || c == '<' || c == '{' {
			return rest[:i]
		}
	}
	return rest
}

// testClassMatchesHandler returns true if the test class name appears to test
// the given handler class. We strip common test prefixes/suffixes and check for
// substring containment in both directions, case-insensitive.
//
// Examples that should match:
//
//	handlerClass="Function"  testClass="MockGetProductFunctionTests"  → true
//	handlerClass="Functions" testClass="FunctionsTests"               → true
//	handlerClass="Function"  testClass="FunctionTest"                 → true
//	handlerClass="Function"  testClass="SomethingUnrelated"           → false
func testClassMatchesHandler(testClass, handlerClass, assemblyName string) bool {
	if testClass == "" || handlerClass == "" {
		return false
	}
	tc := strings.ToLower(testClass)
	hc := strings.ToLower(handlerClass)
	an := strings.ToLower(assemblyName)

	// Strip common test suffixes/prefixes from the test class name.
	stripped := tc
	for _, s := range []string{"integrationtest", "integrationtests", "unittest",
		"unittests", "tests", "test", "mock"} {
		stripped = strings.ReplaceAll(stripped, s, "")
	}
	stripped = strings.TrimSpace(stripped)

	// Check containment in both directions.
	if strings.Contains(tc, hc) {
		return true
	}
	if strings.Contains(hc, stripped) && stripped != "" {
		return true
	}
	// Also match against the assembly name (e.g. "GetProduct" matches "MockGetProductFunctionTests").
	if an != "" && strings.Contains(tc, an) {
		return true
	}
	return false
}

func isMethodSignature(line string) bool {
	// A method signature contains "(" and ends with ")" or "{" or starts a block.
	// It is NOT a class declaration, NOT an attribute, NOT a comment.
	if strings.HasPrefix(line, "[") || strings.HasPrefix(line, "//") ||
		strings.HasPrefix(line, "*") || strings.HasPrefix(line, "/*") {
		return false
	}
	if !strings.Contains(line, "(") {
		return false
	}
	// Must be a method: contains visibility modifier or known return type words.
	lower := strings.ToLower(line)
	for _, kw := range []string{"public ", "private ", "protected ", "internal ",
		"async ", "void ", "task", "static "} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func extractMethodName(line string) string {
	// Method name is the word immediately before the first "(".
	idx := strings.Index(line, "(")
	if idx < 0 {
		return ""
	}
	before := strings.TrimSpace(line[:idx])
	// The method name is the last token.
	parts := strings.Fields(before)
	if len(parts) == 0 {
		return ""
	}
	name := parts[len(parts)-1]
	// Sanity: must start with uppercase (C# convention) and contain only
	// identifier characters.
	if len(name) == 0 || (name[0] < 'A' || name[0] > 'Z') {
		return ""
	}
	return name
}

// extractInlineData returns the raw content inside [InlineData(...)].
// For single-value entries like [InlineData("POST")] it returns `"POST"`.
func extractInlineData(line string) string {
	start := strings.Index(line, "(")
	end := strings.LastIndex(line, ")")
	if start < 0 || end <= start {
		return ""
	}
	inner := strings.TrimSpace(line[start+1 : end])
	// Strip trailing "]" if present.
	inner = strings.TrimSuffix(inner, "]")
	return inner
}

// ─── Handler string helpers ───────────────────────────────────────────────────

// handlerClassName extracts the class name from a dotnet handler string.
// "Assembly::Namespace.ClassName::Method" → "ClassName"
func handlerClassName(handler string) string {
	parts := strings.Split(handler, "::")
	if len(parts) != 3 {
		return ""
	}
	nsParts := strings.Split(parts[1], ".")
	if len(nsParts) == 0 {
		return ""
	}
	return nsParts[len(nsParts)-1]
}

// handlerAssemblyName extracts the assembly name from a dotnet handler string.
// "Assembly::Namespace.ClassName::Method" → "Assembly"
func handlerAssemblyName(handler string) string {
	parts := strings.Split(handler, "::")
	if len(parts) != 3 {
		return ""
	}
	return parts[0]
}

// methodFromHandler returns the method segment of a dotnet handler string.
func methodFromHandler(handler string) string {
	parts := strings.Split(handler, "::")
	if len(parts) == 3 {
		return parts[2]
	}
	return handler
}

// ─── Shared file-walk helpers ─────────────────────────────────────────────────

// walkFiles calls fn for every regular file under root, up to maxDepth levels.
func walkFiles(root string, maxDepth int, fn func(path string, depth int)) {
	walkDir(root, 0, maxDepth, fn)
}

func walkDir(dir string, depth, maxDepth int, fn func(path string, depth int)) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
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

// ─── Runtime interface methods ────────────────────────────────────────────────

func (d *dotnetRuntime) BuildArgs(projectRoot string) []string {
	return []string{"dotnet", "build", projectRoot, "--nologo", "-v", "q"}
}

// BuildFunctionArgs returns build args targeting the specific .csproj for fn,
// avoiding the "MSB1003: Specify a project or solution file" error that occurs
// when dotnet build is run against a directory with no project at its root.
// Accessed via type assertion: rt.(interface{ BuildFunctionArgs(...) []string }).
func (d *dotnetRuntime) BuildFunctionArgs(projectRoot string, fn project.Function) []string {
	assembly := handlerAssemblyName(fn.Handler)
	if assembly != "" {
		if csproj := d.findLambdaCSProj(projectRoot, assembly); csproj != "" {
			return []string{"dotnet", "build", csproj, "--nologo", "-v", "q"}
		}
	}
	// Fallback to the generic build.
	return d.BuildArgs(projectRoot)
}

// findLambdaCSProj walks the project tree for <assembly>.csproj (case-insensitive).
func (d *dotnetRuntime) findLambdaCSProj(projectRoot, assembly string) string {
	target := strings.ToLower(assembly + ".csproj")
	found := ""
	walkFiles(projectRoot, 4, func(path string, _ int) {
		if found != "" {
			return
		}
		if strings.ToLower(filepath.Base(path)) == target {
			found = path
		}
	})
	return found
}

func (d *dotnetRuntime) ShimDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".lambit", "dotnet-runner")
}

func (d *dotnetRuntime) InvokeArgs(projectRoot string, fn project.Function, payload string) []string {
	return []string{
		"dotnet", "run", "--project", d.ShimDir(projectRoot),
		"--no-build", // skip shim rebuild — the lambda was already built by BuildArgs
		"--", fn.Handler, payload,
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

// ─── SourceLocator implementation ────────────────────────────────────────────

// FindTestSource implements runtime.SourceLocator.
// For xUnit tests it finds the .cs file and line of the [Fact]/[Theory] attribute.
// For payload tests it finds the .lambit.toml line containing the test name.
func (d *dotnetRuntime) FindTestSource(projectRoot string, tc project.TestCase) (string, int, bool) {
	if tc.Kind == project.TestCaseXUnit {
		return d.findXUnitSource(projectRoot, tc)
	}
	return findInTOML(projectRoot, tc.Name)
}

// FindFunctionSource implements runtime.SourceLocator.
// Returns the .lambit.toml path and line of the function's handler entry.
func (d *dotnetRuntime) FindFunctionSource(projectRoot string, fn project.Function) (string, int, bool) {
	return findInTOML(projectRoot, fn.Handler)
}

// FindModelSource implements runtime.SourceLocator.
// Returns the .lambit.toml path and line of the model's name entry.
func (d *dotnetRuntime) FindModelSource(projectRoot string, mdl project.Model) (string, int, bool) {
	return findInTOML(projectRoot, mdl.Name)
}

// findXUnitSource locates the [Fact]/[Theory] attribute line for an xUnit test.
// The Filter field is "FullyQualifiedName~Namespace.ClassName.MethodName".
func (d *dotnetRuntime) findXUnitSource(projectRoot string, tc project.TestCase) (string, int, bool) {
	// Extract ClassName and MethodName from the filter string.
	// Filter format: "FullyQualifiedName~Ns.ClassName.MethodName" or "...~Ns.ClassName.MethodName(variant)"
	filter := tc.Filter
	tilde := strings.LastIndex(filter, "~")
	if tilde < 0 {
		return "", 0, false
	}
	qualified := filter[tilde+1:]
	// Strip any variant suffix "(..."
	if pi := strings.Index(qualified, "("); pi >= 0 {
		qualified = qualified[:pi]
	}
	// qualified = "Namespace.ClassName.MethodName"
	parts := strings.Split(qualified, ".")
	if len(parts) < 2 {
		return "", 0, false
	}
	methodName := parts[len(parts)-1]
	className := parts[len(parts)-2]

	for _, dir := range d.findTestProjectDirs(projectRoot) {
		var found string
		var foundLine int
		walkFiles(dir, 3, func(path string, _ int) {
			if found != "" || !strings.HasSuffix(path, ".cs") {
				return
			}
			file, line, ok := findMethodInCSFile(path, className, methodName)
			if ok {
				found = file
				foundLine = line
			}
		})
		if found != "" {
			return found, foundLine, true
		}
	}
	return "", 0, false
}

// findMethodInCSFile scans a .cs file for a [Fact]/[Theory] attribute followed
// by the given method in the given class. Returns the line number of the
// attribute (so the editor opens one line above the method declaration).
func findMethodInCSFile(path, className, methodName string) (string, int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, false
	}
	lines := strings.Split(string(data), "\n")

	inClass := false
	pendingAttrLine := -1

	for i, raw := range lines {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		lineNum := i + 1 // 1-based

		// Track class entry.
		if isClassDeclaration(line) {
			cn := extractClassName(line)
			inClass = strings.EqualFold(cn, className)
			pendingAttrLine = -1
			continue
		}

		if !inClass {
			continue
		}

		// Record [Fact] / [Theory] attribute lines.
		if line == "[Fact]" || strings.HasPrefix(line, "[Fact(") ||
			line == "[Theory]" || strings.HasPrefix(line, "[Theory(") {
			pendingAttrLine = lineNum
			continue
		}

		// Skip other attribute lines without resetting.
		if strings.HasPrefix(line, "[") && !isMethodSignature(line) {
			continue
		}

		// Method signature after a pending attribute.
		if pendingAttrLine > 0 && isMethodSignature(line) {
			name := extractMethodName(line)
			if strings.EqualFold(name, methodName) {
				return path, pendingAttrLine, true
			}
			pendingAttrLine = -1
		}
	}
	return "", 0, false
}

// findInTOML scans the .lambit.toml file in projectRoot for a line containing
// the given search string inside a quoted value, and returns its 1-based line.
func findInTOML(projectRoot, search string) (string, int, bool) {
	tomlPath := filepath.Join(projectRoot, project.ProjectFile)
	data, err := os.ReadFile(tomlPath)
	if err != nil {
		return "", 0, false
	}
	escaped := strings.ReplaceAll(search, `"`, `\"`)
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.Contains(line, `"`+escaped+`"`) ||
			strings.Contains(line, `'`+search+`'`) {
			return tomlPath, i + 1, true
		}
	}
	return tomlPath, 1, false // file found but line not matched — return line 1
}
