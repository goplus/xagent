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
	"encoding/json"
	"strconv"

	"github.com/goplus/xagent"
)

func buildArgs(cfg xagent.SessionConfig, prompt string, resumeID string, fork bool) []string {
	args := []string{"--output-format", "stream-json", "--verbose"}

	if resumeID != "" {
		args = append(args, "--resume", resumeID)
	}
	if fork && resumeID != "" {
		args = append(args, "--fork-session")
	}

	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	if cfg.SystemPrompt != "" {
		args = append(args, "--system-prompt", cfg.SystemPrompt)
	}

	switch cfg.Permission {
	case xagent.PermReadOnly:
		args = append(args, "--permission-mode", "plan")
	case xagent.PermAutoApprove:
		args = append(args, "--dangerously-skip-permissions")
	}

	if cfg.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(cfg.MaxTurns))
	}

	if cfg.MCPConfig != nil && len(cfg.MCPConfig.Servers) > 0 {
		mcpJSON, err := json.Marshal(buildMCPConfigJSON(cfg.MCPConfig))
		if err == nil {
			args = append(args, "--mcp-config", string(mcpJSON))
		}
	}

	args = append(args, cfg.ExtraBinaryFlags...)
	args = append(args, "-p", "--", prompt)
	return args
}

func buildMCPConfigJSON(cfg *xagent.MCPConfig) map[string]any {
	servers := map[string]any{}
	for _, s := range cfg.Servers {
		entry := map[string]any{"command": s.Command}
		if len(s.Args) > 0 {
			entry["args"] = s.Args
		}
		if len(s.Env) > 0 {
			entry["env"] = s.Env
		}
		servers[s.Name] = entry
	}
	return map[string]any{"mcpServers": servers}
}
