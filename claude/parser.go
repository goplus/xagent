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
	"bytes"
	"encoding/json"
	"strconv"
	"time"

	"github.com/goplus/xagent"
	"github.com/goplus/xagent/internal/ndjson"
)

func parseNDJSON(data []byte) ([]xagent.Event, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}

	var events []xagent.Event
	scanner := ndjson.NewScanner(bytes.NewReader(data))

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var obj map[string]any
		if err := json.Unmarshal(line, &obj); err != nil {
			return nil, err
		}

		events = append(events, mapEvent(obj, line)...)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return events, nil
}

func mapEvent(obj map[string]any, raw []byte) []xagent.Event {
	now := time.Now()
	typ := toString(obj["type"])

	switch typ {
	case "system":
		if toString(obj["subtype"]) == "init" {
			var toolNames []string
			if tools, ok := obj["tools"].([]any); ok {
				for _, t := range tools {
					if tm, ok := t.(map[string]any); ok {
						if name := toString(tm["name"]); name != "" {
							toolNames = append(toolNames, name)
						}
					}
				}
			}
			return []xagent.Event{xagent.InitEvent{
				SessionID:  toString(obj["session_id"]),
				Model:      toString(obj["model"]),
				AgentName:  "claude",
				ToolNames:  toolNames,
				CLIVersion: toString(obj["claude_code_version"]),
				Timestamp:  now,
			}}
		}
	case "assistant":
		content, _ := obj["message"].(map[string]any)
		parts, _ := content["content"].([]any)
		if len(parts) > 0 {
			out := make([]xagent.Event, 0, len(parts))
			for _, p := range parts {
				m, _ := p.(map[string]any)
				subType := toString(m["type"])
				switch subType {
				case "text":
					out = append(out, xagent.TextEvent{Delta: toString(m["text"]), Timestamp: now})
				case "thinking", "thinking_delta":
					out = append(out, xagent.ThinkingEvent{Delta: toString(m["thinking"]), Timestamp: now})
				case "tool_use":
					inputJSON, _ := json.Marshal(m["input"])
					out = append(out, xagent.ToolStartEvent{
						ToolName:  toString(m["name"]),
						CallID:    toString(m["id"]),
						Input:     inputJSON,
						Source:    "builtin",
						Timestamp: now,
					})
				}
			}
			if len(out) > 0 {
				return out
			}
		}
		if delta := toString(obj["delta"]); delta != "" {
			return []xagent.Event{xagent.TextEvent{Delta: delta, Timestamp: now}}
		}
	case "result":
		usage, _ := obj["usage"].(map[string]any)
		out := make([]xagent.Event, 0, 2)
		if text := toString(obj["result"]); text != "" {
			out = append(out, xagent.TextEvent{Delta: text, Timestamp: now})
		}
		out = append(out, xagent.TurnCompleteEvent{
			InputTokens:  toInt(usage["input_tokens"]),
			OutputTokens: toInt(usage["output_tokens"]),
			CostUSD:      toFloat(obj["total_cost_usd"]),
			StopReason:   toString(obj["stop_reason"]),
			Timestamp:    now,
		})
		return out
	case "tool_result":
		return []xagent.Event{xagent.ToolEndEvent{
			ToolName:  toString(obj["name"]),
			CallID:    toString(obj["tool_use_id"]),
			Output:    toString(obj["content"]),
			IsError:   toBool(obj["is_error"]),
			Timestamp: now,
		}}
	case "error":
		return []xagent.Event{xagent.ErrorEvent{
			Message:   toString(obj["message"]),
			Code:      xagent.ErrExecution,
			Fatal:     true,
			Timestamp: now,
		}}
	}

	return []xagent.Event{xagent.RawEvent{AgentName: "claude", RawJSON: append([]byte(nil), raw...), Timestamp: now}}
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return ""
	}
}

func toInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		i, _ := t.Int64()
		return int(i)
	default:
		return 0
	}
}

func toFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	default:
		return 0
	}
}

func toBool(v any) bool {
	b, _ := v.(bool)
	return b
}

// mapLine is the line-level parser for use with NewProcessStream.
func mapLine(line []byte) []xagent.Event {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(line, &obj); err != nil {
		return nil
	}
	return mapEvent(obj, line)
}
