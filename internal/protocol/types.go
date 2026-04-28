package protocol

// 本包定义 Agent 与服务器之间通过 WebSocket 传输的最小协议骨架

const (
	FrameTypeEvent    = "event"
	FrameTypeRequest  = "req"
	FrameTypeResponse = "res"

	EventConnectChallenge = "connect.challenge"
	EventAgentTick        = "agent.tick"
	EventCommandPush      = "command.push"
	EventResultChunk      = "result.chunk"
	EventResultAck        = "result.ack"

	MethodConnect = "connect"
)

// EventFrame 表示服务端或客户端发送的事件帧，例如 connect.challenge、agent.tick、command.push。
//
// 对于结果回传相关的事件：
//   - result.chunk: Agent → Server，流式回传执行结果分片；
//   - result.ack:   Server → Agent，确认已持久化特定分片。
//
// type 字段固定为 "event"。
type EventFrame struct {
	Type    string      `json:"type"`
	Event   string      `json:"event"`
	Payload interface{} `json:"payload"`
}

// RequestFrame 表示请求帧，例如 connect、后续可扩展的 RPC 方法。
//
// 带有 id 与可选 idempotencyKey，便于在“至少一次”投递语义下实现幂等。
// TODO: 后续可以为 Params 使用 json.RawMessage 以减少拷贝。
type RequestFrame struct {
	Type           string      `json:"type"`
	ID             string      `json:"id"`
	Method         string      `json:"method"`
	Params         interface{} `json:"params"`
	IdempotencyKey string      `json:"idempotencyKey,omitempty"`
}

// ResponseFrame 表示响应帧，对应某个请求 ID。
type ResponseFrame struct {
	Type    string      `json:"type"`
	ID      string      `json:"id"`
	OK      bool        `json:"ok"`
	Payload interface{} `json:"payload,omitempty"`
	Error   *ErrorBody  `json:"error,omitempty"`
}

// ErrorBody 是最小错误结构，MVP 中仅保留 code/message。
// TODO: 后续可扩展 details 字段以传递恢复建议（类似 AUTH_TOKEN_MISMATCH 的恢复 hint）。
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ChallengePayload 对应 connect.challenge 事件的负载。
type ChallengePayload struct {
	Nonce string `json:"nonce"`
	TS    int64  `json:"ts"`
}

// ClientInfo 描述 Agent 客户端信息，对齐 openclaw 的 client 字段。
type ClientInfo struct {
	ID       string `json:"id"`
	Version  string `json:"version"`
	Platform string `json:"platform"`
	Mode     string `json:"mode"`
}

// DeviceInfo 描述设备身份和签名信息，对齐 openclaw 的 device 字段。
type DeviceInfo struct {
	ID        string `json:"id"`
	PublicKey string `json:"publicKey"`
	Signature string `json:"signature"`
	SignedAt  int64  `json:"signedAt"`
	Nonce     string `json:"nonce"`
}

// AuthInfo 包装静态网关 Token 或后续扩展的鉴权信息。
type AuthInfo struct {
	Token string `json:"token"`
}

// ConnectParams 是 connect 请求的参数结构。
type ConnectParams struct {
	MinProtocol int        `json:"minProtocol"`
	MaxProtocol int        `json:"maxProtocol"`
	Client      ClientInfo `json:"client"`
	Role        string     `json:"role"`
	Scopes      []string   `json:"scopes"`
	Device      DeviceInfo `json:"device"`
	Auth        AuthInfo   `json:"auth"`
}

// HelloPolicy 在 hello-ok 中返回的策略信息。
type HelloPolicy struct {
	TickIntervalMs int `json:"tickIntervalMs"`
}

// HelloAuth 描述服务端下发的设备令牌等信息。
type HelloAuth struct {
	DeviceToken string   `json:"deviceToken"`
	Role        string   `json:"role"`
	Scopes      []string `json:"scopes"`
}

// HelloOkPayload 是 hello-ok 响应负载。
type HelloOkPayload struct {
	Type     string      `json:"type"`
	Protocol int         `json:"protocol"`
	Policy   HelloPolicy `json:"policy"`
	Auth     *HelloAuth  `json:"auth,omitempty"`
}

// HeartbeatPayload 对应 agent.tick 心跳事件负载。
type HeartbeatPayload struct {
	DeviceID string         `json:"deviceId"`
	TS       int64          `json:"ts"`
	Metrics  *MetricsSnapshot `json:"metrics,omitempty"`
}

// MetricsSnapshot 描述 Agent 所在节点的资源快照，嵌入在心跳中上报。
type MetricsSnapshot struct {
	CPUPercent   float64 `json:"cpuPercent"`
	MemPercent   float64 `json:"memPercent"`
	MemUsed      uint64  `json:"memUsed"`
	MemTotal     uint64  `json:"memTotal"`
	Load1        float64 `json:"load1"`
	NumGoroutine int     `json:"numGoroutine"`
}

// CommandPushPayload 对应 command.push.
type CommandPushPayload struct {
	TaskUUID       string `json:"task_uuid"`
	Command        string `json:"command"`
	CorrelationID  string `json:"correlationId"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	// WorkDir 为本次命令执行的工作目录；为空时使用 Agent 侧默认（shell.workDir 或进程当前目录）。
	WorkDir string `json:"workDir,omitempty"`
}

// ResultChunkPayload 对应 result.chunk 事件的负载。
//
// Agent 侧会将执行结果切分为多个分片按顺序回传：
//   - Seq: 从 1 开始递增的分片序号；
//   - StdoutChunk / StderrChunk: 本分片携带的 stdout/stderr 内容（二选一或都为空）；
//   - Final: 是否为最后一个分片；
//   - ExitCode: 仅在 Final=true 的最后一个分片中填写退出码，其余分片为空。
type ResultChunkPayload struct {
	TaskUUID      string `json:"task_uuid"`
	CorrelationID string `json:"correlationId"`
	AgentID       string `json:"agentId"`
	Seq           int    `json:"seq"`
	StdoutChunk   string `json:"stdoutChunk,omitempty"`
	StderrChunk   string `json:"stderrChunk,omitempty"`
	ExitCode      *int   `json:"exitCode,omitempty"`
	Final         bool   `json:"isFinal"`
}

// ResultAckPayload 对应 result.ack 事件的负载。
//
// 由 Server 在成功持久化某个 result.chunk 分片后回发给 Agent，用于未来实现重传/窗口控制。
type ResultAckPayload struct {
	TaskUUID   string `json:"task_uuid"`
	AgentID    string `json:"agentId"`
	Seq        int    `json:"seq"`
	ReceivedAt int64  `json:"receivedAt"`
}
