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

package gemini

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/goplus/xagent"
	"github.com/goplus/xagent/internal/discover"
)

// Option is a functional option that configures a Gemini Agent.
type Option func(*Agent)

// Agent implements xagent.Agent using the Gemini CLI.
type Agent struct {
	exec       xagent.Executor
	apiKey     string
	binaryPath string
}

// New creates a new Gemini Agent with the provided options.
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

// WithAPIKey sets the Gemini API key used by the Gemini CLI.
// If not set, the GEMINI_API_KEY environment variable is used.
func WithAPIKey(key string) Option { return func(a *Agent) { a.apiKey = key } }

// WithBinaryPath specifies an explicit path to the gemini CLI binary.
func WithBinaryPath(path string) Option { return func(a *Agent) { a.binaryPath = path } }

func (a *Agent) Name() string { return "gemini" }

func (a *Agent) Capabilities() xagent.Capabilities {
	return xagent.Capabilities{Streaming: true, SessionResume: true}
}

func (a *Agent) Validate(ctx context.Context) error {
	_, err := discover.FindWithPath(a.cmdName(), a.binaryPath)
	if err != nil {
		return err
	}
	if a.apiKey == "" && os.Getenv("GEMINI_API_KEY") == "" {
		return &xagent.AgentError{Code: xagent.ErrAuth, Agent: a.Name(), Message: "GEMINI_API_KEY is required"}
	}
	_, stderr, exitCode, err := xagent.RunCollect(ctx, a.exec, []string{a.command(), "--version"}, a.baseEnv(nil), "")
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return &xagent.AgentError{Code: xagent.ErrBinaryNotFound, Agent: a.Name(), Message: fmt.Sprintf("gemini unavailable: %s", string(stderr))}
	}
	return nil
}

func (a *Agent) Start(ctx context.Context, cfg xagent.SessionConfig) (xagent.Session, error) {
	return &session{agent: a, cfg: cfg, sessionID: cfg.SessionID}, nil
}

func (a *Agent) Close(ctx context.Context) error { return a.exec.Close(ctx) }

func (a *Agent) cmdName() string {
	if runtime.GOOS == "windows" {
		return "gemini.cmd"
	}
	return "gemini"
}

func (a *Agent) command() string {
	b, err := discover.FindWithPath(a.cmdName(), a.binaryPath)
	if err == nil {
		return b
	}
	return a.cmdName()
}

func (a *Agent) baseEnv(extra map[string]string) map[string]string {
	env := map[string]string{"NO_COLOR": "1", "GEMINI_CLI_NO_RELAUNCH": "1"}
	if a.apiKey != "" {
		env["GEMINI_API_KEY"] = a.apiKey
	}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

var _ xagent.Agent = (*Agent)(nil)
