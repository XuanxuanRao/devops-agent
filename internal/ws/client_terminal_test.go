package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"strings"
	"testing"

	agentconfig "devops-agent/internal/config"
	agentcrypto "devops-agent/internal/crypto"
	"devops-agent/internal/protocol"
	"devops-agent/internal/terminal"
)

func TestClientCloseTerminalSessionsDelegatesToManager(t *testing.T) {
	manager := &stubTerminalManager{}
	client := &Client{terminalManager: manager}

	if err := client.CloseTerminalSessions(context.Background()); err != nil {
		t.Fatalf("CloseTerminalSessions() error = %v", err)
	}
	if manager.calls != 1 {
		t.Fatalf("close calls = %d, want 1", manager.calls)
	}
	if manager.reason != "agent_closed" {
		t.Fatalf("close reason = %q, want %q", manager.reason, "agent_closed")
	}
}

func TestClientCloseTerminalSessionsReturnsManagerError(t *testing.T) {
	closeErr := errors.New("close failed")
	manager := &stubTerminalManager{stubTerminalManagerBase: stubTerminalManagerBase{err: closeErr}}
	client := &Client{terminalManager: manager}

	err := client.CloseTerminalSessions(context.Background())
	if !errors.Is(err, closeErr) {
		t.Fatalf("CloseTerminalSessions() error = %v, want %v", err, closeErr)
	}
}

func TestHandleTerminalOpenEventDispatchesToManager(t *testing.T) {
	manager := &stubTerminalManager{}
	client := &Client{
		logger:          log.New(io.Discard, "", 0),
		terminalManager: manager,
	}

	raw, err := json.Marshal(protocol.TerminalSessionOpenPayload{
		RequestID: "req-1",
		SessionID: "ts-1",
		DeviceID:  "agent-1",
		Shell:     "/bin/sh",
		Cwd:       t.TempDir(),
		Env:       map[string]string{"FOO": "bar"},
		Cols:      80,
		Rows:      24,
		Title:     "sh",
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	if err := client.handleTerminalEvent(context.Background(), protocol.EventTerminalSessionOpen, raw); err != nil {
		t.Fatalf("handleTerminalEvent() error = %v", err)
	}
	if manager.openCalls != 1 {
		t.Fatalf("openCalls = %d, want 1", manager.openCalls)
	}
	if manager.lastOpen.RequestID != "req-1" || manager.lastOpen.SessionID != "ts-1" {
		t.Fatalf("lastOpen = %#v, want request/session ids propagated", manager.lastOpen)
	}
	if manager.lastOpen.Env["FOO"] != "bar" {
		t.Fatalf("lastOpen.Env = %#v, want FOO=bar", manager.lastOpen.Env)
	}
}

func TestHandleTerminalResizeEventDispatchesToManager(t *testing.T) {
	manager := &stubTerminalManager{}
	client := &Client{
		logger:          log.New(io.Discard, "", 0),
		terminalManager: manager,
	}

	raw, err := json.Marshal(protocol.TerminalSessionResizePayload{
		SessionID: "ts-1",
		Cols:      120,
		Rows:      40,
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	if err := client.handleTerminalEvent(context.Background(), protocol.EventTerminalSessionResize, raw); err != nil {
		t.Fatalf("handleTerminalEvent() error = %v", err)
	}
	if manager.resizeCalls != 1 {
		t.Fatalf("resizeCalls = %d, want 1", manager.resizeCalls)
	}
	if manager.lastResizeSessionID != "ts-1" || manager.lastCols != 120 || manager.lastRows != 40 {
		t.Fatalf("resize args = session=%q cols=%d rows=%d", manager.lastResizeSessionID, manager.lastCols, manager.lastRows)
	}
}

func TestHandleTerminalWriteSignalAndCloseDispatchToManager(t *testing.T) {
	manager := &stubTerminalManager{}
	client := &Client{
		logger:          log.New(io.Discard, "", 0),
		terminalManager: manager,
	}

	writeRaw, _ := json.Marshal(protocol.TerminalStdinWritePayload{SessionID: "ts-1", Data: "pwd\n"})
	signalRaw, _ := json.Marshal(protocol.TerminalSessionSignalPayload{SessionID: "ts-1", Signal: "SIGINT"})
	closeRaw, _ := json.Marshal(protocol.TerminalSessionClosePayload{SessionID: "ts-1", Reason: "client_closed"})

	if err := client.handleTerminalEvent(context.Background(), protocol.EventTerminalStdinWrite, writeRaw); err != nil {
		t.Fatalf("write event error = %v", err)
	}
	if err := client.handleTerminalEvent(context.Background(), protocol.EventTerminalSessionSignal, signalRaw); err != nil {
		t.Fatalf("signal event error = %v", err)
	}
	if err := client.handleTerminalEvent(context.Background(), protocol.EventTerminalSessionClose, closeRaw); err != nil {
		t.Fatalf("close event error = %v", err)
	}

	if manager.writeCalls != 1 || manager.lastWriteData != "pwd\n" {
		t.Fatalf("write calls/data = %d/%q, want 1/%q", manager.writeCalls, manager.lastWriteData, "pwd\n")
	}
	if manager.signalCalls != 1 || manager.lastSignal != "SIGINT" {
		t.Fatalf("signal calls/value = %d/%q, want 1/%q", manager.signalCalls, manager.lastSignal, "SIGINT")
	}
	if manager.calls != 1 || manager.reason != "client_closed" {
		t.Fatalf("close calls/reason = %d/%q, want 1/%q", manager.calls, manager.reason, "client_closed")
	}
}

func TestEmitSessionOpenedUsesSendEvent(t *testing.T) {
	client, sent := newEventRecordingClient()

	err := client.EmitSessionOpened(context.Background(), protocol.TerminalSessionOpenedPayload{
		RequestID:       "req-1",
		SessionID:       "ts-1",
		AgentSessionRef: "pty_42",
		ShellPID:        42,
		Cwd:             "/tmp",
		Title:           "sh",
	})
	if err != nil {
		t.Fatalf("EmitSessionOpened() error = %v", err)
	}
	if len(*sent) != 1 {
		t.Fatalf("sent events = %d, want 1", len(*sent))
	}
	if (*sent)[0].event != protocol.EventTerminalSessionOpened {
		t.Fatalf("event = %q, want %q", (*sent)[0].event, protocol.EventTerminalSessionOpened)
	}
}

func TestEmitStdoutChunkUsesSendEvent(t *testing.T) {
	client, sent := newEventRecordingClient()

	err := client.EmitStdoutChunk(context.Background(), protocol.TerminalStdoutChunkPayload{
		SessionID: "ts-1",
		Seq:       1,
		Stream:    "stdout",
		Data:      "hello",
	})
	if err != nil {
		t.Fatalf("EmitStdoutChunk() error = %v", err)
	}
	if len(*sent) != 1 {
		t.Fatalf("sent events = %d, want 1", len(*sent))
	}
	if (*sent)[0].event != protocol.EventTerminalStdoutChunk {
		t.Fatalf("event = %q, want %q", (*sent)[0].event, protocol.EventTerminalStdoutChunk)
	}
}

func TestNewClientOpenAndWriteUseInjectedRealFactory(t *testing.T) {
	client := NewClient(&agentconfig.Config{
		Shell: agentconfig.ShellConfig{
			WorkDir: t.TempDir(),
		},
	}, agentcrypto.KeyPair{}, log.New(io.Discard, "", 0), nil)

	openRaw, err := json.Marshal(protocol.TerminalSessionOpenPayload{
		RequestID: "req-1",
		SessionID: "ts-real",
		Shell:     "/bin/sh",
		Cwd:       t.TempDir(),
		Cols:      80,
		Rows:      24,
		Title:     "sh",
	})
	if err != nil {
		t.Fatalf("Marshal(open) error = %v", err)
	}

	if err := client.handleTerminalEvent(context.Background(), protocol.EventTerminalSessionOpen, openRaw); err != nil {
		t.Fatalf("handleTerminalEvent(open) error = %v", err)
	}
	t.Cleanup(func() {
		_ = client.CloseTerminalSessions(context.Background())
	})

	writeRaw, err := json.Marshal(protocol.TerminalStdinWritePayload{
		SessionID: "ts-real",
		Data:      "printf '__WS_OK__\\n'\n",
	})
	if err != nil {
		t.Fatalf("Marshal(write) error = %v", err)
	}

	if err := client.handleTerminalEvent(context.Background(), protocol.EventTerminalStdinWrite, writeRaw); err != nil {
		t.Fatalf("handleTerminalEvent(write) error = %v", err)
	}
}

func TestHandleTerminalOpenLogsSuccessAndFailure(t *testing.T) {
	buf := &bytes.Buffer{}
	manager := &stubTerminalManager{}
	client := &Client{
		logger:          log.New(buf, "", 0),
		terminalManager: manager,
	}

	openRaw, err := json.Marshal(protocol.TerminalSessionOpenPayload{
		RequestID: "req-1",
		SessionID: "ts-log",
		Shell:     "/bin/sh",
		Cwd:       t.TempDir(),
		Cols:      80,
		Rows:      24,
		Title:     "sh",
	})
	if err != nil {
		t.Fatalf("Marshal(open) error = %v", err)
	}

	if err := client.handleTerminalEvent(context.Background(), protocol.EventTerminalSessionOpen, openRaw); err != nil {
		t.Fatalf("handleTerminalEvent(open success) error = %v", err)
	}
	gotSuccessLog := buf.String()
	if !strings.Contains(gotSuccessLog, "open success") || !strings.Contains(gotSuccessLog, "session=ts-log") {
		t.Fatalf("success log = %q, want contains open success + session id", gotSuccessLog)
	}

	buf.Reset()
	manager.err = errors.New("boom")
	err = client.handleTerminalEvent(context.Background(), protocol.EventTerminalSessionOpen, openRaw)
	if !errors.Is(err, manager.err) {
		t.Fatalf("handleTerminalEvent(open failure) error = %v, want %v", err, manager.err)
	}
	gotFailureLog := buf.String()
	if !strings.Contains(gotFailureLog, "open failed") || !strings.Contains(gotFailureLog, "session=ts-log") || !strings.Contains(gotFailureLog, "boom") {
		t.Fatalf("failure log = %q, want contains open failed + session id + error", gotFailureLog)
	}
}

type stubTerminalManagerBase struct {
	calls  int
	reason string
	err    error
}

func (s *stubTerminalManagerBase) CloseAll(_ context.Context, reason string) error {
	s.calls++
	s.reason = reason
	return s.err
}

type stubTerminalManager struct {
	stubTerminalManagerBase
	openCalls   int
	writeCalls  int
	resizeCalls int
	signalCalls int

	lastOpen            terminal.OpenPayload
	lastWriteSessionID  string
	lastWriteData       string
	lastResizeSessionID string
	lastCols            int
	lastRows            int
	lastSignalSessionID string
	lastSignal          string
	lastCloseSessionID  string
}

func (s *stubTerminalManager) OpenSession(_ context.Context, payload terminal.OpenPayload) error {
	s.openCalls++
	s.lastOpen = payload
	return s.err
}

func (s *stubTerminalManager) Write(_ context.Context, sessionID, data string) error {
	s.writeCalls++
	s.lastWriteSessionID = sessionID
	s.lastWriteData = data
	return s.err
}

func (s *stubTerminalManager) Resize(_ context.Context, sessionID string, cols, rows int) error {
	s.resizeCalls++
	s.lastResizeSessionID = sessionID
	s.lastCols = cols
	s.lastRows = rows
	return s.err
}

func (s *stubTerminalManager) Signal(_ context.Context, sessionID, signal string) error {
	s.signalCalls++
	s.lastSignalSessionID = sessionID
	s.lastSignal = signal
	return s.err
}

func (s *stubTerminalManager) Close(_ context.Context, sessionID, reason string) error {
	s.calls++
	s.lastCloseSessionID = sessionID
	s.reason = reason
	return s.err
}

type sentEvent struct {
	event   string
	payload any
}

func newEventRecordingClient() (*Client, *[]sentEvent) {
	sent := make([]sentEvent, 0, 1)
	client := &Client{
		logger: log.New(io.Discard, "", 0),
	}
	client.sendEventFn = func(_ context.Context, event string, payload any) error {
		sent = append(sent, sentEvent{event: event, payload: payload})
		return nil
	}
	return client, &sent
}
