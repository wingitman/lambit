package awsmeta

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectTemplateMappings(t *testing.T) {
	dir := t.TempDir()
	template := `AWSTemplateFormatVersion: '2010-09-09'
Globals:
  Function:
    CodeUri: src/Fn/
Resources:
  LogicalFunction:
    Type: AWS::Serverless::Function
    Properties:
      FunctionName: physical-name
      Handler: index.handler
`
	if err := os.WriteFile(filepath.Join(dir, "template.yaml"), []byte(template), 0644); err != nil {
		t.Fatal(err)
	}
	mappings := Detect(dir)
	if len(mappings) != 1 {
		t.Fatalf("len(mappings) = %d, want 1: %#v", len(mappings), mappings)
	}
	m := mappings[0]
	if m.FunctionName != "physical-name" || m.Handler != "index.handler" || m.CodeURI != "src/Fn/" {
		t.Fatalf("mapping = %#v", m)
	}
}

func TestDetectToolsDefaultsMapping(t *testing.T) {
	dir := t.TempDir()
	data := `{
  "function-name": "prod-dotnet",
  "function-handler": "Assembly::Namespace.Function::Handler",
  "region": "ap-southeast-2",
  "profile": "work"
}`
	if err := os.WriteFile(filepath.Join(dir, "aws-lambda-tools-defaults.json"), []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	mappings := Detect(dir)
	if len(mappings) != 1 {
		t.Fatalf("len(mappings) = %d, want 1: %#v", len(mappings), mappings)
	}
	m := mappings[0]
	if m.FunctionName != "prod-dotnet" || m.Handler != "Assembly::Namespace.Function::Handler" || m.Region != "ap-southeast-2" || m.Profile != "work" {
		t.Fatalf("mapping = %#v", m)
	}
}
