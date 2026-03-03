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

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	"github.com/goplus/xagent"
	"github.com/goplus/xagent/claude"
	"github.com/goplus/xagent/codex"
	"github.com/goplus/xagent/gemini"
	"github.com/goplus/xagent/opencode"
)

type demoBackend struct {
	name  string
	agent xagent.Agent
	exec  xagent.Executor
}

func main() {
	backend := flag.String("backend", "claude", "backend: claude|codex|gemini|opencode")
	all := flag.Bool("all", false, "运行所有可配置 backend")
	workDir := flag.String("workdir", ".", "会话工作目录")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	backends, err := buildBackends(*backend, *all)
	if err != nil {
		logger.Error("build backends failed", "err", err)
		os.Exit(1)
	}
	if len(backends) == 0 {
		logger.Error("no backends selected")
		os.Exit(1)
	}

	for _, backend := range backends {
		fmt.Printf("\n================ %s ================\n", backend.name)

		if err := backend.agent.Validate(ctx); err != nil {
			fmt.Printf("[SKIP] validate failed: %v\n", err)
			_ = backend.agent.Close(ctx)
			continue
		}

		fmt.Printf("backend=%s caps=%+v\n", backend.agent.Name(), backend.agent.Capabilities())
		runAllScenarios(ctx, backend, *workDir, logger)

		if err := backend.agent.Close(ctx); err != nil {
			logger.Warn("agent close failed", "backend", backend.name, "err", err)
		}
	}

	fmt.Println("\n✅ demo completed")
}

func buildBackends(backend string, all bool) ([]demoBackend, error) {
	selected := map[string]bool{}
	if all {
		selected["claude"] = true
		selected["codex"] = true
		selected["gemini"] = true
		selected["opencode"] = true
	} else {
		b := strings.ToLower(strings.TrimSpace(backend))
		switch b {
		case "claude", "codex", "gemini", "opencode":
			selected[b] = true
		default:
			return nil, errors.New("invalid --backend, expected one of: claude|codex|gemini|opencode")
		}
	}

	out := make([]demoBackend, 0, len(selected))
	if selected["claude"] {
		exec := xagent.NewLocalExecutor()
		a := claude.New(
			claude.WithExecutor(exec),
			claude.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")),
			claude.WithBaseURL(os.Getenv("ANTHROPIC_BASE_URL")),
		)
		out = append(out, demoBackend{name: "claude", agent: a, exec: exec})
	}
	if selected["codex"] {
		exec := xagent.NewLocalExecutor()
		a := codex.New(
			codex.WithExecutor(exec),
			codex.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
		)
		out = append(out, demoBackend{name: "codex", agent: a, exec: exec})
	}
	if selected["gemini"] {
		exec := xagent.NewLocalExecutor()
		a := gemini.New(
			gemini.WithExecutor(exec),
			gemini.WithAPIKey(os.Getenv("GEMINI_API_KEY")),
		)
		out = append(out, demoBackend{name: "gemini", agent: a, exec: exec})
	}
	if selected["opencode"] {
		exec := xagent.NewLocalExecutor()
		a := opencode.New(opencode.WithExecutor(exec))
		out = append(out, demoBackend{name: "opencode", agent: a, exec: exec})
	}
	return out, nil
}

func runAllScenarios(ctx context.Context, backend demoBackend, workDir string, logger *slog.Logger) {
	runScenario := func(name string, fn func() error) {
		fmt.Printf("\n--- %s ---\n", name)
		if err := fn(); err != nil {
			logger.Error("scenario failed", "backend", backend.name, "scenario", name, "err", err)
		}
	}

	runScenario("A Validate + Capabilities", func() error {
		fmt.Printf("backend=%s caps=%+v\n", backend.agent.Name(), backend.agent.Capabilities())
		return nil
	})

	runScenario("B 单轮 Run", func() error {
		if backend.name == "gemini" {
			fmt.Println("[SKIP] gemini 当前对 PermReadOnly 无对应 CLI 映射")
			return nil
		}
		result, err := xagent.Run(ctx, backend.agent, xagent.SessionConfig{
			WorkDir:    workDir,
			Permission: xagent.PermReadOnly,
			MaxTurns:   2,
		}, "用一句话描述 xagent 的作用")
		if err != nil {
			return err
		}
		fmt.Println(result.Text)
		fmt.Printf("usage: %d in / %d out, $%.4f, %v\n", result.Usage.InputTokens, result.Usage.OutputTokens, result.Usage.CostUSD, result.Duration)
		return nil
	})

	runScenario("C 手动流式消费", func() error {
		sess, err := backend.agent.Start(ctx, xagent.SessionConfig{
			WorkDir:    workDir,
			Permission: xagent.PermAutoApprove,
		})
		if err != nil {
			return err
		}
		defer sess.Close(ctx)

		stream, err := sess.Send(ctx, "列出当前目录下最关键的3个文件")
		if err != nil {
			return err
		}
		defer stream.Close()

		for stream.Next(ctx) {
			switch e := stream.Event().(type) {
			case xagent.InitEvent:
				fmt.Printf("init: session=%s cli=%s\n", e.SessionID, e.CLIVersion)
			case xagent.TextEvent:
				fmt.Print(e.Delta)
			case xagent.ThinkingEvent:
				fmt.Printf("\n[thinking] %s", truncate(e.Delta, 80))
			case xagent.ToolStartEvent:
				fmt.Printf("\n[tool:start] %s callID=%s input=%s\n", e.ToolName, e.CallID, truncate(string(e.Input), 120))
			case xagent.ToolEndEvent:
				fmt.Printf("\n[tool:end] %s error=%v output=%s\n", e.ToolName, e.IsError, truncate(e.Output, 120))
			case xagent.TurnCompleteEvent:
				fmt.Printf("\n[turn] %d in/%d out $%.4f reason=%s\n", e.InputTokens, e.OutputTokens, e.CostUSD, e.StopReason)
			case xagent.ErrorEvent:
				if e.Fatal {
					return fmt.Errorf("fatal error [%s]: %s", e.Code, e.Message)
				}
				fmt.Printf("\n[warn] [%s] %s\n", e.Code, e.Message)
			case xagent.RawEvent:
				fmt.Printf("\n[raw] %s\n", truncate(string(e.RawJSON), 120))
			}
		}
		if err := stream.Err(); err != nil {
			return err
		}
		fmt.Println()
		return nil
	})

	runScenario("D 多轮会话", func() error {
		sess, err := backend.agent.Start(ctx, xagent.SessionConfig{
			WorkDir:    workDir,
			Permission: xagent.PermAutoApprove,
		})
		if err != nil {
			return err
		}
		defer sess.Close(ctx)

		t1, err := collectTextFromPrompt(ctx, sess, "Turn 1: 用一句话说明 xagent 能做什么")
		if err != nil {
			return err
		}
		t2, err := collectTextFromPrompt(ctx, sess, "Turn 2: 基于上一轮，补充一句关于 Session 的说明")
		if err != nil {
			return err
		}
		fmt.Printf("turn1=%s\nturn2=%s\n", truncate(t1, 160), truncate(t2, 160))
		return nil
	})

	runScenario("E Session Resume / Fork", func() error {
		caps := backend.agent.Capabilities()
		if !caps.SessionResume {
			fmt.Printf("[SKIP] backend %s does not support session resume\n", backend.name)
			return nil
		}

		sess, err := backend.agent.Start(ctx, xagent.SessionConfig{
			WorkDir:    workDir,
			Permission: xagent.PermAutoApprove,
		})
		if err != nil {
			return err
		}
		_, err = collectTextFromPrompt(ctx, sess, "先回复：resume 准备完成")
		if err != nil {
			sess.Close(ctx)
			return err
		}
		sessionID := sess.ID()
		_ = sess.Close(ctx)

		resumed, err := backend.agent.Start(ctx, xagent.SessionConfig{
			SessionID:  sessionID,
			WorkDir:    workDir,
			Permission: xagent.PermAutoApprove,
		})
		if err != nil {
			return err
		}
		defer resumed.Close(ctx)

		text, err := collectTextFromPrompt(ctx, resumed, "继续上次会话，确认你还记得刚才的话")
		if err != nil {
			return err
		}
		fmt.Printf("resume ok: %s\n", truncate(text, 160))

		if !caps.ForkSession {
			fmt.Printf("[SKIP] backend %s does not support fork session\n", backend.name)
			return nil
		}

		forked, err := backend.agent.Start(ctx, xagent.SessionConfig{
			SessionID:   sessionID,
			ForkSession: true,
			WorkDir:     workDir,
			Permission:  xagent.PermAutoApprove,
		})
		if err != nil {
			return err
		}
		defer forked.Close(ctx)

		forkText, err := collectTextFromPrompt(ctx, forked, "这是 fork 分支，回复 fork ok")
		if err != nil {
			return err
		}
		fmt.Printf("fork ok: %s\n", truncate(forkText, 160))
		return nil
	})

	runScenario("F SessionConfig 覆盖", func() error {
		cfg := xagent.SessionConfig{
			WorkDir:      workDir,
			Permission:   xagent.PermAutoApprove,
			MaxTurns:     1,
			SystemPrompt: "你是简洁的代码助手，用中文回答。",
			Model:        suggestedModel(backend.name),
		}
		result, err := xagent.Run(ctx, backend.agent, cfg, "返回一句简洁确认语")
		if err != nil {
			return err
		}
		fmt.Printf("config override result: %s\n", truncate(result.Text, 120))
		return nil
	})

	runScenario("G MCP Server (claude only)", func() error {
		if backend.name != "claude" {
			fmt.Printf("[SKIP] backend %s does not support MCP config demo\n", backend.name)
			return nil
		}
		if _, err := exec.LookPath("uvx"); err != nil {
			fmt.Println("[SKIP] mcp demo requires uvx")
			return nil
		}

		sess, err := backend.agent.Start(ctx, xagent.SessionConfig{
			WorkDir:    workDir,
			Permission: xagent.PermAutoApprove,
			MCPConfig: &xagent.MCPConfig{Servers: []xagent.MCPServer{{
				Name:    "demo",
				Command: "uvx",
				Args:    []string{"mcp-server-fetch"},
			}}},
		})
		if err != nil {
			return err
		}
		defer sess.Close(ctx)

		stream, err := sess.Send(ctx, "调用可用工具并说明工具来源")
		if err != nil {
			return err
		}
		defer stream.Close()

		seenMCP := false
		for stream.Next(ctx) {
			switch e := stream.Event().(type) {
			case xagent.ToolStartEvent:
				if e.Source == "mcp" {
					seenMCP = true
					fmt.Printf("mcp tool: %s server=%s\n", e.ToolName, e.MCPServer)
				}
			}
		}
		if err := stream.Err(); err != nil {
			return err
		}
		if !seenMCP {
			fmt.Println("[WARN] no MCP tool call observed in this run")
		}
		return nil
	})

	runScenario("H IsHealthy", func() error {
		fmt.Printf("executor healthy: %v\n", backend.exec.IsHealthy(ctx))

		sess, err := backend.agent.Start(ctx, xagent.SessionConfig{
			WorkDir:    workDir,
			Permission: xagent.PermReadOnly,
		})
		if err != nil {
			return err
		}
		defer sess.Close(ctx)

		fmt.Printf("session healthy: %v\n", sess.IsHealthy(ctx))
		return nil
	})
}

func collectTextFromPrompt(ctx context.Context, sess xagent.Session, prompt string) (string, error) {
	stream, err := sess.Send(ctx, prompt)
	if err != nil {
		return "", err
	}
	return xagent.CollectText(ctx, stream)
}

func suggestedModel(backend string) string {
	switch backend {
	case "claude":
		return "claude-4.6-sonnet"
	case "codex":
		return "gpt-5-codex"
	case "gemini":
		return "gemini-2.5-pro"
	default:
		return ""
	}
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 3 {
		return string(runes[:n])
	}
	return string(runes[:n-3]) + "..."
}
