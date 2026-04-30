package terminal

import (
	"context"
	"devops-agent/internal/protocol"
	"errors"
	"io"
	"log"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestSessionStartTransitionsToOpen(t *testing.T) {
	factory := newFakeFactory()
	session := newSession(OpenPayload{
		RequestID: "req-1",
		SessionID: "ts-1",
		Shell:     "/bin/sh",
		Cwd:       t.TempDir(),
		Cols:      80,
		Rows:      24,
		Title:     "shell",
	}, sessionOptions{
		factory: factory,
		logger:  log.New(io.Discard, "", 0),
	})

	if session.meta.Status != SessionOpening {
		t.Fatalf("initial status = %q, want %q", session.meta.Status, SessionOpening)
	}

	if err := session.start(context.Background()); err != nil {
		t.Fatalf("start() error = %v", err)
	}

	if session.meta.Status != SessionOpen {
		t.Fatalf("status after start = %q, want %q", session.meta.Status, SessionOpen)
	}
}

func TestSessionStartRejectsInvalidFactoryProcess(t *testing.T) {
	factory := newFakeFactory()
	factory.startResult = &PtyProcess{
		File: nil,
		PID:  42,
		Ref:  "pty_42",
	}
	session := newSession(OpenPayload{
		RequestID: "req-1",
		SessionID: "ts-invalid",
		Shell:     "/bin/sh",
		Cwd:       t.TempDir(),
		Cols:      80,
		Rows:      24,
		Title:     "shell",
	}, sessionOptions{
		factory: factory,
		logger:  log.New(io.Discard, "", 0),
	})

	err := session.start(context.Background())
	if !errors.Is(err, ErrSessionInvalidProcess) {
		t.Fatalf("start() error = %v, want %v", err, ErrSessionInvalidProcess)
	}
	if session.meta.Status != SessionError {
		t.Fatalf("status after start = %q, want %q", session.meta.Status, SessionError)
	}
}

func TestSessionOperationsRequireOpenStatus(t *testing.T) {
	factory := newFakeFactory()
	session := newSession(OpenPayload{
		SessionID: "ts-1",
		Cols:      80,
		Rows:      24,
	}, sessionOptions{factory: factory})

	ctx := context.Background()
	if err := session.write(ctx, "pwd\n"); !errors.Is(err, ErrSessionNotOpen) {
		t.Fatalf("write() error = %v, want %v", err, ErrSessionNotOpen)
	}
	if err := session.resize(ctx, 100, 40); !errors.Is(err, ErrSessionNotOpen) {
		t.Fatalf("resize() error = %v, want %v", err, ErrSessionNotOpen)
	}
	if err := session.signal(ctx, "SIGINT"); !errors.Is(err, ErrSessionNotOpen) {
		t.Fatalf("signal() error = %v, want %v", err, ErrSessionNotOpen)
	}

	if err := session.start(ctx); err != nil {
		t.Fatalf("start() error = %v", err)
	}

	if err := session.write(ctx, "pwd\n"); err != nil {
		t.Fatalf("write() after start error = %v", err)
	}
	if err := session.resize(ctx, 100, 40); err != nil {
		t.Fatalf("resize() after start error = %v", err)
	}
	if err := session.signal(ctx, "SIGINT"); err != nil {
		t.Fatalf("signal() after start error = %v", err)
	}
}

func TestSessionWriteFailsWhenPTYUnavailable(t *testing.T) {
	session := &Session{
		meta: SessionMeta{
			SessionID: "ts-1",
			Status:    SessionOpen,
		},
	}

	err := session.write(context.Background(), "pwd\n")
	if !errors.Is(err, ErrSessionPTYUnavailable) {
		t.Fatalf("write() error = %v, want %v", err, ErrSessionPTYUnavailable)
	}
}

func TestSessionCloseIsIdempotent(t *testing.T) {
	closeCalls := 0
	factory := newFakeFactory()
	session := newSession(OpenPayload{
		SessionID: "ts-1",
		Cols:      80,
		Rows:      24,
	}, sessionOptions{factory: factory})
	session.closeFn = func(context.Context, string) error {
		closeCalls++
		return nil
	}

	if err := session.start(context.Background()); err != nil {
		t.Fatalf("start() error = %v", err)
	}

	if err := session.close(context.Background(), "client_closed"); err != nil {
		t.Fatalf("first close() error = %v", err)
	}
	if err := session.close(context.Background(), "client_closed"); err != nil {
		t.Fatalf("second close() error = %v", err)
	}

	if closeCalls != 1 {
		t.Fatalf("close calls = %d, want 1", closeCalls)
	}
	if session.meta.Status != SessionClosed {
		t.Fatalf("status after close = %q, want %q", session.meta.Status, SessionClosed)
	}
	if session.meta.CloseReason != "client_closed" {
		t.Fatalf("close reason = %q, want %q", session.meta.CloseReason, "client_closed")
	}
}

func TestSessionCloseAllowsRetryAfterFailure(t *testing.T) {
	closeErr := errors.New("close failed")
	closeCalls := 0
	factory := newFakeFactory()

	session := newSession(OpenPayload{
		SessionID: "ts-1",
		Cols:      80,
		Rows:      24,
	}, sessionOptions{factory: factory})
	session.closeFn = func(context.Context, string) error {
		closeCalls++
		if closeCalls == 1 {
			return closeErr
		}
		return nil
	}

	if err := session.start(context.Background()); err != nil {
		t.Fatalf("start() error = %v", err)
	}

	if err := session.close(context.Background(), "client_closed"); !errors.Is(err, closeErr) {
		t.Fatalf("first close() error = %v, want %v", err, closeErr)
	}
	if session.meta.Status != SessionError {
		t.Fatalf("status after failed close = %q, want %q", session.meta.Status, SessionError)
	}

	if err := session.close(context.Background(), "client_closed"); err != nil {
		t.Fatalf("second close() error = %v", err)
	}
	if closeCalls != 2 {
		t.Fatalf("close calls = %d, want 2", closeCalls)
	}
	if session.meta.Status != SessionClosed {
		t.Fatalf("status after retry close = %q, want %q", session.meta.Status, SessionClosed)
	}
}

func TestSessionConcurrentCloseCallsOnlyInvokeCloseFnOnce(t *testing.T) {
	var (
		mu         sync.Mutex
		closeCalls int
	)
	firstEntered := make(chan struct{})
	secondEntered := make(chan struct{}, 1)
	release := make(chan struct{})
	factory := newFakeFactory()

	session := newSession(OpenPayload{
		SessionID: "ts-1",
		Cols:      80,
		Rows:      24,
	}, sessionOptions{factory: factory})
	session.closeFn = func(context.Context, string) error {
		mu.Lock()
		closeCalls++
		callNumber := closeCalls
		mu.Unlock()
		if callNumber == 1 {
			close(firstEntered)
		}
		if callNumber == 2 {
			secondEntered <- struct{}{}
		}
		<-release
		return nil
	}

	if err := session.start(context.Background()); err != nil {
		t.Fatalf("start() error = %v", err)
	}

	errCh1 := make(chan error, 1)
	errCh2 := make(chan error, 1)
	go func() { errCh1 <- session.close(context.Background(), "client_closed") }()
	<-firstEntered
	go func() { errCh2 <- session.close(context.Background(), "client_closed") }()

	select {
	case <-secondEntered:
		t.Fatalf("second close entered closeFn, want it to wait for the first close")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)

	if err := <-errCh1; err != nil {
		t.Fatalf("first close() error = %v", err)
	}
	if err := <-errCh2; err != nil {
		t.Fatalf("second close() error = %v", err)
	}

	mu.Lock()
	got := closeCalls
	mu.Unlock()
	if got != 1 {
		t.Fatalf("close calls = %d, want 1", got)
	}
}

func TestSessionStartUsesFactoryAndEmitsOpenState(t *testing.T) {
	sink := &recordingSink{}
	factory := newFakeFactory()
	session := newSession(OpenPayload{
		RequestID: "req-1",
		SessionID: "ts-1",
		Shell:     "/bin/sh",
		Cwd:       t.TempDir(),
		Env: map[string]string{
			"Z": "last",
			"A": "first",
		},
		Cols:  80,
		Rows:  24,
		Title: "shell",
	}, sessionOptions{
		factory: factory,
		sink:    sink,
		logger:  log.New(io.Discard, "", 0),
	})

	if err := session.start(context.Background()); err != nil {
		t.Fatalf("start() error = %v", err)
	}
	t.Cleanup(func() {
		if err := session.close(context.Background(), "test_cleanup"); err != nil {
			t.Fatalf("cleanup close() error = %v", err)
		}
	})

	if factory.startCalls.Load() != 1 {
		t.Fatalf("start calls = %d, want 1", factory.startCalls.Load())
	}
	gotSpec := factory.lastSpec()
	if gotSpec.SessionID != "ts-1" {
		t.Fatalf("StartSpec.SessionID = %q, want %q", gotSpec.SessionID, "ts-1")
	}
	if gotSpec.Shell != "/bin/sh" {
		t.Fatalf("StartSpec.Shell = %q, want %q", gotSpec.Shell, "/bin/sh")
	}
	if gotSpec.Cwd != session.meta.Cwd {
		t.Fatalf("StartSpec.Cwd = %q, want %q", gotSpec.Cwd, session.meta.Cwd)
	}
	if !slices.Equal(gotSpec.Env, []string{"A=first", "Z=last"}) {
		t.Fatalf("StartSpec.Env = %v, want %v", gotSpec.Env, []string{"A=first", "Z=last"})
	}
	if session.meta.Status != SessionOpen {
		t.Fatalf("status after start = %q, want %q", session.meta.Status, SessionOpen)
	}
	waitFor(t, time.Second, func() bool {
		return len(sink.openedSnapshot()) >= 1
	})
	opened := sink.openedSnapshot()
	if opened[0].RequestID != "req-1" {
		t.Fatalf("opened requestId = %q, want %q", opened[0].RequestID, "req-1")
	}
	if opened[0].SessionID != "ts-1" {
		t.Fatalf("opened sessionId = %q, want %q", opened[0].SessionID, "ts-1")
	}
	if opened[0].AgentSessionRef != "pty_42" {
		t.Fatalf("opened agentSessionRef = %q, want %q", opened[0].AgentSessionRef, "pty_42")
	}
	if opened[0].ShellPID != 42 {
		t.Fatalf("opened shellPid = %d, want 42", opened[0].ShellPID)
	}
	waitFor(t, time.Second, func() bool {
		return len(sink.stateSnapshot()) >= 1
	})
	states := sink.stateSnapshot()
	if states[0].Status != string(SessionOpen) {
		t.Fatalf("state status = %q, want %q", states[0].Status, SessionOpen)
	}
}

func TestSessionStartEmitsErrorOnStartFailure(t *testing.T) {
	sink := &recordingSink{}
	startErr := errors.New("boom")
	session := newSession(OpenPayload{
		RequestID: "req-err",
		SessionID: "ts-err",
		Cols:      80,
		Rows:      24,
	}, sessionOptions{
		sink:   sink,
		logger: log.New(io.Discard, "", 0),
	})
	session.startFn = func(context.Context) error {
		return startErr
	}

	err := session.start(context.Background())
	if !errors.Is(err, startErr) {
		t.Fatalf("start() error = %v, want %v", err, startErr)
	}

	waitFor(t, time.Second, func() bool {
		return len(sink.errorSnapshot()) >= 1
	})
	errorEvents := sink.errorSnapshot()
	if errorEvents[0].SessionID != "ts-err" {
		t.Fatalf("error sessionId = %q, want %q", errorEvents[0].SessionID, "ts-err")
	}
	if errorEvents[0].Code == "" {
		t.Fatalf("error code is empty, want non-empty")
	}
	if errorEvents[0].Message == "" {
		t.Fatalf("error message is empty, want non-empty")
	}
	if session.meta.Status != SessionError {
		t.Fatalf("status after failed start = %q, want %q", session.meta.Status, SessionError)
	}
}

func TestSessionReadLoopEmitsIncreasingSeq(t *testing.T) {
	sink := &recordingSink{}
	factory := newFakeFactory()
	factory.pty.queueRead("hello")
	factory.pty.queueRead("world")
	session := newSession(OpenPayload{
		SessionID: "ts-1",
		Shell:     "/bin/sh",
		Cwd:       t.TempDir(),
		Cols:      80,
		Rows:      24,
		Title:     "shell",
	}, sessionOptions{
		factory: factory,
		sink:    sink,
		logger:  log.New(io.Discard, "", 0),
	})

	if err := session.start(context.Background()); err != nil {
		t.Fatalf("start() error = %v", err)
	}
	t.Cleanup(func() {
		if err := session.close(context.Background(), "test_cleanup"); err != nil {
			t.Fatalf("cleanup close() error = %v", err)
		}
	})

	waitFor(t, time.Second, func() bool {
		return len(sink.chunkSnapshot()) >= 2
	})
	chunks := sink.chunkSnapshot()

	if chunks[0].Seq != 1 {
		t.Fatalf("first seq = %d, want 1", chunks[0].Seq)
	}
	if chunks[1].Seq != 2 {
		t.Fatalf("second seq = %d, want 2", chunks[1].Seq)
	}
	if chunks[0].Data != "hello" || chunks[1].Data != "world" {
		t.Fatalf("chunk data = [%q %q], want [hello world]", chunks[0].Data, chunks[1].Data)
	}
}

func TestSessionWriteResizeAndSignalUseUnderlyingPTY(t *testing.T) {
	sink := &recordingSink{}
	factory := newFakeFactory()
	session := newSession(OpenPayload{
		SessionID: "ts-1",
		Shell:     "/bin/sh",
		Cwd:       t.TempDir(),
		Cols:      80,
		Rows:      24,
		Title:     "shell",
	}, sessionOptions{
		factory: factory,
		sink:    sink,
		logger:  log.New(io.Discard, "", 0),
	})

	if err := session.start(context.Background()); err != nil {
		t.Fatalf("start() error = %v", err)
	}
	t.Cleanup(func() {
		if err := session.close(context.Background(), "test_cleanup"); err != nil {
			t.Fatalf("cleanup close() error = %v", err)
		}
	})

	if err := session.write(context.Background(), "pwd\n"); err != nil {
		t.Fatalf("write() error = %v", err)
	}
	if err := session.resize(context.Background(), 100, 40); err != nil {
		t.Fatalf("resize() error = %v", err)
	}
	if err := session.signal(context.Background(), "SIGINT"); err != nil {
		t.Fatalf("signal() error = %v", err)
	}
	if err := session.signal(context.Background(), "SIGUSR1"); !errors.Is(err, ErrUnsupportedSignal) {
		t.Fatalf("signal(SIGUSR1) error = %v, want %v", err, ErrUnsupportedSignal)
	}

	if got := factory.pty.writes(); !slices.Equal(got, []string{"pwd\n"}) {
		t.Fatalf("pty writes = %v, want %v", got, []string{"pwd\n"})
	}
	if factory.resizeCalls.Load() != 1 {
		t.Fatalf("resize calls = %d, want 1", factory.resizeCalls.Load())
	}
	if factory.lastCols.Load() != 100 || factory.lastRows.Load() != 40 {
		t.Fatalf("resize size = %dx%d, want 100x40", factory.lastCols.Load(), factory.lastRows.Load())
	}
	if got := factory.process.signals(); len(got) != 1 || got[0] != syscall.SIGINT {
		t.Fatalf("signals = %v, want [%v]", got, syscall.SIGINT)
	}
	waitFor(t, time.Second, func() bool {
		return len(sink.stateSnapshot()) >= 2
	})
	states := sink.stateSnapshot()
	lastState := states[len(states)-1]
	if lastState.Cols != 100 || lastState.Rows != 40 {
		t.Fatalf("last state size = %dx%d, want 100x40", lastState.Cols, lastState.Rows)
	}
}

func TestSessionCloseConvergesStateAndIsIdempotent(t *testing.T) {
	sink := &recordingSink{}
	factory := newFakeFactory()
	session := newSession(OpenPayload{
		SessionID: "ts-1",
		Shell:     "/bin/sh",
		Cwd:       t.TempDir(),
		Cols:      80,
		Rows:      24,
		Title:     "shell",
	}, sessionOptions{
		factory: factory,
		sink:    sink,
		logger:  log.New(io.Discard, "", 0),
	})

	if err := session.start(context.Background()); err != nil {
		t.Fatalf("start() error = %v", err)
	}
	if err := session.close(context.Background(), "client_closed"); err != nil {
		t.Fatalf("first close() error = %v", err)
	}
	if err := session.close(context.Background(), "client_closed"); err != nil {
		t.Fatalf("second close() error = %v", err)
	}

	if session.meta.Status != SessionClosed {
		t.Fatalf("status after close = %q, want %q", session.meta.Status, SessionClosed)
	}
	if session.meta.CloseReason != "client_closed" {
		t.Fatalf("close reason = %q, want %q", session.meta.CloseReason, "client_closed")
	}
	if factory.pty.closeCalls.Load() != 1 {
		t.Fatalf("pty close calls = %d, want 1", factory.pty.closeCalls.Load())
	}
	waitFor(t, time.Second, func() bool {
		return len(sink.closedSnapshot()) >= 1
	})
	closed := sink.closedSnapshot()
	if closed[0].Reason != "client_closed" {
		t.Fatalf("closed reason = %q, want %q", closed[0].Reason, "client_closed")
	}
}

func TestSessionWaitLoopIncludesExitCodeInClosedEvent(t *testing.T) {
	sink := &recordingSink{}
	factory := newFakeFactory()
	factory.process.waitErr = fakeExitError(7)
	session := newSession(OpenPayload{
		RequestID: "req-exit",
		SessionID: "ts-exit",
		Shell:     "/bin/sh",
		Cwd:       t.TempDir(),
		Cols:      80,
		Rows:      24,
		Title:     "shell",
	}, sessionOptions{
		factory: factory,
		sink:    sink,
		logger:  log.New(io.Discard, "", 0),
	})

	if err := session.start(context.Background()); err != nil {
		t.Fatalf("start() error = %v", err)
	}
	if err := session.close(context.Background(), "client_closed"); err != nil {
		t.Fatalf("close() error = %v", err)
	}

	waitFor(t, time.Second, func() bool {
		return len(sink.closedSnapshot()) >= 1
	})
	closed := sink.closedSnapshot()
	if closed[0].ExitCode == nil {
		t.Fatalf("closed exitCode = nil, want 7")
	}
	if *closed[0].ExitCode != 7 {
		t.Fatalf("closed exitCode = %d, want 7", *closed[0].ExitCode)
	}
}

func waitFor(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

type recordingSink struct {
	mu     sync.Mutex
	opened []protocol.TerminalSessionOpenedPayload
	states []protocol.TerminalSessionStatePayload
	chunks []protocol.TerminalStdoutChunkPayload
	closed []protocol.TerminalSessionClosedPayload
	errors []protocol.TerminalSessionErrorPayload
}

func (s *recordingSink) EmitSessionOpened(_ context.Context, payload protocol.TerminalSessionOpenedPayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opened = append(s.opened, payload)
	return nil
}

func (s *recordingSink) EmitStdoutChunk(_ context.Context, payload protocol.TerminalStdoutChunkPayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chunks = append(s.chunks, payload)
	return nil
}

func (s *recordingSink) EmitSessionState(_ context.Context, payload protocol.TerminalSessionStatePayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states = append(s.states, payload)
	return nil
}

func (s *recordingSink) EmitSessionClosed(_ context.Context, payload protocol.TerminalSessionClosedPayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = append(s.closed, payload)
	return nil
}

func (s *recordingSink) EmitSessionError(_ context.Context, payload protocol.TerminalSessionErrorPayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errors = append(s.errors, payload)
	return nil
}

func (s *recordingSink) openedSnapshot() []protocol.TerminalSessionOpenedPayload {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.opened)
}

func (s *recordingSink) stateSnapshot() []protocol.TerminalSessionStatePayload {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.states)
}

func (s *recordingSink) chunkSnapshot() []protocol.TerminalStdoutChunkPayload {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.chunks)
}

func (s *recordingSink) closedSnapshot() []protocol.TerminalSessionClosedPayload {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.closed)
}

func (s *recordingSink) errorSnapshot() []protocol.TerminalSessionErrorPayload {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.errors)
}

type fakeFactory struct {
	pty         *fakePtyFile
	process     *fakeProcess
	startCalls  atomic.Int32
	resizeCalls atomic.Int32
	lastCols    atomic.Int32
	lastRows    atomic.Int32

	mu       sync.Mutex
	lastSeen StartSpec

	startResult *PtyProcess
	startErr    error
}

func newFakeFactory() *fakeFactory {
	pty := newFakePtyFile()
	proc := newFakeProcess(pty)
	return &fakeFactory{pty: pty, process: proc}
}

func (f *fakeFactory) Start(spec StartSpec) (*PtyProcess, error) {
	f.startCalls.Add(1)
	f.mu.Lock()
	f.lastSeen = spec
	f.mu.Unlock()
	if f.startErr != nil {
		return nil, f.startErr
	}
	if f.startResult != nil {
		return f.startResult, nil
	}
	return &PtyProcess{
		File:   f.pty,
		PID:    42,
		Ref:    "pty_42",
		Wait:   f.process.wait,
		Signal: f.process.signal,
	}, nil
}

func (f *fakeFactory) Resize(file PtyFile, cols, rows int) error {
	f.resizeCalls.Add(1)
	f.lastCols.Store(int32(cols))
	f.lastRows.Store(int32(rows))
	return nil
}

func (f *fakeFactory) lastSpec() StartSpec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastSeen
}

type fakePtyFile struct {
	readCh     chan string
	closedCh   chan struct{}
	closeOnce  sync.Once
	closeCalls atomic.Int32

	mu      sync.Mutex
	written []string
	closed  bool
	readEOF bool
}

func newFakePtyFile() *fakePtyFile {
	return &fakePtyFile{
		readCh:   make(chan string, 16),
		closedCh: make(chan struct{}),
	}
}

func (f *fakePtyFile) queueRead(data string) {
	f.readCh <- data
}

func (f *fakePtyFile) Read(p []byte) (int, error) {
	select {
	case data := <-f.readCh:
		if data == "" {
			f.mu.Lock()
			alreadyEOF := f.readEOF
			f.readEOF = true
			f.mu.Unlock()
			if alreadyEOF {
				return 0, io.EOF
			}
			return 0, io.EOF
		}
		n := copy(p, []byte(data))
		return n, nil
	case <-f.closedCh:
		return 0, io.EOF
	case <-time.After(20 * time.Millisecond):
		return 0, nil
	}
}

func (f *fakePtyFile) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, os.ErrClosed
	}
	f.written = append(f.written, string(p))
	return len(p), nil
}

func (f *fakePtyFile) Close() error {
	f.closeOnce.Do(func() {
		f.closeCalls.Add(1)
		f.mu.Lock()
		f.closed = true
		f.mu.Unlock()
		close(f.closedCh)
	})
	return nil
}

func (f *fakePtyFile) writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.written)
}

type fakeProcess struct {
	pty *fakePtyFile

	mu        sync.Mutex
	signalLog []os.Signal
	waitErr   error
}

func newFakeProcess(pty *fakePtyFile) *fakeProcess {
	return &fakeProcess{pty: pty}
}

func (p *fakeProcess) wait() error {
	<-p.pty.closedCh
	return p.waitErr
}

func (p *fakeProcess) signal(sig os.Signal) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.signalLog = append(p.signalLog, sig)
	return nil
}

func (p *fakeProcess) signals() []os.Signal {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.signalLog)
}

type fakeExitError int

func (e fakeExitError) Error() string {
	return "exit status"
}

func (e fakeExitError) ExitCode() int {
	return int(e)
}
