# 编译阶段
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY main.go .
# 静态编译
RUN CGO_ENABLED=0 go build -o pr-review-service main.go

# 运行阶段 (极小镜像)
FROM alpine:latest
WORKDIR /app
# 安装 ca-certificates 否则无法访问 github https
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/pr-review-service .
CMD ["./pr-review-service"]