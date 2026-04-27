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
- Separate **invoke** (`i`, no build) and **build + invoke** (`I`) keybinds
- Create, edit and delete named test cases and data model templates
- **Quick-bench** (`r`) — run a function N times and see min/max/avg/p95 stats instantly
- **Local HTTP API server** — `POST http://localhost:8080/<function-name>` while lambit is running
- **Live filter / search** across functions, tests, and models with `/`
- **Clipboard copy** — context-sensitive (`y`) or always-curl (`Y`)
- **Jump to source** — open the handler or test definition in `$EDITOR` with `g`
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
| `PgUp` / `PgDn` | Jump 5 items; scroll output pane |
| `Tab` / `Shift+Tab` | Jump to next / previous section |
| `Enter` | Lock selected function (keeps its tests visible while browsing) |
| `Esc` | Back / cancel |
| `/` | Open live filter |
| `i` | Invoke selected function / test (no build) |
| `I` | Build then invoke selected function / test |
| `r` | Quick-bench: run N times and open benchmark with stats |
| `e` | Edit selected item (handler string, payload, or model JSON) |
| `n` | New test case |
| `d` | Delete selected test / model |
| `a` | Toggle local HTTP API server |
| `b` | Toggle benchmark pane |
| `y` | Copy to clipboard (context-sensitive) |
| `Y` | Copy curl command to clipboard |
| `g` | Open source file in `$EDITOR` at the definition line |
| `G` | Open `.lambit.toml` in `$EDITOR` at the selected entry |
| `s` | Scaffold `.lambit.toml` |
| `o` | Open global config file in `$EDITOR` |
| `?` | Keybind help |
| `q` / `Ctrl+C` | Quit |

All keybinds are remappable — see [Config](#config).

### Live filter

Press `/` to open a live filter. Results are grouped: matching Functions first, then Tests (with function name headers), then Models. Use `↑`/`↓` to navigate, `Enter` to jump to an item, and `Esc` to clear the filter.

### Copy to clipboard

`y` copies different things depending on context:

| Context | What is copied |
|---------|----------------|
| Functions section | Function name |
| Tests section (xUnit) | `dotnet test --filter FullyQualifiedName~...` command |
| Tests section (payload) | `curl` command for the local API |
| Models section | Model JSON |

`Y` always copies a `curl` command. For xUnit test cases it uses the two-segment API path (`POST /<fn>/<test-case>`).

### Jump to source

`g` opens the source file in `$EDITOR` at the exact line of the selected handler or test definition. lambit knows how to jump to a specific line in vim, nvim, nano, emacs, VS Code, and Notepad++.

`G` opens `.lambit.toml` at the line matching the selected item.

### Locking a function

Pressing `Enter` on a function "locks" it — the Tests pane continues to show that function's tests even as you browse the Functions list. A locked function is marked with `●`.

### Output pane

After an invocation the output pane opens on the right. Use `↑`/`↓` or `PgUp`/`PgDn` to scroll it, and `i` or `I` to re-invoke (without build or with build respectively) without leaving. Any other key closes it.

### Invoke vs build + invoke

`i` invokes immediately with no build step — useful for rapid iteration when you haven't changed code. `I` runs a full build first (e.g. `dotnet build`) then invokes. Use `I` after making code changes; use `i` for repeated test runs.

### Quick-bench (`r`)

Press `r` to run the selected function or test `bench_runs` times consecutively (default: 10, configurable per-project in `.lambit.toml`). A progress bar shows each iteration as it runs. When done, the benchmark pane opens automatically with a bar chart and summary stats:

```
  FunctionHandler  ████████████████░░░░░░░░   45ms  ✓
  FunctionHandler  ██████████░░░░░░░░░░░░░░   28ms  ✓
  ...
                   min 22ms    avg 38ms    p95 51ms    max 67ms    10/10 ok
```

The stats row (min / avg / p95 / max / success rate) makes the benchmark pane useful as an actual measurement tool rather than just a visual.

### xUnit test discovery

For .NET projects, lambit scans test assemblies (`.csproj` files referencing xUnit) for `[Fact]` and `[Theory]` methods and lists them under each lambda function, marked with `⊕`. Selecting one and pressing `i` runs it via:

```
dotnet test <TestProject.csproj> --filter FullyQualifiedName~ClassName.MethodName
```

`[Theory]` methods with `[InlineData]` appear as separate items — one per data variant.

Discovered tests are never written to `.lambit.toml` — they re-scan from source on every startup.

### Local HTTP API

Press `a` to start a local HTTP server (default port `8080`). The status bar shows the server address and a running call count. While running, two routes are available:

**Invoke with a custom payload:**
```bash
curl -X POST http://localhost:8080/FunctionHandler \
     -H "Content-Type: application/json" \
     -d '{"input": "hello world"}'
```

**Invoke a named test case (including xUnit tests):**
```bash
curl -X POST http://localhost:8080/FunctionHandler/HelloWorld
```

The port is configurable in `.lambit.toml`. API results update the results strip and benchmark without interrupting the TUI.

---

## Project file (`.lambit.toml`)

lambit stores per-project configuration in `.lambit.toml` at the project root. Press `s` to scaffold one, or create it manually:

```toml
[project]
name       = "MyFunction"
runtime    = ""   # leave empty for auto-detect, or "dotnet" / "nodejs"
api_port   = 8080
bench_runs = 10   # iterations for quick-bench (r)

[[functions]]
name        = "FunctionHandler"
handler     = "MyFunction::MyFunction.Function::FunctionHandler"
description = "Main lambda entry point"
root        = ""  # optional: subdirectory where the lambda lives (for monorepos)

[[functions.tests]]
name    = "Hello world"
payload = '{"input": "hello world"}'

[[models]]
name = "SamplePayload"
json = '{"key": "value", "count": 1}'
```

lambit searches the current directory and all parent directories for `.lambit.toml`, so you can run it from a subdirectory of your project. The `s` scaffold command will error if a project file already exists rather than overwriting it.

### Monorepo / multi-lambda projects

When a single `.lambit.toml` covers multiple lambdas in subdirectories, set `root` on each function to tell lambit where that lambda's shim, build output, and source files live:

```toml
[[functions]]
name    = "GreetFunction"
handler = "GreetingFunction::GreetingFunction.Function::Greet"
root    = "dotnet-greeting"   # relative to the .lambit.toml directory

[[functions]]
name    = "TransformFunction"
handler = "index.handler"
root    = "node-transform"
```

When `root` is set, all runtime operations (shim path, build command, invoke command, go-to-source) use `root` as the effective project root for that function. When omitted, the directory containing `.lambit.toml` is used.

---

## Config

lambit reads a global config file from the platform-appropriate location:

| OS | Path |
|----|------|
| Linux | `~/.config/delbysoft/lambit.toml` |
| macOS | `~/Library/Application Support/delbysoft/lambit.toml` |
| Windows | `%APPDATA%\delbysoft\lambit.toml` |

Press `o` inside lambit to open the config file in `$EDITOR`. A default config is created on first launch, and any new keybinds added in a future version are automatically added to your existing config file.

Example config:

```toml
[keybinds]
up           = "up"
down         = "down"
page_up      = "pgup"
page_down    = "pgdown"
tab          = "tab"
shift_tab    = "shift+tab"
confirm      = "enter"
back         = "esc"
invoke       = "i"
invoke_build = "I"
quick_bench  = "r"
edit         = "e"
new_test     = "n"
delete       = "d"
toggle_api   = "a"
benchmark    = "b"
filter       = "/"
copy         = "y"
copy_curl    = "Y"
goto_source  = "g"
goto_config  = "G"
scaffold     = "s"
options      = "o"
help         = "?"
quit         = "q"

[apps]
editor = ""   # leave empty to use $EDITOR / $VISUAL, or set e.g. "nvim"
```

When `editor` is empty, lambit tries `$EDITOR`, then `$VISUAL`, then scans for `nano`, `vi`, `vim`, `nvim`, `code`, and `notepad.exe` in that order.

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

## Support

<a href='https://ko-fi.com/W7W21WP5L7' target='_blank'><img height='36' style='border:0px;height:36px;' src='https://storage.ko-fi.com/cdn/kofi4.png?v=6' border='0' alt='Buy Me a Coffee at ko-fi.com' /></a>

---

## License

MIT — see [LICENSE](LICENSE).

Copyright (c) 2026 [delbysoft](https://github.com/wingitman)
