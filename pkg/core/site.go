package core

import (
	"sync"
	"time"
	"liveuser/pkg/utils"
)

// Site 站点信息结构
type Site struct {
	ID              string                    // 站点唯一ID
	Name            string                    // 站点名称（可选）
	OnlineCount     int                       // 在线人数
	Connections     map[string]*Connection    // 活跃连接映射 (连接ID -> 连接)
	IPTracker       *utils.IPTracker          // IP追踪器
	CreatedAt       time.Time                 // 创建时间
	LastActivity    time.Time                 // 最后活动时间
	Stats           *SiteStats                // 站点统计
	mutex           sync.RWMutex              // 读写锁
}

// Connection 连接信息
type Connection struct {
	ID          string      // 连接唯一ID
	IPInfo      *utils.IPInfo // IP信息
	JoinedAt    time.Time   // 加入时间
	LastPing    time.Time   // 最后Ping时间
	UserAgent   string      // 用户代理
	Referer     string      // 来源页面
}

// SiteStats 站点统计信息
type SiteStats struct {
	TotalVisits       int64     // 总访问量
	UniqueVisitors    int64     // 独立访客数
	PeakOnlineCount   int       // 峰值在线人数
	PeakTime          time.Time // 峰值时间
	AverageStayTime   time.Duration // 平均停留时间
	ConnectionsToday  int64     // 今日连接数
	LastReset         time.Time // 统计重置时间
}

// NewSite 创建新站点
func NewSite(id string, sessionTimeout time.Duration) *Site {
	now := time.Now()
	return &Site{
		ID:           id,
		OnlineCount:  0,
		Connections:  make(map[string]*Connection),
		IPTracker:    utils.NewIPTracker(sessionTimeout),
		CreatedAt:    now,
		LastActivity: now,
		Stats: &SiteStats{
			LastReset: now,
		},
	}
}

// AddConnection 添加新连接
func (s *Site) AddConnection(connID string, ipInfo *utils.IPInfo, userAgent, referer string) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	now := time.Now()
	s.LastActivity = now

	// 检查连接是否已存在
	if _, exists := s.Connections[connID]; exists {
		utils.WithSite(s.ID).Warn("尝试添加已存在的连接", map[string]interface{}{
			"connection_id": connID,
			"ip": ipInfo.RealIP,
		})
		return false
	}

	// 创建新连接
	conn := &Connection{
		ID:        connID,
		IPInfo:    ipInfo,
		JoinedAt:  now,
		LastPing:  now,
		UserAgent: userAgent,
		Referer:   referer,
	}

	s.Connections[connID] = conn
	
	// 检查是否为独特用户（用于计数）
	isUnique := s.IPTracker.IsUnique(ipInfo.Fingerprint, now)
	if isUnique {
		s.OnlineCount++
		s.Stats.UniqueVisitors++
		
		// 更新峰值记录
		if s.OnlineCount > s.Stats.PeakOnlineCount {
			s.Stats.PeakOnlineCount = s.OnlineCount
			s.Stats.PeakTime = now
		}
	}

	// 更新统计
	s.Stats.TotalVisits++
	if s.isToday(now) {
		s.Stats.ConnectionsToday++
	}

	utils.LogWebSocketEvent("connection_added", s.ID, ipInfo.RealIP, map[string]interface{}{
		"connection_id": connID,
		"is_unique": isUnique,
		"online_count": s.OnlineCount,
		"user_agent": userAgent,
	})

	return isUnique
}

// RemoveConnection 移除连接
func (s *Site) RemoveConnection(connID string) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	conn, exists := s.Connections[connID]
	if !exists {
		return false
	}

	now := time.Now()
	s.LastActivity = now

	// 计算停留时间
	stayTime := now.Sub(conn.JoinedAt)
	s.updateAverageStayTime(stayTime)

	// 删除连接
	delete(s.Connections, connID)

	// 检查是否需要减少在线计数
	// 只有当这个IP指纹没有其他活跃连接时才减少计数
	shouldDecrease := true
	for _, otherConn := range s.Connections {
		if otherConn.IPInfo.Fingerprint == conn.IPInfo.Fingerprint {
			shouldDecrease = false
			break
		}
	}

	if shouldDecrease && s.OnlineCount > 0 {
		s.OnlineCount--
	}

	utils.LogWebSocketEvent("connection_removed", s.ID, conn.IPInfo.RealIP, map[string]interface{}{
		"connection_id": connID,
		"stay_time": stayTime.String(),
		"should_decrease": shouldDecrease,
		"online_count": s.OnlineCount,
	})

	return shouldDecrease
}

// UpdateConnection 更新连接活跃时间
func (s *Site) UpdateConnection(connID string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if conn, exists := s.Connections[connID]; exists {
		conn.LastPing = time.Now()
		s.LastActivity = time.Now()
	}
}

// GetOnlineCount 获取在线人数
func (s *Site) GetOnlineCount() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.OnlineCount
}

// GetConnectionCount 获取连接数
func (s *Site) GetConnectionCount() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return len(s.Connections)
}

// GetStats 获取站点统计信息
func (s *Site) GetStats() SiteStats {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return *s.Stats
}

// GetConnectionList 获取连接列表（调试用）
func (s *Site) GetConnectionList() []Connection {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	connections := make([]Connection, 0, len(s.Connections))
	for _, conn := range s.Connections {
		connections = append(connections, *conn)
	}
	return connections
}

// CleanupInactiveConnections 清理不活跃的连接
func (s *Site) CleanupInactiveConnections(timeout time.Duration) int {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	now := time.Now()
	cleaned := 0
	fingerprintCounts := make(map[string]int)

	// 统计每个指纹的连接数
	for _, conn := range s.Connections {
		fingerprintCounts[conn.IPInfo.Fingerprint]++
	}

	// 清理超时连接
	for connID, conn := range s.Connections {
		if now.Sub(conn.LastPing) > timeout {
			// 计算停留时间
			stayTime := now.Sub(conn.JoinedAt)
			s.updateAverageStayTime(stayTime)

			delete(s.Connections, connID)
			cleaned++

			// 检查是否需要减少在线计数
			fingerprintCounts[conn.IPInfo.Fingerprint]--
			if fingerprintCounts[conn.IPInfo.Fingerprint] == 0 && s.OnlineCount > 0 {
				s.OnlineCount--
			}

			utils.LogWebSocketEvent("connection_timeout", s.ID, conn.IPInfo.RealIP, map[string]interface{}{
				"connection_id": connID,
				"stay_time": stayTime.String(),
				"timeout": timeout.String(),
			})
		}
	}

	// 清理IP追踪器
	s.IPTracker.Cleanup(now)

	if cleaned > 0 {
		s.LastActivity = now
		utils.WithSite(s.ID).Info("清理不活跃连接", map[string]interface{}{
			"cleaned_count": cleaned,
			"remaining_connections": len(s.Connections),
			"online_count": s.OnlineCount,
		})
	}

	return cleaned
}

// updateAverageStayTime 更新平均停留时间
func (s *Site) updateAverageStayTime(stayTime time.Duration) {
	// 使用滑动平均算法更新平均停留时间
	if s.Stats.AverageStayTime == 0 {
		s.Stats.AverageStayTime = stayTime
	} else {
		// 新平均值 = 0.9 * 旧平均值 + 0.1 * 新值
		oldAvg := float64(s.Stats.AverageStayTime)
		newVal := float64(stayTime)
		s.Stats.AverageStayTime = time.Duration(0.9*oldAvg + 0.1*newVal)
	}
}

// isToday 检查时间是否为今天
func (s *Site) isToday(t time.Time) bool {
	now := time.Now()
	return t.Year() == now.Year() && t.Month() == now.Month() && t.Day() == now.Day()
}

// ResetDailyStats 重置日统计（通常在午夜调用）
func (s *Site) ResetDailyStats() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.Stats.ConnectionsToday = 0
	s.Stats.LastReset = time.Now()

	utils.WithSite(s.ID).Info("重置日统计", nil)
}

// GetDetailedInfo 获取详细信息（管理用）
func (s *Site) GetDetailedInfo() map[string]interface{} {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	ipStats := s.IPTracker.GetStats()
	
	return map[string]interface{}{
		"id": s.ID,
		"name": s.Name,
		"online_count": s.OnlineCount,
		"connection_count": len(s.Connections),
		"created_at": s.CreatedAt,
		"last_activity": s.LastActivity,
		"stats": s.Stats,
		"ip_stats": ipStats,
		"uptime": time.Since(s.CreatedAt).String(),
	}
}