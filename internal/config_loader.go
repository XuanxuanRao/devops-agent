package internal

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// ConfigFile 配置文件格式
type ConfigFile struct {
	RabbitMQHost       string   `json:"rabbitmq_host"`
	RabbitMQPort       int      `json:"rabbitmq_port"`
	RabbitMQUsername   string   `json:"rabbitmq_username"`
	RabbitMQPassword   string   `json:"rabbitmq_password"`
	RabbitMQVhost      string   `json:"rabbitmq_vhost"`
	Hostname           string   `json:"hostname"`
	Group              string   `json:"group"`
	MaxConcurrentTasks int      `json:"max_concurrent_tasks"`
	CommandTimeout     int      `json:"command_timeout"`
	AllowedCommands    []string `json:"allowed_commands"`
	HeartbeatInterval  int      `json:"heartbeat_interval,omitempty"`
	PrivateKeyPath     string   `json:"private_key_path,omitempty"`
	PublicKeyPath      string   `json:"public_key_path,omitempty"`
	EnableSignature    bool     `json:"enable_signature,omitempty"`
	ConfigPath         string   `json:"config_path,omitempty"`
}

// LoadConfig 从配置文件和环境变量加载配置
func LoadConfig() (*Config, error) {
	// 默认配置文件路径
	configPaths := []string{
		"./agent.json",
		"/etc/devops-agent/agent.json",
		"$HOME/.devops-agent/agent.json",
	}

	// 从环境变量获取配置文件路径
	if configPath := os.Getenv("AGENT_CONFIG_PATH"); configPath != "" {
		configPaths = append([]string{configPath}, configPaths...)
	}

	// 读取配置文件
	var configFile ConfigFile
	configFileLoaded := false

	for _, path := range configPaths {
		// 展开环境变量
		path = os.ExpandEnv(path)

		if _, err := os.Stat(path); err == nil {
			if err := loadConfigFile(path, &configFile); err != nil {
				log.Printf("Error loading config file %s: %v", path, err)
				continue
			}
			configFileLoaded = true
			log.Printf("Loaded config from %s", path)
			break
		}
	}

	// 如果没有加载到配置文件，使用默认值
	if !configFileLoaded {
		log.Println("No config file found, using default values")
	}

	// 从环境变量获取主机名
	hostname := configFile.Hostname
	if hostname == "" {
		var err error
		hostname, err = os.Hostname()
		if err != nil {
			hostname = "unknown"
		}
	}

	// 默认指令白名单
	defaultAllowedCommands := []string{
		"ls",
		"pwd",
		"echo",
		"docker",
		"docker-compose",
		"systemctl",
		"service",
		"curl",
		"wget",
	}

	// 如果配置文件中没有指定白名单，使用默认值
	allowedCommands := configFile.AllowedCommands
	if len(allowedCommands) == 0 {
		allowedCommands = defaultAllowedCommands
	}

	// 处理命令超时
	commandTimeout := 300 * time.Second
	if configFile.CommandTimeout > 0 {
		commandTimeout = time.Duration(configFile.CommandTimeout) * time.Second
	}

	// 处理心跳频率
	heartbeatInterval := 30 * time.Second
	if configFile.HeartbeatInterval > 0 {
		heartbeatInterval = time.Duration(configFile.HeartbeatInterval) * time.Second
	}

	// 处理RabbitMQ配置
	rabbitMQHost := getEnvOrDefault("RABBITMQ_HOST", configFile.RabbitMQHost, "localhost")
	rabbitMQPort := getEnvIntOrDefault("RABBITMQ_PORT", configFile.RabbitMQPort, 5672)
	rabbitMQUsername := getEnvOrDefault("RABBITMQ_USERNAME", configFile.RabbitMQUsername, "guest")
	rabbitMQPassword := getEnvOrDefault("RABBITMQ_PASSWORD", configFile.RabbitMQPassword, "guest")
	rabbitMQVhost := getEnvOrDefault("RABBITMQ_VHOST", configFile.RabbitMQVhost, "/")

	rabbitMQURL := ""
	// 构建正确的vhost路径，避免连续斜杠
	vhostPath := rabbitMQVhost
	if vhostPath == "/" {
		// 如果vhost是根目录，只需要一个斜杠
		rabbitMQURL = fmt.Sprintf("amqp://%s:%s@%s:%d/",
			rabbitMQUsername, rabbitMQPassword, rabbitMQHost, rabbitMQPort)
	} else {
		// 如果vhost不是根目录，添加一个斜杠
		rabbitMQURL = fmt.Sprintf("amqp://%s:%s@%s:%d/%s",
			rabbitMQUsername, rabbitMQPassword, rabbitMQHost, rabbitMQPort, vhostPath)
	}

	// 构建最终配置
	config := &Config{
		RabbitMQURL:        rabbitMQURL,
		RabbitMQHost:       rabbitMQHost,
		RabbitMQPort:       rabbitMQPort,
		RabbitMQUsername:   rabbitMQUsername,
		RabbitMQPassword:   rabbitMQPassword,
		RabbitMQVhost:      rabbitMQVhost,
		Hostname:           getEnvOrDefault("AGENT_HOSTNAME", hostname, hostname),
		Group:              getEnvOrDefault("AGENT_GROUP", configFile.Group, ""),
		MaxConcurrentTasks: getEnvIntOrDefault("AGENT_MAX_TASKS", configFile.MaxConcurrentTasks, 5),
		CommandTimeout:     getEnvDurationOrDefault("AGENT_TIMEOUT", commandTimeout, 300*time.Second),
		AllowedCommands:    allowedCommands,
		HeartbeatInterval:  getEnvDurationOrDefault("AGENT_HEARTBEAT_INTERVAL", heartbeatInterval, 10*time.Second),
		PrivateKeyPath:     getEnvOrDefault("AGENT_PRIVATE_KEY", configFile.PrivateKeyPath, ""),
		PublicKeyPath:      getEnvOrDefault("AGENT_PUBLIC_KEY", configFile.PublicKeyPath, ""),
		EnableSignature:    configFile.EnableSignature,
	}

	return config, nil
}

// loadConfigFile 从文件加载配置
func loadConfigFile(path string, config *ConfigFile) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open config file: %v", err)
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {

		}
	}(file)

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(config); err != nil {
		return fmt.Errorf("failed to decode config file: %v", err)
	}

	// 处理相对路径
	if config.ConfigPath == "" {
		config.ConfigPath = filepath.Dir(path)
	}

	return nil
}

// getEnvOrDefault 获取环境变量，如果不存在则使用默认值
func getEnvOrDefault(key, configValue, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	if configValue != "" {
		return configValue
	}
	return defaultValue
}

// getEnvIntOrDefault 获取整型环境变量
func getEnvIntOrDefault(key string, configValue int, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var intValue int
		if _, err := fmt.Sscanf(value, "%d", &intValue); err == nil {
			return intValue
		}
	}
	if configValue > 0 {
		return configValue
	}
	return defaultValue
}

// getEnvDurationOrDefault 获取时间间隔环境变量
func getEnvDurationOrDefault(key string, configValue time.Duration, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	if configValue > 0 {
		return configValue
	}
	return defaultValue
}
