/**
 * LiveUser 实时在线人数统计
 * 使用方法：
 * <span id="liveuser">加载中...</span>
 * <script src="https://your-domain.com/liveuser.js"></script>
 */
(function() {
    'use strict';
    
    if (typeof window === 'undefined' || typeof document === 'undefined') {
        console.warn('[LiveUser] 需要在浏览器环境中运行');
        return;
    }
    
    // 配置项（由服务器动态生成）
    const CONFIG = {
        serverUrl: '{{.ServerURL}}',
        siteId: '{{.SiteID}}',
        displayElementId: '{{.DisplayElementID}}',
        reconnectDelay: {{.ReconnectDelay}},
        debug: {{.Debug}}
    };
    
    // LiveUser 核心类
    class LiveUser {
        constructor() {
            this.ws = null;
            this.isActive = true;
            this.reconnectTimer = null;
            this.currentCount = 0;
            this.displayElement = document.getElementById(CONFIG.displayElementId);
            
            this.init();
        }
        
        init() {
            this.log('LiveUser 初始化，站点: ' + CONFIG.siteId);
            this.checkDisplayElement();
            this.setupEventListeners();
            this.connect();
        }
        
        checkDisplayElement() {
            if (!this.displayElement) {
                this.log('警告: 找不到元素 #' + CONFIG.displayElementId);
            }
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
            
            this.log('连接 WebSocket: ' + CONFIG.serverUrl);
            
            try {
                this.ws = new WebSocket(CONFIG.serverUrl);
                
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
            
            if (this.displayElement) {
                this.displayElement.classList.add('updating');
                this.displayElement.textContent = count;
                
                setTimeout(() => {
                    if (this.displayElement) {
                        this.displayElement.classList.remove('updating');
                    }
                }, 300);
                
                this.log('更新人数: ' + oldCount + ' -> ' + count);
            } else {
                this.displayElement = document.getElementById(CONFIG.displayElementId);
            }
            
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
