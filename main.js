/**
 * LiveUser 实时在线人数统计
 * 支持多种显示模式：span元素、悬浮显示器、或两者同时
 * 
 * 使用方法：
 * <script src="https://your-domain.com/liveuser.js"></script>
 * <script src="https://your-domain.com/liveuser.js?type=float"></script>
 * <script src="https://your-domain.com/liveuser.js?type=span&displayElementId=custom-id"></script>
 */
(function() {
    'use strict';
    
    if (typeof window === 'undefined' || typeof document === 'undefined') {
        console.warn('[LiveUser] 需要在浏览器环境中运行');
        return;
    }
    
    // 配置项（由服务器动态生成）
    const CONFIG = {
        siteId: '{{.SiteID}}',
        displayElementId: '{{.DisplayElementID}}',
        reconnectDelay: {{.ReconnectDelay}},
        debug: {{.Debug}},
        type: '{{.Type}}'  // all, span, float
    };
    
    // LiveUser 核心类
    class LiveUser {
        constructor() {
            this.ws = null;
            this.isActive = true;
            this.reconnectTimer = null;
            this.currentCount = 0;
            this.displayElement = null;
            this.floatElement = null;
            
            this.init();
        }
        
        init() {
            this.log('LiveUser 初始化，站点: ' + CONFIG.siteId + ', 显示模式: ' + CONFIG.type);
            this.setupDisplayElements();
            this.setupEventListeners();
            this.connect();
        }
        
        setupDisplayElements() {
            // 根据type参数设置显示元素
            if (CONFIG.type === 'all' || CONFIG.type === 'span') {
                this.displayElement = document.getElementById(CONFIG.displayElementId);
                if (!this.displayElement && CONFIG.debug) {
                    this.log('未找到 span 元素 #' + CONFIG.displayElementId + '，将跳过 span 显示');
                }
            }
            
            if (CONFIG.type === 'all' || CONFIG.type === 'float') {
                this.createFloatDisplay();
            }
        }
        
        createFloatDisplay() {
            // 检查是否已存在悬浮显示器
            if (document.getElementById('liveuser-float-counter')) {
                this.floatElement = document.getElementById('liveuser-float-counter');
                return;
            }
            
            // 创建悬浮显示器
            const floatCounter = document.createElement('div');
            floatCounter.id = 'liveuser-float-counter';
            floatCounter.innerHTML = `
                <div class="liveuser-indicator"></div>
                <span class="liveuser-count">0</span>
            `;
            
            // 添加样式
            const style = document.createElement('style');
            style.textContent = `
                #liveuser-float-counter {
                    position: fixed;
                    bottom: 20px;
                    right: 20px;
                    display: flex;
                    align-items: center;
                    gap: 8px;
                    background: rgba(0, 0, 0, 0.8);
                    backdrop-filter: blur(8px);
                    border-radius: 20px;
                    padding: 8px 12px;
                    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
                    font-size: 13px;
                    font-weight: 500;
                    color: #fff;
                    z-index: 9999;
                    transition: all 0.3s ease;
                    cursor: default;
                    user-select: none;
                    box-shadow: 0 4px 20px rgba(0, 0, 0, 0.15);
                }
                #liveuser-float-counter:hover {
                    transform: scale(1.05);
                    background: rgba(0, 0, 0, 0.9);
                }
                #liveuser-float-counter.updating {
                    transform: scale(1.1);
                    background: rgba(0, 212, 170, 0.2);
                }
                .liveuser-indicator {
                    width: 8px;
                    height: 8px;
                    background: #00d4aa;
                    border-radius: 50%;
                    position: relative;
                    animation: liveuser-breathe 3s ease-in-out infinite;
                }
                .liveuser-indicator::before {
                    content: '';
                    position: absolute;
                    top: -2px;
                    left: -2px;
                    right: -2px;
                    bottom: -2px;
                    background: #00d4aa;
                    border-radius: 50%;
                    opacity: 0.3;
                    animation: liveuser-ripple 3s ease-in-out infinite;
                }
                .liveuser-count {
                    transition: all 0.3s ease;
                }
                @keyframes liveuser-breathe {
                    0%, 100% { opacity: 1; }
                    50% { opacity: 0.6; }
                }
                @keyframes liveuser-ripple {
                    0% {
                        transform: scale(0.8);
                        opacity: 0.3;
                    }
                    50% {
                        transform: scale(1.2);
                        opacity: 0.1;
                    }
                    100% {
                        transform: scale(0.8);
                        opacity: 0.3;
                    }
                }
                @media (max-width: 768px) {
                    #liveuser-float-counter {
                        bottom: 15px;
                        right: 15px;
                        padding: 6px 10px;
                        font-size: 12px;
                    }
                }
            `;
            
            // 添加到页面
            document.head.appendChild(style);
            document.body.appendChild(floatCounter);
            this.floatElement = floatCounter;
            
            this.log('已创建悬浮显示器');
        }
        
        setupEventListeners() {
            // 页面可见性变化
            if (typeof document !== 'undefined' && 'visibilitychange' in document) {
                document.addEventListener('visibilitychange', () => {
                    this.isActive = !document.hidden;
                    if (this.isActive && (!this.ws || this.ws.readyState !== WebSocket.OPEN)) {
                        this.connect();
                    }
                });
            }
            
            // 页面卸载清理
            if (typeof window !== 'undefined') {
                window.addEventListener('beforeunload', () => {
                    this.isActive = false;
                    if (this.ws) {
                        this.ws.close(1000, '页面关闭');
                    }
                });
            }
            
            // 网络状态监听
            if (typeof navigator !== 'undefined' && 'onLine' in navigator) {
                window.addEventListener('online', () => {
                    this.log('网络恢复');
                    this.connect();
                });
            }
        }
        
        connect() {
            if (this.reconnectTimer) {
                clearTimeout(this.reconnectTimer);
                this.reconnectTimer = null;
            }
            
            if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                return;
            }
            
            // 从当前脚本的src地址中获取服务器地址
            const serverUrl = this.getServerUrl();
            
            this.log('连接 WebSocket: ' + serverUrl);
            
            try {
                this.ws = new WebSocket(serverUrl);
                
                this.ws.onopen = () => {
                    this.log('连接成功');
                    this.ws.send(JSON.stringify({
                        type: 'join',
                        siteId: CONFIG.siteId
                    }));
                };
                
                this.ws.onmessage = (event) => {
                    try {
                        const data = JSON.parse(event.data);
                        this.handleMessage(data);
                    } catch (err) {
                        this.log('解析消息失败: ' + err.message);
                    }
                };
                
                this.ws.onclose = (event) => {
                    this.log('连接关闭: ' + event.code);
                    if (this.isActive) {
                        this.scheduleReconnect();
                    }
                };
                
                this.ws.onerror = () => {
                    this.log('连接错误');
                };
                
            } catch (err) {
                this.log('连接失败: ' + err.message);
                this.scheduleReconnect();
            }
        }
        
        getServerUrl() {
            // 尝试从当前脚本标签获取服务器地址
            const scripts = document.getElementsByTagName('script');
            for (let i = scripts.length - 1; i >= 0; i--) {
                const script = scripts[i];
                if (script.src && script.src.includes('liveuser.js')) {
                    try {
                        const url = new URL(script.src);
                        const protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
                        return protocol + '//' + url.host + '/';
                    } catch (err) {
                        this.log('解析脚本URL失败: ' + err.message);
                    }
                    break;
                }
            }
            
            // 降级方案：使用当前页面的地址
            const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            return protocol + '//' + window.location.host + '/';
        }
        
        handleMessage(data) {
            switch (data.type) {
                case 'update':
                    if (data.siteId === CONFIG.siteId) {
                        this.updateCount(data.count);
                    }
                    break;
                case 'shutdown':
                    this.log('服务器通知: ' + (data.message || '服务器维护'));
                    break;
            }
        }
        
        updateCount(count) {
            const oldCount = this.currentCount;
            this.currentCount = count;
            
            // 更新 span 元素（如果存在且启用）
            if (this.displayElement && (CONFIG.type === 'all' || CONFIG.type === 'span')) {
                this.displayElement.classList.add('updating');
                this.displayElement.textContent = count;
                
                setTimeout(() => {
                    if (this.displayElement) {
                        this.displayElement.classList.remove('updating');
                    }
                }, 300);
            }
            
            // 更新悬浮显示器（如果存在且启用）
            if (this.floatElement && (CONFIG.type === 'all' || CONFIG.type === 'float')) {
                const countElement = this.floatElement.querySelector('.liveuser-count');
                if (countElement) {
                    this.floatElement.classList.add('updating');
                    countElement.textContent = count;
                    
                    setTimeout(() => {
                        if (this.floatElement) {
                            this.floatElement.classList.remove('updating');
                        }
                    }, 300);
                }
            }
            
            this.log('更新人数: ' + oldCount + ' -> ' + count);
            
            // 触发自定义事件
            if (typeof window !== 'undefined' && typeof CustomEvent !== 'undefined') {
                try {
                    const event = new CustomEvent('liveuser:update', {
                        detail: { count: count, oldCount: oldCount, siteId: CONFIG.siteId }
                    });
                    window.dispatchEvent(event);
                } catch (err) {
                    // 忽略事件触发错误
                }
            }
        }
        
        scheduleReconnect() {
            if (this.reconnectTimer || !this.isActive) {
                return;
            }
            
            this.log('将在 ' + (CONFIG.reconnectDelay / 1000) + ' 秒后重连');
            this.reconnectTimer = setTimeout(() => {
                this.reconnectTimer = null;
                if (this.isActive) {
                    this.connect();
                }
            }, CONFIG.reconnectDelay);
        }
        
        getCount() {
            return this.currentCount;
        }
        
        getStatus() {
            if (!this.ws) return 'disconnected';
            const states = {
                [WebSocket.CONNECTING]: 'connecting',
                [WebSocket.OPEN]: 'connected',
                [WebSocket.CLOSING]: 'closing',
                [WebSocket.CLOSED]: 'closed'
            };
            return states[this.ws.readyState] || 'unknown';
        }
        
        disconnect() {
            this.isActive = false;
            if (this.reconnectTimer) {
                clearTimeout(this.reconnectTimer);
                this.reconnectTimer = null;
            }
            if (this.ws) {
                this.ws.close(1000, '手动断开');
                this.ws = null;
            }
        }
        
        reconnect() {
            this.isActive = true;
            this.connect();
        }
        
        log(message) {
            if (CONFIG.debug) {
                console.log('[LiveUser] ' + message);
            }
        }
    }
    
    // 初始化
    function initLiveUser() {
        if (typeof document === 'undefined') {
            return;
        }
        
        if (typeof window !== 'undefined') {
            window.LiveUser = new LiveUser();
            
            // 全局方法
            window.getLiveUserCount = function() {
                return window.LiveUser ? window.LiveUser.getCount() : 0;
            };
            
            window.getLiveUserStatus = function() {
                return window.LiveUser ? window.LiveUser.getStatus() : 'not-initialized';
            };
        }
    }
    
    // 等待 DOM 加载
    if (typeof document !== 'undefined') {
        if (document.readyState === 'loading') {
            document.addEventListener('DOMContentLoaded', initLiveUser);
        } else {
            initLiveUser();
        }
    }
    
})();
