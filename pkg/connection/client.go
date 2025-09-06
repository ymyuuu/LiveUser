package connection

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
	"liveuser/pkg/core"
	"liveuser/pkg/utils"
	"github.com/gorilla/websocket"
	"github.com/google/uuid"
)

// Client WebSocket客户端连接
type Client struct {
	ID          string                 // 客户端唯一ID
	conn        *websocket.Conn        // WebSocket连接
	hub         *Hub                   // 所属Hub
	manager     *core.Manager          // 核心管理器
	siteID      string                 // 所属站点ID
	ipInfo      *utils.IPInfo          // IP信息
	userAgent   string                 // 用户代理
	referer     string                 // 来源页面
	send        chan *core.Message     // 发送消息通道
	joinedAt    time.Time              // 加入时间
	lastPong    time.Time              // 最后Pong时间
	isActive    bool                   // 是否活跃
	mutex       sync.RWMutex           // 读写锁
}

// ClientMessage 客户端消息结构
type ClientMessage struct {
	Type      string      `json:"type"`
	SiteID    string      `json:"siteId,omitempty"`
	Data      interface{} `json:"data,omitempty"`
	Timestamp int64       `json:"timestamp,omitempty"`
}

// 消息类型常量
const (
	ClientMessageTypeJoin = "join"
	ClientMessageTypePing = "ping"
	ClientMessageTypePong = "pong"
)

// NewClient 创建新的客户端连接
func NewClient(conn *websocket.Conn, hub *Hub, manager *core.Manager, r *http.Request) *Client {
	now := time.Now()
	
	client := &Client{
		ID:        uuid.New().String(),
		conn:      conn,
		hub:       hub,
		manager:   manager,
		ipInfo:    hub.ipTracker.Track(r),
		userAgent: r.Header.Get("User-Agent"),
		referer:   r.Header.Get("Referer"),
		send:      make(chan *core.Message, 256),
		joinedAt:  now,
		lastPong:  now,
		isActive:  true,
	}

	utils.WithClient(client.ipInfo.RealIP, client.userAgent).Info("创建新客户端", map[string]interface{}{
		"client_id":   client.ID,
		"fingerprint": client.ipInfo.Fingerprint,
		"referer":     client.referer,
	})

	return client
}

// Start 启动客户端处理
func (c *Client) Start() {
	defer func() {
		c.cleanup()
	}()

	// 启动读写协程
	go c.writePump()
	c.readPump()
}

// readPump 处理从客户端读取消息
func (c *Client) readPump() {
	defer func() {
		c.setInactive()
		c.conn.Close()
	}()

	// 设置读取限制和超时
	c.conn.SetReadLimit(c.hub.config.WebSocket.MaxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(c.hub.config.WebSocket.PongTimeout))
	
	// 设置Pong处理器
	c.conn.SetPongHandler(func(string) error {
		c.mutex.Lock()
		c.lastPong = time.Now()
		c.mutex.Unlock()
		
		c.conn.SetReadDeadline(time.Now().Add(c.hub.config.WebSocket.PongTimeout))
		
		// 更新管理器中的连接活跃时间
		if c.siteID != "" {
			c.manager.UpdateConnection(c.siteID, c.ID)
		}
		
		return nil
	})

	for {
		// 检查是否还活跃
		if !c.IsActive() {
			break
		}

		_, messageData, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				utils.WithClient(c.ipInfo.RealIP, c.userAgent).Error("WebSocket读取错误", map[string]interface{}{
					"client_id": c.ID,
					"error":     err.Error(),
				})
			}
			break
		}

		// 解析消息
		var msg ClientMessage
		if err := json.Unmarshal(messageData, &msg); err != nil {
			utils.WithClient(c.ipInfo.RealIP, c.userAgent).Warn("消息解析失败", map[string]interface{}{
				"client_id": c.ID,
				"error":     err.Error(),
				"raw_data":  string(messageData),
			})
			continue
		}

		// 处理消息
		if err := c.handleMessage(&msg); err != nil {
			utils.WithClient(c.ipInfo.RealIP, c.userAgent).Error("消息处理失败", map[string]interface{}{
				"client_id": c.ID,
				"message_type": msg.Type,
				"error":     err.Error(),
			})
		}
	}
}

// writePump 处理向客户端发送消息
func (c *Client) writePump() {
	ticker := time.NewTicker(c.hub.config.WebSocket.PingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// 通道被关闭
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteJSON(message); err != nil {
				utils.WithClient(c.ipInfo.RealIP, c.userAgent).Error("发送消息失败", map[string]interface{}{
					"client_id": c.ID,
					"error":     err.Error(),
				})
				return
			}

		case <-ticker.C:
			// 发送Ping消息
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				utils.WithClient(c.ipInfo.RealIP, c.userAgent).Debug("Ping发送失败", map[string]interface{}{
					"client_id": c.ID,
					"error":     err.Error(),
				})
				return
			}
		}

		// 检查是否还活跃
		if !c.IsActive() {
			return
		}
	}
}

// handleMessage 处理客户端消息
func (c *Client) handleMessage(msg *ClientMessage) error {
	switch msg.Type {
	case ClientMessageTypeJoin:
		return c.handleJoinMessage(msg)
	case ClientMessageTypePing:
		return c.handlePingMessage(msg)
	default:
		return fmt.Errorf("未知消息类型: %s", msg.Type)
	}
}

// handleJoinMessage 处理加入消息
func (c *Client) handleJoinMessage(msg *ClientMessage) error {
	if msg.SiteID == "" {
		return fmt.Errorf("站点ID不能为空")
	}

	newSiteID := msg.SiteID
	
	// 如果客户端已经加入了其他站点，先退出
	if c.siteID != "" && c.siteID != newSiteID {
		if err := c.manager.RemoveConnection(c.siteID, c.ID); err != nil {
			utils.WithClient(c.ipInfo.RealIP, c.userAgent).Warn("退出旧站点失败", map[string]interface{}{
				"client_id": c.ID,
				"old_site_id": c.siteID,
				"error": err.Error(),
			})
		}
	}

	// 加入新站点
	if c.siteID != newSiteID {
		err := c.manager.AddConnection(newSiteID, c.ID, c.ipInfo, c.userAgent, c.referer)
		if err != nil {
			// 发送错误消息给客户端
			errorMsg := &core.Message{
				Type:      "error",
				Message:   fmt.Sprintf("加入站点失败: %s", err.Error()),
				Timestamp: time.Now().Unix(),
			}
			
			select {
			case c.send <- errorMsg:
			default:
				// 发送失败，记录日志
			}
			
			return err
		}

		c.siteID = newSiteID
		
		utils.WithClient(c.ipInfo.RealIP, c.userAgent).Info("客户端加入站点", map[string]interface{}{
			"client_id": c.ID,
			"site_id":   c.siteID,
		})
	}

	return nil
}

// handlePingMessage 处理Ping消息
func (c *Client) handlePingMessage(msg *ClientMessage) error {
	// 发送Pong响应
	pongMsg := &core.Message{
		Type:      "pong",
		Timestamp: time.Now().Unix(),
	}

	select {
	case c.send <- pongMsg:
		return nil
	default:
		return fmt.Errorf("发送队列已满")
	}
}

// SendMessage 向客户端发送消息
func (c *Client) SendMessage(message *core.Message) bool {
	if !c.IsActive() {
		return false
	}

	select {
	case c.send <- message:
		return true
	default:
		// 发送队列满了，客户端可能有问题
		utils.WithClient(c.ipInfo.RealIP, c.userAgent).Warn("发送队列满", map[string]interface{}{
			"client_id": c.ID,
			"site_id":   c.siteID,
		})
		return false
	}
}

// IsActive 检查客户端是否活跃
func (c *Client) IsActive() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.isActive
}

// setInactive 设置客户端为非活跃状态
func (c *Client) setInactive() {
	c.mutex.Lock()
	c.isActive = false
	c.mutex.Unlock()
}

// GetInfo 获取客户端信息
func (c *Client) GetInfo() map[string]interface{} {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return map[string]interface{}{
		"id":           c.ID,
		"site_id":      c.siteID,
		"ip":           c.ipInfo.RealIP,
		"fingerprint":  c.ipInfo.Fingerprint,
		"user_agent":   c.userAgent,
		"referer":      c.referer,
		"joined_at":    c.joinedAt,
		"last_pong":    c.lastPong,
		"is_active":    c.isActive,
		"online_time":  time.Since(c.joinedAt).String(),
	}
}

// cleanup 清理客户端资源
func (c *Client) cleanup() {
	c.setInactive()
	
	// 从站点中移除
	if c.siteID != "" {
		if err := c.manager.RemoveConnection(c.siteID, c.ID); err != nil {
			utils.WithClient(c.ipInfo.RealIP, c.userAgent).Error("清理时移除连接失败", map[string]interface{}{
				"client_id": c.ID,
				"site_id":   c.siteID,
				"error":     err.Error(),
			})
		}
	}

	// 关闭发送通道
	close(c.send)

	// 计算在线时长
	onlineTime := time.Since(c.joinedAt)
	
	utils.WithClient(c.ipInfo.RealIP, c.userAgent).Info("客户端清理完成", map[string]interface{}{
		"client_id":   c.ID,
		"site_id":     c.siteID,
		"online_time": onlineTime.String(),
	})
}

// IsHealthy 检查客户端健康状态
func (c *Client) IsHealthy() bool {
	if !c.IsActive() {
		return false
	}

	c.mutex.RLock()
	lastPong := c.lastPong
	c.mutex.RUnlock()

	// 检查是否超时
	timeout := c.hub.config.WebSocket.PongTimeout
	return time.Since(lastPong) <= timeout
}

// GetStats 获取客户端统计信息
func (c *Client) GetStats() map[string]interface{} {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	stats := map[string]interface{}{
		"online_duration": time.Since(c.joinedAt).String(),
		"last_pong_ago":   time.Since(c.lastPong).String(),
		"send_queue_len":  len(c.send),
		"send_queue_cap":  cap(c.send),
		"is_healthy":      c.IsHealthy(),
	}

	return stats
}