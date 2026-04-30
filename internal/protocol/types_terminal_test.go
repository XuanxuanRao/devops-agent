package protocol

import (
	"encoding/json"
	"testing"
)

func TestTerminalSessionOpenPayloadJSONShape(t *testing.T) {
	payload := TerminalSessionOpenPayload{
		RequestID: "req-1",
		SessionID: "ts-1",
		DeviceID:  "agent-1",
		Shell:     "/bin/zsh",
		Cwd:       "/tmp",
		Cols:      120,
		Rows:      32,
		Title:     "zsh",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded["sessionId"] != "ts-1" {
		t.Fatalf("sessionId = %v, want ts-1", decoded["sessionId"])
	}
	if decoded["cols"] != float64(120) {
		t.Fatalf("cols = %v, want 120", decoded["cols"])
	}
}
