# 编译阶段
FROM 172.24.173.77:30500/golang:1.24-alpine AS builder
WORKDIR /app

ENV GOPROXY=https://goproxy.cn,direct

# 复制依赖文件
COPY go.mod go.sum ./
# 下载依赖
RUN go mod download

# 复制所有源代码
COPY . .

# 静态编译
RUN CGO_ENABLED=0 go build -o pr-review-service .

# 运行阶段 (使用 Node.js 镜像，内置 npm)
FROM 172.24.173.77:30500/node:24.13.0-alpine
WORKDIR /app

# 设置时区
ENV TZ=Asia/Shanghai

# 安装 git (用于克隆仓库)
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
    && apk add --no-cache git ca-certificates tzdata

# 安装 Claude CLI
RUN npm install -g @anthropic-ai/claude-code && printf '{\n  "hasCompletedOnboarding": true,\n  "preferredLoginMethod": "console"\n}\n' > /root/.claude.json

# 复制二进制文件
COPY --from=builder /app/pr-review-service .
# 复制静态文件
COPY --from=builder /app/static ./static

# 验证 Claude CLI 安装
RUN claude --version || echo "Claude CLI installed"

CMD ["./pr-review-service"]