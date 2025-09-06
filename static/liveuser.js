/**
 * LiveUser 2.0 - 实时在线用户统计客户端
 * 重构版本：解决计数不准、连接不稳定等问题
 * 
 * 使用方法:
 * <span id="liveuser">加载中...</span>
 * <script src="https://your-domain.com/liveuser.js?siteId=your-site"></script>
 */
(function() {
    'use strict';

    // 检查环境
    if (typeof window === 'undefined' || typeof document === 'undefined') {
        console.warn('[LiveUser] 需要在浏览器环境中运行');
        return;
    }

    // 防重复加载
    if (window.LiveUserInstance) {
        console.warn('[LiveUser] 检测到重复加载，忽略');
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

    // 验证配置
    if (!CONFIG.serverUrl || !CONFIG.siteId) {
        console.error('[LiveUser] 配置无效，无法初始化');
        return;
    }

    /**
     * LiveUser 核心类 - 重构版
     */
    class LiveUser {
        constructor() {
            this.ws = null;
            this.isActive = true;
            this.reconnectTimer = null;
            this.currentCount = 0;
            this.connectionId = this.generateConnectionId();
            this.retryCount = 0;
            this.maxRetries = 10;
            this.connectionState = 'disconnected'; // disconnected, connecting, connected
            
            // 统计信息
            this.stats = {
                connectTime: null,
                totalReconnects: 0,
                lastUpdate: null,
                messagesReceived: 0
            };

            // 获取显示元素
            this.displayElement = document.getElementById(CONFIG.displayElementId);
            
            this.init();
        }

        /**
         * 初始化
         */
        init() {
            this.log('LiveUser 2.0 初始化开始', {
                siteId: CONFIG.siteId,
                connectionId: this.connectionId,
                serverUrl: CONFIG.serverUrl
            });

            this.checkDisplayElement();
            this.setupEventListeners();
            this.connect();
        }

        /**
         * 生成连接ID
         */
        generateConnectionId() {
            return 'conn_' + Date.now() + '_' + Math.random().toString(36).substring(2, 9);
        }

        /**
         * 检查显示元素
         */
        checkDisplayElement() {
            if (!this.displayElement) {
                this.log('警告: 找不到显示元素', { elementId: CONFIG.displayElementId });
                // 尝试延迟查找
                setTimeout(() => {
                    this.displayElement = document.getElementById(CONFIG.displayElementId);
                    if (this.displayElement) {
                        this.log('延迟找到显示元素');
                        this.updateDisplay(this.currentCount);
                    }
                }, 1000);
            }
        }

        /**
         * 设置事件监听器
         */
        setupEventListeners() {
            // 页面可见性变化
            if (typeof document !== 'undefined' && 'visibilitychange' in document) {
                document.addEventListener('visibilitychange', () => {
                    const isVisible = !document.hidden;
                    this.log('页面可见性变化', { isVisible });
                    
                    if (isVisible && this.connectionState !== 'connected') {
                        this.connect();
                    }
                });
            }

            // 页面卸载清理
            if (typeof window !== 'undefined') {
                window.addEventListener('beforeunload', () => {
                    this.log('页面即将关闭');
                    this.isActive = false;
                    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                        this.ws.close(1000, '页面关闭');
                    }
                });

                // 网络状态监听
                if ('navigator' in window && 'onLine' in navigator) {
                    window.addEventListener('online', () => {
                        this.log('网络恢复');
                        setTimeout(() => {
                            if (this.connectionState !== 'connected') {
                                this.connect();
                            }
                        }, 1000);
                    });

                    window.addEventListener('offline', () => {
                        this.log('网络断开');
                        this.updateConnectionState('disconnected');
                    });
                }
            }
        }

        /**
         * 建立WebSocket连接
         */
        connect() {
            // 清除重连定时器
            if (this.reconnectTimer) {
                clearTimeout(this.reconnectTimer);
                this.reconnectTimer = null;
            }

            // 检查当前连接状态
            if (this.connectionState === 'connecting' || 
                (this.ws && this.ws.readyState === WebSocket.CONNECTING)) {
                this.log('连接正在进行中，跳过');
                return;
            }

            if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                this.log('连接已存在，跳过');
                return;
            }

            // 检查重试次数
            if (this.retryCount >= this.maxRetries) {
                this.log('达到最大重试次数，停止重连', { maxRetries: this.maxRetries });
                this.updateConnectionState('failed');
                return;
            }

            this.updateConnectionState('connecting');
            this.log('开始建立WebSocket连接', { 
                serverUrl: CONFIG.serverUrl,
                retryCount: this.retryCount 
            });

            try {
                this.ws = new WebSocket(CONFIG.serverUrl);
                this.setupWebSocketHandlers();
            } catch (error) {
                this.log('WebSocket创建失败', { error: error.message });
                this.scheduleReconnect();
            }
        }

        /**
         * 设置WebSocket事件处理器
         */
        setupWebSocketHandlers() {
            this.ws.onopen = () => {
                this.log('WebSocket连接成功');
                this.updateConnectionState('connected');
                this.retryCount = 0;
                this.stats.connectTime = Date.now();

                // 发送加入消息
                this.sendMessage({
                    type: 'join',
                    siteId: CONFIG.siteId,
                    timestamp: Date.now()
                });
            };

            this.ws.onmessage = (event) => {
                try {
                    const data = JSON.parse(event.data);
                    this.handleMessage(data);
                } catch (error) {
                    this.log('消息解析失败', { error: error.message, data: event.data });
                }
            };

            this.ws.onclose = (event) => {
                this.log('WebSocket连接关闭', { 
                    code: event.code, 
                    reason: event.reason 
                });
                this.updateConnectionState('disconnected');
                
                if (this.isActive && event.code !== 1000) {
                    this.scheduleReconnect();
                }
            };

            this.ws.onerror = (error) => {
                this.log('WebSocket连接错误', { error });
                this.updateConnectionState('error');
            };
        }

        /**
         * 处理收到的消息
         */
        handleMessage(data) {
            this.stats.messagesReceived++;
            this.stats.lastUpdate = Date.now();

            this.log('收到消息', { type: data.type, data });

            switch (data.type) {
                case 'update':
                    if (data.siteId === CONFIG.siteId) {
                        this.updateCount(data.count);
                    }
                    break;
                    
                case 'shutdown':
                    this.log('服务器通知关闭', { message: data.message });
                    this.isActive = false;
                    break;
                    
                case 'error':
                    this.log('服务器错误', { message: data.message });
                    break;
                    
                case 'pong':
                    this.log('收到Pong响应');
                    break;
                    
                default:
                    this.log('未知消息类型', { type: data.type });
            }
        }

        /**
         * 发送消息
         */
        sendMessage(message) {
            if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
                this.log('连接未就绪，无法发送消息', { message });
                return false;
            }

            try {
                this.ws.send(JSON.stringify(message));
                this.log('消息发送成功', { message });
                return true;
            } catch (error) {
                this.log('消息发送失败', { error: error.message, message });
                return false;
            }
        }

        /**
         * 更新人数显示
         */
        updateCount(count) {
            const oldCount = this.currentCount;
            this.currentCount = count;

            this.log('更新人数', { oldCount, newCount: count });

            // 更新显示元素
            if (this.displayElement) {
                this.updateDisplay(count);
            }

            // 触发自定义事件
            this.dispatchUpdateEvent(count, oldCount);
        }

        /**
         * 更新显示
         */
        updateDisplay(count) {
            if (!this.displayElement) return;

            // 添加更新动画类
            this.displayElement.classList.add('liveuser-updating');
            
            // 更新内容
            this.displayElement.textContent = count;
            
            // 移除动画类
            setTimeout(() => {
                if (this.displayElement) {
                    this.displayElement.classList.remove('liveuser-updating');
                }
            }, 300);
        }

        /**
         * 触发自定义事件
         */
        dispatchUpdateEvent(count, oldCount) {
            if (typeof window !== 'undefined' && typeof CustomEvent !== 'undefined') {
                try {
                    const event = new CustomEvent('liveuser:update', {
                        detail: {
                            count,
                            oldCount,
                            siteId: CONFIG.siteId,
                            connectionId: this.connectionId,
                            timestamp: Date.now()
                        }
                    });
                    window.dispatchEvent(event);
                } catch (error) {
                    this.log('触发事件失败', { error: error.message });
                }
            }
        }

        /**
         * 安排重连
         */
        scheduleReconnect() {
            if (this.reconnectTimer || !this.isActive) {
                return;
            }

            this.retryCount++;
            const delay = Math.min(CONFIG.reconnectDelay * Math.pow(1.5, this.retryCount - 1), 30000);

            this.log('安排重连', { 
                retryCount: this.retryCount,
                delay: delay,
                maxRetries: this.maxRetries
            });

            this.reconnectTimer = setTimeout(() => {
                this.reconnectTimer = null;
                if (this.isActive) {
                    this.connect();
                }
            }, delay);
        }

        /**
         * 更新连接状态
         */
        updateConnectionState(state) {
            const oldState = this.connectionState;
            this.connectionState = state;
            
            if (oldState !== state) {
                this.log('连接状态变化', { oldState, newState: state });
                
                // 触发状态变化事件
                if (typeof window !== 'undefined' && typeof CustomEvent !== 'undefined') {
                    try {
                        const event = new CustomEvent('liveuser:statechange', {
                            detail: {
                                state,
                                oldState,
                                connectionId: this.connectionId
                            }
                        });
                        window.dispatchEvent(event);
                    } catch (error) {
                        // 忽略事件触发错误
                    }
                }
            }
        }

        /**
         * 手动重连
         */
        reconnect() {
            this.log('手动重连');
            this.isActive = true;
            this.retryCount = 0;
            this.connect();
        }

        /**
         * 断开连接
         */
        disconnect() {
            this.log('手动断开连接');
            this.isActive = false;
            
            if (this.reconnectTimer) {
                clearTimeout(this.reconnectTimer);
                this.reconnectTimer = null;
            }
            
            if (this.ws) {
                this.ws.close(1000, '手动断开');
                this.ws = null;
            }
            
            this.updateConnectionState('disconnected');
        }

        /**
         * 获取当前人数
         */
        getCount() {
            return this.currentCount;
        }

        /**
         * 获取连接状态
         */
        getStatus() {
            return {
                state: this.connectionState,
                count: this.currentCount,
                retryCount: this.retryCount,
                isActive: this.isActive,
                stats: { ...this.stats }
            };
        }

        /**
         * 发送心跳
         */
        sendHeartbeat() {
            return this.sendMessage({
                type: 'ping',
                timestamp: Date.now()
            });
        }

        /**
         * 日志输出
         */
        log(message, data = {}) {
            if (CONFIG.debug) {
                const logData = {
                    timestamp: new Date().toISOString(),
                    connectionId: this.connectionId,
                    siteId: CONFIG.siteId,
                    ...data
                };
                console.log(`[LiveUser] ${message}`, logData);
            }
        }
    }

    /**
     * 初始化函数
     */
    function initLiveUser() {
        if (typeof document === 'undefined') {
            return;
        }

        try {
            // 创建全局实例
            const instance = new LiveUser();
            
            if (typeof window !== 'undefined') {
                // 设置全局引用
                window.LiveUserInstance = instance;
                
                // 兼容旧版本的全局方法
                window.getLiveUserCount = function() {
                    return instance.getCount();
                };
                
                window.getLiveUserStatus = function() {
                    return instance.getStatus();
                };

                // 新版本的API
                window.LiveUser = {
                    getCount: () => instance.getCount(),
                    getStatus: () => instance.getStatus(),
                    reconnect: () => instance.reconnect(),
                    disconnect: () => instance.disconnect(),
                    sendHeartbeat: () => instance.sendHeartbeat()
                };
            }

            // 全局CSS样式注入
            injectStyles();

        } catch (error) {
            console.error('[LiveUser] 初始化失败:', error);
        }
    }

    /**
     * 注入默认样式
     */
    function injectStyles() {
        if (typeof document === 'undefined' || document.getElementById('liveuser-styles')) {
            return;
        }

        const styles = `
            #${CONFIG.displayElementId} {
                transition: all 0.3s ease;
                display: inline-block;
            }
            
            #${CONFIG.displayElementId}.liveuser-updating {
                transform: scale(1.1);
                color: #FFB800;
                font-weight: bold;
            }
            
            .liveuser-offline {
                opacity: 0.6;
                color: #999;
            }
            
            .liveuser-connecting {
                animation: liveuser-pulse 1.5s ease-in-out infinite;
            }
            
            @keyframes liveuser-pulse {
                0%, 100% { opacity: 1; }
                50% { opacity: 0.5; }
            }
        `;

        const styleElement = document.createElement('style');
        styleElement.id = 'liveuser-styles';
        styleElement.textContent = styles;
        document.head.appendChild(styleElement);
    }

    // DOM加载完成后初始化
    if (typeof document !== 'undefined') {
        if (document.readyState === 'loading') {
            document.addEventListener('DOMContentLoaded', initLiveUser);
        } else {
            // DOM已加载完成，直接初始化
            initLiveUser();
        }
    }

    // 模块导出（如果在模块环境中）
    if (typeof module !== 'undefined' && module.exports) {
        module.exports = { LiveUser };
    }

})();