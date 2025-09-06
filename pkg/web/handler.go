package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"liveuser/pkg/connection"
	"liveuser/pkg/core"
	"liveuser/pkg/utils"
)

// Handler Web处理器
type Handler struct {
	hub            *connection.Hub  // WebSocket Hub
	manager        *core.Manager    // 核心管理器
	config         *utils.Config    // 配置
	jsTemplate     *template.Template // JavaScript模板
	demoTemplate   *template.Template // 演示页面模板
}

// JSConfig JavaScript配置结构
type JSConfig struct {
	ServerURL        string `json:"serverUrl"`
	SiteID           string `json:"siteId"`
	DisplayElementID string `json:"displayElementId"`
	ReconnectDelay   int    `json:"reconnectDelay"`
	Debug            bool   `json:"debug"`
}

// NewHandler 创建新的Web处理器
func NewHandler(hub *connection.Hub, manager *core.Manager, config *utils.Config, jsContent, demoContent string) (*Handler, error) {
	// 解析JavaScript模板
	jsTemplate, err := template.New("liveuser.js").Parse(jsContent)
	if err != nil {
		return nil, fmt.Errorf("解析JavaScript模板失败: %w", err)
	}

	// 解析演示页面模板
	demoTemplate, err := template.New("demo.html").Parse(demoContent)
	if err != nil {
		return nil, fmt.Errorf("解析演示页面模板失败: %w", err)
	}

	return &Handler{
		hub:          hub,
		manager:      manager,
		config:       config,
		jsTemplate:   jsTemplate,
		demoTemplate: demoTemplate,
	}, nil
}

// ServeHTTP 实现http.Handler接口
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 记录请求
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		utils.Logger.Debug("HTTP请求", map[string]interface{}{
			"method":     r.Method,
			"path":       r.URL.Path,
			"remote_ip":  utils.GetRealIP(r),
			"user_agent": r.Header.Get("User-Agent"),
			"duration":   duration.String(),
		})
	}()

	// 添加CORS头
	h.setCORSHeaders(w, r)

	// 处理OPTIONS请求
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// 路由请求
	switch {
	case isWebSocketRequest(r):
		h.handleWebSocket(w, r)
	case r.Method == "GET" && strings.HasSuffix(r.URL.Path, ".js"):
		h.handleJavaScript(w, r)
	case r.Method == "GET" && r.URL.Path == "/stats":
		h.handleStats(w, r)
	case r.Method == "GET" && r.URL.Path == "/health":
		h.handleHealth(w, r)
	case r.Method == "GET":
		h.handleDemo(w, r)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

// handleWebSocket 处理WebSocket连接
func (h *Handler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	h.hub.HandleWebSocket(w, r)
}

// handleJavaScript 处理JavaScript文件请求
func (h *Handler) handleJavaScript(w http.ResponseWriter, r *http.Request) {
	config := h.parseJSConfig(r)

	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	if err := h.jsTemplate.Execute(w, config); err != nil {
		utils.WithError(err).Error("JavaScript模板执行失败", map[string]interface{}{
			"remote_ip": utils.GetRealIP(r),
		})
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// handleDemo 处理演示页面
func (h *Handler) handleDemo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")

	data := map[string]interface{}{
		"ServerURL": fmt.Sprintf("%s://%s", getScheme(r), r.Host),
		"Title":     "LiveUser 实时在线用户统计 - 演示页面",
	}

	if err := h.demoTemplate.Execute(w, data); err != nil {
		utils.WithError(err).Error("演示页面模板执行失败", map[string]interface{}{
			"remote_ip": utils.GetRealIP(r),
		})
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// handleStats 处理统计信息API
func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	// 检查是否有站点ID参数
	siteID := r.URL.Query().Get("siteId")
	
	var stats interface{}
	if siteID != "" {
		// 返回特定站点统计
		siteStats, err := h.manager.GetSiteStats(siteID)
		if err != nil {
			http.Error(w, fmt.Sprintf("获取站点统计失败: %s", err.Error()), http.StatusNotFound)
			return
		}
		stats = map[string]interface{}{
			"site_id": siteID,
			"stats":   siteStats,
		}
	} else {
		// 返回系统统计
		systemStats := h.manager.GetSystemStats()
		hubStats := h.hub.GetStats()
		
		stats = map[string]interface{}{
			"system": systemStats,
			"hub":    hubStats,
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		utils.WithError(err).Error("JSON编码失败", map[string]interface{}{
			"remote_ip": utils.GetRealIP(r),
		})
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleHealth 处理健康检查
func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	// 获取系统统计以获得正确的启动时间
	systemStats := h.manager.GetSystemStats()
	uptime := "unknown"
	if uptimeStr, ok := systemStats["uptime"].(string); ok {
		uptime = uptimeStr
	}
	
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"uptime":    uptime,
		"version":   "2.0.0",
	}

	// 检查各个组件的健康状态
	hubStats := h.hub.GetStats()

	// 添加简单的健康检查逻辑
	if currentConns, ok := hubStats["current_connections"].(int); ok && currentConns > 0 {
		health["connections"] = "active"
	} else {
		health["connections"] = "idle"
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(health); err != nil {
		utils.WithError(err).Error("健康检查JSON编码失败")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// parseJSConfig 解析JavaScript配置
func (h *Handler) parseJSConfig(r *http.Request) JSConfig {
	params := r.URL.Query()

	// 确定协议
	scheme := "ws"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "wss"
	}

	// 默认服务器URL
	defaultServerURL := fmt.Sprintf("%s://%s/", scheme, r.Host)

	config := JSConfig{
		ServerURL:        getParam(params, "serverUrl", defaultServerURL),
		SiteID:           h.determineSiteID(r, params),
		DisplayElementID: getParam(params, "displayElementId", "liveuser"),
		ReconnectDelay:   getIntParam(params, "reconnectDelay", 3000),
		Debug:            getBoolParam(params, "debug", false), // 生产环境默认关闭调试
	}

	utils.Logger.Debug("JavaScript配置", map[string]interface{}{
		"config":    config,
		"remote_ip": utils.GetRealIP(r),
		"referer":   r.Header.Get("Referer"),
	})

	return config
}

// determineSiteID 确定站点ID
func (h *Handler) determineSiteID(r *http.Request, params url.Values) string {
	// 1. 优先使用URL参数中的siteId
	if siteID := params.Get("siteId"); siteID != "" {
		return siteID
	}

	// 2. 从Referer头中提取域名
	if referer := r.Header.Get("Referer"); referer != "" {
		if u, err := url.Parse(referer); err == nil && u.Host != "" {
			return u.Host
		}
	}

	// 3. 使用请求的Host头
	if r.Host != "" {
		return r.Host
	}

	// 4. 默认站点ID
	return "default-site"
}

// setCORSHeaders 设置CORS头
func (h *Handler) setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = "*"
	}

	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With")
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Max-Age", "86400") // 24小时
}

// 工具函数

// isWebSocketRequest 检查是否为WebSocket请求
func isWebSocketRequest(r *http.Request) bool {
	return strings.ToLower(r.Header.Get("Upgrade")) == "websocket"
}

// getScheme 获取请求协议
func getScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		return "https"
	}
	return "http"
}

// getParam 获取字符串参数
func getParam(params url.Values, key, defaultValue string) string {
	if value := params.Get(key); value != "" {
		return value
	}
	return defaultValue
}

// getIntParam 获取整数参数
func getIntParam(params url.Values, key string, defaultValue int) int {
	if value := params.Get(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getBoolParam 获取布尔参数
func getBoolParam(params url.Values, key string, defaultValue bool) bool {
	if value := params.Get(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}