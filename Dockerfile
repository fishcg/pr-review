# 编译阶段
FROM golang:1.24-alpine AS builder
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

# 运行阶段 (使用 Node.js Debian slim 镜像：glibc，兼容 codegraph 原生二进制)
FROM node:24.13.0-slim
WORKDIR /app

# 设置时区
ENV TZ=Asia/Shanghai

# 安装 git (用于克隆仓库) 及证书、时区数据
# 同时兼容旧版 sources.list 与新版 deb822(.sources) 两种格式，换用阿里云镜像加速
RUN sed -i 's@deb.debian.org@mirrors.aliyun.com@g; s@security.debian.org@mirrors.aliyun.com@g' /etc/apt/sources.list 2>/dev/null; \
    sed -i 's@deb.debian.org@mirrors.aliyun.com@g; s@security.debian.org@mirrors.aliyun.com@g' /etc/apt/sources.list.d/debian.sources 2>/dev/null; \
    apt-get update \
    && apt-get install -y --no-install-recommends git ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*

# 安装 Claude CLI
RUN npm install -g @anthropic-ai/claude-code && printf '{\n  "hasCompletedOnboarding": true,\n  "preferredLoginMethod": "console"\n}\n' > /root/.claude.json

# 安装 CodeGraph（语义索引 + MCP server，给 Claude/Codex 当代码探索的旁路工具）
RUN npm install -g @colbymchenry/codegraph && codegraph --version

# 复制二进制文件
COPY --from=builder /app/pr-review-service .
# 复制静态文件
COPY --from=builder /app/static ./static

# 验证 Claude CLI 安装
RUN claude --version || echo "Claude CLI installed"

CMD ["./pr-review-service"]