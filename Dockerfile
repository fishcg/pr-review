# 编译阶段
FROM golang:1.24.4-alpine AS builder
WORKDIR /app

# 复制依赖文件
COPY go.mod go.sum ./
# 下载依赖
RUN go mod download

# 复制所有源代码
COPY . .

# 静态编译
RUN CGO_ENABLED=0 go build -o pr-review-service .

# 运行阶段 (极小镜像)
FROM alpine:latest
WORKDIR /app
# 安装 ca-certificates 否则无法访问 github https
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/pr-review-service .
CMD ["./pr-review-service"]