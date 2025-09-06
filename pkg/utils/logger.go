package utils

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Logger 全局日志实例
var Logger *logrus.Logger

// LogLevel 日志级别映射
var LogLevelMap = map[string]logrus.Level{
	"debug": logrus.DebugLevel,
	"info":  logrus.InfoLevel,
	"warn":  logrus.WarnLevel,
	"error": logrus.ErrorLevel,
}

// InitLogger 初始化日志系统
func InitLogger(config LoggingConfig) error {
	Logger = logrus.New()

	// 设置日志级别
	if level, exists := LogLevelMap[config.Level]; exists {
		Logger.SetLevel(level)
	} else {
		Logger.SetLevel(logrus.InfoLevel)
		Logger.Warnf("未知的日志级别 '%s'，使用默认级别 'info'", config.Level)
	}

	// 设置日志格式
	Logger.SetFormatter(&logrus.TextFormatter{
		TimestampFormat: "2006-01-02 15:04:05",
		FullTimestamp:   true,
		ForceColors:     false,
		DisableColors:   true,
	})

	// 设置输出
	var writers []io.Writer

	// 控制台输出
	writers = append(writers, os.Stdout)

	// 文件输出
	if config.File != "" {
		// 确保日志目录存在
		if err := ensureLogDir(config.File); err != nil {
			return err
		}

		// 配置日志轮转
		fileWriter := &lumberjack.Logger{
			Filename:   config.File,
			MaxSize:    config.MaxSize,    // MB
			MaxBackups: config.MaxBackups, // 保留文件数
			MaxAge:     config.MaxAge,     // 保留天数
			LocalTime:  true,              // 使用本地时间
			Compress:   true,              // 压缩旧文件
		}

		writers = append(writers, fileWriter)
	}

	// 设置多重输出
	Logger.SetOutput(io.MultiWriter(writers...))

	// 输出启动信息
	Logger.WithFields(logrus.Fields{
		"level":      config.Level,
		"file":       config.File,
		"max_size":   config.MaxSize,
		"max_age":    config.MaxAge,
		"timestamp":  time.Now().Format("2006-01-02 15:04:05"),
	}).Info("日志系统初始化完成")

	return nil
}

// ensureLogDir 确保日志目录存在
func ensureLogDir(logFile string) error {
	dir := filepath.Dir(logFile)
	if dir == "" || dir == "." {
		return nil
	}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	return nil
}

// LogEntry 创建带上下文的日志条目
type LogEntry struct {
	*logrus.Entry
}

// WithFields 添加额外字段
func (le *LogEntry) WithFields(fields map[string]interface{}) *LogEntry {
	return &LogEntry{
		Entry: le.Entry.WithFields(logrus.Fields(fields)),
	}
}

// WithContext 创建带上下文的日志条目
func WithContext(ctx map[string]interface{}) *LogEntry {
	fields := logrus.Fields{}
	for k, v := range ctx {
		fields[k] = v
	}
	
	return &LogEntry{
		Entry: Logger.WithFields(fields),
	}
}

// WithSite 创建带站点信息的日志条目
func WithSite(siteID string) *LogEntry {
	return &LogEntry{
		Entry: Logger.WithField("site_id", siteID),
	}
}

// WithClient 创建带客户端信息的日志条目
func WithClient(clientIP string, userAgent string) *LogEntry {
	return &LogEntry{
		Entry: Logger.WithFields(logrus.Fields{
			"client_ip":  clientIP,
			"user_agent": userAgent,
		}),
	}
}

// 重新定义LogEntry的方法以支持可选的fields参数
func (le *LogEntry) Info(msg string, fields ...map[string]interface{}) {
	if len(fields) > 0 && len(fields[0]) > 0 {
		le.Entry.WithFields(logrus.Fields(fields[0])).Info(msg)
	} else {
		le.Entry.Info(msg)
	}
}

func (le *LogEntry) Warn(msg string, fields ...map[string]interface{}) {
	if len(fields) > 0 && len(fields[0]) > 0 {
		le.Entry.WithFields(logrus.Fields(fields[0])).Warn(msg)
	} else {
		le.Entry.Warn(msg)
	}
}

func (le *LogEntry) Error(msg string, fields ...map[string]interface{}) {
	if len(fields) > 0 && len(fields[0]) > 0 {
		le.Entry.WithFields(logrus.Fields(fields[0])).Error(msg)
	} else {
		le.Entry.Error(msg)
	}
}

func (le *LogEntry) Debug(msg string, fields ...map[string]interface{}) {
	if len(fields) > 0 && len(fields[0]) > 0 {
		le.Entry.WithFields(logrus.Fields(fields[0])).Debug(msg)
	} else {
		le.Entry.Debug(msg)
	}
}

// WithError 创建带错误信息的日志条目
func WithError(err error) *LogEntry {
	return &LogEntry{
		Entry: Logger.WithError(err),
	}
}

// Performance 性能日志结构
type Performance struct {
	Operation string        `json:"operation"`
	Duration  time.Duration `json:"duration"`
	Success   bool          `json:"success"`
	Details   interface{}   `json:"details,omitempty"`
}

// LogPerformance 记录性能日志
func LogPerformance(perf Performance) {
	Logger.WithFields(logrus.Fields{
		"type":      "performance",
		"operation": perf.Operation,
		"duration":  perf.Duration.String(),
		"duration_ms": perf.Duration.Milliseconds(),
		"success":   perf.Success,
		"details":   perf.Details,
	}).Info("性能统计")
}

// LogWebSocketEvent 记录WebSocket事件日志
func LogWebSocketEvent(event string, siteID string, clientIP string, details interface{}) {
	Logger.WithFields(logrus.Fields{
		"type":      "websocket",
		"event":     event,
		"site_id":   siteID,
		"client_ip": clientIP,
		"details":   details,
	}).Info("WebSocket事件")
}

// LogSiteStats 记录站点统计日志
func LogSiteStats(siteID string, onlineCount int, connectionCount int) {
	Logger.WithFields(logrus.Fields{
		"type":             "stats",
		"site_id":          siteID,
		"online_count":     onlineCount,
		"connection_count": connectionCount,
		"timestamp":        time.Now().Unix(),
	}).Info("站点统计")
}

// LogSystemStats 记录系统统计日志
func LogSystemStats(stats interface{}) {
	Logger.WithFields(logrus.Fields{
		"type":      "system",
		"stats":     stats,
		"timestamp": time.Now().Unix(),
	}).Info("系统统计")
}

// Emergency 紧急情况日志（会写入标准错误）
func Emergency(msg string, fields map[string]interface{}) {
	entry := Logger.WithFields(logrus.Fields(fields))
	entry.Error(msg)
	
	// 同时写入标准错误，确保紧急信息不会丢失
	log.Printf("[EMERGENCY] %s %+v", msg, fields)
}