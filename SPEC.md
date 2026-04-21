# lambit Runtime Interface Specification

This document describes the `Runtime` interface that lambit uses to detect,
build, and invoke AWS Lambda functions locally.  If you are using a lambda
runtime not covered by the built-in `.NET` and `Node.js` implementations,
you can write your own by implementing this interface.

---

## Overview

lambit invokes lambdas **locally as subprocesses** — no AWS credentials or
network connection required.  Each runtime implementation is responsible for:

1. **Detecting** whether a given project directory contains its lambda type.
2. **Scanning** the project for handler function entry points.
3. **Producing build arguments** so lambit can compile the project before
   invoking it.
4. **Producing invoke arguments** — the full command line used to run the
   lambda handler with a JSON payload.
5. **Parsing the result** from the subprocess stdout/stderr.

---

## The Go Interface

```go
package runtime

import (
    "time"
    "github.com/wingitman/lambit/internal/project"
)

type InvokeResult struct {
    Stdout   string
    Stderr   string
    Duration time.Duration
    Success  bool
    Error    string // non-empty when Success == false
}

type Runtime interface {
    // Name returns a short identifier, e.g. "dotnet", "nodejs", "python".
    Name() string

    // Detect returns true when this runtime recognises the project.
    // It should be fast and read-only (no writes, no network).
    Detect(projectRoot string) bool

    // Scan discovers handler functions and returns them.
    // Return an empty slice if you cannot detect any — lambit will prompt
    // the user to fill in .lambit.toml manually.
    Scan(projectRoot string) ([]project.Function, error)

    // BuildArgs returns the OS/exec command + arguments for building the
    // project.  Return nil to skip the build step.
    BuildArgs(projectRoot string) []string

    // ShimDir returns the path (relative to projectRoot) where lambit will
    // extract the invocation shim.  Convention: ".lambit/<runtime-name>-runner".
    ShimDir(projectRoot string) string

    // InvokeArgs returns the OS/exec command + arguments to invoke fn with
    // the given JSON payload.  The shim at ShimDir() is available.
    InvokeArgs(projectRoot string, fn project.Function, payload string) []string

    // ParseResult converts raw subprocess output into an InvokeResult.
    ParseResult(stdout, stderr string, dur time.Duration) InvokeResult
}
```

---

## Registering Your Runtime

Place your implementation in a `.go` file inside `internal/runtime/` and
register it in an `init()` function:

```go
package runtime

import "time"

func init() {
    Register(&myRuntime{})
}

type myRuntime struct{}

func (r *myRuntime) Name() string { return "mypython" }
// ... implement remaining methods
```

lambit calls `Detect()` on each registered runtime in registration order.
The **first** match wins.  To give your runtime higher priority, register it
before the built-ins (use a separate package imported from `main.go`).

---

## Invocation Shim Pattern

The built-in runtimes use a **thin shim** injected into `.lambit/` inside
the user's project.  The shim:

- Loads the user's compiled lambda assembly / module.
- Instantiates the handler class / imports the module.
- Calls the handler function with the JSON payload deserialized into the
  correct input type.
- Writes the serialized JSON result to **stdout**.
- Writes errors / logs to **stderr**.
- Exits with code `0` on success, non-zero on failure.

lambit captures stdout as the invocation result and stderr as diagnostic
output.  The shim is gitignored (`.lambit/` should be in `.gitignore`).

---

## Built-in Runtimes

### .NET (`dotnet`)

**Detection:** presence of a `*.csproj` file containing `Amazon.Lambda` in
any `PackageReference`.

**Handler string format:** `Assembly::Namespace.ClassName::MethodName`  
Example: `MyFunction::MyFunction.Function::FunctionHandler`

**Build:** `dotnet build <projectRoot> --nologo -v q`

**Invoke:** runs a `dotnet run --project .lambit/dotnet-runner -- <handler> <payload>`

The shim project at `.lambit/dotnet-runner` is a minimal console application
that loads your lambda assembly via reflection, deserializes the payload, calls
the handler method, serializes the result to JSON, and writes it to stdout.

---

### Node.js (`nodejs`)

**Detection:** presence of `package.json` containing `aws-lambda` or
`@aws-sdk` in dependencies, or a common handler filename (`index.js`,
`handler.js`, etc.).

**Handler string format:** `<file>.<exportedFunction>`  
Example: `index.handler`

**Build:** runs `npm run build` if a `build` script is present in
`package.json`, otherwise skipped.

**Invoke:** runs `node .lambit/node-runner/runner.mjs <handler> <payload>`

The shim at `.lambit/node-runner/runner.mjs` dynamic-imports your module,
calls the exported function with the deserialized event object, and writes
the JSON-serialized result to stdout.

---

## Project File (`.lambit.toml`)

```toml
[project]
name     = "MyFunction"
runtime  = ""         # leave empty for auto-detect, or set e.g. "dotnet"
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

Run `lambit` with the `[s]` keybind in the TUI to scaffold this file
automatically.

---

## Local HTTP API Server

While lambit is running, pressing `[a]` starts a local HTTP server
(default port 8080).  You can then call your lambda via:

```
POST http://localhost:8080/<function-name>
Content-Type: application/json

{"input": "hello world"}
```

The server routes the request body as the lambda payload and returns the
invocation result as the response body.  This lets you test your lambda
with `curl`, Postman, or any HTTP client without deploying to AWS.

---

*lambit is a delbysoft project.*
