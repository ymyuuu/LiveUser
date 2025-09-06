package utils

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config 应用配置结构
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	WebSocket WebSocketConfig `yaml:"websocket"`
	Analytics AnalyticsConfig `yaml:"analytics"`
	Logging   LoggingConfig   `yaml:"logging"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Host         string        `yaml:"host"`           // 监听主机，默认 "0.0.0.0"
	Port         int           `yaml:"port"`           // 监听端口，默认 10086
	ReadTimeout  time.Duration `yaml:"read_timeout"`   // 读取超时，默认 30s
	WriteTimeout time.Duration `yaml:"write_timeout"`  // 写入超时，默认 30s
	IdleTimeout  time.Duration `yaml:"idle_timeout"`   // 空闲超时，默认 120s
}

// WebSocketConfig WebSocket配置
type WebSocketConfig struct {
	ReadBufferSize  int           `yaml:"read_buffer_size"`  // 读缓冲区大小，默认 1024
	WriteBufferSize int           `yaml:"write_buffer_size"` // 写缓冲区大小，默认 1024
	PingPeriod      time.Duration `yaml:"ping_period"`       // Ping周期，默认 54s
	PongTimeout     time.Duration `yaml:"pong_timeout"`      // Pong超时，默认 60s
	MaxMessageSize  int64         `yaml:"max_message_size"`  // 最大消息大小，默认 512字节
}

// AnalyticsConfig 统计分析配置
type AnalyticsConfig struct {
	EnableIPDedup   bool          `yaml:"enable_ip_dedup"`   // 启用IP去重，默认 true
	SessionTimeout  time.Duration `yaml:"session_timeout"`   // 会话超时，默认 30m
	CleanupInterval time.Duration `yaml:"cleanup_interval"`  // 清理间隔，默认 5m
	MaxSites        int           `yaml:"max_sites"`         // 最大站点数，默认 1000
	MaxConnections  int           `yaml:"max_connections"`   // 单站点最大连接数，默认 10000
}

// LoggingConfig 日志配置
type LoggingConfig struct {
	Level      string `yaml:"level"`       // 日志级别：debug, info, warn, error
	File       string `yaml:"file"`        // 日志文件路径
	MaxSize    int    `yaml:"max_size"`    // 日志文件最大大小(MB)
	MaxBackups int    `yaml:"max_backups"` // 保留的日志文件数量
	MaxAge     int    `yaml:"max_age"`     // 日志文件保留天数
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:         "0.0.0.0",
			Port:         10086,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
		WebSocket: WebSocketConfig{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			PingPeriod:      54 * time.Second,
			PongTimeout:     60 * time.Second,
			MaxMessageSize:  512,
		},
		Analytics: AnalyticsConfig{
			EnableIPDedup:   true,
			SessionTimeout:  30 * time.Minute,
			CleanupInterval: 5 * time.Minute,
			MaxSites:        1000,
			MaxConnections:  10000,
		},
		Logging: LoggingConfig{
			Level:      "info",
			File:       "logs/liveuser.log",
			MaxSize:    100,
			MaxBackups: 3,
			MaxAge:     7,
		},
	}
}

// LoadFromEnv 从环境变量加载配置（覆盖默认值）
func (c *Config) LoadFromEnv() {
	// 服务器配置
	if host := os.Getenv("LIVEUSER_HOST"); host != "" {
		c.Server.Host = host
	}
	if portStr := os.Getenv("LIVEUSER_PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			c.Server.Port = port
		}
	}

	// WebSocket配置
	if bufferStr := os.Getenv("LIVEUSER_READ_BUFFER_SIZE"); bufferStr != "" {
		if size, err := strconv.Atoi(bufferStr); err == nil {
			c.WebSocket.ReadBufferSize = size
		}
	}

	// 分析配置
	if dedupStr := os.Getenv("LIVEUSER_ENABLE_IP_DEDUP"); dedupStr != "" {
		if dedup, err := strconv.ParseBool(dedupStr); err == nil {
			c.Analytics.EnableIPDedup = dedup
		}
	}

	// 日志配置
	if level := os.Getenv("LIVEUSER_LOG_LEVEL"); level != "" {
		c.Logging.Level = level
	}
	if file := os.Getenv("LIVEUSER_LOG_FILE"); file != "" {
		c.Logging.File = file
	}
}

// Validate 验证配置有效性
func (c *Config) Validate() error {
	// 验证端口范围
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("端口号必须在1-65535范围内，当前值: %d", c.Server.Port)
	}

	// 验证超时时间
	if c.Server.ReadTimeout <= 0 {
		return fmt.Errorf("读取超时必须大于0，当前值: %v", c.Server.ReadTimeout)
	}
	if c.Server.WriteTimeout <= 0 {
		return fmt.Errorf("写入超时必须大于0，当前值: %v", c.Server.WriteTimeout)
	}

	// 验证缓冲区大小
	if c.WebSocket.ReadBufferSize <= 0 {
		return fmt.Errorf("WebSocket读缓冲区大小必须大于0，当前值: %d", c.WebSocket.ReadBufferSize)
	}
	if c.WebSocket.WriteBufferSize <= 0 {
		return fmt.Errorf("WebSocket写缓冲区大小必须大于0，当前值: %d", c.WebSocket.WriteBufferSize)
	}

	// 验证周期设置
	if c.WebSocket.PingPeriod <= 0 {
		return fmt.Errorf("Ping周期必须大于0，当前值: %v", c.WebSocket.PingPeriod)
	}
	if c.WebSocket.PongTimeout <= c.WebSocket.PingPeriod {
		return fmt.Errorf("Pong超时必须大于Ping周期，Pong: %v, Ping: %v", 
			c.WebSocket.PongTimeout, c.WebSocket.PingPeriod)
	}

	// 验证分析配置
	if c.Analytics.MaxSites <= 0 {
		return fmt.Errorf("最大站点数必须大于0，当前值: %d", c.Analytics.MaxSites)
	}
	if c.Analytics.MaxConnections <= 0 {
		return fmt.Errorf("最大连接数必须大于0，当前值: %d", c.Analytics.MaxConnections)
	}

	// 验证日志级别
	validLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true,
	}
	if !validLevels[c.Logging.Level] {
		return fmt.Errorf("无效的日志级别: %s，有效值: debug, info, warn, error", c.Logging.Level)
	}

	return nil
}

// GetAddress 获取完整的监听地址
func (c *Config) GetAddress() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}