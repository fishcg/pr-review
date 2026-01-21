# 编译阶段
FROM 172.24.173.77:30500/golang:1.24-alpine AS builder
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
FROM 172.24.173.77:30500/alpine:latest
WORKDIR /app
# 从 builder 复制 ca-certificates（避免网络问题）
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
# 复制二进制文件
COPY --from=builder /app/pr-review-service .
# 复制静态文件
COPY --from=builder /app/static ./static
CMD ["./pr-review-service"]