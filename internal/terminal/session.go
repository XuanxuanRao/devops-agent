package terminal

import (
	"context"
	"errors"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"devops-agent/internal/protocol"
)

type sessionOptions struct {
	defaultShell   string
	defaultWorkDir string
	factory        PtyFactory
	sink           EventSink
	logger         *log.Logger
}

type Session struct {
	mu sync.RWMutex

	meta SessionMeta
	seq  uint64

	requestID string

	factory PtyFactory
	sink    EventSink
	logger  *log.Logger
	env     []string

	proc PtyProcess
	pty  PtyFile

	startFn  func(context.Context) error
	writeFn  func(context.Context, string) error
	resizeFn func(context.Context, int, int) error
	signalFn func(context.Context, string) error
	closeFn  func(context.Context, string) error

	closeDone chan struct{}
	closeErr  error
	waitDone  chan struct{}
	waitErr   error
	waitExit  *int
}

func newSession(payload OpenPayload, opts sessionOptions) *Session {
	now := time.Now().UTC()

	shell := payload.Shell
	if shell == "" {
		shell = opts.defaultShell
	}

	cwd := payload.Cwd
	if cwd == "" {
		cwd = opts.defaultWorkDir
	}

	title := payload.Title
	if title == "" {
		title = shell
	}

	return &Session{
		meta: SessionMeta{
			SessionID:    payload.SessionID,
			Shell:        shell,
			Cwd:          cwd,
			Title:        title,
			Cols:         payload.Cols,
			Rows:         payload.Rows,
			Status:       SessionOpening,
			CreatedAt:    now,
			UpdatedAt:    now,
			LastActiveAt: now,
		},
		requestID: payload.RequestID,
		factory:   opts.factory,
		sink:      opts.sink,
		logger:    opts.logger,
		env:       envMapToList(payload.Env),
	}
}

func (s *Session) start(ctx context.Context) error {
	s.mu.RLock()
	status := s.meta.Status
	s.mu.RUnlock()
	if status != SessionOpening {
		return ErrSessionNotOpen
	}

	if s.startFn != nil {
		if err := s.startFn(ctx); err != nil {
			s.mu.Lock()
			s.meta.Status = SessionError
			s.meta.UpdatedAt = time.Now().UTC()
			s.mu.Unlock()
			s.emitError(ctx, errorCodeForStart(err), err.Error())
			return err
		}
	} else if s.factory != nil {
		proc, err := s.factory.Start(StartSpec{
			SessionID: s.meta.SessionID,
			Shell:     s.meta.Shell,
			Cwd:       s.meta.Cwd,
			Env:       append([]string(nil), s.env...),
			Cols:      s.meta.Cols,
			Rows:      s.meta.Rows,
			Title:     s.meta.Title,
		})
		if err != nil {
			s.mu.Lock()
			s.meta.Status = SessionError
			s.meta.UpdatedAt = time.Now().UTC()
			s.mu.Unlock()
			s.emitError(ctx, errorCodeForStart(err), err.Error())
			return err
		}
		s.mu.Lock()
		s.proc = *proc
		s.pty = proc.File
		if proc.Wait != nil && s.waitDone == nil {
			s.waitDone = make(chan struct{})
		}
		s.mu.Unlock()
	}

	if !s.hasValidProcess() {
		s.mu.Lock()
		s.meta.Status = SessionError
		s.meta.UpdatedAt = time.Now().UTC()
		s.mu.Unlock()
		s.emitError(ctx, ErrSessionInvalidProcess.Code(), ErrSessionInvalidProcess.Error())
		return ErrSessionInvalidProcess
	}

	s.mu.Lock()
	if s.meta.Status != SessionOpening {
		s.mu.Unlock()
		return ErrSessionNotOpen
	}
	s.meta.Status = SessionOpen
	s.meta.UpdatedAt = time.Now().UTC()
	s.meta.LastActiveAt = s.meta.UpdatedAt
	s.mu.Unlock()

	s.emitOpened(ctx)
	s.emitState(ctx)
	if s.pty != nil {
		s.startReadLoop()
	}
	if s.proc.Wait != nil {
		s.startWaitLoop()
	}
	return nil
}

func (s *Session) write(ctx context.Context, data string) error {
	if !s.isOpen() {
		return ErrSessionNotOpen
	}
	if s.writeFn != nil {
		if err := s.writeFn(ctx, data); err != nil {
			return err
		}
	} else if s.pty != nil {
		if _, err := s.pty.Write([]byte(data)); err != nil {
			return err
		}
	} else {
		return ErrSessionPTYUnavailable
	}

	s.touch()
	return nil
}

func (s *Session) resize(ctx context.Context, cols, rows int) error {
	if !s.isOpen() {
		return ErrSessionNotOpen
	}
	if s.resizeFn != nil {
		if err := s.resizeFn(ctx, cols, rows); err != nil {
			return err
		}
	} else if s.factory != nil && s.pty != nil {
		if err := s.factory.Resize(s.pty, cols, rows); err != nil {
			return err
		}
	} else {
		return ErrSessionPTYUnavailable
	}

	s.mu.Lock()
	s.meta.Cols = cols
	s.meta.Rows = rows
	s.meta.UpdatedAt = time.Now().UTC()
	s.meta.LastActiveAt = s.meta.UpdatedAt
	s.mu.Unlock()
	s.emitState(ctx)
	return nil
}

func (s *Session) signal(ctx context.Context, signal string) error {
	if !s.isOpen() {
		return ErrSessionNotOpen
	}
	if s.signalFn != nil {
		if err := s.signalFn(ctx, signal); err != nil {
			return err
		}
	} else {
		sig, err := normalizeSignal(signal)
		if err != nil {
			return err
		}
		if s.proc.Signal != nil {
			if err := s.proc.Signal(sig); err != nil {
				return err
			}
		} else {
			return ErrSessionPTYUnavailable
		}
	}

	s.touch()
	return nil
}

func (s *Session) close(ctx context.Context, reason string) error {
	s.mu.Lock()
	if s.meta.Status == SessionClosed {
		s.mu.Unlock()
		return nil
	}
	if s.meta.Status == SessionClosing {
		done := s.closeDone
		s.mu.Unlock()
		if done != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-done:
			}
		}

		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.closeErr
	}

	done := make(chan struct{})
	s.closeDone = done
	s.closeErr = nil
	s.meta.Status = SessionClosing
	s.meta.CloseReason = reason
	s.meta.UpdatedAt = time.Now().UTC()
	s.meta.LastActiveAt = s.meta.UpdatedAt
	s.mu.Unlock()

	s.emitState(ctx)

	var err error
	if s.closeFn != nil {
		err = s.closeFn(ctx, reason)
	} else {
		if s.proc.Signal != nil {
			if signalErr := s.proc.Signal(syscall.SIGHUP); signalErr != nil && !errors.Is(signalErr, syscall.ESRCH) {
				err = signalErr
			}
		}
		if err == nil && s.pty != nil {
			err = s.pty.Close()
		}
	}
	if err != nil {
		s.mu.Lock()
		s.closeErr = err
		s.meta.Status = SessionError
		s.meta.UpdatedAt = time.Now().UTC()
		close(s.closeDone)
		s.closeDone = nil
		s.mu.Unlock()
		return err
	}

	waitDone := s.currentWaitDone()
	if waitDone == nil {
		s.finishClose(ctx, reason, nil)
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-waitDone:
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.waitErr != nil && s.waitExit == nil && !errors.Is(s.waitErr, os.ErrClosed) && !errors.Is(s.waitErr, syscall.EIO) {
		return s.waitErr
	}
	return s.closeErr
}

func (s *Session) isOpen() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.meta.Status == SessionOpen
}

func (s *Session) touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meta.UpdatedAt = time.Now().UTC()
	s.meta.LastActiveAt = s.meta.UpdatedAt
}

func (s *Session) startReadLoop() {
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := s.pty.Read(buf)
			if n > 0 {
				s.recordChunk(context.Background(), string(buf[:n]))
			}
			if err != nil {
				return
			}
		}
	}()
}

func (s *Session) startWaitLoop() {
	go func() {
		err := s.proc.Wait()
		exitCode := exitCodeFromWaitErr(err)
		s.mu.Lock()
		s.waitErr = err
		s.waitExit = exitCode
		waitDone := s.waitDone
		s.waitDone = nil
		closeReason := s.meta.CloseReason
		s.mu.Unlock()

		if waitDone != nil {
			close(waitDone)
		}

		if closeReason == "" {
			closeReason = "process_exit"
		}
		s.finishClose(context.Background(), closeReason, exitCode)
	}()
}

func (s *Session) recordChunk(ctx context.Context, data string) {
	if data == "" {
		return
	}

	s.mu.Lock()
	s.seq++
	payload := protocol.TerminalStdoutChunkPayload{
		SessionID: s.meta.SessionID,
		Seq:       s.seq,
		Stream:    "stdout",
		Data:      data,
		Cwd:       s.meta.Cwd,
		Title:     s.meta.Title,
		IsBinary:  false,
	}
	s.meta.UpdatedAt = time.Now().UTC()
	s.meta.LastActiveAt = s.meta.UpdatedAt
	s.mu.Unlock()

	if s.sink != nil {
		if err := s.sink.EmitStdoutChunk(ctx, payload); err != nil && s.logger != nil {
			s.logger.Printf("terminal stdout emit failed: session=%s err=%v", payload.SessionID, err)
		}
	}
}

func (s *Session) emitState(ctx context.Context) {
	if s.sink == nil {
		return
	}

	s.mu.RLock()
	payload := protocol.TerminalSessionStatePayload{
		SessionID: s.meta.SessionID,
		Status:    string(s.meta.Status),
		Title:     s.meta.Title,
		Cwd:       s.meta.Cwd,
		Cols:      s.meta.Cols,
		Rows:      s.meta.Rows,
		Seq:       s.seq,
		UpdatedAt: s.meta.UpdatedAt.Format(time.RFC3339Nano),
	}
	s.mu.RUnlock()

	if err := s.sink.EmitSessionState(ctx, payload); err != nil && s.logger != nil {
		s.logger.Printf("terminal state emit failed: session=%s err=%v", payload.SessionID, err)
	}
}

func (s *Session) emitOpened(ctx context.Context) {
	if s.sink == nil {
		return
	}

	s.mu.RLock()
	payload := protocol.TerminalSessionOpenedPayload{
		RequestID:       s.requestID,
		SessionID:       s.meta.SessionID,
		AgentSessionRef: s.proc.Ref,
		ShellPID:        s.proc.PID,
		Cwd:             s.meta.Cwd,
		Title:           s.meta.Title,
	}
	s.mu.RUnlock()

	if err := s.sink.EmitSessionOpened(ctx, payload); err != nil && s.logger != nil {
		s.logger.Printf("terminal opened emit failed: session=%s err=%v", payload.SessionID, err)
	}
}

func (s *Session) emitError(ctx context.Context, code, message string) {
	if s.sink == nil {
		return
	}

	s.mu.RLock()
	payload := protocol.TerminalSessionErrorPayload{
		SessionID: s.meta.SessionID,
		Code:      code,
		Message:   message,
	}
	s.mu.RUnlock()

	if err := s.sink.EmitSessionError(ctx, payload); err != nil && s.logger != nil {
		s.logger.Printf("terminal error emit failed: session=%s err=%v", payload.SessionID, err)
	}
}

func (s *Session) finishClose(ctx context.Context, reason string, exitCode *int) {
	s.mu.Lock()
	if s.meta.Status == SessionClosed {
		closeDone := s.closeDone
		s.closeDone = nil
		s.mu.Unlock()
		if closeDone != nil {
			close(closeDone)
		}
		return
	}

	s.closeErr = nil
	s.meta.Status = SessionClosed
	s.meta.CloseReason = reason
	s.meta.ExitCode = exitCode
	s.meta.UpdatedAt = time.Now().UTC()
	s.meta.LastActiveAt = s.meta.UpdatedAt
	closeDone := s.closeDone
	s.closeDone = nil
	closedPayload := protocol.TerminalSessionClosedPayload{
		SessionID: s.meta.SessionID,
		ExitCode:  exitCode,
		Reason:    reason,
	}
	s.mu.Unlock()

	s.emitState(ctx)
	if s.sink != nil {
		if err := s.sink.EmitSessionClosed(ctx, closedPayload); err != nil && s.logger != nil {
			s.logger.Printf("terminal closed emit failed: session=%s err=%v", closedPayload.SessionID, err)
		}
	}
	if closeDone != nil {
		close(closeDone)
	}
}

func (s *Session) currentWaitDone() chan struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.waitDone
}

func envMapToList(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+values[key])
	}
	return out
}

func normalizeSignal(name string) (os.Signal, error) {
	switch name {
	case "SIGINT":
		return syscall.SIGINT, nil
	case "SIGTERM":
		return syscall.SIGTERM, nil
	case "SIGKILL":
		return syscall.SIGKILL, nil
	case "SIGHUP":
		return syscall.SIGHUP, nil
	default:
		return nil, ErrUnsupportedSignal
	}
}

func errorCodeForStart(err error) string {
	var terminalErr *Error
	if errors.As(err, &terminalErr) && terminalErr.Code() != "" {
		return terminalErr.Code()
	}
	return "SESSION_START_FAILED"
}

func (s *Session) hasValidProcess() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pty != nil && s.proc.PID > 0 && strings.TrimSpace(s.proc.Ref) != ""
}

func exitCodeFromWaitErr(err error) *int {
	type exitCoder interface {
		ExitCode() int
	}

	if err == nil {
		code := 0
		return &code
	}

	var coder exitCoder
	if errors.As(err, &coder) {
		code := coder.ExitCode()
		return &code
	}

	return nil
}
