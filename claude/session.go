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
	"sync"
	"time"

	"github.com/goplus/xagent"
	"github.com/goplus/xagent/internal/ndjson"
)

type session struct {
	agent *Agent
	cfg   xagent.SessionConfig

	mu        sync.Mutex
	sessionID string
	forceFork bool
	closed    bool
}

func (s *session) ID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID
}

func (s *session) Send(ctx context.Context, prompt string) (xagent.Stream, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, &xagent.AgentError{Code: xagent.ErrInvalidConfig, Agent: "claude", Message: "session already closed"}
	}
	resumeID := s.sessionID
	cfg := s.cfg
	s.mu.Unlock()

	args := buildArgs(cfg, prompt, resumeID, s.forceFork)
	fullArgs := append([]string{s.agent.command()}, args...)
	env := s.agent.buildBaseEnv(cfg.Env)

	proc, err := s.agent.exec.Exec(ctx, fullArgs, env, cfg.WorkDir)
	if err != nil {
		return nil, &xagent.AgentError{Code: xagent.ErrExecution, Agent: "claude", Message: "failed to execute claude", Cause: err}
	}
	if proc.Stdin != nil {
		proc.Stdin.Close()
		proc.Stdin = nil
	}

	// statefulMapLine suppresses the duplicate TextEvent that claude writes into the
	// final 'result' line when text was already delivered via streaming 'assistant'
	// events (normal mode). In plan mode no assistant TextEvents are emitted, so the
	// 'result' line text is kept as the sole source.
	var assistantTextSeen bool
	statefulMapLine := func(line []byte) []xagent.Event {
		events := mapLine(line)

		// Single pass: detect result line and text events simultaneously.
		var isResultLine, hasText bool
		for _, e := range events {
			switch e.(type) {
			case xagent.TurnCompleteEvent:
				isResultLine = true
			case xagent.TextEvent:
				hasText = true
			}
		}

		if !isResultLine {
			if hasText {
				assistantTextSeen = true
			}
			return events
		}

		// result line: drop its TextEvent only when assistant streaming already delivered text.
		if !assistantTextSeen {
			return events
		}
		out := make([]xagent.Event, 0, len(events))
		for _, e := range events {
			if _, ok := e.(xagent.TextEvent); ok {
				continue // suppress duplicate
			}
			out = append(out, e)
		}
		return out
	}

	scanner := ndjson.NewScanner(proc.Stdout)
	return xagent.NewProcessStream(proc, scanner, statefulMapLine,
		xagent.WithOnInit(func(init xagent.InitEvent) {
			if init.SessionID != "" {
				s.mu.Lock()
				s.sessionID = init.SessionID
				s.mu.Unlock()
			}
		}),
		xagent.WithExitHandler(func(exitCode int, stderr []byte) []xagent.Event {
			return []xagent.Event{xagent.ErrorEvent{
				Message:   string(stderr),
				Code:      xagent.ErrExecution,
				Fatal:     true,
				Timestamp: time.Now(),
			}}
		}),
	), nil
}

func (s *session) IsHealthy(ctx context.Context) bool {
	if s.agent == nil || s.agent.exec == nil {
		return false
	}
	return s.agent.exec.IsHealthy(ctx)
}

func (s *session) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

var _ xagent.Session = (*session)(nil)
