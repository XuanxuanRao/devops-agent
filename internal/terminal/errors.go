package terminal

type Error struct {
	code    string
	message string
}

func (e *Error) Error() string {
	if e == nil {
		return "terminal error"
	}
	return e.code + ": " + e.message
}

func (e *Error) Code() string {
	if e == nil {
		return ""
	}
	return e.code
}

var (
	ErrSessionNotFound       = &Error{code: "SESSION_NOT_FOUND", message: "session not found"}
	ErrSessionAlreadyExists  = &Error{code: "SESSION_ALREADY_EXISTS", message: "session already exists"}
	ErrSessionNotOpen        = &Error{code: "SESSION_NOT_OPEN", message: "session is not open"}
	ErrSessionInvalidProcess = &Error{code: "SESSION_INVALID_PROCESS", message: "session start returned invalid pty process"}
	ErrSessionPTYUnavailable = &Error{code: "SESSION_PTY_UNAVAILABLE", message: "session pty is unavailable"}
	ErrUnsupportedSignal     = &Error{code: "UNSUPPORTED_SIGNAL", message: "unsupported signal"}
)
