package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveLoadAWSFunctionMapping(t *testing.T) {
	dir := t.TempDir()
	p := &Project{
		Path:    dir,
		Name:    "mapped",
		Runtime: "nodejs",
		Functions: []Function{
			{
				Name:            "handler",
				Handler:         "index.handler",
				AWSFunctionName: "prod-handler",
				AWSRegion:       "eu-west-1",
				AWSProfile:      "work",
			},
		},
	}
	if err := Save(p); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ProjectFile))
	if err != nil {
		t.Fatalf("reading project file: %v", err)
	}
	for _, want := range []string{"aws_function_name", "aws_region", "aws_profile"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("project file missing %q", want)
		}
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	fn := loaded.Functions[0]
	if fn.AWSFunctionName != "prod-handler" || fn.AWSRegion != "eu-west-1" || fn.AWSProfile != "work" {
		t.Fatalf("loaded mapping = %#v", fn)
	}
}
