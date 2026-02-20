package internal

import (
	"time"
)

// Config 定义 Agent 配置
type Config struct {
	// RabbitMQ 连接 URL
	RabbitMQURL string

	// 主机名，用于队列命名和路由键
	Hostname string

	// 分组名称，用于分组指令
	Group string

	// 最大并发任务数
	MaxConcurrentTasks int

	// 命令执行超时时间
	CommandTimeout time.Duration

	// 指令类型白名单
	AllowedCommands []string

	// 心跳频率（秒）
	HeartbeatInterval time.Duration

	// 签名配置
	PrivateKeyPath  string `json:"private_key_path,omitempty"`
	PublicKeyPath   string `json:"public_key_path,omitempty"`
	EnableSignature bool   `json:"enable_signature,omitempty"`
}
