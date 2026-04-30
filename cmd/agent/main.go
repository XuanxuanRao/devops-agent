package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	agentconfig "devops-agent/internal/config"
	agentcrypto "devops-agent/internal/crypto"
	"devops-agent/internal/ws"
)

const defaultConfigPath = "config.yaml"

const (
	reconnectInitialBackoff = 1 * time.Second
	reconnectMaxBackoff     = 60 * time.Second
	reconnectJitterFraction = 0.2
)

type serviceClient interface {
	ConnectAndServe(ctx context.Context) error
	CloseTerminalSessions(ctx context.Context) error
}

var defaultNewServiceClient = func(cfg *agentconfig.Config, keyPair agentcrypto.KeyPair, logger *log.Logger, onDeviceToken func(string)) serviceClient {
	return ws.NewClient(cfg, keyPair, logger, onDeviceToken)
}

var newServiceClient = defaultNewServiceClient

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		log.New(os.Stderr, "", log.LstdFlags).Println(err)
		os.Exit(1)
	}
}

func run(parent context.Context, args []string, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("agent", flag.ContinueOnError)
	flagSet.SetOutput(stderr)

	configPath := flagSet.String("config", defaultConfigPath, "path to config file")
	if err := flagSet.Parse(args); err != nil {
		return err
	}

	logger := log.New(stdout, "", log.LstdFlags)

	cfg, err := agentconfig.Load(*configPath)
	if err != nil {
		return err
	}

	keyPair, err := agentcrypto.EnsureKeyPair(cfg.Keys.Dir)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	return serveWithReconnect(ctx, &cfg, keyPair, logger)
}

func serveWithReconnect(ctx context.Context, cfg *agentconfig.Config, keyPair agentcrypto.KeyPair, logger *log.Logger) error {
	backoff := reconnectInitialBackoff

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		client := newServiceClient(cfg, keyPair, logger, newDeviceTokenHandler(cfg.Auth.DeviceTokenPath, cfg, logger))
		err := client.ConnectAndServe(ctx)
		if closeErr := client.CloseTerminalSessions(context.Background()); closeErr != nil {
			logger.Printf("[agent] close terminal sessions error: %v", closeErr)
		}

		if err == nil || errors.Is(err, context.Canceled) {
			return nil
		}

		if errors.Is(err, ws.ErrAuthRejected) && cfg.Auth.DeviceToken != "" {
			logger.Printf("[agent] device token rejected by server, falling back to static auth token: %v", err)
			if clearErr := agentconfig.ClearDeviceToken(cfg.Auth.DeviceTokenPath); clearErr != nil {
				logger.Printf("[agent] clear device token error: %v", clearErr)
			}
			cfg.Auth.DeviceToken = ""
			backoff = reconnectInitialBackoff
			continue
		}

		logger.Printf("[agent] connection closed: %v; reconnecting in %s", err, backoff)

		if sleepErr := sleepWithContext(ctx, withJitter(backoff)); sleepErr != nil {
			return sleepErr
		}

		backoff *= 2
		if backoff > reconnectMaxBackoff {
			backoff = reconnectMaxBackoff
		}
	}
}

func withJitter(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	delta := float64(base) * reconnectJitterFraction
	offset := (rand.Float64()*2 - 1) * delta
	return base + time.Duration(offset)
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func newDeviceTokenHandler(path string, cfg *agentconfig.Config, logger *log.Logger) func(string) {
	return func(token string) {
		if cfg != nil {
			cfg.UpdateDeviceToken(token)
		}
		if err := agentconfig.SaveDeviceToken(path, token); err != nil && logger != nil {
			logger.Printf("[config] save device token error: %v", err)
		}
	}
}
