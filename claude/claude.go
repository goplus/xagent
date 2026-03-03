/*
 * Copyright (c) 2026 The XGo Authors (xgo.dev). All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package claude

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/goplus/xagent"
	"github.com/goplus/xagent/internal/discover"
)

// Option is a functional option that configures a Claude Agent.
type Option func(*Agent)

// Agent implements xagent.Agent using the Claude Code CLI.
type Agent struct {
	exec       xagent.Executor
	apiKey     string
	baseURL    string
	binaryPath string
	env        map[string]string
	logger     *slog.Logger
}

// New creates a new Claude Agent with the provided options.
// By default it uses a LocalExecutor and logs to stderr.
func New(opts ...Option) *Agent {
	a := &Agent{
		exec:   xagent.NewLocalExecutor(),
		env:    map[string]string{},
		logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// WithExecutor overrides the default LocalExecutor with a custom implementation
// (e.g. a DockerExecutor for sandboxed execution).
func WithExecutor(exec xagent.Executor) Option {
	return func(a *Agent) {
		if exec != nil {
			a.exec = exec
		}
	}
}

// WithAPIKey sets the Anthropic API key used by the Claude CLI.
// If not set, the ANTHROPIC_API_KEY environment variable is used.
func WithAPIKey(apiKey string) Option {
	return func(a *Agent) { a.apiKey = apiKey }
}

// WithBaseURL overrides the Anthropic API base URL (useful for proxies or local stubs).
func WithBaseURL(baseURL string) Option {
	return func(a *Agent) { a.baseURL = baseURL }
}

// WithBinaryPath specifies an explicit filesystem path to the claude CLI binary,
// bypassing automatic PATH discovery.
func WithBinaryPath(path string) Option {
	return func(a *Agent) { a.binaryPath = path }
}

// WithEnv merges additional environment variables into every agent invocation.
func WithEnv(env map[string]string) Option {
	return func(a *Agent) {
		if env == nil {
			return
		}
		if a.env == nil {
			a.env = map[string]string{}
		}
		for k, v := range env {
			a.env[k] = v
		}
	}
}

// WithLogger sets a custom structured logger for the agent.
func WithLogger(l *slog.Logger) Option {
	return func(a *Agent) {
		if l != nil {
			a.logger = l
		}
	}
}

func (a *Agent) Name() string {
	return "claude"
}

// Capabilities returns the feature flags supported by claude CLI 2.1.x.
func (a *Agent) Capabilities() xagent.Capabilities {
	return xagent.Capabilities{
		Streaming:     true,
		SessionResume: true,
		ForkSession:   true,
	}
}

func (a *Agent) Validate(ctx context.Context) error {
	binary := a.command()
	if _, ok := a.exec.(*xagent.LocalExecutor); ok {
		if _, findErr := discover.FindWithPath("claude", a.binaryPath); findErr != nil {
			return findErr
		}
	}
	env := a.buildBaseEnv(nil)
	_, stderr, exitCode, err := xagent.RunCollect(ctx, a.exec, []string{binary, "--version"}, env, "")
	if err != nil {
		return &xagent.AgentError{Code: xagent.ErrBinaryNotFound, Agent: a.Name(), Message: "failed to run claude --version", Cause: err}
	}
	if exitCode != 0 {
		return &xagent.AgentError{Code: xagent.ErrBinaryNotFound, Agent: a.Name(), Message: fmt.Sprintf("claude not available: %s", string(stderr))}
	}
	if env["ANTHROPIC_API_KEY"] == "" && os.Getenv("ANTHROPIC_API_KEY") == "" {
		return &xagent.AgentError{Code: xagent.ErrAuth, Agent: a.Name(), Message: "ANTHROPIC_API_KEY is required"}
	}
	return nil
}

func (a *Agent) Start(ctx context.Context, cfg xagent.SessionConfig) (xagent.Session, error) {
	return &session{agent: a, cfg: cfg, sessionID: cfg.SessionID, forceFork: cfg.ForkSession}, nil
}

func (a *Agent) Close(ctx context.Context) error {
	if a.exec == nil {
		return nil
	}
	return a.exec.Close(ctx)
}

var _ xagent.Agent = (*Agent)(nil)

func (a *Agent) command() string {
	if _, ok := a.exec.(*xagent.LocalExecutor); !ok {
		return "claude"
	}
	b, err := discover.FindWithPath("claude", a.binaryPath)
	if err == nil {
		return b
	}
	return "claude"
}

func (a *Agent) buildBaseEnv(extra map[string]string) map[string]string {
	env := map[string]string{}
	for k, v := range a.env {
		env[k] = v
	}
	if a.apiKey != "" {
		env["ANTHROPIC_API_KEY"] = a.apiKey
	}
	if a.baseURL != "" {
		env["ANTHROPIC_BASE_URL"] = a.baseURL
	}
	for k, v := range extra {
		env[k] = v
	}
	return filterEnv(env)

}

func filterEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		if k == "CLAUDECODE" || k == "CLAUDECODE_SESSION" {
			continue
		}
		out[k] = v
	}
	return out
}
