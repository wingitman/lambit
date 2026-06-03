// Package awscli wraps the AWS CLI commands used by Lambit's Cloud tab.
package awscli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Client shells out to AWS CLI. It does not read or store credentials.
type Client struct {
	CLI     string
	Profile string
	Region  string
}

// Function is the subset of Lambda metadata Lambit displays.
type Function struct {
	Name         string
	Runtime      string
	Handler      string
	LastModified string
}

// Profile describes an AWS CLI profile and whether its config block looks SSO-capable.
type Profile struct {
	Name       string
	SSO        bool
	Configured bool
}

func (c Client) binary() string {
	if strings.TrimSpace(c.CLI) == "" {
		return "aws"
	}
	return c.CLI
}

// Available reports whether the configured AWS CLI executable can be found.
func (c Client) Available() error {
	if _, err := exec.LookPath(c.binary()); err != nil {
		return fmt.Errorf("AWS CLI executable not found: %s", c.binary())
	}
	return nil
}

func (c Client) profileRegionArgs() []string {
	var args []string
	if strings.TrimSpace(c.Profile) != "" {
		args = append(args, "--profile", c.Profile)
	}
	if strings.TrimSpace(c.Region) != "" {
		args = append(args, "--region", c.Region)
	}
	return args
}

// ListProfiles returns configured AWS CLI profile names.
func (c Client) ListProfiles(ctx context.Context) ([]string, error) {
	out, err := c.run(ctx, "configure", "list-profiles")
	if err != nil {
		return nil, err
	}
	var profiles []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			profiles = append(profiles, line)
		}
	}
	return profiles, nil
}

// ListProfileInfo returns profiles annotated with SSO config metadata.
func (c Client) ListProfileInfo(ctx context.Context) ([]Profile, error) {
	profiles, err := c.ListProfiles(ctx)
	if err != nil {
		return nil, err
	}
	configProfiles := LoadConfigProfiles(DefaultConfigPath())
	out := make([]Profile, 0, len(profiles))
	for _, name := range profiles {
		info := configProfiles[name]
		out = append(out, Profile{Name: name, SSO: info.SSO, Configured: info.Configured})
	}
	return out, nil
}

// SSOLogin runs aws sso login for the configured profile.
func (c Client) SSOLogin(ctx context.Context) (string, error) {
	args := []string{"sso", "login"}
	if strings.TrimSpace(c.Profile) != "" {
		args = append(args, "--profile", c.Profile)
	}
	return c.run(ctx, args...)
}

// ListFunctions lists Lambda functions in the configured profile/region.
func (c Client) ListFunctions(ctx context.Context) ([]Function, error) {
	args := []string{"lambda", "list-functions"}
	args = append(args, c.profileRegionArgs()...)
	args = append(args, "--output", "json")
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Functions []struct {
			FunctionName string `json:"FunctionName"`
			Runtime      string `json:"Runtime"`
			Handler      string `json:"Handler"`
			LastModified string `json:"LastModified"`
		} `json:"Functions"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return nil, fmt.Errorf("parsing list-functions output: %w", err)
	}
	fns := make([]Function, 0, len(parsed.Functions))
	for _, fn := range parsed.Functions {
		fns = append(fns, Function{
			Name:         fn.FunctionName,
			Runtime:      fn.Runtime,
			Handler:      fn.Handler,
			LastModified: fn.LastModified,
		})
	}
	return fns, nil
}

// CodeURL returns the signed Lambda deployment package URL.
func (c Client) CodeURL(ctx context.Context, functionName string) (string, error) {
	args := []string{"lambda", "get-function", "--function-name", functionName}
	args = append(args, c.profileRegionArgs()...)
	args = append(args, "--query", "Code.Location", "--output", "text")
	out, err := c.run(ctx, args...)
	if err != nil {
		return "", err
	}
	url := strings.TrimSpace(out)
	if url == "" || url == "None" {
		return "", fmt.Errorf("AWS CLI did not return a code download URL")
	}
	return url, nil
}

// DownloadURL downloads a signed Lambda package URL to zipPath.
func DownloadURL(ctx context.Context, url, zipPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(zipPath), 0755); err != nil {
		return err
	}
	out, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

// UpdateFunctionCode updates a Lambda's deployment package from zipPath.
func (c Client) UpdateFunctionCode(ctx context.Context, functionName, zipPath string) (string, error) {
	abs, err := filepath.Abs(zipPath)
	if err != nil {
		return "", err
	}
	args := []string{"lambda", "update-function-code", "--function-name", functionName}
	args = append(args, c.profileRegionArgs()...)
	args = append(args, "--zip-file", "fileb://"+abs)
	return c.run(ctx, args...)
}

func (c Client) run(ctx context.Context, args ...string) (string, error) {
	if err := c.Available(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.binary(), args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	text := strings.TrimSpace(out.String())
	if ctx.Err() != nil {
		return text, ctx.Err()
	}
	if err != nil {
		if text != "" {
			return text, fmt.Errorf("%s", text)
		}
		return text, err
	}
	return text, nil
}

// DefaultConfigPath returns the conventional AWS CLI config path.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".aws", "config")
}

// LoadConfigProfiles parses AWS CLI config profile blocks from path.
func LoadConfigProfiles(path string) map[string]Profile {
	profiles := map[string]Profile{}
	if path == "" {
		return profiles
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return profiles
	}
	var current string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			current = ""
			if name == "default" {
				current = "default"
			} else if strings.HasPrefix(name, "profile ") {
				current = strings.TrimSpace(strings.TrimPrefix(name, "profile "))
			}
			if current != "" {
				p := profiles[current]
				p.Name = current
				p.Configured = true
				profiles[current] = p
			}
			continue
		}
		if current == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if key == "sso_session" || key == "sso_start_url" || key == "sso_account_id" || key == "sso_role_name" || key == "sso_region" {
			p := profiles[current]
			p.Name = current
			p.Configured = true
			p.SSO = true
			profiles[current] = p
		}
	}
	return profiles
}
