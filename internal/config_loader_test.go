package internal

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Test_LoadConfig_Correct 测试正确加载配置
func Test_LoadConfig_Correct(t *testing.T) {
	// 保存原始环境变量
	originalConfigPath := os.Getenv("AGENT_CONFIG_PATH")
	defer os.Setenv("AGENT_CONFIG_PATH", originalConfigPath)

	// 创建临时配置文件
	tempDir, err := os.MkdirTemp("", "agent-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	configFile := filepath.Join(tempDir, "agent.json")
	configContent := `{
		"rabbitmq_url": "amqp://test:test@localhost:5672/",
		"hostname": "test-agent",
		"group": "test-group",
		"max_concurrent_tasks": 10,
		"command_timeout": 60,
		"allowed_commands": ["ls", "pwd"],
		"allowed_directories": ["/tmp", "/data"]
	}`

	err = os.WriteFile(configFile, []byte(configContent), 0644)
	assert.NoError(t, err)

	// 设置环境变量指向临时配置文件
	os.Setenv("AGENT_CONFIG_PATH", configFile)

	// 加载配置
	config, err := LoadConfig()
	assert.NoError(t, err)
	assert.NotNil(t, config)

	// 验证配置
	assert.Equal(t, "amqp://test:test@localhost:5672/", config.RabbitMQURL)
	assert.Equal(t, "test-agent", config.Hostname)
	assert.Equal(t, "test-group", config.Group)
	assert.Equal(t, 10, config.MaxConcurrentTasks)
	assert.Equal(t, 60*time.Second, config.CommandTimeout)
	assert.Equal(t, []string{"ls", "pwd"}, config.AllowedCommands)

}

// Test_LoadConfig_NoConfigFile 测试没有配置文件时的默认值
func Test_LoadConfig_NoConfigFile(t *testing.T) {
	// 保存原始环境变量
	originalConfigPath := os.Getenv("AGENT_CONFIG_PATH")
	defer os.Setenv("AGENT_CONFIG_PATH", originalConfigPath)

	// 创建临时目录，确保其中没有配置文件
	tempDir, err := os.MkdirTemp("", "agent-test-no-config")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// 保存原始工作目录
	originalWD, err := os.Getwd()
	assert.NoError(t, err)
	defer os.Chdir(originalWD)

	// 切换到临时目录
	err = os.Chdir(tempDir)
	assert.NoError(t, err)

	// 确保没有配置文件
	os.Setenv("AGENT_CONFIG_PATH", "/nonexistent/path/agent.json")

	// 加载配置
	config, err := LoadConfig()
	assert.NoError(t, err)
	assert.NotNil(t, config)

	// 验证默认值
	assert.Equal(t, "amqp://guest:guest@localhost:5672/", config.RabbitMQURL)
	assert.NotEmpty(t, config.Hostname)
	assert.Empty(t, config.Group)
	assert.Equal(t, 5, config.MaxConcurrentTasks)
	assert.Equal(t, 300*time.Second, config.CommandTimeout)
	assert.NotEmpty(t, config.AllowedCommands)

}

// Test_LoadConfig_EnvironmentVariables 测试环境变量覆盖配置文件
func Test_LoadConfig_EnvironmentVariables(t *testing.T) {
	// 保存原始环境变量
	originalConfigPath := os.Getenv("AGENT_CONFIG_PATH")
	originalRabbitMQURL := os.Getenv("RABBITMQ_URL")
	originalHostname := os.Getenv("AGENT_HOSTNAME")
	originalGroup := os.Getenv("AGENT_GROUP")
	originalMaxTasks := os.Getenv("AGENT_MAX_TASKS")
	originalTimeout := os.Getenv("AGENT_TIMEOUT")

	defer func() {
		os.Setenv("AGENT_CONFIG_PATH", originalConfigPath)
		os.Setenv("RABBITMQ_URL", originalRabbitMQURL)
		os.Setenv("AGENT_HOSTNAME", originalHostname)
		os.Setenv("AGENT_GROUP", originalGroup)
		os.Setenv("AGENT_MAX_TASKS", originalMaxTasks)
		os.Setenv("AGENT_TIMEOUT", originalTimeout)
	}()

	// 创建临时配置文件
	tempDir, err := os.MkdirTemp("", "agent-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	configFile := filepath.Join(tempDir, "agent.json")
	configContent := `{
		"rabbitmq_url": "amqp://file:file@localhost:5672/",
		"hostname": "file-agent",
		"group": "file-group",
		"max_concurrent_tasks": 5,
		"command_timeout": 300
	}`

	err = os.WriteFile(configFile, []byte(configContent), 0644)
	assert.NoError(t, err)

	// 设置环境变量
	os.Setenv("AGENT_CONFIG_PATH", configFile)
	os.Setenv("RABBITMQ_URL", "amqp://env:env@localhost:5672/")
	os.Setenv("AGENT_HOSTNAME", "env-agent")
	os.Setenv("AGENT_GROUP", "env-group")
	os.Setenv("AGENT_MAX_TASKS", "15")
	os.Setenv("AGENT_TIMEOUT", "120s")

	// 加载配置
	config, err := LoadConfig()
	assert.NoError(t, err)
	assert.NotNil(t, config)

	// 验证环境变量覆盖了配置文件
	assert.Equal(t, "amqp://env:env@localhost:5672/", config.RabbitMQURL)
	assert.Equal(t, "env-agent", config.Hostname)
	assert.Equal(t, "env-group", config.Group)
	assert.Equal(t, 15, config.MaxConcurrentTasks)
	assert.Equal(t, 120*time.Second, config.CommandTimeout)
}
