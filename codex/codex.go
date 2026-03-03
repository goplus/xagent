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

package codex

import (
	"context"
	"os"

	"github.com/goplus/xagent"
	"github.com/goplus/xagent/internal/discover"
)

// Option is a functional option that configures a Codex Agent.
type Option func(*Agent)

// Agent implements xagent.Agent using the Codex CLI.
type Agent struct {
	exec       xagent.Executor
	apiKey     string
	binaryPath string
}

// New creates a new Codex Agent with the provided options.
func New(opts ...Option) *Agent {
	a := &Agent{exec: xagent.NewLocalExecutor()}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// WithExecutor overrides the default LocalExecutor.
func WithExecutor(exec xagent.Executor) Option {
	return func(a *Agent) {
		if exec != nil {
			a.exec = exec
		}
	}
}

// WithAPIKey sets the OpenAI API key used by the Codex CLI.
// If not set, the OPENAI_API_KEY environment variable is used.
func WithAPIKey(key string) Option {
	return func(a *Agent) { a.apiKey = key }
}

// WithBinaryPath specifies an explicit path to the codex CLI binary.
func WithBinaryPath(path string) Option {
	return func(a *Agent) { a.binaryPath = path }
}

func (a *Agent) Name() string { return "codex" }

func (a *Agent) Capabilities() xagent.Capabilities {
	return xagent.Capabilities{Streaming: true, SessionResume: true}
}

func (a *Agent) Validate(ctx context.Context) error {
	binary, err := discover.FindWithPath("codex", a.binaryPath)
	if err != nil {
		return err
	}
	_, stderr, exitCode, err := xagent.RunCollect(ctx, a.exec, []string{binary, "--version"}, a.baseEnv(nil), "")
	if err != nil {
		return &xagent.AgentError{Code: xagent.ErrBinaryNotFound, Agent: a.Name(), Message: "failed to run codex --version", Cause: err}
	}
	if exitCode != 0 {
		return &xagent.AgentError{Code: xagent.ErrBinaryNotFound, Agent: a.Name(), Message: string(stderr)}
	}
	if a.apiKey == "" && os.Getenv("OPENAI_API_KEY") == "" {
		return &xagent.AgentError{Code: xagent.ErrAuth, Agent: a.Name(), Message: "OPENAI_API_KEY is required"}
	}
	return nil
}

func (a *Agent) Start(ctx context.Context, cfg xagent.SessionConfig) (xagent.Session, error) {
	if cfg.SessionID != "" {
		// Resume always uses exec mode (single-turn CLI subprocess).
		cfg.MaxTurns = 1
		return &execSession{agent: a, cfg: cfg, threadID: cfg.SessionID}, nil
	}
	if cfg.MaxTurns == 1 {
		return &execSession{agent: a, cfg: cfg}, nil
	}
	return &appServerSession{agent: a, cfg: cfg}, nil
}

func (a *Agent) Close(ctx context.Context) error { return a.exec.Close(ctx) }

func (a *Agent) command() string {
	binary, err := discover.FindWithPath("codex", a.binaryPath)
	if err == nil {
		return binary
	}
	return "codex"
}

func (a *Agent) baseEnv(extra map[string]string) map[string]string {
	env := map[string]string{"CODEX_INTERNAL_ORIGINATOR_OVERRIDE": "xagent"}
	if a.apiKey != "" {
		env["OPENAI_API_KEY"] = a.apiKey
	}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

var _ xagent.Agent = (*Agent)(nil)
