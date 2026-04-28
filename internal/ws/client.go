package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"

	agentconfig "devops-agent/internal/config"
	agentcrypto "devops-agent/internal/crypto"
	agentexec "devops-agent/internal/exec"
	"devops-agent/internal/heartbeat"
	"devops-agent/internal/metrics"
	"devops-agent/internal/protocol"
)

var ErrAuthRejected = errors.New("connect rejected by server: auth")

type Client struct {
	cfg           *agentconfig.Config
	keyPair       agentcrypto.KeyPair
	logger        *log.Logger
	conn          *websocket.Conn
	onDeviceToken func(string)

	executor agentexec.Executor

	writeMu sync.Mutex
}

func NewClient(cfg *agentconfig.Config, kp agentcrypto.KeyPair, logger *log.Logger, onDeviceToken func(string)) *Client {
	return &Client{
		cfg:           cfg,
		keyPair:       kp,
		logger:        logger,
		onDeviceToken: onDeviceToken,
		executor:      agentexec.ShellExecutor{Enabled: cfg.Shell.Enabled, DefaultWorkDir: cfg.Shell.WorkDir},
	}
}

func (c *Client) ConnectAndServe(ctx context.Context) error {
	if c.cfg == nil {
		return fmt.Errorf("nil config")
	}

	authToken := c.selectAuthToken()
	if authToken == "" {
		c.logger.Println("[ws] warning: no auth token configured; server will likely reject connect")
	}

	c.logger.Printf("[ws] dialing %s", c.cfg.Server.URL)
	conn, _, err := websocket.Dial(ctx, c.cfg.Server.URL, nil)
	if err != nil {
		return fmt.Errorf("dial websocket: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "agent shutdown")

	c.conn = conn

	_, msg, err := conn.Read(ctx)
	if err != nil {
		return fmt.Errorf("read challenge: %w", err)
	}

	var challengeEnvelope struct {
		Type    string                    `json:"type"`
		Event   string                    `json:"event"`
		Payload protocol.ChallengePayload `json:"payload"`
	}
	if err := json.Unmarshal(msg, &challengeEnvelope); err != nil {
		return fmt.Errorf("decode challenge frame: %w", err)
	}
	if challengeEnvelope.Type != protocol.FrameTypeEvent || challengeEnvelope.Event != protocol.EventConnectChallenge {
		return fmt.Errorf("unexpected first frame: type=%s event=%s", challengeEnvelope.Type, challengeEnvelope.Event)
	}
	challenge := challengeEnvelope.Payload

	deviceID := agentcrypto.DeviceID(c.keyPair.Public)

	reqFrame, err := BuildConnectRequest(c.keyPair, authToken, deviceID, challenge.Nonce, 3, 3)
	if err != nil {
		return fmt.Errorf("build connect request: %w", err)
	}

	reqBytes, err := json.Marshal(reqFrame)
	if err != nil {
		return fmt.Errorf("marshal connect request: %w", err)
	}

	c.writeMu.Lock()
	err = conn.Write(ctx, websocket.MessageText, reqBytes)
	c.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("send connect request: %w", err)
	}

	_, msg, err = conn.Read(ctx)
	if err != nil {
		return fmt.Errorf("read hello-ok: %w", err)
	}

	var resEnvelope struct {
		Type    string                  `json:"type"`
		ID      string                  `json:"id"`
		OK      bool                    `json:"ok"`
		Payload protocol.HelloOkPayload `json:"payload"`
		Error   *protocol.ErrorBody     `json:"error,omitempty"`
	}
	if err := json.Unmarshal(msg, &resEnvelope); err != nil {
		return fmt.Errorf("decode hello-ok frame: %w", err)
	}
	if resEnvelope.Type != protocol.FrameTypeResponse || !resEnvelope.OK {
		if resEnvelope.Error != nil {
			if isAuthErrorCode(resEnvelope.Error.Code) {
				return fmt.Errorf("%w: %s: %s", ErrAuthRejected, resEnvelope.Error.Code, resEnvelope.Error.Message)
			}
			return fmt.Errorf("connect failed: %s: %s", resEnvelope.Error.Code, resEnvelope.Error.Message)
		}
		return fmt.Errorf("connect failed: unexpected frame type=%s ok=%v", resEnvelope.Type, resEnvelope.OK)
	}

	hello := resEnvelope.Payload
	c.logger.Printf("[ws] connected: protocol=%d tickIntervalMs=%d", hello.Protocol, hello.Policy.TickIntervalMs)

	if hello.Auth != nil && hello.Auth.DeviceToken != "" {
		c.logger.Printf("[ws] received deviceToken from server")
		if c.onDeviceToken != nil {
			c.onDeviceToken(hello.Auth.DeviceToken)
		}
		c.cfg.Auth.DeviceToken = hello.Auth.DeviceToken
	}

	tickMs := hello.Policy.TickIntervalMs
	if tickMs <= 0 {
		tickMs = c.cfg.TickInterval()
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go heartbeat.Start(ctx, tickMs, c)

	go func() {
		if err := c.readLoop(ctx); err != nil {
			c.logger.Printf("[ws] read loop error: %v", err)
		}
		cancel()
	}()

	<-ctx.Done()
	return ctx.Err()
}

func (c *Client) SendHeartbeat(ctx context.Context, snap metrics.Snapshot) error {
	if c.conn == nil {
		return fmt.Errorf("no active websocket connection")
	}

	payload := protocol.HeartbeatPayload{
		DeviceID: agentcrypto.DeviceID(c.keyPair.Public),
		TS:       time.Now().UnixMilli(),
		Metrics: &protocol.MetricsSnapshot{
			CPUPercent:   snap.CPUPercent,
			MemPercent:   snap.MemPercent,
			MemUsed:      snap.MemUsed,
			MemTotal:     snap.MemTotal,
			Load1:        snap.Load1,
			NumGoroutine: snap.NumGoroutine,
		},
	}

	frame := protocol.EventFrame{
		Type:    protocol.FrameTypeEvent,
		Event:   protocol.EventAgentTick,
		Payload: payload,
	}

	data, err := json.Marshal(frame)
	if err != nil {
		return fmt.Errorf("marshal heartbeat: %w", err)
	}

	c.writeMu.Lock()
	err = c.conn.Write(ctx, websocket.MessageText, data)
	c.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("send heartbeat: %w", err)
	}

	c.logger.Printf("[ws] sent agent.tick %s", snap.String())
	return nil
}

func (c *Client) HandleCommand(ctx context.Context, payload protocol.CommandPushPayload) error {
	c.logger.Printf("[ws] received command.push: task=%s cmd=%s", payload.TaskUUID, payload.Command)

	if c.executor == nil {
		c.logger.Printf("[ws] executor not configured, skip execution")
		return nil
	}

	timeout := time.Duration(payload.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	agentID := agentcrypto.DeviceID(c.keyPair.Public)

	chunks := c.executor.RunStream(ctx, payload.Command, payload.WorkDir, timeout)
	for chunk := range chunks {
		rc := protocol.ResultChunkPayload{
			TaskUUID:      payload.TaskUUID,
			CorrelationID: payload.CorrelationID,
			AgentID:       agentID,
			Seq:           chunk.Seq,
			StdoutChunk:   chunk.StdoutChunk,
			StderrChunk:   chunk.StderrChunk,
			Final:         chunk.Final,
		}
		if chunk.ExitCode != nil {
			rc.ExitCode = chunk.ExitCode
		}

		if err := c.sendResultChunk(ctx, rc); err != nil {
			c.logger.Printf("[ws] send result.chunk failed: %v", err)
			break
		}
	}

	return nil
}

func (c *Client) readLoop(ctx context.Context) error {
	for {
		_, msg, err := c.conn.Read(ctx)
		if err != nil {
			return err
		}

		var base struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(msg, &base); err != nil {
			c.logger.Printf("[ws] invalid frame: %v", err)
			continue
		}

		switch base.Type {
		case protocol.FrameTypeEvent:
			var ev struct {
				Type    string          `json:"type"`
				Event   string          `json:"event"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal(msg, &ev); err != nil {
				c.logger.Printf("[ws] invalid event frame: %v", err)
				continue
			}

			switch ev.Event {
			case protocol.EventCommandPush:
				var payload protocol.CommandPushPayload
				if err := json.Unmarshal(ev.Payload, &payload); err != nil {
					c.logger.Printf("[ws] invalid command.push payload: %v", err)
					continue
				}
				// 命令执行放到独立 goroutine，避免阻塞 readLoop。
				go func(p protocol.CommandPushPayload) {
					if err := c.HandleCommand(ctx, p); err != nil {
						c.logger.Printf("[ws] handle command error: %v", err)
					}
				}(payload)
			case protocol.EventResultAck:
				var ack protocol.ResultAckPayload
				if err := json.Unmarshal(ev.Payload, &ack); err != nil {
					c.logger.Printf("[ws] invalid result.ack payload: %v", err)
					continue
				}
				c.logger.Printf("[ws] received result.ack: task=%s seq=%d", ack.TaskUUID, ack.Seq)
			case "disconnect":
				c.logger.Printf("[ws] received disconnect event, closing")
				return nil
			default:
				c.logger.Printf("[ws] ignore event=%s", ev.Event)
			}
		default:
			c.logger.Printf("[ws] ignore frame type=%s", base.Type)
		}
	}
}

func (c *Client) sendResultChunk(ctx context.Context, payload protocol.ResultChunkPayload) error {
	if c.conn == nil {
		return fmt.Errorf("no active websocket connection")
	}

	frame := protocol.EventFrame{
		Type:    protocol.FrameTypeEvent,
		Event:   protocol.EventResultChunk,
		Payload: payload,
	}

	data, err := json.Marshal(frame)
	if err != nil {
		return fmt.Errorf("marshal result.chunk: %w", err)
	}

	c.writeMu.Lock()
	err = c.conn.Write(ctx, websocket.MessageText, data)
	c.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("send result.chunk: %w", err)
	}

	c.logger.Printf("[ws] sent result.chunk: task=%s seq=%d final=%v", payload.TaskUUID, payload.Seq, payload.Final)
	return nil
}

func (c *Client) selectAuthToken() string {
	if c.cfg.Auth.DeviceToken != "" {
		return c.cfg.Auth.DeviceToken
	}
	return c.cfg.Auth.Token
}

func isAuthErrorCode(code string) bool {
	up := strings.ToUpper(strings.TrimSpace(code))
	if up == "" {
		return false
	}
	for _, key := range []string{"AUTH", "TOKEN", "UNAUTHORIZED", "FORBIDDEN", "SIGNATURE"} {
		if strings.Contains(up, key) {
			return true
		}
	}
	return false
}
