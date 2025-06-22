# LiveUser

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Docker](https://img.shields.io/badge/Docker-Supported-2496ED?style=flat&logo=docker)](https://hub.docker.com/)

基于 Go + WebSocket 的高性能实时在线用户统计服务。

---

**联系方式**

[![Telegram](https://img.shields.io/badge/群聊-HeroCore-blue?logo=telegram&logoColor=white)](https://t.me/HeroCore) 
[![Telegram](https://img.shields.io/badge/频道-HeroMsg-blue?logo=telegram&logoColor=white)](https://t.me/HeroMsg)
[![Email](https://img.shields.io/badge/邮箱-联系我们-red?logo=gmail&logoColor=white)](mailto:admin@030101.xyz)

---


## 开始

### 方式一：下载二进制文件

1. 从 [Releases](https://github.com/your-repo/LiveUser/releases) 下载对应平台的二进制文件
2. 解压并运行：

```bash
# 解压
tar -xzf liveuser-v1.0.0-linux-amd64.tar.gz

# 运行（默认监听 0.0.0.0:10086）
./liveuser

# 自定义端口
./liveuser -addr :8080

# 自定义IP和端口
./liveuser -addr 127.0.0.1:3000
```

### 方式二：Docker 部署

```bash
# 使用默认端口
docker run -d -p 10086:10086 --name liveuser username/liveuser:latest

# 自定义端口
docker run -d -p 8080:8080 --name liveuser username/liveuser:latest ./liveuser -addr 0.0.0.0:8080

# 使用 Docker Compose
cat > docker-compose.yml << EOF
version: '3.8'
services:
  liveuser:
    image: username/liveuser:latest
    ports:
      - "10086:10086"
    command: ["./liveuser", "-addr", "0.0.0.0:10086"]
    restart: unless-stopped
EOF

docker-compose up -d
```

### 方式三：源码构建

```bash
# 克隆仓库
git clone https://github.com/your-repo/LiveUser.git
cd LiveUser

# 运行
go run main.go

# 构建
go build -o liveuser .
```

## 使用方法

### 基础集成

在你的网页中添加以下代码：

```html
<!-- 1. 添加显示元素 -->
<span id="liveuser">加载中...</span>

<!-- 2. 引入统计脚本 -->
<script src="https://your-domain.com/liveuser.js"></script>
```

### 高级配置

通过 URL 参数自定义配置：

```html
<!-- 自定义站点ID -->
<script src="https://your-domain.com/liveuser.js?siteId=blog"></script>

<!-- 关闭调试模式 -->
<script src="https://your-domain.com/liveuser.js?debug=false"></script>

<!-- 完整配置 -->
<script src="https://your-domain.com/liveuser.js?siteId=my-site&displayElementId=counter&debug=false&reconnectDelay=5000"></script>
```

### CSS 样式定制

```css
#liveuser {
  transition: all 0.3s ease;
  display: inline-block;
  font-weight: bold;
  color: #333;
}

/* 数字更新时的动画效果 */
#liveuser.updating {
  transform: scale(1.2);
  color: #FFB800;
}
```

## 性能

- **并发连接**：支持万级 WebSocket 并发连接
- **内存占用**：基础内存占用约 10MB，每个连接额外占用约 4KB
- **CPU 使用**：空闲时 CPU 使用率接近 0%
- **网络带宽**：每次人数更新约消耗 50 字节流量

## 许可证

本项目基于 MIT 许可证开源。查看 [LICENSE](LICENSE) 文件了解更多信息。
