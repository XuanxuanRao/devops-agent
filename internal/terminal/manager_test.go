package terminal

import (
	"context"
	"errors"
	"io"
	"log"
	"sync"
	"testing"

	"devops-agent/internal/protocol"
)

func TestManagerOpenSessionRejectsDuplicateSessionID(t *testing.T) {
	mgr := NewManager(Options{
		DefaultShell:   "/bin/sh",
		DefaultWorkDir: t.TempDir(),
		Sink:           noopSink{},
		Logger:         log.New(io.Discard, "", 0),
	})

	payload := OpenPayload{
		RequestID: "req-1",
		SessionID: "ts-1",
		Shell:     "/bin/sh",
		Cwd:       t.TempDir(),
		Cols:      80,
		Rows:      24,
		Title:     "sh",
	}

	if err := mgr.OpenSession(context.Background(), payload); err != nil {
		t.Fatalf("first OpenSession() error = %v", err)
	}

	if err := mgr.OpenSession(context.Background(), payload); !errors.Is(err, ErrSessionAlreadyExists) {
		t.Fatalf("second OpenSession() error = %v, want %v", err, ErrSessionAlreadyExists)
	}
}

func TestManagerOpenSessionRollsBackOnStartError(t *testing.T) {
	startErr := errors.New("boom")
	mgr := NewManager(Options{
		DefaultShell:   "/bin/sh",
		DefaultWorkDir: t.TempDir(),
		Sink:           noopSink{},
		Logger:         log.New(io.Discard, "", 0),
	})
	mgr.newSession = func(payload OpenPayload, opts sessionOptions) *Session {
		s := newSession(payload, opts)
		s.startFn = func(context.Context) error { return startErr }
		return s
	}

	err := mgr.OpenSession(context.Background(), OpenPayload{
		RequestID: "req-1",
		SessionID: "ts-rollback",
		Cwd:       t.TempDir(),
		Cols:      80,
		Rows:      24,
	})
	if !errors.Is(err, startErr) {
		t.Fatalf("OpenSession() error = %v, want %v", err, startErr)
	}
	if _, ok := mgr.Get("ts-rollback"); ok {
		t.Fatalf("Get(ts-rollback) ok = true, want false after rollback")
	}
}

func TestManagerRoutesReturnSessionNotFoundForMissingSession(t *testing.T) {
	mgr := NewManager(Options{})
	ctx := context.Background()

	cases := []struct {
		name string
		run  func() error
	}{
		{
			name: "write",
			run:  func() error { return mgr.Write(ctx, "missing", "pwd\n") },
		},
		{
			name: "resize",
			run:  func() error { return mgr.Resize(ctx, "missing", 80, 24) },
		},
		{
			name: "signal",
			run:  func() error { return mgr.Signal(ctx, "missing", "SIGINT") },
		},
		{
			name: "close",
			run:  func() error { return mgr.Close(ctx, "missing", "client_closed") },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); !errors.Is(err, ErrSessionNotFound) {
				t.Fatalf("%s error = %v, want %v", tc.name, err, ErrSessionNotFound)
			}
		})
	}
}

func TestManagerRoutesInvokeSessionMethods(t *testing.T) {
	var writes, resizes, signals, closes int
	mgr := NewManager(Options{})
	mgr.sessions["ts-1"] = &Session{
		meta: SessionMeta{SessionID: "ts-1", Status: SessionOpen},
		writeFn: func(context.Context, string) error {
			writes++
			return nil
		},
		resizeFn: func(context.Context, int, int) error {
			resizes++
			return nil
		},
		signalFn: func(context.Context, string) error {
			signals++
			return nil
		},
		closeFn: func(context.Context, string) error {
			closes++
			return nil
		},
	}

	ctx := context.Background()
	if err := mgr.Write(ctx, "ts-1", "pwd\n"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := mgr.Resize(ctx, "ts-1", 100, 40); err != nil {
		t.Fatalf("Resize() error = %v", err)
	}
	if err := mgr.Signal(ctx, "ts-1", "SIGINT"); err != nil {
		t.Fatalf("Signal() error = %v", err)
	}
	if err := mgr.Close(ctx, "ts-1", "client_closed"); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if writes != 1 || resizes != 1 || signals != 1 || closes != 1 {
		t.Fatalf("calls = write:%d resize:%d signal:%d close:%d, want 1 each", writes, resizes, signals, closes)
	}
}

func TestManagerOpenSessionReturnsErrorIfClosedDuringStart(t *testing.T) {
	enterStart := make(chan struct{})
	releaseStart := make(chan struct{})

	mgr := NewManager(Options{})
	mgr.newSession = func(payload OpenPayload, opts sessionOptions) *Session {
		s := newSession(payload, opts)
		s.startFn = func(context.Context) error {
			close(enterStart)
			<-releaseStart
			return nil
		}
		return s
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- mgr.OpenSession(context.Background(), OpenPayload{
			SessionID: "ts-race",
			Cols:      80,
			Rows:      24,
		})
	}()

	<-enterStart
	if err := mgr.Close(context.Background(), "ts-race", "client_closed"); err != nil {
		t.Fatalf("Close() during start error = %v", err)
	}
	close(releaseStart)

	if err := <-errCh; !errors.Is(err, ErrSessionNotOpen) {
		t.Fatalf("OpenSession() error = %v, want %v", err, ErrSessionNotOpen)
	}
	if _, ok := mgr.Get("ts-race"); ok {
		t.Fatalf("Get(ts-race) ok = true, want false after close-during-start")
	}
}

func TestManagerCloseAllContinuesAfterErrorAndKeepsFailedSession(t *testing.T) {
	closeErr := errors.New("close failed")
	var mu sync.Mutex
	closed := make([]string, 0, 2)

	mgr := NewManager(Options{})
	mgr.sessions["ok"] = &Session{
		meta: SessionMeta{SessionID: "ok", Status: SessionOpen},
		closeFn: func(context.Context, string) error {
			mu.Lock()
			closed = append(closed, "ok")
			mu.Unlock()
			return nil
		},
	}
	mgr.sessions["fail"] = &Session{
		meta: SessionMeta{SessionID: "fail", Status: SessionOpen},
		closeFn: func(context.Context, string) error {
			mu.Lock()
			closed = append(closed, "fail")
			mu.Unlock()
			return closeErr
		},
	}

	err := mgr.CloseAll(context.Background(), "agent_closed")
	if !errors.Is(err, closeErr) {
		t.Fatalf("CloseAll() error = %v, want %v", err, closeErr)
	}

	mu.Lock()
	gotClosed := len(closed)
	mu.Unlock()
	if gotClosed != 2 {
		t.Fatalf("close calls = %d, want 2", gotClosed)
	}
	if _, ok := mgr.Get("ok"); ok {
		t.Fatalf("Get(ok) ok = true, want false for successfully closed session")
	}
	if _, ok := mgr.Get("fail"); !ok {
		t.Fatalf("Get(fail) ok = false, want true for failed close session")
	}
}

type noopSink struct{}

func (noopSink) EmitSessionOpened(context.Context, protocol.TerminalSessionOpenedPayload) error {
	return nil
}

func (noopSink) EmitStdoutChunk(context.Context, protocol.TerminalStdoutChunkPayload) error {
	return nil
}

func (noopSink) EmitSessionState(context.Context, protocol.TerminalSessionStatePayload) error {
	return nil
}

func (noopSink) EmitSessionClosed(context.Context, protocol.TerminalSessionClosedPayload) error {
	return nil
}

func (noopSink) EmitSessionError(context.Context, protocol.TerminalSessionErrorPayload) error {
	return nil
}
