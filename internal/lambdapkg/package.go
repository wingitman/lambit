// Package lambdapkg builds deployment zips for Lambda functions.
package lambdapkg

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wingitman/lambit/internal/archiveutil"
	"github.com/wingitman/lambit/internal/project"
)

// Build creates a deployment zip for fn and returns the zip path and build log.
func Build(runtimeName, projectRoot string, fn project.Function) (string, string, error) {
	outDir := filepath.Join(projectRoot, ".lambit", "deploy", safeName(fn.Name))
	zipPath := outDir + ".zip"
	if err := os.RemoveAll(outDir); err != nil {
		return "", "", err
	}
	if err := os.Remove(zipPath); err != nil && !os.IsNotExist(err) {
		return "", "", err
	}

	switch runtimeName {
	case "dotnet":
		log, err := publishDotnet(projectRoot, fn, outDir)
		if err != nil {
			return "", log, err
		}
		if err := archiveutil.ZipDir(outDir, zipPath, nil); err != nil {
			return "", log, err
		}
		return zipPath, log, nil
	case "nodejs":
		log, err := buildNode(projectRoot)
		if err != nil {
			return "", log, err
		}
		exclude := func(rel string, info os.FileInfo) bool {
			first := strings.Split(rel, "/")[0]
			if first == ".git" || first == ".lambit" {
				return true
			}
			name := info.Name()
			return name == zipPath || strings.HasSuffix(name, ".heapsnapshot")
		}
		if err := archiveutil.ZipDir(projectRoot, zipPath, exclude); err != nil {
			return "", log, err
		}
		return zipPath, log, nil
	default:
		return "", "", fmt.Errorf("automatic packaging is not implemented for runtime %q", runtimeName)
	}
}

func publishDotnet(projectRoot string, fn project.Function, outDir string) (string, error) {
	csproj := findDotnetProject(projectRoot, fn)
	if csproj == "" {
		return "", fmt.Errorf("could not find .csproj for %s", fn.Name)
	}
	args := []string{"publish", csproj, "-c", "Release", "-o", outDir, "--nologo"}
	return run(projectRoot, "dotnet", args...)
}

func findDotnetProject(projectRoot string, fn project.Function) string {
	assembly := fn.Handler
	if idx := strings.Index(assembly, "::"); idx >= 0 {
		assembly = assembly[:idx]
	}
	var first, matched string
	_ = filepath.WalkDir(projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || matched != "" {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != projectRoot && (name == ".git" || name == ".lambit" || name == "bin" || name == "obj") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".csproj") {
			return nil
		}
		base := strings.TrimSuffix(filepath.Base(path), ".csproj")
		lower := strings.ToLower(base)
		if strings.Contains(lower, "test") || strings.Contains(lower, "spec") {
			return nil
		}
		if first == "" {
			first = path
		}
		if assembly != "" && strings.EqualFold(base, assembly) {
			matched = path
		}
		return nil
	})
	if matched != "" {
		return matched
	}
	return first
}

func buildNode(projectRoot string) (string, error) {
	data, err := os.ReadFile(filepath.Join(projectRoot, "package.json"))
	if err != nil {
		return "", nil
	}
	if !strings.Contains(string(data), `"build"`) {
		return "", nil
	}
	return run(projectRoot, "npm", "run", "build")
}

func run(dir, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if ctx.Err() != nil {
		return out.String(), ctx.Err()
	}
	return out.String(), err
}

func safeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "lambda"
	}
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}
