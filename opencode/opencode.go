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

package opencode

import (
	"context"
	"fmt"

	"github.com/goplus/xagent"
	"github.com/goplus/xagent/internal/discover"
)

// Option is a functional option that configures an OpenCode Agent.
type Option func(*Agent)

// Agent implements xagent.Agent using the OpenCode CLI.
type Agent struct {
	exec       xagent.Executor
	binaryPath string
	env        map[string]string
	server     *server
}

// New creates a new OpenCode Agent with the provided options.
func New(opts ...Option) *Agent {
	a := &Agent{exec: xagent.NewLocalExecutor(), env: map[string]string{}}
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

// WithBinaryPath specifies an explicit path to the opencode CLI binary.
func WithBinaryPath(path string) Option { return func(a *Agent) { a.binaryPath = path } }

func (a *Agent) Name() string { return "opencode" }

func (a *Agent) Capabilities() xagent.Capabilities {
	return xagent.Capabilities{Streaming: true, SessionResume: true, ForkSession: true}
}

func (a *Agent) Validate(ctx context.Context) error {
	if _, ok := a.exec.(*xagent.LocalExecutor); ok {
		_, err := discover.FindWithPath("opencode", a.binaryPath)
		if err != nil {
			return err
		}
	}

	_, stderr, exitCode, err := xagent.RunCollect(ctx, a.exec, []string{a.command(), "--version"}, a.baseEnv(nil), "")
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return &xagent.AgentError{Code: xagent.ErrBinaryNotFound, Agent: a.Name(), Message: fmt.Sprintf("opencode unavailable: %s", string(stderr))}
	}
	return nil
}

func (a *Agent) Start(ctx context.Context, cfg xagent.SessionConfig) (xagent.Session, error) {
	if a.server == nil {
		a.server = &server{agent: a}
	}
	if err := a.server.ensureStarted(ctx, cfg); err != nil {
		return nil, err
	}
	return &session{agent: a, cfg: cfg, sessionID: cfg.SessionID, forceFork: cfg.ForkSession}, nil
}

func (a *Agent) Close(ctx context.Context) error {
	if a.server != nil {
		_ = a.server.close(ctx)
	}
	return a.exec.Close(ctx)
}

func (a *Agent) command() string {
	b, err := discover.FindWithPath("opencode", a.binaryPath)
	if err == nil {
		return b
	}
	return "opencode"
}

func (a *Agent) baseEnv(extra map[string]string) map[string]string {
	env := map[string]string{"OPENCODE_DISABLE_AUTOUPDATE": "1"}
	for k, v := range a.env {
		env[k] = v
	}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

var _ xagent.Agent = (*Agent)(nil)
