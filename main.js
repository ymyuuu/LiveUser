(function() {
    'use strict';
    
    // 配置信息（由服务端注入）
    const config = {
        serverUrl: '{{.ServerURL}}',
        siteId: '{{.SiteID}}',
        displayElementId: '{{.DisplayElementID}}',
        reconnectDelay: {{.ReconnectDelay}},
        debug: {{.Debug}}
    };
    
    // 全局状态
    let ws = null;
    let sessionId = null;
    let isVerified = false;
    let reconnectTimer = null;
    let heartbeatTimer = null;
    let behaviorDetected = false;
    let interactionCount = 0;
    
    // 日志函数
    function log(message) {
        if (config.debug) {
            console.log('[LiveUser]', message);
        }
    }
    
    // 生成会话ID
    function generateSessionId() {
        const timestamp = Date.now();
        const random = Math.random().toString(36).substring(2, 15);
        const browser = btoa(navigator.userAgent.substring(0, 20)).substring(0, 8);
        return `${timestamp}-${random}-${browser}`;
    }
    
    // 获取或创建会话ID
    function getSessionId() {
        if (sessionId) return sessionId;
        
        // 尝试从localStorage获取
        try {
            const stored = localStorage.getItem('liveuser_session');
            if (stored) {
                const session = JSON.parse(stored);
                // 检查是否过期（24小时）
                if (Date.now() - session.created < 24 * 60 * 60 * 1000) {
                    sessionId = session.id;
                    log(`恢复会话: ${sessionId.substring(0, 8)}...`);
                    return sessionId;
                }
            }
        } catch (e) {
            log('无法读取localStorage');
        }
        
        // 生成新的会话ID
        sessionId = generateSessionId();
        
        // 保存到localStorage
        try {
            localStorage.setItem('liveuser_session', JSON.stringify({
                id: sessionId,
                created: Date.now()
            }));
        } catch (e) {
            log('无法写入localStorage');
        }
        
        log(`创建新会话: ${sessionId.substring(0, 8)}...`);
        return sessionId;
    }
    
    // 行为检测器
    const BehaviorDetector = {
        init() {
            this.setupListeners();
            log('行为检测器已启动');
        },
        
        setupListeners() {
            // 鼠标移动
            document.addEventListener('mousemove', this.onInteraction.bind(this), { passive: true });
            
            // 鼠标点击
            document.addEventListener('click', this.onInteraction.bind(this), { passive: true });
            
            // 键盘输入
            document.addEventListener('keydown', this.onInteraction.bind(this), { passive: true });
            
            // 滚动
            document.addEventListener('scroll', this.onInteraction.bind(this), { passive: true });
            
            // 触摸事件（移动端）
            document.addEventListener('touchstart', this.onInteraction.bind(this), { passive: true });
            document.addEventListener('touchmove', this.onInteraction.bind(this), { passive: true });
            
            // 窗口焦点
            window.addEventListener('focus', this.onInteraction.bind(this), { passive: true });
            
            // 页面可见性变化
            document.addEventListener('visibilitychange', this.onVisibilityChange.bind(this));
        },
        
        onInteraction() {
            if (!behaviorDetected) {
                behaviorDetected = true;
                interactionCount++;
                log(`检测到用户交互 (#${interactionCount})`);
                this.sendVerification();
            } else {
                // 已验证用户的额外交互
                interactionCount++;
                if (interactionCount % 10 === 0) {
                    log(`用户持续活跃 (#${interactionCount})`);
                    this.sendVerification();
                }
            }
        },
        
        onVisibilityChange() {
            if (!document.hidden) {
                // 页面重新可见
                this.onInteraction();
            }
        },
        
        sendVerification() {
            if (ws && ws.readyState === WebSocket.OPEN) {
                const message = {
                    type: 'verify',
                    sessionId: getSessionId(),
                    timestamp: Date.now()
                };
                ws.send(JSON.stringify(message));
                log('发送行为验证');
            }
        }
    };
    
    // WebSocket管理器
    const WSManager = {
        connect() {
            if (ws && ws.readyState === WebSocket.CONNECTING) {
                return;
            }
            
            log(`连接到 ${config.serverUrl}`);
            ws = new WebSocket(config.serverUrl);
            
            ws.onopen = this.onOpen.bind(this);
            ws.onmessage = this.onMessage.bind(this);
            ws.onclose = this.onClose.bind(this);
            ws.onerror = this.onError.bind(this);
        },
        
        onOpen() {
            log('WebSocket连接已建立');
            
            // 发送加入消息
            const message = {
                type: 'join',
                siteId: config.siteId,
                sessionId: getSessionId()
            };
            ws.send(JSON.stringify(message));
            
            // 启动心跳
            this.startHeartbeat();
            
            // 清除重连定时器
            if (reconnectTimer) {
                clearTimeout(reconnectTimer);
                reconnectTimer = null;
            }
        },
        
        onMessage(event) {
            try {
                const message = JSON.parse(event.data);
                
                switch (message.type) {
                    case 'update':
                        this.updateDisplay(message.count);
                        break;
                        
                    case 'needVerify':
                        log('服务器请求行为验证');
                        // 等待用户交互
                        break;
                        
                    case 'shutdown':
                        log('服务器通知：' + message.message);
                        break;
                        
                    default:
                        log('未知消息类型：' + message.type);
                }
            } catch (e) {
                log('解析消息失败：' + e.message);
            }
        },
        
        onClose() {
            log('WebSocket连接已关闭');
            this.stopHeartbeat();
            this.scheduleReconnect();
        },
        
        onError(error) {
            log('WebSocket错误：' + error);
        },
        
        startHeartbeat() {
            this.stopHeartbeat();
            heartbeatTimer = setInterval(() => {
                if (ws && ws.readyState === WebSocket.OPEN) {
                    const message = {
                        type: 'heartbeat',
                        sessionId: getSessionId(),
                        timestamp: Date.now()
                    };
                    ws.send(JSON.stringify(message));
                    log('发送心跳');
                }
            }, 30000); // 30秒心跳
        },
        
        stopHeartbeat() {
            if (heartbeatTimer) {
                clearInterval(heartbeatTimer);
                heartbeatTimer = null;
            }
        },
        
        scheduleReconnect() {
            if (reconnectTimer) return;
            
            log(`${config.reconnectDelay/1000}秒后重连`);
            reconnectTimer = setTimeout(() => {
                reconnectTimer = null;
                this.connect();
            }, config.reconnectDelay);
        },
        
        updateDisplay(count) {
            const element = document.getElementById(config.displayElementId);
            if (element) {
                // 添加更新动画
                element.classList.add('updating');
                setTimeout(() => {
                    element.classList.remove('updating');
                }, 300);
                
                element.textContent = count;
                log(`更新显示：${count} 人在线`);
            }
        }
    };
    
    // 初始化
    function init() {
        log('LiveUser 客户端启动');
        log(`站点ID: ${config.siteId}`);
        log(`会话ID: ${getSessionId().substring(0, 8)}...`);
        
        // 初始化行为检测
        BehaviorDetector.init();
        
        // 连接WebSocket
        WSManager.connect();
        
        // 页面卸载时清理
        window.addEventListener('beforeunload', () => {
            if (ws) {
                ws.close();
            }
        });
    }
    
    // 等待DOM加载完成
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
    
    // 暴露到全局（用于调试）
    if (config.debug) {
        window.LiveUser = {
            getSessionId,
            getBehaviorDetected: () => behaviorDetected,
            getInteractionCount: () => interactionCount,
            getConnectionState: () => ws ? ws.readyState : -1,
            forceVerify: () => BehaviorDetector.sendVerification()
        };
    }
})();
