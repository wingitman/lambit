package update

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/wingitman/lambit/internal/config"
)

const defaultRepoURL = "https://github.com/wingitman/lambit.git"

// Commit is one git commit shown in update prompts/history.
type Commit struct {
	Hash    string
	Short   string
	Subject string
	Body    string
	Date    string
}

// Info describes the local source checkout and available upstream commits.
type Info struct {
	RepoPath       string
	Branch         string
	Upstream       string
	CurrentCommit  string
	LatestCommit   string
	Available      []Commit
	History        []Commit
	CheckError     string
	UpdatesEnabled bool
}

// InstallRequest describes the install the detached helper should run.
type InstallRequest struct {
	RepoPath       string
	TargetCommit   string
	Latest         bool
	Terminal       string
	RecorderBinary string
}

// Check ensures a source checkout exists, fetches its origin, and returns the
// commits newer than currentCommit on the checkout's current branch/upstream.
func Check(cfg *config.Config, currentCommit string, historyLimit int) Info {
	info := Info{UpdatesEnabled: cfg == nil || !cfg.Updates.DisableChecks}
	if cfg != nil && cfg.Updates.DisableChecks {
		return info
	}

	repoPath, err := ensureRepoPath(cfg)
	if err != nil {
		info.CheckError = err.Error()
		return info
	}
	info.RepoPath = repoPath

	if out, err := git(repoPath, "fetch", "--prune", "--all"); err != nil {
		info.CheckError = strings.TrimSpace(out)
		if info.CheckError == "" {
			info.CheckError = err.Error()
		}
		return info
	}

	branch, _ := gitTrim(repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	info.Branch = branch
	upstream, err := gitTrim(repoPath, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil || upstream == "" {
		if branch != "" && branch != "HEAD" {
			upstream = "origin/" + branch
		} else {
			upstream = "origin/HEAD"
		}
	}
	info.Upstream = upstream

	if currentCommit == "" || currentCommit == "dev" {
		if cfg != nil && cfg.Updates.CurrentCommit != "" {
			currentCommit = cfg.Updates.CurrentCommit
		}
	}
	if currentCommit == "" || currentCommit == "dev" {
		currentCommit, _ = gitTrim(repoPath, "rev-parse", "HEAD")
	}
	info.CurrentCommit = currentCommit
	info.LatestCommit, _ = gitTrim(repoPath, "rev-parse", upstream)

	if currentCommit != "" && info.LatestCommit != "" && currentCommit != info.LatestCommit {
		info.Available = gitLog(repoPath, fmt.Sprintf("%s..%s", currentCommit, upstream), historyLimit)
	}
	info.History = gitLog(repoPath, "HEAD", historyLimit)
	return info
}

// LaunchDetached writes an update script and opens it in a separate terminal.
func LaunchDetached(req InstallRequest) error {
	if strings.TrimSpace(req.RepoPath) == "" {
		return errors.New("missing update repo path")
	}
	if runtime.GOOS == "windows" {
		return launchWindows(req)
	}
	return launchUnix(req)
}

func ensureRepoPath(cfg *config.Config) (string, error) {
	if cfg != nil && cfg.Updates.RepoPath != "" && isGitRepo(cfg.Updates.RepoPath) {
		return cfg.Updates.RepoPath, nil
	}
	if cwd, err := os.Getwd(); err == nil && isLambitRepo(cwd) {
		_ = config.RecordUpdateMetadata("", cwd)
		return cwd, nil
	}
	repoPath := filepath.Join(config.ConfigDir(), "lambit-src")
	if isGitRepo(repoPath) {
		_ = config.RecordUpdateMetadata("", repoPath)
		return repoPath, nil
	}
	if err := os.MkdirAll(filepath.Dir(repoPath), 0755); err != nil {
		return "", err
	}
	cmd := exec.Command("git", "clone", defaultRepoURL, repoPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("clone update repo: %v: %s", err, strings.TrimSpace(string(out)))
	}
	_ = config.RecordUpdateMetadata("", repoPath)
	return repoPath, nil
}

func isLambitRepo(path string) bool {
	if !isGitRepo(path) {
		return false
	}
	remote, err := gitTrim(path, "remote", "get-url", "origin")
	if err != nil {
		return false
	}
	return strings.Contains(remote, "lambit")
}

func isGitRepo(path string) bool {
	if path == "" {
		return false
	}
	cmd := exec.Command("git", "-C", path, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func gitTrim(repoPath string, args ...string) (string, error) {
	out, err := git(repoPath, args...)
	return strings.TrimSpace(out), err
}

func git(repoPath string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.Command("git", cmdArgs...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func gitLog(repoPath string, rev string, limit int) []Commit {
	if limit < 1 {
		limit = 12
	}
	format := "%H%x1f%h%x1f%s%x1f%b%x1f%ad%x1e"
	args := []string{"log", "--date=short", "--format=" + format, "-n", fmt.Sprint(limit)}
	if rev != "" {
		args = append(args, rev)
	}
	out, err := git(repoPath, args...)
	if err != nil {
		return nil
	}
	records := strings.Split(out, "\x1e")
	commits := make([]Commit, 0, len(records))
	for _, rec := range records {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		parts := strings.SplitN(rec, "\x1f", 5)
		if len(parts) < 5 {
			continue
		}
		commits = append(commits, Commit{
			Hash:    parts[0],
			Short:   parts[1],
			Subject: parts[2],
			Body:    strings.TrimSpace(parts[3]),
			Date:    parts[4],
		})
	}
	return commits
}

func launchUnix(req InstallRequest) error {
	script, err := writeUnixScript(req)
	if err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("osascript", "-e", fmt.Sprintf(`tell application "Terminal" to do script %q`, script))
		return cmd.Start()
	}
	terminal, args, err := terminalCommand(req.Terminal, script)
	if err != nil {
		return err
	}
	cmd := exec.Command(terminal, args...)
	return cmd.Start()
}

func writeUnixScript(req InstallRequest) (string, error) {
	dir := filepath.Join(os.TempDir(), "lambit-updates")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("update-%d.sh", time.Now().UnixNano()))
	var b bytes.Buffer
	b.WriteString("#!/bin/sh\nset -eu\n")
	b.WriteString("repo=" + shQuote(req.RepoPath) + "\n")
	b.WriteString("target=" + shQuote(req.TargetCommit) + "\n")
	b.WriteString("recorder=" + shQuote(req.RecorderBinary) + "\n")
	b.WriteString("cd \"$repo\"\n")
	b.WriteString("prev_ref=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || printf HEAD)\n")
	b.WriteString("restore_ref=$prev_ref\n")
	b.WriteString("git fetch --prune --all\n")
	if req.Latest {
		b.WriteString("if [ \"$prev_ref\" != HEAD ]; then\n")
		b.WriteString("  if git rev-parse --abbrev-ref --symbolic-full-name '@{u}' >/dev/null 2>&1; then git pull --ff-only; else git merge --ff-only \"origin/$prev_ref\"; fi\n")
		b.WriteString("elif [ -n \"$target\" ]; then git checkout --detach \"$target\"\n")
		b.WriteString("fi\n")
	} else {
		b.WriteString("git checkout --detach \"$target\"\n")
	}
	b.WriteString("make install\n")
	b.WriteString("installed=$(git rev-parse HEAD)\n")
	b.WriteString("if [ -n \"$recorder\" ] && [ -x \"$recorder\" ]; then \"$recorder\" --record-update --update-commit \"$installed\" --update-repo \"$repo\"; fi\n")
	b.WriteString("if [ \"$restore_ref\" != HEAD ]; then git checkout \"$restore_ref\" >/dev/null 2>&1 || true; fi\n")
	b.WriteString("printf '\\nlambit update complete: %s\\n' \"$installed\"\n")
	b.WriteString("printf 'Press Enter to close...'; read _\n")
	if err := os.WriteFile(path, b.Bytes(), 0755); err != nil {
		return "", err
	}
	return path, nil
}

func launchWindows(req InstallRequest) error {
	script, err := writeWindowsScript(req)
	if err != nil {
		return err
	}
	if req.Terminal != "" {
		return exec.Command(req.Terminal, script).Start()
	}
	if _, err := exec.LookPath("wt.exe"); err == nil {
		return exec.Command("wt.exe", "powershell.exe", "-NoExit", "-ExecutionPolicy", "Bypass", "-File", script).Start()
	}
	return exec.Command("powershell.exe", "-NoExit", "-ExecutionPolicy", "Bypass", "-File", script).Start()
}

func writeWindowsScript(req InstallRequest) (string, error) {
	dir := filepath.Join(os.TempDir(), "lambit-updates")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("update-%d.ps1", time.Now().UnixNano()))
	latest := "$false"
	if req.Latest {
		latest = "$true"
	}
	content := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$repo = %s
$target = %s
$recorder = %s
$latest = %s
Set-Location $repo
$prevRef = (git rev-parse --abbrev-ref HEAD).Trim()
git fetch --prune --all
if ($latest) {
    if ($prevRef -ne 'HEAD') {
        git rev-parse --abbrev-ref --symbolic-full-name '@{u}' *> $null
        if ($LASTEXITCODE -eq 0) { git pull --ff-only } else { git merge --ff-only "origin/$prevRef" }
    } elseif ($target) {
        git checkout --detach $target
    }
} else {
    git checkout --detach $target
}
& .\install.ps1 -Update
$installed = (git rev-parse HEAD).Trim()
if ($recorder -and (Test-Path $recorder)) { & $recorder --record-update --update-commit $installed --update-repo $repo }
if ($prevRef -ne 'HEAD') { git checkout $prevRef | Out-Null }
Write-Host ""
Write-Host "lambit update complete: $installed" -ForegroundColor Green
Read-Host 'Press Enter to close'
`, psQuote(req.RepoPath), psQuote(req.TargetCommit), psQuote(req.RecorderBinary), latest)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}
	return path, nil
}

func terminalCommand(preferred string, script string) (string, []string, error) {
	if preferred != "" {
		return preferred, []string{"-e", script}, nil
	}
	candidates := []struct {
		name string
		args []string
	}{
		{"x-terminal-emulator", []string{"-e", script}},
		{"gnome-terminal", []string{"--", script}},
		{"konsole", []string{"-e", script}},
		{"xfce4-terminal", []string{"-e", script}},
		{"alacritty", []string{"-e", script}},
		{"kitty", []string{script}},
		{"wezterm", []string{"start", "--", script}},
		{"foot", []string{script}},
	}
	for _, c := range candidates {
		if _, err := exec.LookPath(c.name); err == nil {
			return c.name, c.args, nil
		}
	}
	return "", nil, errors.New("no supported terminal found for detached update")
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
