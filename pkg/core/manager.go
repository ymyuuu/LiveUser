package core

import (
	"context"
	"fmt"
	"sync"
	"time"
	"liveuser/pkg/utils"
)

// Manager 核心管理器，负责管理所有站点和连接
type Manager struct {
	sites           map[string]*Site      // 站点映射
	config          *utils.Config         // 配置
	ctx             context.Context       // 上下文
	cancel          context.CancelFunc    // 取消函数
	mutex           sync.RWMutex          // 读写锁
	startTime       time.Time             // 启动时间
	messageChannel  chan *Message         // 消息通道
	cleanupTicker   *time.Ticker          // 清理定时器
}

// Message 消息结构
type Message struct {
	Type      string      `json:"type"`
	SiteID    string      `json:"siteId,omitempty"`
	Count     int         `json:"count,omitempty"`
	Message   string      `json:"message,omitempty"`
	Timestamp int64       `json:"timestamp"`
	Data      interface{} `json:"data,omitempty"`
}

// MessageType 消息类型常量
const (
	MessageTypeUpdate   = "update"   // 人数更新
	MessageTypeShutdown = "shutdown" // 服务关闭
	MessageTypeError    = "error"    // 错误信息
	MessageTypeStats    = "stats"    // 统计信息
)

// NewManager 创建新的管理器
func NewManager(config *utils.Config) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	
	manager := &Manager{
		sites:          make(map[string]*Site),
		config:         config,
		ctx:            ctx,
		cancel:         cancel,
		startTime:      time.Now(),
		messageChannel: make(chan *Message, 1000), // 缓冲1000条消息
		cleanupTicker:  time.NewTicker(config.Analytics.CleanupInterval),
	}

	// 启动后台协程
	go manager.runCleanupTask()
	go manager.runStatsTask()

	utils.Logger.Info("核心管理器初始化完成", map[string]interface{}{
		"max_sites":        config.Analytics.MaxSites,
		"max_connections":  config.Analytics.MaxConnections,
		"cleanup_interval": config.Analytics.CleanupInterval.String(),
	})

	return manager
}

// GetSite 获取或创建站点
func (m *Manager) GetSite(siteID string) (*Site, error) {
	if siteID == "" {
		return nil, fmt.Errorf("站点ID不能为空")
	}

	m.mutex.RLock()
	site, exists := m.sites[siteID]
	m.mutex.RUnlock()

	if exists {
		return site, nil
	}

	// 需要创建新站点
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 再次检查（双重检查锁定）
	if site, exists := m.sites[siteID]; exists {
		return site, nil
	}

	// 检查站点数量限制
	if len(m.sites) >= m.config.Analytics.MaxSites {
		return nil, fmt.Errorf("站点数量已达到最大限制: %d", m.config.Analytics.MaxSites)
	}

	// 创建新站点
	site = NewSite(siteID, m.config.Analytics.SessionTimeout)
	m.sites[siteID] = site

	utils.WithSite(siteID).Info("创建新站点", map[string]interface{}{
		"total_sites": len(m.sites),
	})

	return site, nil
}

// AddConnection 添加新连接到指定站点
func (m *Manager) AddConnection(siteID, connID string, ipInfo *utils.IPInfo, userAgent, referer string) error {
	site, err := m.GetSite(siteID)
	if err != nil {
		return fmt.Errorf("获取站点失败: %w", err)
	}

	// 检查站点连接数限制
	if site.GetConnectionCount() >= m.config.Analytics.MaxConnections {
		return fmt.Errorf("站点连接数已达到最大限制: %d", m.config.Analytics.MaxConnections)
	}

	// 添加连接
	isUnique := site.AddConnection(connID, ipInfo, userAgent, referer)
	
	// 如果是独特用户，发送更新消息
	if isUnique {
		m.broadcastUpdate(siteID, site.GetOnlineCount())
	}

	return nil
}

// RemoveConnection 从站点移除连接
func (m *Manager) RemoveConnection(siteID, connID string) error {
	m.mutex.RLock()
	site, exists := m.sites[siteID]
	m.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("站点不存在: %s", siteID)
	}

	// 移除连接
	shouldDecrease := site.RemoveConnection(connID)
	
	// 如果需要减少计数，发送更新消息
	if shouldDecrease {
		m.broadcastUpdate(siteID, site.GetOnlineCount())
	}

	// 如果站点没有连接了，考虑清理
	if site.GetConnectionCount() == 0 {
		m.considerSiteCleanup(siteID, site)
	}

	return nil
}

// UpdateConnection 更新连接活跃时间
func (m *Manager) UpdateConnection(siteID, connID string) error {
	m.mutex.RLock()
	site, exists := m.sites[siteID]
	m.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("站点不存在: %s", siteID)
	}

	site.UpdateConnection(connID)
	return nil
}

// GetSiteStats 获取站点统计信息
func (m *Manager) GetSiteStats(siteID string) (SiteStats, error) {
	m.mutex.RLock()
	site, exists := m.sites[siteID]
	m.mutex.RUnlock()

	if !exists {
		return SiteStats{}, fmt.Errorf("站点不存在: %s", siteID)
	}

	return site.GetStats(), nil
}

// GetSystemStats 获取系统统计信息
func (m *Manager) GetSystemStats() map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	totalOnline := 0
	totalConnections := 0
	totalVisits := int64(0)
	siteInfos := make([]map[string]interface{}, 0, len(m.sites))

	for _, site := range m.sites {
		stats := site.GetStats()
		totalOnline += site.GetOnlineCount()
		totalConnections += site.GetConnectionCount()
		totalVisits += stats.TotalVisits

		siteInfos = append(siteInfos, map[string]interface{}{
			"id":           site.ID,
			"online":       site.GetOnlineCount(),
			"connections":  site.GetConnectionCount(),
			"total_visits": stats.TotalVisits,
			"peak_online":  stats.PeakOnlineCount,
		})
	}

	return map[string]interface{}{
		"uptime":            time.Since(m.startTime).String(),
		"total_sites":       len(m.sites),
		"total_online":      totalOnline,
		"total_connections": totalConnections,
		"total_visits":      totalVisits,
		"sites":             siteInfos,
		"config": map[string]interface{}{
			"max_sites":        m.config.Analytics.MaxSites,
			"max_connections":  m.config.Analytics.MaxConnections,
			"session_timeout":  m.config.Analytics.SessionTimeout.String(),
		},
	}
}

// broadcastUpdate 广播更新消息
func (m *Manager) broadcastUpdate(siteID string, count int) {
	message := &Message{
		Type:      MessageTypeUpdate,
		SiteID:    siteID,
		Count:     count,
		Timestamp: time.Now().Unix(),
	}

	// 非阻塞发送
	select {
	case m.messageChannel <- message:
		utils.LogSiteStats(siteID, count, 0)
	default:
		utils.Logger.Warn("消息通道满，跳过更新消息", map[string]interface{}{
			"site_id": siteID,
			"count":   count,
		})
	}
}

// GetMessageChannel 获取消息通道（用于WebSocket广播）
func (m *Manager) GetMessageChannel() <-chan *Message {
	return m.messageChannel
}

// considerSiteCleanup 考虑清理站点
func (m *Manager) considerSiteCleanup(siteID string, site *Site) {
	// 如果站点空闲时间超过阈值，则清理
	idleThreshold := m.config.Analytics.CleanupInterval * 2
	if time.Since(site.LastActivity) > idleThreshold {
		m.mutex.Lock()
		defer m.mutex.Unlock()

		// 再次检查（可能在等待锁的过程中有新连接）
		if site.GetConnectionCount() == 0 && time.Since(site.LastActivity) > idleThreshold {
			delete(m.sites, siteID)
			utils.WithSite(siteID).Info("清理空闲站点", map[string]interface{}{
				"idle_time": time.Since(site.LastActivity).String(),
				"remaining_sites": len(m.sites),
			})
		}
	}
}

// runCleanupTask 运行清理任务
func (m *Manager) runCleanupTask() {
	defer m.cleanupTicker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.cleanupTicker.C:
			m.performCleanup()
		}
	}
}

// performCleanup 执行清理操作
func (m *Manager) performCleanup() {
	startTime := time.Now()
	
	m.mutex.RLock()
	sites := make([]*Site, 0, len(m.sites))
	for _, site := range m.sites {
		sites = append(sites, site)
	}
	m.mutex.RUnlock()

	totalCleaned := 0
	activeTimeout := m.config.WebSocket.PongTimeout

	for _, site := range sites {
		cleaned := site.CleanupInactiveConnections(activeTimeout)
		totalCleaned += cleaned
		
		// 检查是否需要清理站点
		if site.GetConnectionCount() == 0 {
			m.considerSiteCleanup(site.ID, site)
		}
	}

	duration := time.Since(startTime)
	
	if totalCleaned > 0 {
		utils.LogPerformance(utils.Performance{
			Operation: "cleanup",
			Duration:  duration,
			Success:   true,
			Details: map[string]interface{}{
				"cleaned_connections": totalCleaned,
				"active_sites":       len(sites),
			},
		})
	}
}

// runStatsTask 运行统计任务
func (m *Manager) runStatsTask() {
	ticker := time.NewTicker(60 * time.Second) // 每分钟记录一次统计
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			stats := m.GetSystemStats()
			utils.LogSystemStats(stats)
		}
	}
}

// Shutdown 优雅关闭管理器
func (m *Manager) Shutdown() {
	utils.Logger.Info("开始关闭核心管理器")
	
	// 发送关闭消息到所有连接
	shutdownMsg := &Message{
		Type:      MessageTypeShutdown,
		Message:   "服务器维护中，请稍后重连",
		Timestamp: time.Now().Unix(),
	}
	
	// 尝试发送关闭消息
	select {
	case m.messageChannel <- shutdownMsg:
	default:
		utils.Logger.Warn("无法发送关闭消息，通道可能已满")
	}

	// 取消上下文，停止所有后台任务
	m.cancel()

	// 等待一小段时间让消息处理完
	time.Sleep(100 * time.Millisecond)

	// 关闭消息通道
	close(m.messageChannel)

	// 记录最终统计
	finalStats := m.GetSystemStats()
	utils.Logger.Info("核心管理器关闭完成", map[string]interface{}{
		"final_stats": finalStats,
	})
}