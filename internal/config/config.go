package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Keys      KeysConfig      `yaml:"keys"`
	Auth      AuthConfig      `yaml:"auth"`
	Heartbeat HeartbeatConfig `yaml:"heartbeat"`
	Shell     ShellConfig     `yaml:"shell"`
	Logging   LoggingConfig   `yaml:"logging"`
}

type ServerConfig struct {
	URL string `yaml:"url" env:"AGENT_SERVER__URL" env-default:"ws://localhost:8000/ws"`
}

type KeysConfig struct {
	Dir string `yaml:"dir" env:"AGENT_KEYS__DIR" env-default:"./keys"`
}

type AuthConfig struct {
	Token           string `yaml:"token" env:"AGENT_AUTH__TOKEN"`
	DeviceTokenPath string `yaml:"deviceTokenPath" env:"AGENT_AUTH__DEVICE_TOKEN_PATH"`
	DeviceToken     string `yaml:"-" env:"-"` // 运行时单独从 DeviceTokenPath 加载
}

type HeartbeatConfig struct {
	TickIntervalMs int `yaml:"tickIntervalMs" env:"AGENT_HEARTBEAT__TICK_INTERVAL_MS" env-default:"15000"`
}

type ShellConfig struct {
	Enabled bool   `yaml:"enabled" env:"AGENT_SHELL__ENABLED"`
	WorkDir string `yaml:"workDir" env:"AGENT_SHELL__WORK_DIR"`
}

type LoggingConfig struct {
	Level string `yaml:"level" env:"AGENT_LOGGING__LEVEL" env-default:"info"`
}

const defaultTickIntervalMs = 15000

func Load(path string) (Config, error) {
	var cfg Config

	if path != "" {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return cfg, fmt.Errorf("resolve config path: %w", err)
		}
		if _, statErr := os.Stat(absPath); statErr == nil {
			if err := cleanenv.ReadConfig(absPath, &cfg); err != nil {
				return cfg, fmt.Errorf("read config file: %w", err)
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return cfg, fmt.Errorf("stat config file: %w", statErr)
		} else {
			if err := cleanenv.ReadEnv(&cfg); err != nil {
				return cfg, fmt.Errorf("read env: %w", err)
			}
		}
	} else {
		if err := cleanenv.ReadEnv(&cfg); err != nil {
			return cfg, fmt.Errorf("read env: %w", err)
		}
	}

	if cfg.Auth.DeviceTokenPath != "" {
		token, err := LoadDeviceToken(cfg.Auth.DeviceTokenPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return cfg, fmt.Errorf("load device token: %w", err)
		}
		cfg.Auth.DeviceToken = token
	}

	return cfg, nil
}

func (c Config) TickInterval() int {
	if c.Heartbeat.TickIntervalMs <= 0 {
		return defaultTickIntervalMs
	}
	return c.Heartbeat.TickIntervalMs
}

func (c Config) SelectedAuthToken() string {
	if c.Auth.DeviceToken != "" {
		return c.Auth.DeviceToken
	}
	return c.Auth.Token
}

func (c *Config) UpdateDeviceToken(token string) {
	if c == nil || strings.TrimSpace(token) == "" {
		return
	}
	c.Auth.DeviceToken = token
}

func LoadDeviceToken(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve deviceTokenPath: %w", err)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(data))
	return token, nil
}

func SaveDeviceToken(path, token string) error {
	path = strings.TrimSpace(path)
	token = strings.TrimSpace(token)
	if path == "" || token == "" {
		return nil
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve deviceTokenPath: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o700); err != nil {
		return fmt.Errorf("create device token dir: %w", err)
	}
	if err := os.WriteFile(absPath, []byte(token+"\n"), 0o600); err != nil {
		return fmt.Errorf("write device token: %w", err)
	}
	return nil
}

func ClearDeviceToken(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve deviceTokenPath: %w", err)
	}
	if err := os.Remove(absPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove device token: %w", err)
	}
	return nil
}
