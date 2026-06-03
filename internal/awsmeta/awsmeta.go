// Package awsmeta detects AWS Lambda deployment metadata from local project
// files without requiring credentials or network access.
package awsmeta

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Mapping describes a local handler's likely AWS Lambda target.
type Mapping struct {
	FunctionName string
	Handler      string
	Region       string
	Profile      string
	CodeURI      string
	Source       string
}

// Detect scans projectRoot for SAM/CloudFormation templates and
// aws-lambda-tools-defaults.json files.
func Detect(projectRoot string) []Mapping {
	var mappings []Mapping
	walk(projectRoot, 5, func(path string) {
		switch filepath.Base(path) {
		case "template.yaml", "template.yml":
			mappings = append(mappings, parseTemplate(path)...)
		case "aws-lambda-tools-defaults.json":
			if m := parseToolsDefaults(path); m.FunctionName != "" || m.Handler != "" {
				mappings = append(mappings, m)
			}
		}
	})
	return mappings
}

func parseToolsDefaults(path string) Mapping {
	data, err := os.ReadFile(path)
	if err != nil {
		return Mapping{}
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return Mapping{}
	}
	return Mapping{
		FunctionName: stringValue(raw, "function-name"),
		Handler:      stringValue(raw, "function-handler"),
		Region:       firstString(raw, "region", "function-region", "aws-region"),
		Profile:      firstString(raw, "profile", "aws-profile"),
		Source:       path,
	}
}

func parseTemplate(path string) []Mapping {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	var mappings []Mapping
	inResources := false
	current := ""
	inLambda := false
	globalCodeURI := ""
	cur := Mapping{Source: path}
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			if inLambda {
				mappings = appendTemplateMapping(mappings, current, globalCodeURI, cur)
			}
			inResources = strings.HasSuffix(trimmed, ":") && strings.TrimSuffix(trimmed, ":") == "Resources"
			current = ""
			inLambda = false
			cur = Mapping{Source: path}
			continue
		}

		if strings.Contains(line, "CodeUri:") && globalCodeURI == "" && !inResources {
			globalCodeURI = yamlValue(line, "CodeUri:")
		}

		if !inResources {
			continue
		}

		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") && strings.HasSuffix(trimmed, ":") {
			if inLambda {
				mappings = appendTemplateMapping(mappings, current, globalCodeURI, cur)
			}
			current = strings.TrimSuffix(trimmed, ":")
			inLambda = false
			cur = Mapping{Source: path}
			continue
		}

		if strings.Contains(line, "Type:") && (strings.Contains(line, "AWS::Serverless::Function") || strings.Contains(line, "AWS::Lambda::Function")) {
			inLambda = true
		}
		if !inLambda {
			continue
		}
		switch {
		case strings.Contains(line, "Handler:"):
			cur.Handler = yamlValue(line, "Handler:")
		case strings.Contains(line, "FunctionName:"):
			cur.FunctionName = yamlValue(line, "FunctionName:")
		case strings.Contains(line, "CodeUri:"):
			cur.CodeURI = yamlValue(line, "CodeUri:")
		}
	}
	if inLambda {
		mappings = appendTemplateMapping(mappings, current, globalCodeURI, cur)
	}
	return mappings
}

func appendTemplateMapping(mappings []Mapping, resourceName, globalCodeURI string, m Mapping) []Mapping {
	if m.FunctionName == "" {
		m.FunctionName = resourceName
	}
	if m.CodeURI == "" {
		m.CodeURI = globalCodeURI
	}
	if m.FunctionName == "" && m.Handler == "" {
		return mappings
	}
	return append(mappings, m)
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

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if v := stringValue(raw, key); v != "" {
			return v
		}
	}
	return ""
}

func stringValue(raw map[string]any, key string) string {
	if v, ok := raw[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func walk(root string, maxDepth int, visit func(path string)) {
	baseDepth := strings.Count(filepath.Clean(root), string(os.PathSeparator))
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path != root && d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".lambit" || name == "node_modules" || name == "bin" || name == "obj" {
				return filepath.SkipDir
			}
			depth := strings.Count(filepath.Clean(path), string(os.PathSeparator)) - baseDepth
			if depth > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			visit(path)
		}
		return nil
	})
}
