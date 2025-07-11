(function() {
    'use strict';

    // 配置参数（由服务器模板注入）
    const config = {
        serverUrl: '{{.ServerURL}}',
        siteId: '{{.SiteID}}',
        displayElementId: '{{.DisplayElementID}}',
        reconnectDelay: {{.ReconnectDelay}},
        debug: {{.Debug}}
    };

    // 全局变量
    let ws = null;
    let sessionId = null;
    let verified = false;
    let reconnectTimer = null;
    let activityTimer = null;
    let behaviorDetected = false;

    // 生成唯一会话ID
    function generateSessionId() {
        const timestamp = Date.now();
        const random = Math.random().toString(36).substring(2, 15);
        const browserInfo = btoa(navigator.userAgent.substring(0, 20)).substring(0, 8);
        return `${timestamp}-${random}-${browserInfo}`;
    }

    // 获取或创建会话ID
    function getSessionId() {
        if (sessionId) return sessionId;
        
        // 尝试从localStorage获取（24小时有效）
        const stored = localStorage.getItem('liveuser_session');
        if (stored) {
            try {
                const data = JSON.parse(stored);
                const now = Date.now();
                if (now - data.created < 24 * 60 * 60 * 1000) { // 24小时内有效
                    sessionId = data.sessionId;
                    return sessionId;
                }
            } catch (e) {
                // 解析失败，删除旧数据
                localStorage.removeItem('liveuser_session');
            }
        }

        // 生成新的会话ID
        sessionId = generateSessionId();
        localStorage.setItem('liveuser_session', JSON.stringify({
            sessionId: sessionId,
            created: Date.now()
        }));
        
        return sessionId;
    }

    // 日志输出
    function log(message) {
        if (config.debug) {
            console.log('[LiveUser]', message);
        }
    }

    // 更新显示的人数
    function updateDisplay(count) {
        const element = document.getElementById(config.displayElementId);
        if (element) {
            // 添加更新动画效果
            element.classList.add('updating');
            element.textContent = count;
            
            setTimeout(() => {
                element.classList.remove('updating');
            }, 300);
        }
    }

    // 发送消息到服务器
    function sendMessage(message) {
        if (ws && ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify(message));
            return true;
        }
        return false;
    }

    // 行为检测器
    function setupBehaviorDetection() {
        let detected = false;
        
        function markBehaviorDetected() {
            if (!detected && !verified) {
                detected = true;
                behaviorDetected = true;
                log('检测到用户行为，发送验证请求');
                
                // 发送验证消息
                sendMessage({
                    type: 'verify',
                    sessionId: getSessionId(),
                    timestamp: Date.now()
                });
            }
        }

        // 检测各种用户行为
        const events = [
            'mousemove', 'mousedown', 'mouseup', 'click',
            'keydown', 'keyup', 'keypress',
            'scroll', 'wheel',
            'touchstart', 'touchmove', 'touchend',
            'focus', 'blur'
        ];

        events.forEach(event => {
            document.addEventListener(event, markBehaviorDetected, { 
                once: false, 
                passive: true 
            });
        });

        // 页面可见性变化检测
        document.addEventListener('visibilitychange', () => {
            if (!document.hidden) {
                markBehaviorDetected();
            }
        });

        log('行为检测器已启动');
    }

    // 启动活动心跳
    function startActivityHeartbeat() {
        if (activityTimer) {
            clearInterval(activityTimer);
        }

        activityTimer = setInterval(() => {
            if (verified && ws && ws.readyState === WebSocket.OPEN) {
                sendMessage({
                    type: 'activity',
                    sessionId: getSessionId(),
                    timestamp: Date.now()
                });
            }
        }, 30000); // 每30秒发送一次心跳
    }

    // 停止活动心跳
    function stopActivityHeartbeat() {
        if (activityTimer) {
            clearInterval(activityTimer);
            activityTimer = null;
        }
    }

    // 连接到WebSocket服务器
    function connect() {
        if (ws) {
            ws.close();
        }

        const wsUrl = config.serverUrl.replace(/^http/, 'ws');
        log(`连接到 ${wsUrl}`);

        ws = new WebSocket(wsUrl);

        ws.onopen = function() {
            log('WebSocket连接已建立');
            verified = false;
            
            // 发送加入消息
            sendMessage({
                type: 'join',
                siteId: config.siteId,
                sessionId: getSessionId(),
                timestamp: Date.now()
            });

            // 如果已经检测到行为，立即发送验证
            if (behaviorDetected) {
                setTimeout(() => {
                    sendMessage({
                        type: 'verify',
                        sessionId: getSessionId(),
                        timestamp: Date.now()
                    });
                }, 100);
            }
        };

        ws.onmessage = function(event) {
            try {
                const data = JSON.parse(event.data);
                handleMessage(data);
            } catch (e) {
                log('消息解析错误:', e);
            }
        };

        ws.onclose = function(event) {
            log('WebSocket连接已关闭:', event.code, event.reason);
            verified = false;
            stopActivityHeartbeat();
            
            // 非正常关闭时自动重连
            if (event.code !== 1000) {
                scheduleReconnect();
            }
        };

        ws.onerror = function(error) {
            log('WebSocket错误:', error);
        };
    }

    // 处理服务器消息
    function handleMessage(data) {
        switch (data.type) {
            case 'update':
                log(`收到人数更新: ${data.count}`);
                updateDisplay(data.count);
                break;

            case 'requestVerification':
                log('服务器请求验证，等待用户行为...');
                break;

            case 'verified':
                log('用户验证成功');
                verified = true;
                startActivityHeartbeat();
                break;

            case 'shutdown':
                log('服务器通知关闭:', data.message);
                if (ws) {
                    ws.close();
                }
                break;

            default:
                log('未知消息类型:', data.type);
        }
    }

    // 计划重连
    function scheduleReconnect() {
        if (reconnectTimer) {
            clearTimeout(reconnectTimer);
        }

        reconnectTimer = setTimeout(() => {
            log('尝试重新连接...');
            connect();
        }, config.reconnectDelay);
    }

    // 清理资源
    function cleanup() {
        if (ws) {
            ws.close();
            ws = null;
        }
        if (reconnectTimer) {
            clearTimeout(reconnectTimer);
            reconnectTimer = null;
        }
        stopActivityHeartbeat();
    }

    // 页面卸载时清理
    window.addEventListener('beforeunload', cleanup);
    window.addEventListener('unload', cleanup);

    // 页面可见性变化处理
    document.addEventListener('visibilitychange', () => {
        if (document.hidden) {
            // 页面隐藏时断开连接以释放资源
            if (ws && ws.readyState === WebSocket.OPEN) {
                ws.close();
            }
        } else {
            // 页面重新可见时重连
            if (!ws || ws.readyState === WebSocket.CLOSED) {
                connect();
            }
        }
    });

    // 初始化
    function init() {
        log('LiveUser 精确统计版本启动');
        log('配置:', config);
        
        // 设置行为检测
        setupBehaviorDetection();
        
        // 连接到服务器
        if (document.readyState === 'loading') {
            document.addEventListener('DOMContentLoaded', connect);
        } else {
            connect();
        }
    }

    // 启动
    init();

})();
