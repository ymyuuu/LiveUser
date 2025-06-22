/**
 * LiveUser 实时在线人数统计
 * 动态配置版本 - 由服务器端生成配置
 * 
 * 使用方法：
 * <span id="liveuser">加载中...</span>
 * <script src="https://your-domain.com/liveuser.js"></script>
 */
(function() {
    'use strict';
    
    // 环境检测：确保在浏览器主线程中运行
    if (typeof window === 'undefined' || typeof document === 'undefined') {
        console.warn('[LiveUser] 检测到非浏览器环境，LiveUser 需要在主线程中运行');
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
            this.ws = null;                     // WebSocket 连接
            this.isActive = true;               // 活跃状态
            this.reconnectTimer = null;         // 重连定时器
            this.currentCount = 0;              // 当前人数
            this.lastUpdateTime = 0;            // 上次更新时间
            
            // 获取显示元素
            this.displayElement = document.getElementById(CONFIG.displayElementId);
            
            // 初始化
            this.init();
        }
        
        // 初始化方法
        init() {
            this.log('LiveUser 初始化，配置: ' + JSON.stringify(CONFIG));
            this.checkDisplayElement();
            this.setupEventListeners();
            this.connect();
        }
        
        // 检查显示元素
        checkDisplayElement() {
            if (!this.displayElement) {
                this.log('警告: 找不到 id="' + CONFIG.displayElementId + '" 的元素');
                console.warn('[LiveUser] 警告: 找不到 id="' + CONFIG.displayElementId + '" 的元素');
            }
        }
        
        // 设置事件监听器
        setupEventListeners() {
            // 页面可见性变化监听
            if (typeof document !== 'undefined' && 'visibilitychange' in document) {
                document.addEventListener('visibilitychange', () => {
                    this.isActive = !document.hidden;
                    
                    if (this.isActive) {
                        this.log('页面可见，检查连接状态');
                        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
                            this.connect();
                        }
                    } else {
                        this.log('页面隐藏，保持连接');
                    }
                });
            }
            
            // 页面卸载前清理
            if (typeof window !== 'undefined') {
                window.addEventListener('beforeunload', () => {
                    this.isActive = false;
                    if (this.ws) {
                        this.ws.close(1000, '页面关闭');
                    }
                    this.log('页面卸载，关闭连接');
                });
            }
            
            // 网络状态监听
            if (typeof navigator !== 'undefined' && 'onLine' in navigator && typeof window !== 'undefined') {
                window.addEventListener('online', () => {
                    this.log('网络连接恢复');
                    this.connect();
                });
                
                window.addEventListener('offline', () => {
                    this.log('网络连接断开');
                });
            }
        }
        
        // 建立 WebSocket 连接
        connect() {
            // 清除现有的重连定时器
            if (this.reconnectTimer) {
                clearTimeout(this.reconnectTimer);
                this.reconnectTimer = null;
            }
            
            // 检查是否已经连接
            if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                this.log('连接已存在，跳过重复连接');
                return;
            }
            
            this.log('开始建立 WebSocket 连接: ' + CONFIG.serverUrl);
            
            try {
                this.ws = new WebSocket(CONFIG.serverUrl);
                
                // 连接建立成功
                this.ws.onopen = () => {
                    this.log('WebSocket 连接建立成功');
                    
                    // 发送站点注册消息
                    const joinMessage = {
                        type: 'join',
                        siteId: CONFIG.siteId
                    };
                    
                    this.ws.send(JSON.stringify(joinMessage));
                    this.log('发送站点注册消息: ' + JSON.stringify(joinMessage));
                };
                
                // 接收服务器消息
                this.ws.onmessage = (event) => {
                    try {
                        const data = JSON.parse(event.data);
                        this.log('收到服务器消息: ' + JSON.stringify(data));
                        this.handleMessage(data);
                    } catch (err) {
                        this.log('解析服务器消息失败: ' + err.message);
                    }
                };
                
                // 连接关闭处理
                this.ws.onclose = (event) => {
                    this.log('WebSocket 连接关闭，代码: ' + event.code + '，原因: ' + (event.reason || '未知'));
                    
                    // 如果页面仍然活跃，安排重连
                    if (this.isActive) {
                        this.scheduleReconnect();
                    }
                };
                
                // 连接错误处理
                this.ws.onerror = (error) => {
                    this.log('WebSocket 连接发生错误');
                };
                
            } catch (err) {
                this.log('创建 WebSocket 连接失败: ' + err.message);
                this.scheduleReconnect();
            }
        }
        
        // 处理服务器消息
        handleMessage(data) {
            switch (data.type) {
                case 'update':
                    // 在线人数更新
                    if (data.siteId === CONFIG.siteId) {
                        this.updateCount(data.count);
                        this.log('在线人数更新: ' + data.count);
                    }
                    break;
                    
                case 'shutdown':
                    // 服务器关闭通知
                    this.log('服务器关闭通知: ' + (data.message || '服务器维护'));
                    break;
                    
                default:
                    this.log('收到未知消息类型: ' + data.type);
            }
        }
        
        // 更新人数显示（包含动画效果）
        updateCount(count) {
            const oldCount = this.currentCount;
            this.currentCount = count;
            this.lastUpdateTime = Date.now();
            
            if (this.displayElement) {
                // 添加更新动画效果和颜色变化
                this.displayElement.classList.add('updating');
                
                // 设置过渡效果
                if (!this.displayElement.style.transition) {
                    this.displayElement.style.transition = 'all 0.3s ease';
                }
                
                // 更新数字
                this.displayElement.textContent = count;
                
                // 300ms 后移除更新样式
                setTimeout(() => {
                    if (this.displayElement) {
                        this.displayElement.classList.remove('updating');
                    }
                }, 300);
                
                this.log('显示元素已更新: ' + oldCount + ' -> ' + count);
            } else {
                this.log('警告: 显示元素不存在，无法更新显示');
                // 重新尝试获取元素
                this.displayElement = document.getElementById(CONFIG.displayElementId);
            }
            
            // 触发自定义事件
            this.dispatchEvent('countUpdate', { 
                count: count, 
                oldCount: oldCount,
                siteId: CONFIG.siteId,
                timestamp: this.lastUpdateTime
            });
        }
        
        // 安排重连
        scheduleReconnect() {
            if (this.reconnectTimer || !this.isActive) {
                return;
            }
            
            this.log('将在 ' + (CONFIG.reconnectDelay / 1000) + ' 秒后重新连接');
            
            this.reconnectTimer = setTimeout(() => {
                this.reconnectTimer = null;
                if (this.isActive) {
                    this.connect();
                }
            }, CONFIG.reconnectDelay);
        }
        
        // 触发自定义事件
        dispatchEvent(eventName, detail) {
            if (typeof window !== 'undefined' && typeof CustomEvent !== 'undefined') {
                try {
                    const event = new CustomEvent('liveuser:' + eventName, {
                        detail: detail
                    });
                    window.dispatchEvent(event);
                    this.log('触发事件: liveuser:' + eventName + ', 详情: ' + JSON.stringify(detail));
                } catch (err) {
                    this.log('触发事件失败: ' + err.message);
                }
            }
        }
        
        // 获取当前在线人数
        getCount() {
            return this.currentCount;
        }
        
        // 获取连接状态
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
        
        // 获取配置信息
        getConfig() {
            return Object.assign({}, CONFIG);
        }
        
        // 手动断开连接
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
            
            this.log('手动断开连接');
        }
        
        // 手动重连
        reconnect() {
            this.log('手动重连');
            this.isActive = true;
            this.connect();
        }
        
        // 刷新显示元素引用
        refreshElement() {
            this.displayElement = document.getElementById(CONFIG.displayElementId);
            this.checkDisplayElement();
            if (this.displayElement && this.currentCount > 0) {
                this.displayElement.textContent = this.currentCount;
            }
        }
        
        // 日志输出
        log(message) {
            if (CONFIG.debug) {
                console.log('[LiveUser] ' + message);
            }
        }
    }
    
    // 自动初始化
    function initLiveUser() {
        // 再次检查环境
        if (typeof document === 'undefined') {
            console.warn('[LiveUser] 初始化失败：document 对象不可用');
            return;
        }
        
        // 创建全局实例
        if (typeof window !== 'undefined') {
            window.LiveUser = new LiveUser();
            
            // 暴露常用方法到全局
            window.getLiveUserCount = function() {
                return window.LiveUser ? window.LiveUser.getCount() : 0;
            };
            
            window.getLiveUserStatus = function() {
                return window.LiveUser ? window.LiveUser.getStatus() : 'not-initialized';
            };
            
            window.getLiveUserConfig = function() {
                return window.LiveUser ? window.LiveUser.getConfig() : null;
            };
            
            // 提供手动控制方法
            window.disconnectLiveUser = function() {
                if (window.LiveUser) {
                    window.LiveUser.disconnect();
                }
            };
            
            window.reconnectLiveUser = function() {
                if (window.LiveUser) {
                    window.LiveUser.reconnect();
                }
            };
            
            window.refreshLiveUserElement = function() {
                if (window.LiveUser) {
                    window.LiveUser.refreshElement();
                }
            };
        }
        
        console.log('[LiveUser] 服务已启动，站点ID: ' + CONFIG.siteId);
    }
    
    // 等待 DOM 加载完成后初始化
    if (typeof document !== 'undefined') {
        if (document.readyState === 'loading') {
            document.addEventListener('DOMContentLoaded', initLiveUser);
        } else {
            initLiveUser();
        }
    } else {
        // 如果没有 document 对象，直接退出
        console.warn('[LiveUser] 跳过初始化：当前环境不支持 DOM 操作');
    }
    
})();
