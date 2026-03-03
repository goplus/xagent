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

// Session represents an active interaction with an agent backend.
// A Session is created by Agent.Start and may span multiple turns.
type Session interface {
	// ID returns the backend-assigned session identifier, or empty string if not yet assigned.
	ID() string
	// Send submits prompt to the agent and returns a Stream of incremental response events.
	Send(ctx context.Context, prompt string) (Stream, error)
	// IsHealthy reports whether the underlying execution environment is still usable.
	IsHealthy(ctx context.Context) bool
	// Close terminates the session and releases associated resources.
	Close(ctx context.Context) error
}

// SessionConfig holds configuration options passed to Agent.Start.
type SessionConfig struct {
	// SessionID resumes an existing session by its backend-assigned identifier.
	// When non-empty, the agent continues the identified conversation instead of
	// starting a new one. Use Session.ID() to obtain the identifier.
	SessionID string
	// ForkSession, when true and SessionID is non-empty, creates a new session
	// branched from the identified conversation instead of resuming it directly.
	ForkSession bool
	// WorkDir sets the working directory for the agent process; empty inherits the executor default.
	WorkDir string
	// Model overrides the default LLM model identifier (e.g. "claude-opus-4-5").
	Model string
	// SystemPrompt adds a custom system prompt to the session.
	SystemPrompt string
	// Permission controls the tool-use permission level granted to the agent.
	Permission PermissionPolicy
	// MaxTurns limits the number of agent turns before stopping; 0 means unlimited.
	MaxTurns int
	// Env is a set of additional environment variables merged into the agent process.
	Env map[string]string
	// MCPConfig configures Model Context Protocol servers that the agent may call.
	MCPConfig *MCPConfig
	// ExtraBinaryFlags appends raw flags to the agent CLI invocation.
	ExtraBinaryFlags []string
}

// MCPConfig holds the list of Model Context Protocol servers to make available to the agent.
type MCPConfig struct {
	Servers []MCPServer
}

// MCPServer describes a single MCP server that the agent can invoke as a tool.
type MCPServer struct {
	// Name is the logical identifier used within the agent's tool namespace.
	Name string
	// Command is the executable to run for this MCP server.
	Command string
	// Args are the command-line arguments passed to the MCP server process.
	Args []string
	// Env is the set of environment variables passed to the MCP server process.
	Env map[string]string
}
