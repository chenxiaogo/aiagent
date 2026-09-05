# 构建阶段
FROM golang:1.25-alpine AS builder

WORKDIR /app

# 安装 FFmpeg（运行时需要）
RUN apk add --no-cache ffmpeg

# 复制 go mod 并下载依赖
COPY go.mod go.sum ./
RUN go mod download

# 复制源码
COPY . .

# 构建
RUN CGO_ENABLED=0 GOOS=linux go build -o aiagent ./cmd/main.go

# 运行阶段
FROM alpine:latest

WORKDIR /app

# 安装 FFmpeg 和 ca-certificates
RUN apk add --no-cache ffmpeg ca-certificates tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone

# 从构建阶段复制二进制
COPY --from=builder /app/aiagent /app/aiagent

# 复制配置文件模板
COPY conf.d/config.yaml /app/conf.d/config.yaml

# 创建必要目录
RUN mkdir -p /app/uploads /app/logs

EXPOSE 8080

# 健康检查
# HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
#     CMD wget --no-verbose --tries=1 --spider http://localhost:8080/api/health || exit 1

CMD ["./aiagent"]
