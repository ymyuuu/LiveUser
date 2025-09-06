package utils

import (
	"crypto/md5"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// IPInfo IP信息结构
type IPInfo struct {
	RealIP        string    // 真实IP地址
	ProxyChain    []string  // 代理链
	Fingerprint   string    // 用户指纹
	FirstSeen     time.Time // 首次见到时间
	LastSeen      time.Time // 最后见到时间
	RequestCount  int       // 请求计数
}

// GetRealIP 获取客户端真实IP，支持多层CDN
func GetRealIP(r *http.Request) string {
	// 按优先级检查各种IP头
	ipHeaders := []string{
		"CF-Connecting-IP",    // Cloudflare
		"True-Client-IP",      // Akamai, Cloudflare
		"X-Real-IP",           // Nginx
		"X-Forwarded-For",     // 标准代理头
		"X-Client-IP",         // Apache
		"X-Forwarded",         // 非标准
		"X-Cluster-Client-IP", // 集群
		"Forwarded-For",       // RFC 7239
		"Forwarded",           // RFC 7239
	}

	for _, header := range ipHeaders {
		if ip := r.Header.Get(header); ip != "" {
			// 处理X-Forwarded-For格式的多IP情况
			ips := strings.Split(ip, ",")
			for _, singleIP := range ips {
				cleanIP := strings.TrimSpace(singleIP)
				if cleanIP != "" && isValidIP(cleanIP) && !isPrivateIP(cleanIP) {
					return cleanIP
				}
			}
		}
	}

	// 如果所有头都没有有效IP，使用RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	
	return host
}

// GetProxyChain 获取完整的代理链
func GetProxyChain(r *http.Request) []string {
	var chain []string
	
	// 检查X-Forwarded-For
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		for _, ip := range ips {
			cleanIP := strings.TrimSpace(ip)
			if cleanIP != "" && isValidIP(cleanIP) {
				chain = append(chain, cleanIP)
			}
		}
	}

	// 添加RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && isValidIP(host) {
		chain = append(chain, host)
	}

	return chain
}

// GenerateFingerprint 生成用户指纹，用于去重
func GenerateFingerprint(r *http.Request) string {
	realIP := GetRealIP(r)
	userAgent := r.Header.Get("User-Agent")
	acceptLang := r.Header.Get("Accept-Language")
	acceptEnc := r.Header.Get("Accept-Encoding")
	
	// 创建指纹字符串
	fingerprint := fmt.Sprintf("%s|%s|%s|%s", 
		realIP, userAgent, acceptLang, acceptEnc)
	
	// 计算MD5哈希
	hash := md5.Sum([]byte(fingerprint))
	return fmt.Sprintf("%x", hash)
}

// isValidIP 检查IP地址是否有效
func isValidIP(ip string) bool {
	// 去除可能的端口
	if colonIndex := strings.LastIndex(ip, ":"); colonIndex != -1 {
		ip = ip[:colonIndex]
	}
	
	// 去除方括号（IPv6）
	ip = strings.Trim(ip, "[]")
	
	return net.ParseIP(ip) != nil
}

// isPrivateIP 检查是否为私有IP
func isPrivateIP(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	// IPv4私有地址段
	private4Blocks := []*net.IPNet{
		{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(8, 32)},     // 10.0.0.0/8
		{IP: net.IPv4(172, 16, 0, 0), Mask: net.CIDRMask(12, 32)},  // 172.16.0.0/12
		{IP: net.IPv4(192, 168, 0, 0), Mask: net.CIDRMask(16, 32)}, // 192.168.0.0/16
		{IP: net.IPv4(127, 0, 0, 0), Mask: net.CIDRMask(8, 32)},    // 127.0.0.0/8
	}

	for _, block := range private4Blocks {
		if block.Contains(parsedIP) {
			return true
		}
	}

	// IPv6私有地址
	if parsedIP.To4() == nil { // IPv6
		if parsedIP.IsLoopback() || parsedIP.IsLinkLocalUnicast() {
			return true
		}
		// IPv6 ULA (Unique Local Addresses) fc00::/7
		if len(parsedIP) >= 1 && (parsedIP[0]&0xfe) == 0xfc {
			return true
		}
	}

	return false
}

// IPTracker IP追踪器，用于去重和统计
type IPTracker struct {
	ips       map[string]*IPInfo // IP信息映射
	sessionTimeout time.Duration  // 会话超时时间
}

// NewIPTracker 创建新的IP追踪器
func NewIPTracker(sessionTimeout time.Duration) *IPTracker {
	return &IPTracker{
		ips:            make(map[string]*IPInfo),
		sessionTimeout: sessionTimeout,
	}
}

// Track 追踪IP访问
func (t *IPTracker) Track(r *http.Request) *IPInfo {
	realIP := GetRealIP(r)
	fingerprint := GenerateFingerprint(r)
	now := time.Now()

	// 查找现有记录
	info, exists := t.ips[fingerprint]
	if !exists {
		// 创建新记录
		info = &IPInfo{
			RealIP:       realIP,
			ProxyChain:   GetProxyChain(r),
			Fingerprint:  fingerprint,
			FirstSeen:    now,
			LastSeen:     now,
			RequestCount: 1,
		}
		t.ips[fingerprint] = info
	} else {
		// 更新现有记录
		info.LastSeen = now
		info.RequestCount++
		info.ProxyChain = GetProxyChain(r) // 更新代理链（可能会变化）
	}

	return info
}

// IsUnique 检查IP是否应该被计数（基于时间窗口的去重）
func (t *IPTracker) IsUnique(fingerprint string, now time.Time) bool {
	info, exists := t.ips[fingerprint]
	if !exists {
		return true
	}

	// 如果超过会话超时时间，认为是新会话
	return now.Sub(info.LastSeen) > t.sessionTimeout
}

// Cleanup 清理过期的IP记录
func (t *IPTracker) Cleanup(now time.Time) int {
	cleaned := 0
	for fingerprint, info := range t.ips {
		if now.Sub(info.LastSeen) > t.sessionTimeout*2 { // 使用2倍超时时间作为清理阈值
			delete(t.ips, fingerprint)
			cleaned++
		}
	}
	return cleaned
}

// GetStats 获取IP统计信息
func (t *IPTracker) GetStats() map[string]interface{} {
	uniqueIPs := make(map[string]int)
	totalRequests := 0
	now := time.Now()
	activeIPs := 0

	for _, info := range t.ips {
		// 统计真实IP
		uniqueIPs[info.RealIP]++
		totalRequests += info.RequestCount
		
		// 统计活跃IP（最近活动的）
		if now.Sub(info.LastSeen) <= t.sessionTimeout {
			activeIPs++
		}
	}

	return map[string]interface{}{
		"total_fingerprints": len(t.ips),
		"unique_real_ips":    len(uniqueIPs),
		"active_sessions":    activeIPs,
		"total_requests":     totalRequests,
	}
}