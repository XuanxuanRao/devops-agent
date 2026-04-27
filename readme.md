# DevOps MVP 脚手架

这是一个集中式服务器管理与命令下发系统的最小可行产品（MVP）脚手架，包含一个 Go 语言编写的 Agent。

本项目旨在提供一个可立即运行的骨架，演示基于 WebSocket 的设备认证、心跳维持和命令流转核心机制，协议设计参考了 openclaw Gateway 的 WebSocket 模型：

- 文本帧 + JSON 序列化；
- `event` / `req` / `res` 三类帧；
- 预连接挑战 `connect.challenge` + `connect` 请求 + `hello-ok` 响应；
- 每设备 Ed25519 签名 + 静态网关 Token / deviceToken 双层认证；
- `policy.tickIntervalMs` 控制心跳节奏。

---

## 快速开始

### 1. 环境准备

- 安装 [Go](https://golang.org/dl/) (版本 >= 1.21)
- 安装 [Python](https://www.python.org/downloads/) (版本 >= 3.10)

### 2. 启动服务端

```bash
# 进入 server-fastapi 目录
cd server-fastapi

# 安装依赖（包含 FastAPI + Uvicorn + Pydantic + python-dotenv + PyNaCl）
pip install -r requirements.txt

# 启动 FastAPI 开发服务器
uvicorn app.main:app --reload --port 8000
```

服务启动后，你可以在 `http://127.0.0.1:8000/docs` 查看 API 文档。

默认配置从 `.env` 或环境变量读取：

```env
PORT=8000
STATIC_GATEWAY_TOKEN=change_me
TICK_INTERVAL_MS=15000
DEVICE_TOKEN_TTL_MINUTES=1440
MAX_SKEW_SECONDS=300
```

- `STATIC_GATEWAY_TOKEN`：首次设备接入时使用的静态网关 Token；
- `DEVICE_TOKEN_TTL_MINUTES`：deviceToken 在内存中的有效期；
- `MAX_SKEW_SECONDS`：允许的签名时间偏移窗口（秒），用于抗重放和时钟漂移。

### 3. 配置并启动 Agent

打开一个新的终端窗口：

```bash
# 进入 agent-go 目录
cd agent-go

# 编译 Agent (将在当前目录生成 agent 可执行文件)
go build ./cmd/agent

# 运行 Agent（默认读取同目录下的 config.yaml）
./agent
```

示例 `config.yaml`：

```yaml
serverUrl: "ws://localhost:8000/ws"
keyDir: "./keys"
logLevel: "debug"
tickIntervalMs: 15000
enableShell: false
authToken: "change_me"          # 首次连接使用的静态网关 Token，可被 AGENT_AUTH_TOKEN 覆盖
deviceTokenPath: "./device.token" # hello-ok 下发的 deviceToken 将持久化在此路径，可被 AGENT_DEVICE_TOKEN_PATH 覆盖
```

支持的环境变量（优先级高于 YAML）：

- `AGENT_SERVER_URL`：覆盖 `serverUrl`；
- `AGENT_KEY_DIR`：覆盖 `keyDir`；
- `AGENT_LOG_LEVEL`：覆盖 `logLevel`；
- `AGENT_TICK_INTERVAL_MS`：覆盖 `tickIntervalMs`；
- `AGENT_ENABLE_SHELL`：`true/1/yes` 时启用本地 shell 执行；
- `AGENT_AUTH_TOKEN`：覆盖 `authToken`；
- `AGENT_DEVICE_TOKEN_PATH`：覆盖 `deviceTokenPath`。

Agent 启动后会：

1. 在 `keyDir` 目录自动生成 Ed25519 公私钥对；
2. 基于公钥指纹计算稳定的 `deviceId`；
3. 尝试从 `deviceTokenPath` 加载历史 deviceToken（若存在）；
4. 通过 WebSocket 连接到 `serverUrl` 并完成握手与心跳；
5. 若握手返回新的 `deviceToken`，会持久化写回 `deviceTokenPath`。

> 提示：由于 Agent 使用 `nhooyr.io/websocket` 和 `github.com/google/uuid`，首次在干净环境中构建前需要联网执行一次 `go get`/`go mod download` 以拉取依赖。

---

## 核心流程（单节点 MVP）

### 1. WebSocket 握手与认证

协议结构与握手流程参考 openclaw Gateway：

1. **连接建立**：Agent 以 WebSocket (`ws://` 或 `wss://`) 连接到 Server 的 `/ws` 端点。
2. **服务端挑战 `connect.challenge`**：
   - Server 接受连接后立即发送一帧事件：
     ```json
     {
       "type": "event",
       "event": "connect.challenge",
       "payload": { "nonce": "…", "ts": 1737264000000 }
     }
     ```
   - 其中 `nonce` 为随机挑战字符串，`ts` 为毫秒时间戳。
3. **客户端 `connect` 请求**：
   - Agent 收到挑战后构造 `connect` 请求（`type=req`，`method=connect`）：
     - `minProtocol` / `maxProtocol`：当前固定为 3/3；
     - `client`：客户端信息（`id`=`go-agent`、`version` 等）；
     - `role`：MVP 中固定为 `node`；
     - `scopes`：MVP 暂留空列表；
     - `auth.token`：优先使用已下发的 `deviceToken`，否则回退到静态 `authToken`；
     - `device`：设备身份与签名信息：
       - `id`：由公钥指纹派生出的稳定设备 ID；
       - `publicKey`：Ed25519 公钥的 Base64 编码；
       - `nonce`：原样回显服务端挑战中的 `nonce`；
       - `signedAt`：当前毫秒时间戳；
       - `signature`：**Ed25519 签名结果**，签名消息为：
         ```text
         deviceId|nonce|signedAt|role|token
         ```
         即将上述字段按顺序用 `|` 连接，使用 UTF-8 编码后调用 `ed25519.Sign`，最终再 Base64 编码写入 `signature`。
4. **服务端验签与时间窗校验**：
   - Server 使用 PyNaCl (`VerifyKey`) 复原相同签名消息串并校验签名；
   - 校验 `device.nonce` 必须等于服务器挑战中的 `nonce`；
   - 校验 `signedAt` 与当前服务器时间的偏差不超过 `MAX_SKEW_SECONDS`（默认 300 秒），超出则认为签名过期/重放；
   - 校验 `auth.token`：
     - 若等于 `STATIC_GATEWAY_TOKEN`，视为使用静态网关 Token；
     - 否则尝试在内存 `tokens[device_id]` 中查找是否存在匹配且未过期的 deviceToken。
5. **设备令牌发放与续期（`hello-ok.auth.deviceToken`）**：
   - 若通过的是 **静态网关 Token** 且该设备尚无有效 deviceToken：
     - Server 生成随机 `deviceToken = secrets.token_urlsafe(32)`；
     - 将 `(device_id, deviceToken, expires_at)` 写入内存 `tokens` 字典，过期时间为当前时间 + `DEVICE_TOKEN_TTL_MINUTES`；
     - 在 `hello-ok.auth.deviceToken` 字段中返回该 token；
   - 若通过的是 **已有 deviceToken**，则视为正常重连，同时刷新该 token 的过期时间（滑动窗口）。
6. **`hello-ok` 响应**：
   - Server 向 Agent 发送 `res` 帧：
     ```json
     {
       "type": "res",
       "id": "…",
       "ok": true,
       "payload": {
         "type": "hello-ok",
         "protocol": 3,
         "policy": { "tickIntervalMs": 15000 },
         "auth": {
           "deviceToken": "…",  // 首次签发时存在
           "role": "node",
           "scopes": []
         }
       }
     }
     ```
   - `policy.tickIntervalMs` 用于控制 Agent 心跳间隔；
   - Agent 在收到 `deviceToken` 时，会立即持久化到 `deviceTokenPath`，下次启动优先使用该 Token 完成“二次认证”。

### 2. 心跳维持

- Agent 完成握手后，根据 `hello-ok.policy.tickIntervalMs` 启动心跳写泵：
  - 周期性发送 `agent.tick` 事件：
    ```json
    {
      "type": "event",
      "event": "agent.tick",
      "payload": { "deviceId": "…", "ts": 1737264000000 }
    }
    ```
- Server 接收 `agent.tick` 后，可在内存中更新对应设备的最后活跃时间（当前实现为占位 TODO）。
- 建议在后续实现中：
  - 若某设备连续多个心跳周期未上报 `agent.tick`，则将其视为 offline 并关闭连接；
  - 在监控中暴露在线设备数、心跳延迟等指标。

### 3. 命令下发与结果回传

1. **创建命令（REST API）**：
   - 用户调用 `POST /api/commands` 下发命令，请求体包含：
     - `targets`：目标 Agent ID 列表（MVP 中暂未生效，当前广播给所有在线 Agent）；
     - `command`：要执行的命令字符串（如 `uname -a`）；
     - `timeoutSeconds`：最大执行时长（秒）；
     - `idempotencyKey`：幂等键（MVP 中仅存储，不做完整幂等实现）。
   - Server 为每个命令生成唯一 `task_uuid`，写入内存 `_commands` 字典并立即返回。
2. **命令推送（WebSocket）**：
   - Server 通过 `ConnectionManager` 向所有在线 Agent 广播 `command.push` 事件：
     ```json
     {
       "type": "event",
       "event": "command.push",
       "payload": {
         "task_uuid": "…",
         "command": "uname -a",
         "correlationId": "…",
         "timeoutSeconds": 30
       }
     }
     ```
   - 当前实现未对目标进行精确筛选，后续可根据 `targets` 与在线状态选择性推送。
3. **Agent 执行与结果回传**：
   - Agent 收到 `command.push` 事件后，当前仅记录日志，未真正执行命令：
     - 当 `enableShell: true` 时，可在后续迭代中接入 `ShellExecutor`，使用本地 shell (`sh -c`) 执行命令；
     - 并将结果封装为 `result.chunk` 事件回传 Server（当前为 TODO）。
4. **结果聚合（WebSocket + REST API）**：
   - Server 侧已实现 `result.chunk` 的模型与聚合入口：
     - `ResultChunkPayload` 描述单个分片结果；
     - `update_result(task_uuid, agent_id, result)` 将结果转为 `CommandResultSummary` 写入内存 `_results` 字典，并更新任务状态为 `Running`。
   - 后续当 Agent 实现 `result.chunk` 回传后，用户可以通过 `GET /api/commands/{task_uuid}` 查询命令执行状态和各 Agent 返回结果。

---

## 后续扩展点（TODO）

此 MVP 脚手架为后续开发预留了大量扩展位，包括但不限于：

- **Agent**：
  - 使用 deviceToken 优先认证，并在 token 失效或配置变更时自动回退到静态 Token；
  - 实现完整的 WebSocket 读写泵和自动重连（带指数退避与限速）；
  - 完善命令执行模块，支持超时、取消和并发控制；
  - 实现结果的流式分片回传（多条 `result.chunk` + 终结标记）；
  - 引入结构化日志与基本指标上报（心跳次数、命令执行耗时等）。
- **Server**：
  - 将内存存储（连接、任务、结果、deviceToken）替换为数据库 (MySQL/PostgreSQL) 与缓存 (Redis)；
  - 实现完整的设备公钥管理、签名验证与设备指纹绑定；
  - 将当前单节点的 deviceToken 内存字典替换为可持久化 / 可扩展的存储；
  - 实现任务的目标选择（单机、分组、全体）和离线投递队列；
  - 完善 WS 事件处理，如 `agent.tick` 的超时检测和 `result.chunk` 的聚合；
- **通用**：
  - 切换到 Protobuf 以优化消息体积和序列化性能；
  - 实现更精细的 RBAC 权限控制与审计日志；
  - 为 QA 提供 Mock Agent、录制/回放工具和常见异常场景的测试脚本。
