package terminal

import (
	"context"
	"errors"
	"io"
	"log"
	"sync"

	"devops-agent/internal/protocol"
)

type EventSink interface {
	EmitSessionOpened(ctx context.Context, payload protocol.TerminalSessionOpenedPayload) error
	EmitStdoutChunk(ctx context.Context, payload protocol.TerminalStdoutChunkPayload) error
	EmitSessionState(ctx context.Context, payload protocol.TerminalSessionStatePayload) error
	EmitSessionClosed(ctx context.Context, payload protocol.TerminalSessionClosedPayload) error
	EmitSessionError(ctx context.Context, payload protocol.TerminalSessionErrorPayload) error
}

type Options struct {
	DefaultShell   string
	DefaultWorkDir string
	Factory        PtyFactory
	Sink           EventSink
	Logger         *log.Logger
}

type OpenPayload struct {
	RequestID string
	SessionID string
	DeviceID  string
	Shell     string
	Cwd       string
	Env       map[string]string
	Cols      int
	Rows      int
	Title     string
}

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session

	defaultShell   string
	defaultWorkDir string
	factory        PtyFactory
	sink           EventSink
	logger         *log.Logger

	newSession func(payload OpenPayload, opts sessionOptions) *Session
}

func NewManager(opts Options) *Manager {
	logger := opts.Logger
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}

	m := &Manager{
		sessions:       make(map[string]*Session),
		defaultShell:   opts.DefaultShell,
		defaultWorkDir: opts.DefaultWorkDir,
		factory:        opts.Factory,
		sink:           opts.Sink,
		logger:         logger,
	}
	m.newSession = func(payload OpenPayload, sopts sessionOptions) *Session {
		return newSession(payload, sopts)
	}
	return m
}

func (m *Manager) Get(sessionID string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[sessionID]
	return session, ok
}

func (m *Manager) OpenSession(ctx context.Context, payload OpenPayload) error {
	m.mu.Lock()
	if _, exists := m.sessions[payload.SessionID]; exists {
		m.mu.Unlock()
		return ErrSessionAlreadyExists
	}

	session := m.newSession(payload, sessionOptions{
		defaultShell:   m.defaultShell,
		defaultWorkDir: m.defaultWorkDir,
		factory:        m.factory,
		sink:           m.sink,
		logger:         m.logger,
	})
	m.sessions[payload.SessionID] = session
	m.mu.Unlock()

	if err := session.start(ctx); err != nil {
		m.mu.Lock()
		delete(m.sessions, payload.SessionID)
		m.mu.Unlock()
		return err
	}

	return nil
}

func (m *Manager) Write(ctx context.Context, sessionID, data string) error {
	session, err := m.requireSession(sessionID)
	if err != nil {
		return err
	}
	return session.write(ctx, data)
}

func (m *Manager) Resize(ctx context.Context, sessionID string, cols, rows int) error {
	session, err := m.requireSession(sessionID)
	if err != nil {
		return err
	}
	return session.resize(ctx, cols, rows)
}

func (m *Manager) Signal(ctx context.Context, sessionID, signal string) error {
	session, err := m.requireSession(sessionID)
	if err != nil {
		return err
	}
	return session.signal(ctx, signal)
}

func (m *Manager) Close(ctx context.Context, sessionID, reason string) error {
	session, err := m.requireSession(sessionID)
	if err != nil {
		return err
	}
	if err := session.close(ctx, reason); err != nil {
		return err
	}

	m.mu.Lock()
	delete(m.sessions, sessionID)
	m.mu.Unlock()
	return nil
}

func (m *Manager) CloseAll(ctx context.Context, reason string) error {
	type namedSession struct {
		id      string
		session *Session
	}

	m.mu.RLock()
	sessions := make([]namedSession, 0, len(m.sessions))
	for id, session := range m.sessions {
		sessions = append(sessions, namedSession{id: id, session: session})
	}
	m.mu.RUnlock()

	var firstErr error
	closedIDs := make([]string, 0, len(sessions))
	for _, item := range sessions {
		if err := item.session.close(ctx, reason); err != nil {
			if firstErr == nil {
				firstErr = err
			} else {
				firstErr = errors.Join(firstErr, err)
			}
			continue
		}
		closedIDs = append(closedIDs, item.id)
	}

	m.mu.Lock()
	for _, id := range closedIDs {
		delete(m.sessions, id)
	}
	m.mu.Unlock()

	return firstErr
}

func (m *Manager) requireSession(sessionID string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return session, nil
}
