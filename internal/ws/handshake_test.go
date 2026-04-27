package ws

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"

	agentcrypto "devops-agent/internal/crypto"
)

func TestBuildConnectRequestMarshalsScopesAsArray(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	req, err := BuildConnectRequest(
		agentcrypto.KeyPair{Public: publicKey, Private: privateKey},
		"auth-token",
		"device-id",
		"nonce",
		3,
		3,
	)
	if err != nil {
		t.Fatalf("BuildConnectRequest() error = %v", err)
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded struct {
		Params struct {
			Scopes []string `json:"scopes"`
		} `json:"params"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.Params.Scopes == nil {
		t.Fatalf("scopes marshaled as null, want empty array")
	}
	if len(decoded.Params.Scopes) != 0 {
		t.Fatalf("scopes length = %d, want 0", len(decoded.Params.Scopes))
	}
}
