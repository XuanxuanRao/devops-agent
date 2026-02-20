package internal

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Test_Executor_isCommandAllowed_Correct 测试正确的命令是否被允许
func Test_Executor_isCommandAllowed_Correct(t *testing.T) {
	// 创建配置
	config := &Config{
		AllowedCommands: []string{"ls", "pwd", "docker"},
	}

	// 创建执行器
	executor := NewExecutor(config, nil)

	// 测试白名单中的命令
	assert.True(t, executor.isCommandAllowed("ls -la"))
	assert.True(t, executor.isCommandAllowed("pwd"))
	assert.True(t, executor.isCommandAllowed("docker ps"))
}

// Test_Executor_isCommandAllowed_Error 测试错误的命令是否被拒绝
func Test_Executor_isCommandAllowed_Error(t *testing.T) {
	// 创建配置
	config := &Config{
		AllowedCommands: []string{"ls", "pwd"},
	}

	// 创建执行器
	executor := NewExecutor(config, nil)

	// 测试不在白名单中的命令
	assert.False(t, executor.isCommandAllowed("rm -rf /"))
	assert.False(t, executor.isCommandAllowed("shutdown -h now"))
	assert.False(t, executor.isCommandAllowed("unknown command"))
}

// Test_Executor_isCommandAllowed_Dangerous 测试危险命令是否被拒绝
func Test_Executor_isCommandAllowed_Dangerous(t *testing.T) {
	// 创建配置（包含危险命令，应该被拒绝）
	config := &Config{
		AllowedCommands: []string{"ls", "rm", "shutdown"},
	}

	// 创建执行器
	executor := NewExecutor(config, nil)

	// 测试危险命令
	assert.False(t, executor.isCommandAllowed("rm -rf /"))
	assert.False(t, executor.isCommandAllowed("shutdown -h now"))
	assert.False(t, executor.isCommandAllowed("reboot"))
	assert.False(t, executor.isCommandAllowed("dd if=/dev/zero of=/dev/sda"))
}

// Test_Executor_runCommand_Correct 测试命令执行
func Test_Executor_runCommand_Correct(t *testing.T) {
	// 创建配置
	config := &Config{
		CommandTimeout: 10 * time.Second,
	}

	// 创建执行器
	executor := NewExecutor(config, nil)

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
	executor := NewExecutor(config, nil)

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
	executor := NewExecutor(config, nil)

	// 测试错误命令
	exitCode, stdout, stderr, err := executor.runCommand("unknown_command_12345", 5)
	assert.NoError(t, err) // 命令执行失败但不是函数错误
	assert.NotEqual(t, 0, exitCode)
	assert.Empty(t, stdout)
	assert.NotEmpty(t, stderr)
}
