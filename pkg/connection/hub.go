package connection

import (
	"context"
	"net/http"
	"sync"
	"time"
	"liveuser/pkg/core"
	"liveuser/pkg/utils"
	"github.com/gorilla/websocket"
)

// Hub WebSocket连接中心
type Hub struct {
	clients      map[*Client]bool      // 活跃客户端
	register     chan *Client          // 注册通道
	unregister   chan *Client          // 注销通道
	broadcast    chan *core.Message    // 广播通道
	manager      *core.Manager         // 核心管理器
	config       *utils.Config         // 配置
	ipTracker    *utils.IPTracker      // IP追踪器
	upgrader     *websocket.Upgrader   // WebSocket升级器
	ctx          context.Context       // 上下文
	cancel       context.CancelFunc    // 取消函数
	mutex        sync.RWMutex          // 读写锁
	startTime    time.Time             // 启动时间
	stats        *HubStats             // Hub统计信息
}

// HubStats Hub统计信息
type HubStats struct {
	TotalConnections    int64 // 总连接数
	CurrentConnections  int   // 当前连接数
	PeakConnections     int   // 峰值连接数
	TotalMessages       int64 // 总消息数
	BroadcastMessages   int64 // 广播消息数
	FailedConnections   int64 // 失败连接数
	mutex               sync.RWMutex
}

// NewHub 创建新的Hub
func NewHub(manager *core.Manager, config *utils.Config) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	
	hub := &Hub{
		clients:     make(map[*Client]bool),
		register:    make(chan *Client, 100),
		unregister:  make(chan *Client, 100),
		broadcast:   make(chan *core.Message, 1000),
		manager:     manager,
		config:      config,
		ipTracker:   utils.NewIPTracker(config.Analytics.SessionTimeout),
		upgrader: &websocket.Upgrader{
			ReadBufferSize:  config.WebSocket.ReadBufferSize,
			WriteBufferSize: config.WebSocket.WriteBufferSize,
			CheckOrigin: func(r *http.Request) bool {
				// 这里可以添加更严格的源检查
				return true
			},
		},
		ctx:       ctx,
		cancel:    cancel,
		startTime: time.Now(),
		stats:     &HubStats{},
	}

	// 启动Hub协程
	go hub.run()
	go hub.listenForMessages()

	utils.Logger.Info("WebSocket Hub初始化完成", map[string]interface{}{
		"read_buffer_size":  config.WebSocket.ReadBufferSize,
		"write_buffer_size": config.WebSocket.WriteBufferSize,
		"ping_period":       config.WebSocket.PingPeriod.String(),
		"pong_timeout":      config.WebSocket.PongTimeout.String(),
	})

	return hub
}

// HandleWebSocket 处理WebSocket连接请求
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 检查连接数限制
	if h.GetCurrentConnections() >= h.config.Analytics.MaxConnections {
		utils.WithContext(map[string]interface{}{
			"remote_addr": r.RemoteAddr,
			"user_agent":  r.Header.Get("User-Agent"),
		}).Warn("连接数达到上限，拒绝新连接")
		
		http.Error(w, "连接数达到上限", http.StatusServiceUnavailable)
		h.stats.mutex.Lock()
		h.stats.FailedConnections++
		h.stats.mutex.Unlock()
		return
	}

	// 升级到WebSocket
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		utils.WithError(err).Error("WebSocket升级失败", map[string]interface{}{
			"remote_addr": r.RemoteAddr,
		})
		
		h.stats.mutex.Lock()
		h.stats.FailedConnections++
		h.stats.mutex.Unlock()
		return
	}

	// 创建客户端
	client := NewClient(conn, h, h.manager, r)
	
	// 注册客户端
	h.register <- client

	// 启动客户端处理（阻塞直到连接关闭）
	client.Start()
}

// run Hub主循环
func (h *Hub) run() {
	defer func() {
		// 关闭所有客户端
		h.mutex.RLock()
		clients := make([]*Client, 0, len(h.clients))
		for client := range h.clients {
			clients = append(clients, client)
		}
		h.mutex.RUnlock()

		for _, client := range clients {
			client.setInactive()
		}
		
		utils.Logger.Info("Hub主循环结束")
	}()

	for {
		select {
		case <-h.ctx.Done():
			return

		case client := <-h.register:
			h.handleRegister(client)

		case client := <-h.unregister:
			h.handleUnregister(client)

		case message := <-h.broadcast:
			h.handleBroadcast(message)
		}
	}
}

// handleRegister 处理客户端注册
func (h *Hub) handleRegister(client *Client) {
	h.mutex.Lock()
	h.clients[client] = true
	currentConnections := len(h.clients)
	h.mutex.Unlock()

	// 更新统计
	h.stats.mutex.Lock()
	h.stats.TotalConnections++
	h.stats.CurrentConnections = currentConnections
	if currentConnections > h.stats.PeakConnections {
		h.stats.PeakConnections = currentConnections
	}
	h.stats.mutex.Unlock()

	utils.Logger.Info("客户端注册成功", map[string]interface{}{
		"client_id":           client.ID,
		"client_ip":           client.ipInfo.RealIP,
		"current_connections": currentConnections,
		"fingerprint":         client.ipInfo.Fingerprint,
	})
}

// handleUnregister 处理客户端注销
func (h *Hub) handleUnregister(client *Client) {
	h.mutex.Lock()
	if _, exists := h.clients[client]; exists {
		delete(h.clients, client)
		currentConnections := len(h.clients)
		h.mutex.Unlock()

		// 更新统计
		h.stats.mutex.Lock()
		h.stats.CurrentConnections = currentConnections
		h.stats.mutex.Unlock()

		utils.Logger.Info("客户端注销成功", map[string]interface{}{
			"client_id":           client.ID,
			"client_ip":           client.ipInfo.RealIP,
			"current_connections": currentConnections,
			"online_time":         time.Since(client.joinedAt).String(),
		})
	} else {
		h.mutex.Unlock()
	}
}

// handleBroadcast 处理广播消息
func (h *Hub) handleBroadcast(message *core.Message) {
	startTime := time.Now()
	sentCount := 0
	failedCount := 0

	h.mutex.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for client := range h.clients {
		// 只向相关站点的客户端发送消息
		if message.SiteID == "" || client.siteID == message.SiteID {
			clients = append(clients, client)
		}
	}
	h.mutex.RUnlock()

	// 并发发送消息
	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Add(1)
		go func(c *Client) {
			defer wg.Done()
			if c.SendMessage(message) {
				sentCount++
			} else {
				failedCount++
			}
		}(client)
	}
	wg.Wait()

	// 更新统计
	h.stats.mutex.Lock()
	h.stats.BroadcastMessages++
	h.stats.mutex.Unlock()

	duration := time.Since(startTime)

	utils.Logger.Debug("广播消息完成", map[string]interface{}{
		"message_type": message.Type,
		"site_id":      message.SiteID,
		"sent_count":   sentCount,
		"failed_count": failedCount,
		"duration":     duration.String(),
	})

	// 记录性能日志
	if duration > 100*time.Millisecond {
		utils.LogPerformance(utils.Performance{
			Operation: "broadcast",
			Duration:  duration,
			Success:   failedCount == 0,
			Details: map[string]interface{}{
				"message_type": message.Type,
				"site_id":      message.SiteID,
				"sent_count":   sentCount,
				"failed_count": failedCount,
			},
		})
	}
}

// listenForMessages 监听管理器的消息
func (h *Hub) listenForMessages() {
	messageChannel := h.manager.GetMessageChannel()
	
	for message := range messageChannel {
		select {
		case h.broadcast <- message:
		case <-h.ctx.Done():
			return
		default:
			utils.Logger.Warn("广播通道满，跳过消息", map[string]interface{}{
				"message_type": message.Type,
				"site_id":      message.SiteID,
			})
		}
	}

	utils.Logger.Info("停止监听管理器消息")
}

// Broadcast 发送广播消息
func (h *Hub) Broadcast(message *core.Message) {
	select {
	case h.broadcast <- message:
	default:
		utils.Logger.Warn("广播队列满", map[string]interface{}{
			"message_type": message.Type,
			"site_id":      message.SiteID,
		})
	}
}

// GetCurrentConnections 获取当前连接数
func (h *Hub) GetCurrentConnections() int {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return len(h.clients)
}

// GetStats 获取Hub统计信息
func (h *Hub) GetStats() map[string]interface{} {
	h.stats.mutex.RLock()
	defer h.stats.mutex.RUnlock()

	currentConnections := h.GetCurrentConnections()
	uptime := time.Since(h.startTime)

	return map[string]interface{}{
		"uptime":              uptime.String(),
		"current_connections": currentConnections,
		"total_connections":   h.stats.TotalConnections,
		"peak_connections":    h.stats.PeakConnections,
		"total_messages":      h.stats.TotalMessages,
		"broadcast_messages":  h.stats.BroadcastMessages,
		"failed_connections":  h.stats.FailedConnections,
		"register_queue_len":  len(h.register),
		"unregister_queue_len": len(h.unregister),
		"broadcast_queue_len": len(h.broadcast),
	}
}

// GetHealthyClients 获取健康的客户端列表
func (h *Hub) GetHealthyClients() []*Client {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	var healthyClients []*Client
	for client := range h.clients {
		if client.IsHealthy() {
			healthyClients = append(healthyClients, client)
		}
	}

	return healthyClients
}

// CleanupUnhealthyClients 清理不健康的客户端
func (h *Hub) CleanupUnhealthyClients() int {
	h.mutex.RLock()
	unhealthyClients := make([]*Client, 0)
	for client := range h.clients {
		if !client.IsHealthy() {
			unhealthyClients = append(unhealthyClients, client)
		}
	}
	h.mutex.RUnlock()

	// 关闭不健康的连接
	for _, client := range unhealthyClients {
		client.setInactive()
		h.unregister <- client
	}

	if len(unhealthyClients) > 0 {
		utils.Logger.Info("清理不健康客户端", map[string]interface{}{
			"cleaned_count": len(unhealthyClients),
		})
	}

	return len(unhealthyClients)
}

// GetClientsBysite 获取指定站点的客户端
func (h *Hub) GetClientsBySite(siteID string) []*Client {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	var siteClients []*Client
	for client := range h.clients {
		if client.siteID == siteID {
			siteClients = append(siteClients, client)
		}
	}

	return siteClients
}

// Shutdown 优雅关闭Hub
func (h *Hub) Shutdown() {
	utils.Logger.Info("开始关闭WebSocket Hub")

	// 发送关闭信号
	h.cancel()

	// 关闭通道
	close(h.register)
	close(h.unregister)
	close(h.broadcast)

	// 等待一段时间让消息处理完
	time.Sleep(100 * time.Millisecond)

	// 记录最终统计
	finalStats := h.GetStats()
	utils.Logger.Info("WebSocket Hub关闭完成", map[string]interface{}{
		"final_stats": finalStats,
	})
}