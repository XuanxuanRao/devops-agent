package terminal

import (
	"os"
	"testing"
)

func TestSessionStatusValues(t *testing.T) {
	if SessionOpening != "opening" {
		t.Fatalf("SessionOpening = %q, want %q", SessionOpening, "opening")
	}
	if SessionOpen != "open" {
		t.Fatalf("SessionOpen = %q, want %q", SessionOpen, "open")
	}
	if SessionClosing != "closing" {
		t.Fatalf("SessionClosing = %q, want %q", SessionClosing, "closing")
	}
	if SessionClosed != "closed" {
		t.Fatalf("SessionClosed = %q, want %q", SessionClosed, "closed")
	}
	if SessionError != "error" {
		t.Fatalf("SessionError = %q, want %q", SessionError, "error")
	}
}

func TestErrorValuesAndFormatting(t *testing.T) {
	if got := (*Error)(nil).Error(); got != "terminal error" {
		t.Fatalf("(*Error)(nil).Error() = %q, want %q", got, "terminal error")
	}

	if ErrSessionNotFound.Code() != "SESSION_NOT_FOUND" {
		t.Fatalf("ErrSessionNotFound.Code() = %q, want %q", ErrSessionNotFound.Code(), "SESSION_NOT_FOUND")
	}
	if ErrSessionAlreadyExists.Code() != "SESSION_ALREADY_EXISTS" {
		t.Fatalf("ErrSessionAlreadyExists.Code() = %q, want %q", ErrSessionAlreadyExists.Code(), "SESSION_ALREADY_EXISTS")
	}
	if ErrSessionNotOpen.Code() != "SESSION_NOT_OPEN" {
		t.Fatalf("ErrSessionNotOpen.Code() = %q, want %q", ErrSessionNotOpen.Code(), "SESSION_NOT_OPEN")
	}
	if ErrUnsupportedSignal.Code() != "UNSUPPORTED_SIGNAL" {
		t.Fatalf("ErrUnsupportedSignal.Code() = %q, want %q", ErrUnsupportedSignal.Code(), "UNSUPPORTED_SIGNAL")
	}

	err := &Error{code: "X", message: "boom"}
	if got := err.Code(); got != "X" {
		t.Fatalf("err.Code() = %q, want %q", got, "X")
	}
	if got := err.Error(); got != "X: boom" {
		t.Fatalf("err.Error() = %q, want %q", got, "X: boom")
	}
}

func TestPtyContracts(t *testing.T) {
	spec := StartSpec{
		SessionID: "ts-1",
		Shell:     "/bin/sh",
		Cwd:       "/tmp",
		Env:       []string{"A=B"},
		Cols:      80,
		Rows:      24,
		Title:     "shell",
		Stdout:    os.Stdout,
	}

	if spec.SessionID != "ts-1" {
		t.Fatalf("SessionID = %q, want %q", spec.SessionID, "ts-1")
	}
	if spec.Shell != "/bin/sh" {
		t.Fatalf("Shell = %q, want %q", spec.Shell, "/bin/sh")
	}
	if spec.Cwd != "/tmp" {
		t.Fatalf("Cwd = %q, want %q", spec.Cwd, "/tmp")
	}
	if len(spec.Env) != 1 || spec.Env[0] != "A=B" {
		t.Fatalf("Env = %v, want %v", spec.Env, []string{"A=B"})
	}
	if spec.Cols != 80 || spec.Rows != 24 {
		t.Fatalf("size = %dx%d, want 80x24", spec.Cols, spec.Rows)
	}
	if spec.Title != "shell" {
		t.Fatalf("Title = %q, want %q", spec.Title, "shell")
	}
	if spec.Stdout != os.Stdout {
		t.Fatalf("Stdout = %v, want %v", spec.Stdout, os.Stdout)
	}

	proc := &PtyProcess{
		File: testPtyFile{},
		PID:  42,
		Ref:  "pty_42",
	}
	if proc.PID != 42 {
		t.Fatalf("PID = %d, want 42", proc.PID)
	}
	if proc.File == nil {
		t.Fatalf("File = nil, want non-nil")
	}
	if proc.Ref != "pty_42" {
		t.Fatalf("Ref = %q, want %q", proc.Ref, "pty_42")
	}

	var _ PtyFile = testPtyFile{}
	var _ PtyFactory = testPtyFactory{}
}

type testPtyFile struct{}

func (testPtyFile) Read(p []byte) (int, error)  { return 0, nil }
func (testPtyFile) Write(p []byte) (int, error) { return len(p), nil }
func (testPtyFile) Close() error                { return nil }

type testPtyFactory struct{}

func (testPtyFactory) Start(spec StartSpec) (*PtyProcess, error) {
	return &PtyProcess{File: testPtyFile{}}, nil
}

func (testPtyFactory) Resize(file PtyFile, cols, rows int) error {
	return nil
}
