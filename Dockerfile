# 编译阶段
FROM golang:1.24-alpine AS builder
WORKDIR /app

# 复制依赖文件
COPY go.mod go.sum ./
# 下载依赖
RUN go mod download

# 复制所有源代码
COPY . .

# 静态编译
RUN CGO_ENABLED=0 go build -o pr-review-service .

# 运行阶段 (使用 Node.js 镜像，内置 npm)
FROM node:24-alpine3.21
WORKDIR /app

# 安装 git (用于克隆仓库)
RUN apk add --no-cache git ca-certificates

# 安装 Claude CLI
RUN npm install -g @anthropic-ai/claude-code

# 复制二进制文件
COPY --from=builder /app/pr-review-service .
# 复制静态文件
COPY --from=builder /app/static ./static

# 验证 Claude CLI 安装
RUN claude --version || echo "Claude CLI installed"

CMD ["./pr-review-service"]