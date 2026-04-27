package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"testing"

	agentconfig "devops-agent/internal/config"
)

func TestDeviceTokenHandlerPersistsToken(t *testing.T) {
	tempDir := t.TempDir()
	tokenPath := filepath.Join(tempDir, "device.token")
	logger := log.New(&bytes.Buffer{}, "", 0)
	cfg := &agentconfig.Config{}

	handler := newDeviceTokenHandler(tokenPath, cfg, logger)
	handler("token-from-server")

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(data); got != "token-from-server\n" {
		t.Fatalf("token file = %q, want %q", got, "token-from-server\\n")
	}
	if cfg.Auth.DeviceToken != "token-from-server" {
		t.Fatalf("cfg.Auth.DeviceToken = %q, want %q", cfg.Auth.DeviceToken, "token-from-server")
	}
}
