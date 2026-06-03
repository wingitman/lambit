package runtime

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPythonRuntimeScansTemplateYAML(t *testing.T) {
	dir := t.TempDir()
	template := `Globals:
  Function:
    Runtime: python3.12
    CodeUri: src/
Resources:
  WorkerFunction:
    Type: AWS::Serverless::Function
    Description: Runs work
    Properties:
      Handler: app.lambda_handler
  NodeFunction:
    Type: AWS::Serverless::Function
    Properties:
      Runtime: nodejs20.x
      Handler: index.handler
`
	if err := os.WriteFile(filepath.Join(dir, "template.yaml"), []byte(template), 0644); err != nil {
		t.Fatal(err)
	}
	rt := &pythonRuntime{}
	fns, err := rt.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(fns) != 1 {
		t.Fatalf("len(fns) = %d, want 1: %#v", len(fns), fns)
	}
	fn := fns[0]
	if fn.Name != "WorkerFunction" || fn.Handler != "app.lambda_handler" || fn.Root != "src" || fn.Description != "Runs work" {
		t.Fatalf("fn = %#v", fn)
	}
}

func TestPythonParseResultAllowsStderrLogs(t *testing.T) {
	rt := &pythonRuntime{}
	res := rt.ParseResult(`{"ok": true}`, "hello from print", time.Millisecond)
	if !res.Success {
		t.Fatalf("Success = false, want true: %#v", res)
	}
	res = rt.ParseResult("", "Traceback (most recent call last):\nboom", time.Millisecond)
	if res.Success {
		t.Fatalf("Success = true, want false: %#v", res)
	}
}

func TestPythonRuntimeScansSourceAndFindsHandler(t *testing.T) {
	dir := t.TempDir()
	source := `def helper(event):
    return None

def lambda_handler(event, context):
    return {"ok": True}
`
	if err := os.WriteFile(filepath.Join(dir, "lambda_function.py"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	rt := &pythonRuntime{}
	if !rt.Detect(dir) {
		t.Fatal("Detect() = false, want true")
	}
	fns, err := rt.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(fns) != 1 || fns[0].Handler != "lambda_function.lambda_handler" {
		t.Fatalf("fns = %#v", fns)
	}
	file, line, ok := rt.FindFunctionSource(dir, fns[0])
	if !ok || file != filepath.Join(dir, "lambda_function.py") || line != 4 {
		t.Fatalf("source = %q:%d ok=%v", file, line, ok)
	}
}
