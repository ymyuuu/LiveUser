# 多阶段构建
FROM golang:1.23-alpine AS builder

# 安装依赖
RUN apk add --no-cache git ca-certificates tzdata

# 设置工作目录
WORKDIR /app

# 复制源代码
COPY . .

# 初始化 Go 模块
RUN if [ ! -f go.mod ]; then \
        go mod init github.com/temp/liveuser; \
    fi && \
    go get github.com/gorilla/websocket@latest && \
    go mod tidy

# 构建应用
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.Version=${VERSION}" \
    -o liveuser \
    .

# 最终镜像
FROM alpine:latest

# 安装运行时依赖
RUN apk --no-cache add ca-certificates tzdata wget

# 创建用户
RUN adduser -D -s /bin/sh liveuser

# 设置工作目录
WORKDIR /app

# 复制二进制文件和静态文件
COPY --from=builder /app/liveuser .
COPY --from=builder /app/main.js .
COPY --from=builder /app/demo.html .

# 设置权限
RUN chown -R liveuser:liveuser /app
USER liveuser

# 暴露端口
EXPOSE 10086

# 健康检查
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:10086/ || exit 1

# 启动命令
CMD ["./liveuser"]
