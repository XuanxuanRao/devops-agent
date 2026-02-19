package main

import (
	"encoding/json"
	"log"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// HeartbeatMessage 心跳消息格式
type HeartbeatMessage struct {
	Hostname  string  `json:"hostname"`
	Timestamp int64   `json:"timestamp"`
	Status    string  `json:"status"`
	CPUUsage  float64 `json:"cpu_usage,omitempty"`
	MemUsage  float64 `json:"mem_usage,omitempty"`
}

// Heartbeat 心跳管理器
type Heartbeat struct {
	connManager *ConnectionManager
	hostname    string
	interval    time.Duration
	ticker      *time.Ticker
	running     bool
}

// NewHeartbeat 创建新的心跳管理器
func NewHeartbeat(connManager *ConnectionManager, hostname string, interval time.Duration) *Heartbeat {
	return &Heartbeat{
		connManager: connManager,
		hostname:    hostname,
		interval:    interval,
	}
}

// Start 启动心跳
func (h *Heartbeat) Start() {
	h.running = true
	h.ticker = time.NewTicker(h.interval)

	go func() {
		for h.running {
			<-h.ticker.C
			h.sendHeartbeat()
		}
	}()

	log.Println("Heartbeat started")
}

// Stop 停止心跳
func (h *Heartbeat) Stop() {
	h.running = false
	if h.ticker != nil {
		h.ticker.Stop()
	}
	log.Println("Heartbeat stopped")
}

// sendHeartbeat 发送心跳消息
func (h *Heartbeat) sendHeartbeat() {
	// 获取系统资源信息
	cpuUsage, memUsage := h.getSystemResources()

	// 构建心跳消息
	msg := HeartbeatMessage{
		Hostname:  h.hostname,
		Timestamp: time.Now().Unix(),
		Status:    "online",
		CPUUsage:  cpuUsage,
		MemUsage:  memUsage,
	}

	// 序列化消息
	msgJSON, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal heartbeat message: %v", err)
		return
	}

	// 发送消息到心跳队列
	if err := h.connManager.Publish("sys_cmd_exchange", "heartbeat", msgJSON); err != nil {
		log.Printf("Failed to send heartbeat: %v", err)
		return
	}

	log.Printf("Heartbeat sent: CPU=%.2f%%, Mem=%.2f%%", cpuUsage, memUsage)
}

// getSystemResources 获取系统 CPU 和内存使用率
func (h *Heartbeat) getSystemResources() (float64, float64) {
	// 获取 CPU 使用率
	cpuUsage := 0.0
	cpuPercent, err := cpu.Percent(0, false)
	if err == nil && len(cpuPercent) > 0 {
		cpuUsage = cpuPercent[0]
	} else {
		log.Printf("Failed to get CPU usage: %v", err)
	}

	// 获取内存使用率
	memUsage := 0.0
	memInfo, err := mem.VirtualMemory()
	if err == nil {
		memUsage = memInfo.UsedPercent
	} else {
		log.Printf("Failed to get memory usage: %v", err)
	}

	return cpuUsage, memUsage
}
