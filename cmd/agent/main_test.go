package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"testing"

	agentconfig "devops-agent/internal/config"
	agentcrypto "devops-agent/internal/crypto"
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

func TestServeWithReconnectClosesTerminalSessionsOnShutdown(t *testing.T) {
	t.Cleanup(resetClientFactory)

	stub := &stubServiceClient{connectErr: context.Canceled}
	newServiceClient = func(*agentconfig.Config, agentcrypto.KeyPair, *log.Logger, func(string)) serviceClient {
		return stub
	}

	cfg := &agentconfig.Config{}
	err := serveWithReconnect(context.Background(), cfg, agentcrypto.KeyPair{}, log.New(&bytes.Buffer{}, "", 0))
	if err != nil {
		t.Fatalf("serveWithReconnect() error = %v, want nil", err)
	}
	if stub.connectCalls != 1 {
		t.Fatalf("connectCalls = %d, want 1", stub.connectCalls)
	}
	if stub.closeCalls != 1 {
		t.Fatalf("closeCalls = %d, want 1", stub.closeCalls)
	}
	if stub.closeReason != "agent_closed" {
		t.Fatalf("close reason = %q, want %q", stub.closeReason, "agent_closed")
	}
}

func TestServeWithReconnectStillClosesTerminalSessionsBeforeReconnect(t *testing.T) {
	t.Cleanup(resetClientFactory)

	stub := &stubServiceClient{connectErr: errors.New("boom")}
	ctx, cancel := context.WithCancel(context.Background())
	newServiceClient = func(*agentconfig.Config, agentcrypto.KeyPair, *log.Logger, func(string)) serviceClient {
		cancel()
		return stub
	}

	err := serveWithReconnect(ctx, &agentconfig.Config{}, agentcrypto.KeyPair{}, log.New(&bytes.Buffer{}, "", 0))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("serveWithReconnect() error = %v, want %v", err, context.Canceled)
	}
	if stub.closeCalls != 1 {
		t.Fatalf("closeCalls = %d, want 1", stub.closeCalls)
	}
}

type stubServiceClient struct {
	connectErr  error
	closeErr    error
	connectCalls int
	closeCalls   int
	closeReason  string
}

func (s *stubServiceClient) ConnectAndServe(context.Context) error {
	s.connectCalls++
	return s.connectErr
}

func (s *stubServiceClient) CloseTerminalSessions(context.Context) error {
	s.closeCalls++
	s.closeReason = "agent_closed"
	return s.closeErr
}

func resetClientFactory() {
	newServiceClient = defaultNewServiceClient
}
