package terminal

import "time"

type SessionStatus string

const (
	SessionOpening SessionStatus = "opening"
	SessionOpen    SessionStatus = "open"
	SessionClosing SessionStatus = "closing"
	SessionClosed  SessionStatus = "closed"
	SessionError   SessionStatus = "error"
)

type SessionMeta struct {
	SessionID    string
	Shell        string
	Cwd          string
	Title        string
	Cols         int
	Rows         int
	Status       SessionStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastActiveAt time.Time
	ExitCode     *int
	CloseReason  string
}
