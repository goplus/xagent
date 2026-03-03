# xagent

A Go SDK for Claude Code, Codex CLI, Gemini CLI, and OpenCode — uniform streaming interface, session resume, and pluggable executors.

## Installation

```sh
go get github.com/goplus/xagent
```

All core packages (`claude`, `codex`, `gemini`, `opencode`, `session`) are part of the root module and require no additional `go get`.

## Quick Start

### a. Single-turn call

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/goplus/xagent"
	"github.com/goplus/xagent/claude"
)

func main() {
	ctx := context.Background()
	agent := claude.New(claude.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")))
	defer agent.Close(ctx)

	res, err := xagent.Run(ctx, agent, xagent.SessionConfig{
		Permission: xagent.PermReadOnly,
	}, "List the Go source files in the current directory")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Text)
	fmt.Printf("tokens: %d in / %d out, cost: $%.4f\n",
		res.Usage.InputTokens, res.Usage.OutputTokens, res.Usage.CostUSD)
}
```

### b. Multi-turn streaming

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/goplus/xagent"
	"github.com/goplus/xagent/claude"
)

func main() {
	ctx := context.Background()
	agent := claude.New(claude.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")))
	defer agent.Close(ctx)

	sess, err := agent.Start(ctx, xagent.SessionConfig{Permission: xagent.PermReadOnly})
	if err != nil {
		log.Fatal(err)
	}
	defer sess.Close(ctx)

	for _, prompt := range []string{"What is Go?", "Show a short example"} {
		stream, err := sess.Send(ctx, prompt)
		if err != nil {
			log.Fatal(err)
		}
		for stream.Next(ctx) {
			if e, ok := stream.Event().(xagent.TextEvent); ok {
				fmt.Print(e.Delta)
			}
		}
		if err := stream.Err(); err != nil {
			log.Fatal(err)
		}
		stream.Close()
		fmt.Println()
	}
}
```

### c. Session resume

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/goplus/xagent"
	"github.com/goplus/xagent/claude"
)

func main() {
	ctx := context.Background()
	agent := claude.New(claude.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")))
	defer agent.Close(ctx)

	sess, err := agent.Start(ctx, xagent.SessionConfig{Permission: xagent.PermReadOnly})
	if err != nil {
		log.Fatal(err)
	}

	if _, err := xagent.CollectText(ctx, mustStream(sess.Send(ctx, "Hello"))); err != nil {
		log.Fatal(err)
	}

	sessionID := sess.ID()
	sess.Close(ctx)

	// Resume the session by passing the session ID to Start.
	resumed, err := agent.Start(ctx, xagent.SessionConfig{
		SessionID:  sessionID,
		Permission: xagent.PermReadOnly,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer resumed.Close(ctx)

	text, err := xagent.CollectText(ctx, mustStream(resumed.Send(ctx, "Continue where we left off")))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(text)
}

func mustStream(s xagent.Stream, err error) xagent.Stream {
	if err != nil {
		log.Fatal(err)
	}
	return s
}
```

## Adapters

| Adapter  | Import path                         | CLI binary required                                                      | Headless flag(s) used                   |
| -------- | ----------------------------------- | ------------------------------------------------------------------------ | --------------------------------------- |
| Claude   | `github.com/goplus/xagent/claude`   | `claude` ([Claude Code](https://docs.anthropic.com/en/docs/claude-code)) | `--output-format stream-json --verbose` |
| Codex    | `github.com/goplus/xagent/codex`    | `codex` ([OpenAI Codex CLI](https://github.com/openai/codex))            | `exec --json --full-auto`               |
| Gemini   | `github.com/goplus/xagent/gemini`   | `gemini` ([Gemini CLI](https://github.com/google-gemini/gemini-cli))     | `--output-format stream-json --yolo`    |
| OpenCode | `github.com/goplus/xagent/opencode` | `opencode` ([OpenCode](https://github.com/sst/opencode))                 | `run --format json`                     |

Each adapter constructor accepts functional options:

```go
// Claude — full option set
agent := claude.New(
	claude.WithAPIKey("sk-ant-..."),
	claude.WithBaseURL("https://api.example.com"),
	claude.WithBinaryPath("/usr/local/bin/claude"),
	claude.WithExecutor(exec),
	claude.WithEnv(map[string]string{"KEY": "val"}),
	claude.WithLogger(slog.Default()),
)

// Codex
agent := codex.New(
	codex.WithAPIKey("sk-..."),
	codex.WithBinaryPath("/usr/local/bin/codex"),
	codex.WithExecutor(exec),
)

// Gemini
agent := gemini.New(
	gemini.WithAPIKey("AIza..."),
	gemini.WithBinaryPath("/usr/local/bin/gemini"),
	gemini.WithExecutor(exec),
)

// OpenCode
agent := opencode.New(
	opencode.WithBinaryPath("/usr/local/bin/opencode"),
	opencode.WithExecutor(exec),
)
```

## Executors

`Executor` is an interface that decouples command dispatch from the agent logic:

```go
type Executor interface {
	// Exec starts a command and returns a Process handle for streaming I/O.
	// args[0] is the binary name; args[1:] are arguments.
	Exec(ctx context.Context, args []string, env map[string]string, workDir string) (*Process, error)
	IsHealthy(ctx context.Context) bool
	Close(ctx context.Context) error
}

// Process is the handle returned by Executor.Exec.
type Process struct {
	Stdout io.ReadCloser                    // subprocess stdout (NDJSON/SSE stream)
	Stdin  io.WriteCloser                   // subprocess stdin; nil when not writable
	Wait   func() (exitCode int, err error) // blocks until process exits
	Stderr *bytes.Buffer                    // captured stderr; read after Wait() returns
}
```

### LocalExecutor

`xagent.NewLocalExecutor()` — the built-in default executor. Runs CLI binaries as child processes using `os/exec`. Zero external dependencies.

### Custom Executors

Any type implementing the `Executor` interface can be used to run agent CLI binaries in a custom environment — for example, inside a Docker container, a cloud sandbox, or over SSH. Pass your implementation via the `WithExecutor()` option when constructing an adapter. See `demo/executor_docker` for a Docker-based reference implementation.

## SessionManager

`session.Manager` provides concurrency-safe caching of `Session` objects, with double-check locking so the factory is called at most once per key.

```go
import (
	"github.com/goplus/xagent"
	"github.com/goplus/xagent/session"
)

var mgr session.Manager

// NewSessionKey URL-path-escapes each part and joins with ':'
key := session.NewSessionKey("user", "42", "repo", "my-project")

sess, err := mgr.Get(key, func() (xagent.Session, error) {
	return agent.Start(ctx, xagent.SessionConfig{
		WorkDir:    "/workspace",
		Permission: xagent.PermAutoApprove,
	})
})

// Evict sessions older than 30 minutes or no longer healthy
mgr.Evict(ctx, 30*time.Minute)
```

## Event types

All events implement the `Event` interface:

```go
type Event interface {
	StreamEventKind() EventKind
	StreamTimestamp() time.Time
}
```

| Event type          | Kind value | Description                                                                                  | Adapters that emit it    |
| ------------------- | ---------- | -------------------------------------------------------------------------------------------- | ------------------------ |
| `InitEvent`         | 1          | Session started; carries `SessionID`, `Model`, `ToolNames`, `CLIVersion`                     | Claude, Gemini, OpenCode |
| `TextEvent`         | 2          | Incremental text delta from the model (`Delta string`)                                       | All                      |
| `ThinkingEvent`     | 3          | Extended thinking delta (`Delta string`)                                                     | Claude                   |
| `ToolStartEvent`    | 4          | Tool invocation started; carries `ToolName`, `CallID`, `Input []byte`, `Source`, `MCPServer` | Claude, Gemini, OpenCode |
| `ToolEndEvent`      | 5          | Tool invocation completed; carries `ToolName`, `CallID`, `Output string`, `IsError bool`     | Claude, Gemini, OpenCode |
| `TurnCompleteEvent` | 6          | Model turn finished; carries `InputTokens`, `OutputTokens`, `CostUSD`, `StopReason`          | All                      |
| `ErrorEvent`        | 7          | Backend error; `Fatal=true` means the stream is terminated                                   | All                      |
| `RawEvent`          | 99         | Backend-specific event with no standard mapping; raw JSON in `RawJSON []byte`                | All                      |

Consume events with a type switch:

```go
for stream.Next(ctx) {
	switch e := stream.Event().(type) {
	case xagent.TextEvent:
		fmt.Print(e.Delta)
	case xagent.TurnCompleteEvent:
		fmt.Printf("\ntokens: %d in / %d out\n", e.InputTokens, e.OutputTokens)
	case xagent.ErrorEvent:
		if e.Fatal {
			log.Fatalf("fatal: %s", e.Message)
		}
	}
}
```

Helper functions `xagent.CollectText` and `xagent.CollectResult` handle the common case of draining a stream to a string or a `TurnResult`.

## Design principles

- **Small interfaces; optional capabilities via `Capabilities`.** The core `Agent` and `Session` interfaces are minimal. Resume and fork are supported through `SessionConfig.SessionID` and `SessionConfig.ForkSession` — callers check `Capabilities()` to discover what each backend supports.

- **Escape hatches.** `RawEvent` carries backend-specific events that do not map to any standard type. `SessionConfig.ExtraBinaryFlags` passes arbitrary flags directly to the CLI binary.

- **Zero external dependencies in the core module.** The root `go.mod` has no third-party dependencies.

## Requirements

- Go 1.22 or later
- Each CLI binary must be installed separately:
  - Claude Code: https://docs.anthropic.com/en/docs/claude-code
  - OpenAI Codex CLI: https://github.com/openai/codex
  - Gemini CLI: https://github.com/google-gemini/gemini-cli
  - OpenCode: https://github.com/sst/opencode

## License

Apache-2.0 — see [LICENSE](LICENSE).
