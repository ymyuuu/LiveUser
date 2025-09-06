package main

import (
	_ "embed"
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"liveuser/pkg/connection"
	"liveuser/pkg/core"
	"liveuser/pkg/utils"
	"liveuser/pkg/web"

	"gopkg.in/yaml.v3"
)

// 版本信息
var Version = "2.0.0-重构版"

// 嵌入静态文件
//go:embed static/liveuser.js
var jsContent string

//go:embed static/demo.html
var demoContent string

// 命令行参数
var (
	configPath = flag.String("config", "config/config.yaml", "配置文件路径")
	addr       = flag.String("addr", "", "监听地址 (覆盖配置文件)")
	version    = flag.Bool("version", false, "显示版本信息")
	debug      = flag.Bool("debug", false, "启用调试模式")
)

func main() {
	flag.Parse()

	// 显示版本信息
	if *version {
		fmt.Printf("LiveUser %s\n", Version)
		fmt.Printf("一个高性能的实时在线用户统计服务\n")
		fmt.Printf("重构版本解决了计数不准、连接不稳定等问题\n\n")
		fmt.Printf("特性:\n")
		fmt.Printf("  ✓ 智能IP去重，解决CDN多层代理问题\n")
		fmt.Printf("  ✓ 模块化架构，易于维护和扩展\n")
		fmt.Printf("  ✓ 高性能并发处理\n")
		fmt.Printf("  ✓ 完善的错误处理和恢复机制\n")
		fmt.Printf("  ✓ 详细的监控和统计功能\n")
		return
	}

	// 加载配置
	config, err := loadConfig(*configPath)
	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		os.Exit(1)
	}

	// 处理命令行覆盖
	if *addr != "" {
		parts := strings.Split(*addr, ":")
		if len(parts) == 2 {
			config.Server.Host = parts[0]
			if parts[1] != "" {
				var port int
				if _, err := fmt.Sscanf(parts[1], "%d", &port); err == nil {
					config.Server.Port = port
				}
			}
		}
	}

	if *debug {
		config.Logging.Level = "debug"
	}

	// 验证配置
	if err := config.Validate(); err != nil {
		fmt.Printf("配置验证失败: %v\n", err)
		os.Exit(1)
	}

	// 初始化日志系统
	if err := utils.InitLogger(config.Logging); err != nil {
		fmt.Printf("初始化日志系统失败: %v\n", err)
		os.Exit(1)
	}

	utils.Logger.Info("LiveUser 启动", map[string]interface{}{
		"version": Version,
		"config":  *configPath,
		"address": config.GetAddress(),
	})

	// 创建核心管理器
	manager := core.NewManager(config)

	// 创建WebSocket Hub
	hub := connection.NewHub(manager, config)

	// 创建Web处理器
	handler, err := web.NewHandler(hub, manager, config, jsContent, demoContent)
	if err != nil {
		utils.Logger.Fatal("创建Web处理器失败", map[string]interface{}{
			"error": err.Error(),
		})
	}

	// 创建HTTP服务器
	server := &http.Server{
		Addr:         config.GetAddress(),
		Handler:      handler,
		ReadTimeout:  config.Server.ReadTimeout,
		WriteTimeout: config.Server.WriteTimeout,
		IdleTimeout:  config.Server.IdleTimeout,
	}

	// 启动服务器
	go func() {
		utils.Logger.Info("HTTP服务器启动", map[string]interface{}{
			"address":       server.Addr,
			"read_timeout":  config.Server.ReadTimeout.String(),
			"write_timeout": config.Server.WriteTimeout.String(),
			"idle_timeout":  config.Server.IdleTimeout.String(),
		})

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			utils.Logger.Fatal("HTTP服务器启动失败", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()

	// 输出启动信息到控制台
	fmt.Printf("🚀 LiveUser %s 启动成功!\n", Version)
	fmt.Printf("📍 服务地址: http://%s\n", config.GetAddress())
	fmt.Printf("🌐 演示页面: http://%s/\n", config.GetAddress())
	fmt.Printf("📊 统计API: http://%s/stats\n", config.GetAddress())
	fmt.Printf("🔍 健康检查: http://%s/health\n", config.GetAddress())
	fmt.Printf("📝 日志级别: %s\n", config.Logging.Level)
	if config.Logging.File != "" {
		fmt.Printf("📁 日志文件: %s\n", config.Logging.File)
	}
	fmt.Printf("⚙️  最大站点: %d\n", config.Analytics.MaxSites)
	fmt.Printf("🔗 最大连接: %d/站点\n", config.Analytics.MaxConnections)
	fmt.Printf("\n按 Ctrl+C 停止服务\n\n")

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	sig := <-quit
	utils.Logger.Info("收到停止信号", map[string]interface{}{
		"signal": sig.String(),
	})

	fmt.Printf("\n🛑 收到停止信号，正在优雅关闭服务...\n")

	// 创建关闭上下文
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 关闭各个组件
	go func() {
		hub.Shutdown()
		manager.Shutdown()
	}()

	// 关闭HTTP服务器
	if err := server.Shutdown(ctx); err != nil {
		utils.Logger.Error("HTTP服务器关闭失败", map[string]interface{}{
			"error": err.Error(),
		})
		fmt.Printf("❌ HTTP服务器关闭失败: %v\n", err)
	} else {
		utils.Logger.Info("HTTP服务器已关闭")
		fmt.Printf("✅ HTTP服务器已关闭\n")
	}

	fmt.Printf("✅ LiveUser 已完全停止\n")
}

// loadConfig 加载配置文件
func loadConfig(configPath string) (*utils.Config, error) {
	// 首先使用默认配置
	config := utils.DefaultConfig()

	// 检查配置文件是否存在
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Printf("⚠️  配置文件不存在: %s，使用默认配置\n", configPath)
		config.LoadFromEnv()
		return config, nil
	}

	// 读取配置文件
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 解析YAML配置
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 从环境变量加载覆盖配置
	config.LoadFromEnv()

	// 确保日志目录存在
	if config.Logging.File != "" {
		logDir := filepath.Dir(config.Logging.File)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			fmt.Printf("⚠️  创建日志目录失败: %v，将只输出到控制台\n", err)
			config.Logging.File = ""
		}
	}

	fmt.Printf("✅ 配置文件加载成功: %s\n", configPath)
	return config, nil
}

// 程序入口点信息
func init() {
	// 设置默认时区
	os.Setenv("TZ", "Asia/Shanghai")

	// 打印启动banner
	fmt.Printf(`
╔══════════════════════════════════════════════╗
║              LiveUser 2.0 重构版              ║
║          实时在线用户统计服务                  ║
║                                              ║
║  🎯 智能去重  🚀 高性能  🔄 稳定连接         ║
║  📊 实时监控  🛡️ 错误处理  🎨 现代前端       ║
╚══════════════════════════════════════════════╝

`)
}