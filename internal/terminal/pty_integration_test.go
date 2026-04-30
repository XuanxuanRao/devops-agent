package terminal

import (
	"context"
	"devops-agent/internal/protocol"
	"io"
	"log"
	"strings"
	"testing"
	"time"
)

func TestRealPtyFactoryStartsShellAndStreamsOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skip PTY integration test in short mode")
	}

	sink := &recordingSink{}
	session := newSession(OpenPayload{
		SessionID: "ts-real-output",
		Shell:     "/bin/sh",
		Cwd:       t.TempDir(),
		Cols:      80,
		Rows:      24,
		Title:     "sh",
	}, sessionOptions{
		factory: NewRealPtyFactory(),
		sink:    sink,
		logger:  log.New(io.Discard, "", 0),
	})

	if err := session.start(context.Background()); err != nil {
		t.Fatalf("start() error = %v", err)
	}
	t.Cleanup(func() {
		_ = session.close(context.Background(), "test_cleanup")
	})

	if err := session.write(context.Background(), "printf '__REAL_OK__\\n'\n"); err != nil {
		t.Fatalf("write() error = %v", err)
	}

	waitFor(t, 3*time.Second, func() bool {
		return strings.Contains(joinChunkData(sink.chunkSnapshot()), "__REAL_OK__")
	})
}

func TestRealPtyFactoryResizeAndSignalAffectShell(t *testing.T) {
	if testing.Short() {
		t.Skip("skip PTY integration test in short mode")
	}

	sink := &recordingSink{}
	session := newSession(OpenPayload{
		SessionID: "ts-real-control",
		Shell:     "/bin/sh",
		Cwd:       t.TempDir(),
		Cols:      80,
		Rows:      24,
		Title:     "sh",
	}, sessionOptions{
		factory: NewRealPtyFactory(),
		sink:    sink,
		logger:  log.New(io.Discard, "", 0),
	})

	if err := session.start(context.Background()); err != nil {
		t.Fatalf("start() error = %v", err)
	}
	t.Cleanup(func() {
		_ = session.close(context.Background(), "test_cleanup")
	})

	if err := session.write(context.Background(), "stty size\n"); err != nil {
		t.Fatalf("write(stty size) error = %v", err)
	}
	waitFor(t, 3*time.Second, func() bool {
		return strings.Contains(joinChunkData(sink.chunkSnapshot()), "24 80")
	})

	if err := session.resize(context.Background(), 100, 40); err != nil {
		t.Fatalf("resize() error = %v", err)
	}
	if err := session.write(context.Background(), "stty size\n"); err != nil {
		t.Fatalf("write(stty size after resize) error = %v", err)
	}
	waitFor(t, 3*time.Second, func() bool {
		return strings.Contains(joinChunkData(sink.chunkSnapshot()), "40 100")
	})

	if err := session.write(context.Background(), "sleep 10\n"); err != nil {
		t.Fatalf("write(sleep) error = %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	if err := session.signal(context.Background(), "SIGINT"); err != nil {
		t.Fatalf("signal(SIGINT) error = %v", err)
	}
	if err := session.write(context.Background(), "printf '__AFTER_SIG__\\n'\n"); err != nil {
		t.Fatalf("write(after signal) error = %v", err)
	}

	waitFor(t, 3*time.Second, func() bool {
		return strings.Contains(joinChunkData(sink.chunkSnapshot()), "__AFTER_SIG__")
	})
}

func joinChunkData(chunks []protocol.TerminalStdoutChunkPayload) string {
	var b strings.Builder
	for _, chunk := range chunks {
		b.WriteString(chunk.Data)
	}
	return b.String()
}
