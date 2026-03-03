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

package xagent

import "context"

// Agent is the top-level interface for an AI coding agent backend.
// Implementations wrap CLI tools such as Claude Code, Codex CLI, Gemini CLI, or OpenCode.
type Agent interface {
	// Name returns the agent backend identifier (e.g. "claude", "codex").
	Name() string
	// Capabilities returns the feature set supported by this agent.
	Capabilities() Capabilities
	// Validate checks that the agent binary is reachable and credentials are configured.
	Validate(ctx context.Context) error
	// Start creates a new Session with the provided configuration.
	Start(ctx context.Context, cfg SessionConfig) (Session, error)
	// Close releases all resources held by the agent (e.g. executor connections).
	Close(ctx context.Context) error
}

// Capabilities describes the optional features supported by an Agent implementation.
type Capabilities struct {
	// Streaming indicates that the agent emits events incrementally via a Stream.
	Streaming bool
	// SessionResume indicates that the agent supports resuming sessions via SessionConfig.SessionID.
	SessionResume bool
	// ForkSession indicates that the agent supports forking sessions via SessionConfig.ForkSession.
	ForkSession bool
}
