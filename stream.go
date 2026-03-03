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

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"time"
)

// Stream is the iterator interface for consuming agent events in a streaming fashion.
// Callers advance the stream with Next and inspect the current event via Event.
type Stream interface {
	// Next advances the stream to the next event. It returns false when the stream is
	// exhausted or the context is cancelled. Check Err() after Next returns false.
	Next(ctx context.Context) bool
	// Event returns the most recent event produced by the last successful call to Next.
	Event() Event
	// Err returns any error that caused the stream to stop. It must be called after
	// Next returns false to distinguish clean completion from failure.
	Err() error
	// Close releases resources associated with the stream and terminates the underlying
	// process if still running.
	Close() error
}

// TurnResult holds the aggregated output of a single agent turn collected by CollectResult.
type TurnResult struct {
	// Text is the concatenated text response produced by the agent.
	Text string
	// ToolCalls contains all tool results received during the turn.
	ToolCalls []ToolEndEvent
	// Usage aggregates token counts and cost for the turn.
	Usage Usage
	// Duration is the wall-clock time from the first to the last event.
	Duration time.Duration
	// StopReason is the reason reported by the agent for ending this turn.
	StopReason string
}

// Usage holds token usage and cost metrics for a single agent turn.
type Usage struct {
	InputTokens  int     // number of input (prompt) tokens consumed
	OutputTokens int     // number of output (completion) tokens produced
	CostUSD      float64 // estimated monetary cost in USD
}

// CollectText drains s to completion and returns the concatenated text content.
// It closes the stream before returning. A fatal ErrorEvent is returned as an *AgentError.
func CollectText(ctx context.Context, s Stream) (string, error) {
	defer s.Close()
	var sb strings.Builder
	for s.Next(ctx) {
		switch e := s.Event().(type) {
		case TextEvent:
			sb.WriteString(e.Delta)
		case ErrorEvent:
			if e.Fatal {
				return "", &AgentError{Code: e.Code, Message: e.Message}
			}
		}
	}
	return sb.String(), s.Err()
}

// CollectResult drains s to completion and returns an aggregated TurnResult containing
// the full text, all tool calls, usage statistics, and stop reason.
// It closes the stream before returning.
func CollectResult(ctx context.Context, s Stream) (*TurnResult, error) {
	defer s.Close()
	var out TurnResult
	start := time.Now()
	var sb strings.Builder
	for s.Next(ctx) {
		switch e := s.Event().(type) {
		case TextEvent:
			sb.WriteString(e.Delta)
		case ToolEndEvent:
			out.ToolCalls = append(out.ToolCalls, e)
		case TurnCompleteEvent:
			out.Usage = Usage{InputTokens: e.InputTokens, OutputTokens: e.OutputTokens, CostUSD: e.CostUSD}
			out.StopReason = e.StopReason
			if e.Duration > 0 {
				out.Duration = e.Duration
			}
		case ErrorEvent:
			if e.Fatal {
				return nil, &AgentError{Code: e.Code, Message: e.Message}
			}
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	out.Text = sb.String()
	if out.Duration <= 0 {
		out.Duration = time.Since(start)
	}
	return &out, nil
}

// Run is a convenience function that starts a single-turn session on agent a,
// sends prompt, collects the full TurnResult, and closes the session.
func Run(ctx context.Context, a Agent, cfg SessionConfig, prompt string) (*TurnResult, error) {
	sess, err := a.Start(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer sess.Close(ctx)
	stream, err := sess.Send(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return CollectResult(ctx, stream)
}

// --- sliceStream (batch, for tests/mock) ---

type sliceStream struct {
	mu     sync.Mutex
	events []Event
	index  int
	cur    Event
	err    error
	closed bool
}

// NewSliceStream returns a Stream backed by a pre-built slice of events.
// If err is non-nil it is returned by Err after all events have been consumed.
// This is primarily useful in tests.
func NewSliceStream(events []Event, err error) Stream {
	return &sliceStream{events: events, err: err}
}

func (s *sliceStream) Next(ctx context.Context) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			s.err = ctx.Err()
			return false
		default:
		}
	}
	if s.index >= len(s.events) {
		return false
	}
	s.cur = s.events[s.index]
	s.index++
	return true
}

func (s *sliceStream) Event() Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

func (s *sliceStream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *sliceStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// --- LineScanner + processStream (streaming) ---

// LineScanner abstracts line-by-line reading from a stream.
// Both ndjson.Scanner and bufio.Scanner satisfy this interface.
type LineScanner interface {
	Scan() bool
	Bytes() []byte
	Err() error
}

// ProcessStreamOption configures NewProcessStream behavior.
type ProcessStreamOption func(*processStream)

// WithOnInit registers a callback invoked for each InitEvent.
func WithOnInit(fn func(InitEvent)) ProcessStreamOption {
	return func(s *processStream) { s.onInit = fn }
}

// WithExitHandler registers a handler that converts non-zero exit codes into events.
func WithExitHandler(fn func(exitCode int, stderr []byte) []Event) ProcessStreamOption {
	return func(s *processStream) { s.exitHandler = fn }
}

// NewProcessStream creates a Stream that reads lines from a Process's stdout,
// parses each line into events via parseLine, and handles process lifecycle.
func NewProcessStream(proc *Process, scanner LineScanner, parseLine func([]byte) []Event, opts ...ProcessStreamOption) Stream {
	s := &processStream{
		proc:      proc,
		scanner:   scanner,
		parseLine: parseLine,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type processStream struct {
	proc      *Process
	scanner   LineScanner
	parseLine func([]byte) []Event

	onInit      func(InitEvent)
	exitHandler func(exitCode int, stderr []byte) []Event

	mu     sync.Mutex
	buf    []Event
	cur    Event
	err    error
	done   bool
	closed bool
}

func (s *processStream) Next(ctx context.Context) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.done {
		return false
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			s.err = ctx.Err()
			return false
		default:
		}
	}

	// Return buffered events from a previous multi-event line.
	if len(s.buf) > 0 {
		s.cur = s.buf[0]
		s.buf = s.buf[1:]
		return true
	}

	// Scan lines until we get events.
	for s.scanner.Scan() {
		line := s.scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		events := s.parseLine(line)
		if len(events) == 0 {
			continue
		}
		if s.onInit != nil {
			for _, ev := range events {
				if init, ok := ev.(InitEvent); ok {
					s.onInit(init)
				}
			}
		}
		s.cur = events[0]
		if len(events) > 1 {
			s.buf = append(s.buf[:0], events[1:]...)
		}
		return true
	}

	if err := s.scanner.Err(); err != nil {
		s.err = err
		s.done = true
		return false
	}

	// Scanner exhausted — handle process exit.
	return s.handleProcessExit()
}

func (s *processStream) handleProcessExit() bool {
	if s.proc == nil {
		s.done = true
		return false
	}
	if s.proc.Stdout != nil {
		if err := s.proc.Stdout.Close(); err != nil && s.err == nil {
			s.err = err
		}
	}
	if s.proc.Stdin != nil {
		if err := s.proc.Stdin.Close(); err != nil && s.err == nil {
			s.err = err
		}
	}
	exitCode, waitErr := s.proc.Wait()
	if waitErr != nil {
		s.err = waitErr
		s.done = true
		s.proc = nil
		return false
	}
	if s.exitHandler != nil && exitCode != 0 {
		var stderrData []byte
		if s.proc.Stderr != nil {
			stderrData = s.proc.Stderr.Bytes()
		}
		exitEvents := s.exitHandler(exitCode, stderrData)
		if len(exitEvents) > 0 {
			s.cur = exitEvents[0]
			if len(exitEvents) > 1 {
				s.buf = append(s.buf[:0], exitEvents[1:]...)
			}
			s.done = true
			s.proc = nil
			return true
		}
	}
	s.done = true
	s.proc = nil
	return false
}

func (s *processStream) Event() Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

func (s *processStream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *processStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var firstErr error
	if s.proc != nil {
		if s.proc.Stdout != nil {
			if err := s.proc.Stdout.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if s.proc.Stdin != nil {
			if err := s.proc.Stdin.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if _, err := s.proc.Wait(); err != nil && firstErr == nil {
			firstErr = err
		}
		s.proc = nil
	}
	return firstErr
}
