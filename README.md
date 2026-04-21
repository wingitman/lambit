# lambit

A TUI for invoking, testing and benchmarking AWS Lambda functions locally — no cloud deployment required.

Run `lambit` in the root of your lambda project. It scans for handler functions and xUnit test cases, lets you invoke them with custom payloads, benchmarks execution time, and can expose your lambdas as a local HTTP API while it is running.

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lip Gloss](https://github.com/charmbracelet/lipgloss).

---

## Features

- Auto-detects **.NET** and **Node.js** AWS Lambda projects
- Reads handler strings from `template.yaml`, `aws-lambda-tools-defaults.json`, or infers them from `.csproj`
- Discovers **xUnit `[Fact]` and `[Theory]`** test methods and surfaces them as invocable items (marked `⊕`)
- Invoke functions with custom JSON payloads or auto-discovered test cases
- Create, edit and delete named test cases and data model templates
- Real-time **benchmark bar chart** (`█░`) across the last 20 invocations
- **Local HTTP API server** — `POST http://localhost:8080/<function-name>` while lambit is running
- Every keybind remappable via a `.toml` config file
- Scaffold a `.lambit.toml` project file with auto-detected handlers in one keypress

---

## Installation

### macOS / Linux

```bash
git clone https://github.com/wingitman/lambit
cd lambit
make install
```

`make install` builds the binary, copies it to `~/.local/bin/lambit`, and ensures `~/.local/bin` is on your `PATH` in whichever shells are configured (`zsh`, `bash`, `fish`, `pwsh`).

### Windows

```powershell
git clone https://github.com/wingitman/lambit
cd lambit
.\install.ps1
```

`install.ps1` builds the binary, installs it to `%LOCALAPPDATA%\Programs\lambit\lambit.exe`, adds that directory to your user `PATH` in the registry, and adds a `PATH` line to your `$PROFILE` as a belt-and-suspenders measure.

> **Execution policy:** if you see a policy error, run once as your user:
> ```powershell
> Set-ExecutionPolicy -Scope CurrentUser RemoteSigned
> ```

Both installers require [Go 1.22+](https://go.dev/dl/) to be installed.

---

## Usage

```bash
cd /path/to/your/lambda-project
lambit
```

On first run in a new project, lambit will show a **no project file** screen. Press `s` to scaffold a `.lambit.toml` — lambit will scan for handlers and open the file in `$EDITOR` for review before loading.

### Default keybinds

| Key | Action |
|-----|--------|
| `↑` / `↓` | Navigate |
| `i` | Invoke selected function / test |
| `e` | Edit selected item (handler string, payload, or model JSON) |
| `n` | New test case |
| `d` | Delete selected test / model |
| `a` | Toggle local HTTP API server |
| `b` | Toggle benchmark pane |
| `s` | Scaffold `.lambit.toml` |
| `o` | Open config file in `$EDITOR` |
| `?` | Keybind help |
| `q` | Quit |

All keybinds are remappable — see [Config](#config).

### xUnit test discovery

For .NET projects, lambit scans test assemblies (`.csproj` files referencing xUnit) for `[Fact]` and `[Theory]` methods and lists them under each lambda function, marked with `⊕`. Selecting one and pressing `i` runs it via:

```
dotnet test <TestProject.csproj> --filter FullyQualifiedName~ClassName.MethodName
```

`[Theory]` methods with `[InlineData]` appear as separate items — one per data variant.

Discovered tests are never written to `.lambit.toml` — they re-scan from source on every startup.

### Local HTTP API

Press `a` to start a local HTTP server (default port `8080`). While running, invoke any function via HTTP:

```bash
curl -X POST http://localhost:8080/FunctionHandler \
     -H "Content-Type: application/json" \
     -d '{"input": "hello world"}'
```

The port is configurable in `.lambit.toml`.

---

## Project file (`.lambit.toml`)

lambit stores per-project configuration in `.lambit.toml` at the project root. Press `s` to scaffold one, or create it manually:

```toml
[project]
name     = "MyFunction"
runtime  = ""       # leave empty for auto-detect, or "dotnet" / "nodejs"
api_port = 8080

[[functions]]
name        = "FunctionHandler"
handler     = "MyFunction::MyFunction.Function::FunctionHandler"
description = "Main lambda entry point"

[[functions.tests]]
name    = "Hello world"
payload = '{"input": "hello world"}'

[[models]]
name = "SamplePayload"
json = '{"key": "value", "count": 1}'
```

---

## Config

lambit reads a global config file from the platform-appropriate location:

| OS | Path |
|----|------|
| Linux | `~/.config/delbysoft/lambit.toml` |
| macOS | `~/Library/Application Support/delbysoft/lambit.toml` |
| Windows | `%APPDATA%\delbysoft\lambit.toml` |

Press `o` inside lambit to open the config file in `$EDITOR`. A default config is created on first launch.

Example config:

```toml
[keybinds]
up         = "up"
down       = "down"
invoke     = "i"
edit       = "e"
new_test   = "n"
delete     = "d"
toggle_api = "a"
benchmark  = "b"
scaffold   = "s"
options    = "o"
help       = "?"
quit       = "q"

[apps]
editor = ""   # leave empty to use $EDITOR env var
```

---

## Custom runtime interface

lambit's runtime system is pluggable. See [`SPEC.md`](SPEC.md) for the full interface specification — you can add support for any lambda runtime (Python, Ruby, Java, etc.) by implementing the `Runtime` interface in Go.

---

## Building from source

```bash
# Current platform
make build

# All supported platforms
make cross-build
```

Cross-compiled binaries are written to `bin/`:

| File | Platform |
|------|----------|
| `lambit-macos-arm64` | macOS Apple Silicon |
| `lambit-macos-amd64` | macOS Intel |
| `lambit-linux-amd64` | Linux x86-64 |
| `lambit-linux-arm64` | Linux ARM64 |
| `lambit-windows-amd64.exe` | Windows x86-64 |

---

## Uninstall

### macOS / Linux

```bash
make uninstall
```

Removes `~/.local/bin/lambit`. Any `PATH` lines added to your shell rc files remain — remove them manually if desired. To also remove the config:

```bash
rm -rf ~/.config/delbysoft   # Linux
rm -rf ~/Library/Application\ Support/delbysoft   # macOS
```

### Windows

```powershell
.\uninstall.ps1
```

Removes `%LOCALAPPDATA%\Programs\lambit\` and its registry `PATH` entry. The `# lambit PATH` block in `$PROFILE` is left in place (it becomes a no-op). Remove it manually if desired.

To remove the config:

```powershell
Remove-Item -Recurse "$env:APPDATA\delbysoft"
```

---

*A [delbysoft](https://github.com/wingitman) project.*
