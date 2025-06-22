package main

import (
	"encoding/json" // JSON 数据处理
	"flag"          // 命令行参数解析
	"fmt"           // 格式化输出
	"io/ioutil"     // 文件读取
	"log"           // 日志输出
	"net/http"      // HTTP 服务器
	"net/url"       // URL 解析
	"os"            // 操作系统接口
	"os/signal"     // 信号处理
	"strconv"       // 字符串转换
	"strings"       // 字符串操作
	"sync"          // 同步原语
	"syscall"       // 系统调用
	"text/template" // 模板引擎
	"time"          // 时间处理

	"github.com/gorilla/websocket" // WebSocket 库
)

// 版本信息
var Version = "dev"

// Site 站点数据结构
type Site struct {
	ID          string           `json:"id"`          // 站点 ID
	Count       int              `json:"count"`       // 在线人数
	Connections map[*Client]bool `json:"-"`           // 连接集合
	mutex       sync.RWMutex     `json:"-"`           // 读写锁
}

// Client 客户端连接
type Client struct {
	conn   *websocket.Conn // WebSocket 连接
	site   *Site           // 所属站点
	hub    *Hub            // 连接管理器
	send   chan Message    // 发送消息通道
	ip     string          // 客户端 IP
}

// Hub 连接管理器
type Hub struct {
	sites      map[string]*Site // 站点映射
	register   chan *Client     // 注册通道
	unregister chan *Client     // 注销通道
	mutex      sync.RWMutex     // 全局锁
}

// Message 消息结构
type Message struct {
	Type      string `json:"type"`                // 消息类型
	SiteID    string `json:"siteId,omitempty"`    // 站点 ID
	Count     int    `json:"count,omitempty"`     // 在线人数
	Message   string `json:"message,omitempty"`   // 消息内容
	Timestamp int64  `json:"timestamp,omitempty"` // 时间戳
}

// JSConfig JavaScript 配置结构
type JSConfig struct {
	ServerURL        string `json:"serverUrl"`        // 服务器地址
	SiteID           string `json:"siteId"`           // 站点 ID
	DisplayElementID string `json:"displayElementId"` // 显示元素 ID
	ReconnectDelay   int    `json:"reconnectDelay"`   // 重连延迟
	Debug            bool   `json:"debug"`            // 调试模式
}

// WebSocket 升级器配置
var upgrader = websocket.Upgrader{
	ReadBufferSize:  512,  // 读缓冲区
	WriteBufferSize: 512,  // 写缓冲区
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许所有跨域连接
	},
}

// 全局 Hub 实例
var hub *Hub

// 命令行参数
var (
	addr    = flag.String("addr", "0.0.0.0:10086", "监听地址 (格式: host:port)")
	version = flag.Bool("version", false, "显示版本信息")
	help    = flag.Bool("help", false, "显示帮助信息")
)

// 显示帮助信息
func showHelp() {
	fmt.Printf("LiveUser v%s - 实时在线用户统计服务\n\n", Version)
	fmt.Println("用法:")
	fmt.Printf("  %s [选项]\n\n", os.Args[0])
	fmt.Println("选项:")
	fmt.Println("  -addr string")
	fmt.Println("        监听地址 (默认: 0.0.0.0:10086)")
	fmt.Println("  -version")
	fmt.Println("        显示版本信息")
	fmt.Println("  -help")
	fmt.Println("        显示此帮助信息")
	fmt.Println()
	fmt.Println("示例:")
	fmt.Printf("  %s                          # 默认监听 0.0.0.0:10086\n", os.Args[0])
	fmt.Printf("  %s -addr :8080              # 监听 8080 端口\n", os.Args[0])
	fmt.Printf("  %s -addr 127.0.0.1:3000     # 仅监听本地 3000 端口\n", os.Args[0])
	fmt.Println()
	fmt.Println("环境变量:")
	fmt.Println("  PORT        端口号 (会被 -addr 参数覆盖)")
	fmt.Println()
}

// NewHub 创建新的 Hub
func NewHub() *Hub {
	return &Hub{
		sites:      make(map[string]*Site), // 初始化站点映射
		register:   make(chan *Client),     // 注册通道
		unregister: make(chan *Client),     // 注销通道
	}
}

// Run Hub 主循环
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

// handleRegister 处理客户端注册
func (h *Hub) handleRegister(client *Client) {
	if client.site == nil {
		return
	}

	site := client.site
	site.mutex.Lock()
	site.Connections[client] = true // 添加到连接集合
	site.Count++                    // 人数增加
	count := site.Count
	site.mutex.Unlock()

	log.Printf("客户端 %s 加入站点 %s，在线人数: %d", client.ip, site.ID, count)

	// 向该站点所有客户端广播更新
	h.broadcastToSite(site.ID, count)
}

// handleUnregister 处理客户端注销
func (h *Hub) handleUnregister(client *Client) {
	if client.site == nil {
		return
	}

	site := client.site
	site.mutex.Lock()

	if _, exists := site.Connections[client]; exists {
		delete(site.Connections, client) // 从连接集合移除
		close(client.send)               // 关闭发送通道
		site.Count--                     // 人数减少
		if site.Count < 0 {
			site.Count = 0
		}
		count := site.Count
		connectionsLeft := len(site.Connections)
		site.mutex.Unlock()

		log.Printf("客户端 %s 离开站点 %s，在线人数: %d", client.ip, site.ID, count)

		// 如果站点没有连接了，清理站点数据
		if connectionsLeft == 0 {
			h.mutex.Lock()
			delete(h.sites, site.ID)
			h.mutex.Unlock()
			log.Printf("站点 %s 已清理", site.ID)
		} else {
			// 广播更新
			h.broadcastToSite(site.ID, count)
		}
	} else {
		site.mutex.Unlock()
	}
}

// broadcastToSite 向指定站点广播消息
func (h *Hub) broadcastToSite(siteID string, count int) {
	message := Message{
		Type:      "update",         // 更新消息
		SiteID:    siteID,           // 站点 ID
		Count:     count,            // 在线人数
		Timestamp: time.Now().Unix(), // 时间戳
	}

	h.mutex.RLock()
	site, exists := h.sites[siteID]
	h.mutex.RUnlock()

	if !exists {
		return
	}

	site.mutex.RLock()
	defer site.mutex.RUnlock()

	// 向站点内所有客户端发送消息
	for client := range site.Connections {
		select {
		case client.send <- message:
			// 发送成功
		default:
			// 发送失败，连接可能已断开
			delete(site.Connections, client)
			close(client.send)
		}
	}
}

// getSite 获取或创建站点
func (h *Hub) getSite(siteID string) *Site {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	site, exists := h.sites[siteID]
	if !exists {
		site = &Site{
			ID:          siteID,                 // 站点 ID
			Count:       0,                      // 初始人数
			Connections: make(map[*Client]bool), // 连接集合
		}
		h.sites[siteID] = site
		log.Printf("创建新站点: %s", siteID)
	}

	return site
}

// getRealIP 获取客户端真实 IP
func getRealIP(r *http.Request) string {
	// 尝试从代理头部获取真实 IP
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

// isWebSocketRequest 检查是否为 WebSocket 请求
func isWebSocketRequest(r *http.Request) bool {
	return strings.ToLower(r.Header.Get("Upgrade")) == "websocket"
}

// handleRequest 处理所有请求
func handleRequest(w http.ResponseWriter, r *http.Request) {
	// 如果是 WebSocket 请求，任何路径都可以升级
	if isWebSocketRequest(r) {
		handleWebSocket(w, r)
		return
	}

	// 如果是 HTTP GET 请求
	if r.Method == "GET" {
		// 处理 JavaScript 文件请求
		if strings.HasSuffix(r.URL.Path, ".js") {
			handleJavaScript(w, r)
			return
		}
		
		// 其他请求返回演示页面
		handleDemoPage(w, r)
		return
	}

	// 非 GET 请求返回 Bad Request
	http.Error(w, "Bad Request", http.StatusBadRequest)
}

// handleJavaScript 处理 JavaScript 文件请求
func handleJavaScript(w http.ResponseWriter, r *http.Request) {
	// 尝试读取 main.js 模板文件
	jsTemplate, err := ioutil.ReadFile("main.js")
	if err != nil {
		// 如果文件不存在，返回 Bad Request
		log.Printf("main.js 文件不存在: %v", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// 解析 URL 参数并生成配置
	config := parseJSConfig(r)

	// 创建模板并渲染
	tmpl, err := template.New("liveuser").Parse(string(jsTemplate))
	if err != nil {
		log.Printf("解析 JavaScript 模板失败: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 设置正确的 Content-Type
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache") // 禁用缓存，确保配置实时更新
	w.WriteHeader(http.StatusOK)

	// 渲染模板
	if err := tmpl.Execute(w, config); err != nil {
		log.Printf("渲染 JavaScript 模板失败: %v", err)
		return
	}
}

// parseJSConfig 解析 URL 参数生成 JavaScript 配置
func parseJSConfig(r *http.Request) JSConfig {
	params := r.URL.Query()
	
	// 获取请求的协议和主机
	protocol := "ws"
	if r.Header.Get("X-Forwarded-Proto") == "https" || r.TLS != nil {
		protocol = "wss"
	}
	defaultServerURL := protocol + "://" + r.Host + "/"

	// 解析参数
	config := JSConfig{
		ServerURL:        getParam(params, "serverUrl", defaultServerURL),
		SiteID:           getParam(params, "siteId", ""),
		DisplayElementID: getParam(params, "displayElementId", "liveuser"),
		ReconnectDelay:   getIntParam(params, "reconnectDelay", 3000),
		Debug:            getBoolParam(params, "debug", true),
	}

	// 如果没有指定 siteId，使用请求的 Referer 头部或默认值
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

// handleDemoPage 处理演示页面请求
func handleDemoPage(w http.ResponseWriter, r *http.Request) {
	// 尝试读取 demo.html 文件
	htmlContent, err := ioutil.ReadFile("demo.html")
	if err != nil {
		// 如果文件不存在，返回 Bad Request
		log.Printf("demo.html 文件不存在: %v", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// 设置正确的 Content-Type
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(htmlContent)

	log.Printf("返回 demo.html 给客户端: %s，路径: %s", getRealIP(r), r.URL.Path)
}

// handleWebSocket 处理 WebSocket 连接
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 升级为 WebSocket 连接
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket 升级失败: %v", err)
		return
	}

	clientIP := getRealIP(r)
	log.Printf("新 WebSocket 连接，IP: %s，路径: %s", clientIP, r.URL.Path)

	// 创建客户端
	client := &Client{
		conn: conn,                  // WebSocket 连接
		hub:  hub,                   // Hub 实例
		send: make(chan Message, 16), // 发送通道
		ip:   clientIP,              // 客户端 IP
	}

	// 启动读写协程
	go client.readPump()
	go client.writePump()
}

// readPump 读取客户端消息
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c // 注销客户端
		c.conn.Close()        // 关闭连接
	}()

	// 设置读取配置
	c.conn.SetReadLimit(256)                                     // 消息大小限制
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))     // 读取超时
	c.conn.SetPongHandler(func(string) error {                   // Pong 处理
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second)) // 重置超时
		return nil
	})

	for {
		// 读取消息
		_, msgData, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket 异常关闭: %v", err)
			}
			break
		}

		// 解析消息
		var msg Message
		if err := json.Unmarshal(msgData, &msg); err != nil {
			log.Printf("解析消息失败: %v", err)
			continue
		}

		// 处理加入站点请求
		if msg.Type == "join" && msg.SiteID != "" {
			siteID := strings.TrimSpace(msg.SiteID)

			// 如果已经在其他站点，先离开
			if c.site != nil && c.site.ID != siteID {
				c.hub.unregister <- c
			}

			// 加入新站点
			if c.site == nil || c.site.ID != siteID {
				site := c.hub.getSite(siteID)
				c.site = site
				c.hub.register <- c
			}
		}
	}
}

// writePump 向客户端发送消息
func (c *Client) writePump() {
	// 心跳定时器
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			// 设置写入超时
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// 通道已关闭
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// 发送 JSON 消息
			if err := c.conn.WriteJSON(message); err != nil {
				log.Printf("发送消息失败: %v", err)
				return
			}

		case <-ticker.C:
			// 发送心跳 ping
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// 主函数
func main() {
	// 解析命令行参数
	flag.Parse()

	// 显示版本信息
	if *version {
		fmt.Printf("LiveUser v%s\n", Version)
		return
	}

	// 显示帮助信息
	if *help {
		showHelp()
		return
	}

	// 获取监听地址
	listenAddr := *addr
	
	// 如果使用默认地址但设置了 PORT 环境变量，则优先使用环境变量
	if *addr == "0.0.0.0:10086" {
		if port := os.Getenv("PORT"); port != "" {
			listenAddr = "0.0.0.0:" + port
		}
	}

	// 初始化 Hub
	hub = NewHub()
	go hub.Run()

	// 设置路由处理器
	http.HandleFunc("/", handleRequest)

	// 创建 HTTP 服务器
	server := &http.Server{
		Addr:    listenAddr,
		Handler: nil, // 使用默认的 ServeMux
	}

	// 启动服务器
	go func() {
		log.Printf("🚀 LiveUser v%s 服务器启动成功！", Version)
		log.Printf("📡 监听地址: %s", listenAddr)
		log.Printf("🔗 WebSocket: 任意路径支持 WebSocket 连接")
		log.Printf("📄 演示页面: 任意路径的 HTTP 请求都返回演示页面")
		log.Printf("📜 动态脚本: 任意 .js 文件请求返回配置化的 LiveUser 脚本")
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
