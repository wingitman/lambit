package config

import (
	"strings"
	"testing"
)

func TestDefaultConfigIncludesUpdates(t *testing.T) {
	cfg := Default()
	if cfg.Keybinds.ShowUpdates != "U" {
		t.Fatalf("show_updates = %q, want U", cfg.Keybinds.ShowUpdates)
	}
	if cfg.Keybinds.Cloud != "A" {
		t.Fatalf("cloud = %q, want A", cfg.Keybinds.Cloud)
	}
	if cfg.AWS.CLI != "aws" {
		t.Fatalf("aws.cli = %q, want aws", cfg.AWS.CLI)
	}
	if cfg.AWS.Region != "us-east-1" {
		t.Fatalf("aws.region = %q, want us-east-1", cfg.AWS.Region)
	}

	toml := buildTOML(cfg)
	for _, want := range []string{
		"cloud",
		"[aws]",
		"cli",
		"profile",
		"region",
		"download_dir",
		"show_updates",
		"[updates]",
		"disable_checks",
		"current_commit",
		"repo_path",
		"terminal",
	} {
		if !strings.Contains(toml, want) {
			t.Fatalf("default TOML missing %q", want)
		}
	}
}
