package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

// CommandMessage 命令消息格式
type CommandMessage struct {
	TaskID    string `json:"task_id"`
	Command   string `json:"command"`
	Timeout   int    `json:"timeout"`
	User      string `json:"user"`
	Timestamp int64  `json:"timestamp"`
	Signature string `json:"signature"`
}

// CommandResult 命令执行结果
type CommandResult struct {
	TaskID    string `json:"task_id"`
	ExitCode  int    `json:"exit_code"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Hostname  string `json:"hostname"`
	Timestamp int64  `json:"timestamp"`
}

// Executor 命令执行器
type Executor struct {
	config      *Config
	connManager *ConnectionManager
}

// NewExecutor 创建新的执行器
func NewExecutor(config *Config, connManager *ConnectionManager) *Executor {
	return &Executor{
		config:      config,
		connManager: connManager,
	}
}

// Execute 执行命令
func (e *Executor) Execute(msg []byte) error {
	// 1. 解析消息
	var cmdMsg CommandMessage
	if err := json.Unmarshal(msg, &cmdMsg); err != nil {
		return fmt.Errorf("failed to unmarshal command message: %v", err)
	}

	// 2. 验证命令安全性
	if !e.isCommandAllowed(cmdMsg.Command) {
		log.Printf("Command not allowed: %s", cmdMsg.Command)
		return fmt.Errorf("command not allowed: %s", cmdMsg.Command)
	}

	// 3. 执行命令
	exitCode, stdout, stderr, err := e.runCommand(cmdMsg.Command, cmdMsg.Timeout)
	if err != nil {
		log.Printf("Error running command: %v", err)
	}

	// 4. 构建结果
	result := CommandResult{
		TaskID:    cmdMsg.TaskID,
		ExitCode:  exitCode,
		Stdout:    stdout,
		Stderr:    stderr,
		Hostname:  e.config.Hostname,
		Timestamp: time.Now().Unix(),
	}

	// 5. 发送结果
	if err := e.sendResult(result); err != nil {
		log.Printf("Failed to send result: %v", err)
	}

	log.Printf("Command executed: %s, Exit code: %d", cmdMsg.Command, exitCode)
	return nil
}

// isCommandAllowed 检查命令是否允许执行
func (e *Executor) isCommandAllowed(command string) bool {
	// 移除命令中的参数，只检查命令本身
	cmdParts := strings.Fields(command)
	if len(cmdParts) == 0 {
		return false
	}

	cmdName := cmdParts[0]

	// 检查命令是否在白名单中
	allowed := false
	for _, allowedCmd := range e.config.AllowedCommands {
		if strings.HasPrefix(cmdName, allowedCmd) {
			allowed = true
			break
		}
	}

	if !allowed {
		return false
	}

	// 禁止危险命令
	dangerousCommands := []string{
		"rm",
		"shutdown",
		"reboot",
		"halt",
		"poweroff",
		"dd",
		"mkfs",
		"fdisk",
	}

	for _, dangerous := range dangerousCommands {
		if strings.HasPrefix(cmdName, dangerous) {
			return false
		}
	}

	// 检查目录白名单
	if !e.isPathAllowed(command) {
		return false
	}

	return true
}

// isPathAllowed 检查命令中使用的路径是否在白名单中
func (e *Executor) isPathAllowed(command string) bool {
	// 简单的路径提取和检查
	// 注意：这只是一个基本实现，实际应用中可能需要更复杂的路径解析
	cmdParts := strings.Fields(command)

	for i, part := range cmdParts {
		// 跳过命令本身
		if i == 0 {
			continue
		}

		// 跳过选项参数
		if strings.HasPrefix(part, "-") {
			continue
		}

		// 检查是否是路径
		if strings.HasPrefix(part, "/") {
			// 检查路径是否在白名单中
			if !e.isDirectoryAllowed(part) {
				return false
			}
		}
	}

	return true
}

// isDirectoryAllowed 检查目录是否在白名单中
func (e *Executor) isDirectoryAllowed(path string) bool {
	// 规范化路径
	path = strings.TrimSuffix(path, "/")

	// 检查路径是否在白名单中
	for _, allowedDir := range e.config.AllowedDirectories {
		allowedDir = strings.TrimSuffix(allowedDir, "/")

		// 如果路径是白名单目录的子目录，或者完全匹配
		if path == allowedDir || strings.HasPrefix(path, allowedDir+"/") {
			return true
		}
	}

	return false
}

// runCommand 运行系统命令
func (e *Executor) runCommand(command string, timeout int) (int, string, string, error) {
	// 设置默认超时
	if timeout <= 0 {
		timeout = int(e.config.CommandTimeout.Seconds())
	}

	// 创建命令
	cmd := exec.Command("/bin/sh", "-c", command)

	// 捕获输出
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// 启动命令
	if err := cmd.Start(); err != nil {
		return -1, "", "", err
	}

	// 设置超时
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
				return exitErr.ExitCode(), stdout.String(), stderr.String(), nil
			}
			return -1, stdout.String(), stderr.String(), err
		}
		return 0, stdout.String(), stderr.String(), nil
	case <-time.After(time.Duration(timeout) * time.Second):
		// 超时，终止命令
		if err := cmd.Process.Kill(); err != nil {
			log.Printf("Failed to kill process: %v", err)
		}
		return -2, stdout.String(), stderr.String() + "\nCommand timed out", nil
	}
}

// sendResult 发送执行结果
func (e *Executor) sendResult(result CommandResult) error {
	// 序列化结果
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return err
	}

	// 构建 routing key
	routingKey := "result.node." + e.config.Hostname

	// 发送结果到消息队列
	if e.connManager != nil {
		if err := e.connManager.Publish("sys_result_exchange", routingKey, resultJSON); err != nil {
			log.Printf("Failed to send result: %v", err)
		}
	}

	// 打印结果日志
	log.Printf("Command result sent: %s", string(resultJSON))

	return nil
}
