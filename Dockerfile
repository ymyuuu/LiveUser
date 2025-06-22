# 多阶段构建，减小最终镜像大小
FROM golang:1.23-alpine AS builder

# 安装必要的工具
RUN apk add --no-cache git ca-certificates tzdata

# 设置工作目录
WORKDIR /app

# 复制源代码
COPY . .

# 初始化 Go 模块（如果不存在）
RUN if [ ! -f go.mod ]; then \
        go mod init github.com/temp/liveuser; \
    fi

# 添加依赖
RUN go get github.com/gorilla/websocket@latest

# 整理依赖
RUN go mod tidy

# 构建应用
ARG VERSION=dev
ARG GO_VERSION=1.23

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.Version=${VERSION}" \
    -o liveuser \
    .

# 最终镜像
FROM alpine:latest

# 安装ca证书和时区数据
RUN apk --no-cache add ca-certificates tzdata

# 创建非root用户
RUN adduser -D -s /bin/sh liveuser

# 设置工作目录
WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /app/liveuser .

# 复制必要的静态文件
COPY --from=builder /app/main.js /app/demo.html ./

# 修改文件权限
RUN chown -R liveuser:liveuser /app
USER liveuser

# 暴露端口
EXPOSE 10086

# 健康检查
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:10086/ || exit 1

# 启动应用
CMD ["./liveuser"]
