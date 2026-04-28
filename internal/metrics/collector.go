package metrics

import (
	"fmt"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
)

// Snapshot 描述某一时刻的系统资源快照。
type Snapshot struct {
	// CPU 使用率（0-100），表示所有核心的平均利用率。
	CPUPercent float64 `json:"cpuPercent"`

	// 内存使用率（0-100）。
	MemPercent float64 `json:"memPercent"`

	// 已用内存（字节）。
	MemUsed uint64 `json:"memUsed"`

	// 总内存（字节）。
	MemTotal uint64 `json:"memTotal"`

	// 1分钟平均负载（Linux/macOS 有效；Windows 可能为 0）。
	Load1 float64 `json:"load1"`

	// Goroutine 数量（Go runtime 层面）。
	NumGoroutine int `json:"numGoroutine"`

	// 采集时间戳（毫秒）。
	TS int64 `json:"ts"`
}

// Collector 负责采集系统指标。
//
// 目前使用 gopsutil 实现，支持 Linux/macOS/Windows。
type Collector struct{}

// NewCollector 创建一个新的指标采集器。
func NewCollector() *Collector {
	return &Collector{}
}

// Collect 采集当前系统状态并返回 Snapshot。
//
// 单个指标采集失败不会中断整体流程，而是记录零值并继续。
func (c *Collector) Collect() Snapshot {
	now := time.Now().UnixMilli()

	snap := Snapshot{
		TS:           now,
		NumGoroutine: runtime.NumGoroutine(),
	}

	// CPU 使用率（间隔 100ms 采样，与 top 行为一致）。
	if percents, err := cpu.Percent(100*time.Millisecond, false); err == nil && len(percents) > 0 {
		snap.CPUPercent = percents[0]
	}

	// 内存信息。
	if vmStat, err := mem.VirtualMemory(); err == nil {
		snap.MemPercent = vmStat.UsedPercent
		snap.MemUsed = vmStat.Used
		snap.MemTotal = vmStat.Total
	}

	// 系统负载。
	if loadStat, err := load.Avg(); err == nil {
		snap.Load1 = loadStat.Load1
	}

	return snap
}

// String 返回人类可读的资源摘要，用于日志输出。
func (s Snapshot) String() string {
	return fmt.Sprintf("cpu=%.1f%% mem=%.1f%%(%s/%s) load1=%.2f goroutines=%d",
		s.CPUPercent,
		s.MemPercent,
		formatBytes(s.MemUsed),
		formatBytes(s.MemTotal),
		s.Load1,
		s.NumGoroutine,
	)
}

func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
