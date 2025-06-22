package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	"github.com/gorilla/websocket"
)

// 版本信息
var Version = "dev"

// 内置静态文件
//go:embed demo.html
var demoHTML string

//go:embed main.js
var mainJS string

// 站点数据结构
type Site struct {
	ID          string           `json:"id"`
	Count       int              `json:"count"`
	Connections map[*Client]bool `json:"-"`
	mutex       sync.RWMutex     `json:"-"`
}

// 客户端连接
type Client struct {
	conn *websocket.Conn
	site *Site
	hub  *Hub
	send chan Message
	ip   string
}

// 连接管理器
type Hub struct {
	sites      map[string]*Site
	register   chan *Client
	unregister chan *Client
	mutex      sync.RWMutex
}

// 消息结构
type Message struct {
	Type      string `json:"type"`
	SiteID    string `json:"siteId,omitempty"`
	Count     int    `json:"count,omitempty"`
	Message   string `json:"message,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// JavaScript 配置结构
type JSConfig struct {
	ServerURL        string `json:"serverUrl"`
	SiteID           string `json:"siteId"`
	DisplayElementID string `json:"displayElementId"`
	ReconnectDelay   int    `json:"reconnectDelay"`
	Debug            bool   `json:"debug"`
}

// WebSocket 升级器
var upgrader = websocket.Upgrader{
	ReadBufferSize:  512,
	WriteBufferSize: 512,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// 全局变量
var hub *Hub

// 命令行参数
var addr = flag.String("addr", "0.0.0.0:10086", "监听地址")

// 创建新的Hub
func NewHub() *Hub {
	return &Hub{
		sites:      make(map[string]*Site),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Hub 主循环
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.handleRegister(client)
		case client := <-h.unregister:
			h.handleUnregister(client)
		}
	}
}

// 处理客户端注册
func (h *Hub) handleRegister(client *Client) {
	if client.site == nil {
		return
	}

	site := client.site
	site.mutex.Lock()
	site.Connections[client] = true
	site.Count++
	count := site.Count
	site.mutex.Unlock()

	log.Printf("客户端 %s 加入站点 %s，在线人数: %d", client.ip, site.ID, count)
	h.broadcastToSite(site.ID, count)
}

// 处理客户端注销
func (h *Hub) handleUnregister(client *Client) {
	if client.site == nil {
		return
	}

	site := client.site
	site.mutex.Lock()

	if _, exists := site.Connections[client]; exists {
		delete(site.Connections, client)
		close(client.send)
		site.Count--
		if site.Count < 0 {
			site.Count = 0
		}
		count := site.Count
		connectionsLeft := len(site.Connections)
		site.mutex.Unlock()

		log.Printf("客户端 %s 离开站点 %s，在线人数: %d", client.ip, site.ID, count)

		if connectionsLeft == 0 {
			h.mutex.Lock()
			delete(h.sites, site.ID)
			h.mutex.Unlock()
			log.Printf("站点 %s 已清理", site.ID)
		} else {
			h.broadcastToSite(site.ID, count)
		}
	} else {
		site.mutex.Unlock()
	}
}

// 向指定站点广播消息
func (h *Hub) broadcastToSite(siteID string, count int) {
	message := Message{
		Type:      "update",
		SiteID:    siteID,
		Count:     count,
		Timestamp: time.Now().Unix(),
	}

	h.mutex.RLock()
	site, exists := h.sites[siteID]
	h.mutex.RUnlock()

	if !exists {
		return
	}

	site.mutex.RLock()
	defer site.mutex.RUnlock()

	for client := range site.Connections {
		select {
		case client.send <- message:
		default:
			delete(site.Connections, client)
			close(client.send)
		}
	}
}

// 获取或创建站点
func (h *Hub) getSite(siteID string) *Site {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	site, exists := h.sites[siteID]
	if !exists {
		site = &Site{
			ID:          siteID,
			Count:       0,
			Connections: make(map[*Client]bool),
		}
		h.sites[siteID] = site
		log.Printf("创建新站点: %s", siteID)
	}

	return site
}

// 获取客户端真实IP
func getRealIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.Split(ip, ",")[0]
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}

// 检查是否为WebSocket请求
func isWebSocketRequest(r *http.Request) bool {
	return strings.ToLower(r.Header.Get("Upgrade")) == "websocket"
}

// 处理所有请求
func handleRequest(w http.ResponseWriter, r *http.Request) {
	if isWebSocketRequest(r) {
		handleWebSocket(w, r)
		return
	}

	if r.Method == "GET" {
		if strings.HasSuffix(r.URL.Path, ".js") {
			handleJavaScript(w, r)
			return
		}
		handleDemoPage(w, r)
		return
	}

	http.Error(w, "Bad Request", http.StatusBadRequest)
}

// 处理JavaScript文件请求
func handleJavaScript(w http.ResponseWriter, r *http.Request) {
	config := parseJSConfig(r)

	tmpl, err := template.New("liveuser").Parse(mainJS)
	if err != nil {
		log.Printf("解析JavaScript模板失败: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	if err := tmpl.Execute(w, config); err != nil {
		log.Printf("渲染JavaScript模板失败: %v", err)
		return
	}
}

// 解析JavaScript配置
func parseJSConfig(r *http.Request) JSConfig {
	params := r.URL.Query()

	protocol := "ws"
	if r.Header.Get("X-Forwarded-Proto") == "https" || r.TLS != nil {
		protocol = "wss"
	}
	defaultServerURL := protocol + "://" + r.Host + "/"

	config := JSConfig{
		ServerURL:        getParam(params, "serverUrl", defaultServerURL),
		SiteID:           getParam(params, "siteId", ""),
		DisplayElementID: getParam(params, "displayElementId", "liveuser"),
		ReconnectDelay:   getIntParam(params, "reconnectDelay", 3000),
		Debug:            getBoolParam(params, "debug", true),
	}

	if config.SiteID == "" {
		referer := r.Header.Get("Referer")
		if referer != "" {
			if u, err := url.Parse(referer); err == nil {
				config.SiteID = u.Host
			}
		}
		if config.SiteID == "" {
			config.SiteID = "default-site"
		}
	}

	return config
}

// 获取字符串参数
func getParam(params url.Values, key, defaultValue string) string {
	if value := params.Get(key); value != "" {
		return value
	}
	return defaultValue
}

// 获取整数参数
func getIntParam(params url.Values, key string, defaultValue int) int {
	if value := params.Get(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// 获取布尔参数
func getBoolParam(params url.Values, key string, defaultValue bool) bool {
	if value := params.Get(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

// 处理演示页面请求
func handleDemoPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(demoHTML))
	log.Printf("返回demo.html给客户端: %s，路径: %s", getRealIP(r), r.URL.Path)
}

// 处理WebSocket连接
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket升级失败: %v", err)
		return
	}

	clientIP := getRealIP(r)
	log.Printf("新WebSocket连接，IP: %s，路径: %s", clientIP, r.URL.Path)

	client := &Client{
		conn: conn,
		hub:  hub,
		send: make(chan Message, 16),
		ip:   clientIP,
	}

	go client.readPump()
	go client.writePump()
}

// 读取客户端消息
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(256)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, msgData, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket异常关闭: %v", err)
			}
			break
		}

		var msg Message
		if err := json.Unmarshal(msgData, &msg); err != nil {
			log.Printf("解析消息失败: %v", err)
			continue
		}

		if msg.Type == "join" && msg.SiteID != "" {
			siteID := strings.TrimSpace(msg.SiteID)

			if c.site != nil && c.site.ID != siteID {
				c.hub.unregister <- c
			}

			if c.site == nil || c.site.ID != siteID {
				site := c.hub.getSite(siteID)
				c.site = site
				c.hub.register <- c
			}
		}
	}
}

// 向客户端发送消息
func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteJSON(message); err != nil {
				log.Printf("发送消息失败: %v", err)
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// 主函数
func main() {
	flag.Parse()

	// 初始化Hub
	hub = NewHub()
	go hub.Run()

	// 设置路由
	http.HandleFunc("/", handleRequest)

	// 创建服务器
	server := &http.Server{
		Addr:    *addr,
		Handler: nil,
	}

	// 启动服务器
	go func() {
		log.Printf("🚀 LiveUser v%s 服务器启动成功！", Version)
		log.Printf("📡 监听地址: %s", *addr)
		log.Printf("🔗 WebSocket: 任意路径支持WebSocket连接")
		log.Printf("📄 演示页面: 任意路径的HTTP请求都返回演示页面")
		log.Printf("📜 动态脚本: 任意.js文件请求返回配置化的LiveUser脚本")
		log.Printf("💡 准备接收连接...")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("❌ 服务器启动失败: %v", err)
		}
	}()

	// 等待关闭信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("📴 收到关闭信号，正在优雅关闭...")

	// 通知所有客户端即将关闭
	hub.mutex.RLock()
	for _, site := range hub.sites {
		site.mutex.RLock()
		for client := range site.Connections {
			shutdownMsg := Message{
				Type:    "shutdown",
				Message: "服务器即将重启，请稍后重连",
			}
			select {
			case client.send <- shutdownMsg:
			default:
			}
			client.conn.Close()
		}
		site.mutex.RUnlock()
	}
	hub.mutex.RUnlock()

	log.Println("✅ 服务器已关闭")
}
