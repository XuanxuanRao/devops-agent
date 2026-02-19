package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Test_Executor_isCommandAllowed_Correct 测试正确的命令是否被允许
func Test_Executor_isCommandAllowed_Correct(t *testing.T) {
	// 创建配置
	config := &Config{
		AllowedCommands:    []string{"ls", "pwd", "docker"},
		AllowedDirectories: []string{"/tmp", "/data"},
	}

	// 创建执行器
	executor := NewExecutor(config)

	// 测试白名单中的命令
	assert.True(t, executor.isCommandAllowed("ls -la"))
	assert.True(t, executor.isCommandAllowed("pwd"))
	assert.True(t, executor.isCommandAllowed("docker ps"))
}

// Test_Executor_isCommandAllowed_Error 测试错误的命令是否被拒绝
func Test_Executor_isCommandAllowed_Error(t *testing.T) {
	// 创建配置
	config := &Config{
		AllowedCommands:    []string{"ls", "pwd"},
		AllowedDirectories: []string{"/tmp", "/data"},
	}

	// 创建执行器
	executor := NewExecutor(config)

	// 测试不在白名单中的命令
	assert.False(t, executor.isCommandAllowed("rm -rf /"))
	assert.False(t, executor.isCommandAllowed("shutdown -h now"))
	assert.False(t, executor.isCommandAllowed("unknown command"))
}

// Test_Executor_isCommandAllowed_Dangerous 测试危险命令是否被拒绝
func Test_Executor_isCommandAllowed_Dangerous(t *testing.T) {
	// 创建配置（包含危险命令，应该被拒绝）
	config := &Config{
		AllowedCommands:    []string{"ls", "rm", "shutdown"},
		AllowedDirectories: []string{"/tmp", "/data"},
	}

	// 创建执行器
	executor := NewExecutor(config)

	// 测试危险命令
	assert.False(t, executor.isCommandAllowed("rm -rf /"))
	assert.False(t, executor.isCommandAllowed("shutdown -h now"))
	assert.False(t, executor.isCommandAllowed("reboot"))
	assert.False(t, executor.isCommandAllowed("dd if=/dev/zero of=/dev/sda"))
}

// Test_Executor_isPathAllowed_Correct 测试正确的路径是否被允许
func Test_Executor_isPathAllowed_Correct(t *testing.T) {
	// 创建配置
	config := &Config{
		AllowedCommands:    []string{"ls", "pwd"},
		AllowedDirectories: []string{"/tmp", "/data"},
	}

	// 创建执行器
	executor := NewExecutor(config)

	// 测试白名单中的路径
	assert.True(t, executor.isPathAllowed("ls /tmp"))
	assert.True(t, executor.isPathAllowed("ls /data"))
	assert.True(t, executor.isPathAllowed("ls /tmp/subdir"))
	assert.True(t, executor.isPathAllowed("ls /data/subdir/file.txt"))
}

// Test_Executor_isPathAllowed_Error 测试错误的路径是否被拒绝
func Test_Executor_isPathAllowed_Error(t *testing.T) {
	// 创建配置
	config := &Config{
		AllowedCommands:    []string{"ls", "pwd"},
		AllowedDirectories: []string{"/tmp", "/data"},
	}

	// 创建执行器
	executor := NewExecutor(config)

	// 测试不在白名单中的路径
	assert.False(t, executor.isPathAllowed("ls /etc"))
	assert.False(t, executor.isPathAllowed("ls /root"))
	assert.False(t, executor.isPathAllowed("ls /var"))
}

// Test_Executor_isPathAllowed_Border 测试边界情况
func Test_Executor_isPathAllowed_Border(t *testing.T) {
	// 创建配置
	config := &Config{
		AllowedCommands:    []string{"ls", "pwd"},
		AllowedDirectories: []string{"/"},
	}

	// 创建执行器
	executor := NewExecutor(config)

	// 测试根目录的子目录
	assert.True(t, executor.isPathAllowed("ls /"))
	assert.True(t, executor.isPathAllowed("ls /etc"))
	assert.True(t, executor.isPathAllowed("ls /home"))
	assert.True(t, executor.isPathAllowed("ls /var"))
}

// Test_Executor_isDirectoryAllowed_Correct 测试正确的目录是否被允许
func Test_Executor_isDirectoryAllowed_Correct(t *testing.T) {
	// 创建配置
	config := &Config{
		AllowedCommands:    []string{"ls", "pwd"},
		AllowedDirectories: []string{"/tmp", "/data"},
	}

	// 创建执行器
	executor := NewExecutor(config)

	// 测试白名单中的目录
	assert.True(t, executor.isDirectoryAllowed("/tmp"))
	assert.True(t, executor.isDirectoryAllowed("/data"))
	assert.True(t, executor.isDirectoryAllowed("/tmp/"))
	assert.True(t, executor.isDirectoryAllowed("/data/"))
	assert.True(t, executor.isDirectoryAllowed("/tmp/subdir"))
	assert.True(t, executor.isDirectoryAllowed("/data/subdir/file.txt"))
}

// Test_Executor_isDirectoryAllowed_Error 测试错误的目录是否被拒绝
func Test_Executor_isDirectoryAllowed_Error(t *testing.T) {
	// 创建配置
	config := &Config{
		AllowedCommands:    []string{"ls", "pwd"},
		AllowedDirectories: []string{"/tmp", "/data"},
	}

	// 创建执行器
	executor := NewExecutor(config)

	// 测试不在白名单中的目录
	assert.False(t, executor.isDirectoryAllowed("/etc"))
	assert.False(t, executor.isDirectoryAllowed("/root"))
	assert.False(t, executor.isDirectoryAllowed("/var"))
	assert.False(t, executor.isDirectoryAllowed("/etc/passwd"))
}

// Test_Executor_runCommand_Correct 测试命令执行
func Test_Executor_runCommand_Correct(t *testing.T) {
	// 创建配置
	config := &Config{
		CommandTimeout: 10 * time.Second,
	}

	// 创建执行器
	executor := NewExecutor(config)

	// 测试简单命令
	exitCode, stdout, stderr, err := executor.runCommand("echo hello world", 5)
	assert.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "hello world")
	assert.Empty(t, stderr)

	// 测试 pwd 命令
	exitCode, stdout, stderr, err = executor.runCommand("pwd", 5)
	assert.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.NotEmpty(t, stdout)
	assert.Empty(t, stderr)
}

// Test_Executor_runCommand_Timeout 测试命令超时
func Test_Executor_runCommand_Timeout(t *testing.T) {
	// 创建配置
	config := &Config{
		CommandTimeout: 1 * time.Second,
	}

	// 创建执行器
	executor := NewExecutor(config)

	// 测试超时命令
	exitCode, _, stderr, err := executor.runCommand("sleep 3", 1)
	assert.NoError(t, err)
	assert.Equal(t, -2, exitCode) // 超时返回码
	assert.Contains(t, stderr, "Command timed out")
}

// Test_Executor_runCommand_Error 测试错误命令
func Test_Executor_runCommand_Error(t *testing.T) {
	// 创建配置
	config := &Config{
		CommandTimeout: 5 * time.Second,
	}

	// 创建执行器
	executor := NewExecutor(config)

	// 测试错误命令
	exitCode, stdout, stderr, err := executor.runCommand("unknown_command_12345", 5)
	assert.NoError(t, err) // 命令执行失败但不是函数错误
	assert.NotEqual(t, 0, exitCode)
	assert.Empty(t, stdout)
	assert.NotEmpty(t, stderr)
}
