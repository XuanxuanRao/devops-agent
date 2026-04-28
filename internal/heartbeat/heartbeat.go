package heartbeat

import (
	"context"
	"time"

	"devops-agent/internal/metrics"
)

// Sender 抽象出心跳发送能力，避免直接依赖具体的 WS 客户端实现。
type Sender interface {
	SendHeartbeat(ctx context.Context, snap metrics.Snapshot) error
}

// Start 以给定的毫秒间隔触发心跳发送。
//
// MVP 仅做简单循环，不做 jitter 与健康状态判断。
// TODO: 后续可以增加：
//   - 连续失败计数与退避
//   - 与服务端 hello-ok 中的 tickIntervalMs 对齐与热更新
func Start(ctx context.Context, intervalMs int, sender Sender) {
	if intervalMs <= 0 {
		intervalMs = 15000
	}

	collector := metrics.NewCollector()
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap := collector.Collect()
			_ = sender.SendHeartbeat(ctx, snap)
		}
	}
}
