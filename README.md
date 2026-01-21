# PR Review Service

基于 AI 的自动代码审查服务，支持 GitHub Pull Request 自动审查。

## 功能特性

- ✅ 自动获取 GitHub PR 的代码变更
- ✅ 调用 AI 服务进行代码审查
- ✅ 自动将审查结果评论到 PR
- ✅ 支持代码质量评分（满分 10 分）
- ✅ 全面的安全检查（SQL 注入、XSS、权限等）
- ✅ 性能和代码规范建议
- ✅ 可配置的 Prompt 模板

## 快速开始

### 1. 配置文件

复制示例配置文件并填写你的配置：

```bash
cp config.yaml.example config.yaml
```

编辑 `config.yaml`，填写以下信息：
- `ai_api_url`: 你的 AI 服务地址
- `ai_api_key`: AI 服务的 API Key
- `github_token`: GitHub Personal Access Token（需要 `repo` 或 `public_repo` 权限）

### 2. 安装依赖

```bash
go mod tidy
```

### 3. 构建

```bash
go build -o pr-review-service main.go
```

### 4. 运行

```bash
./pr-review-service
```

服务将在配置的端口启动（默认 7995）。

## API 使用

### 触发 PR 审查

**端点**: `POST /review`

**请求头**:
- `Content-Type: application/json`
- `X-Github-Token: <github_token>` (可选，如果未设置则使用配置文件中的 token)

**请求体**:
```json
{
  "repo": "owner/repo-name",
  "pr_number": 123
}
```

**响应**:
```
Review started for owner/repo-name #123
```

### 健康检查

**端点**: `GET /health`

**响应**:
```
ok
```

## 配置说明

### AI 服务配置

- `ai_api_url`: AI 服务的 API 地址（OpenAI 格式）
- `ai_api_key`: API 认证密钥
- `ai_model`: 使用的模型名称（如 `qwen-plus-latest`）

### Prompt 配置

你可以自定义 AI 审查的 Prompt：

- `system_prompt`: 定义 AI 的角色和行为
- `user_prompt_template`: 审查请求模板，使用 `{diff}` 作为代码变更的占位符

## 审查输出格式

AI 将按照以下结构输出审查结果：

1. **📊 代码质量评分** - 多维度评分（总分、规范、功能、安全、性能、可维护性）
2. **✅ 做得好的地方** - 正面反馈
3. **⚠️ 需要注意的问题** - 分为严重问题和建议优化
4. **🔒 安全检查** - 安全漏洞检测
5. **⚡ 性能建议** - 性能优化建议
6. **📝 代码规范** - 命名、注释等规范建议
7. **💡 总体建议** - 总结和改进方向

## Docker 部署

```bash
# 构建镜像
docker build -t pr-review-service:v1 .

# 运行容器
docker run -d \
  -p 7995:7995 \
  -v $(pwd)/config.yaml:/app/config.yaml \
  pr-review-service:v1
```

## Kubernetes 部署

参考 `k8s.yaml` 文件进行部署：

```bash
kubectl apply -f k8s.yaml
```

**注意**: 需要先创建包含配置的 ConfigMap 或 Secret。

## 示例：GitHub Actions 集成

在你的仓库中创建 `.github/workflows/pr-review.yml`：

```yaml
name: AI PR Review

on:
  pull_request:
    types: [opened, synchronize]

jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - name: Trigger AI Review
        run: |
          curl -X POST http://your-pr-review-service:7995/review \
            -H "Content-Type: application/json" \
            -H "X-Github-Token: ${{ secrets.GITHUB_TOKEN }}" \
            -d '{
              "repo": "${{ github.repository }}",
              "pr_number": ${{ github.event.pull_request.number }}
            }'
```

## 开发

### 项目结构

```
.
├── main.go              # 程序入口，启动逻辑
├── config.go            # 配置管理
├── lib/                 # 第三方服务集成库
│   ├── ai.go           # AI 服务客户端
│   └── github.go       # GitHub API 客户端
├── router/              # HTTP 路由处理
│   └── handler.go      # 请求处理器
├── config.yaml          # 配置文件（不提交到 git）
├── config.yaml.example  # 配置文件示例
├── Dockerfile           # Docker 构建文件
├── k8s.yaml            # Kubernetes 部署文件
├── go.mod              # Go 依赖管理
└── README.md           # 说明文档
```

### 代码架构

**根目录**
- **main.go** - 程序入口点，负责加载配置、设置路由和启动 HTTP 服务器
- **config.go** - 配置文件加载、验证和访问接口

**lib/** - 第三方服务集成
- **ai.go** - AI 服务客户端，负责调用 AI 进行代码审查
  - `AIClient` - AI 客户端结构体
  - `ReviewCode()` - 调用 AI 审查代码
- **github.go** - GitHub API 客户端，处理 PR diff 获取和评论发布
  - `GitHubClient` - GitHub 客户端结构体
  - `GetPRDiff()` - 获取 PR 代码变更
  - `PostComment()` - 发布评论到 PR

**router/** - HTTP 路由和业务逻辑
- **handler.go** - HTTP 路由处理器，协调整个审查流程
  - `HandleReview()` - 处理审查请求
  - `HandleHealth()` - 健康检查
  - `ProcessReview()` - 完整的审查流程编排

### 技术栈

- Go 1.21+
- GitHub API
- OpenAI 格式 API（兼容通义千问等）

## 注意事项

1. **敏感信息安全**: 不要将 `config.yaml` 提交到 git，它包含 API Key 和 Token
2. **GitHub Token 权限**: Token 需要有读取 PR 和写评论的权限
3. **代码长度限制**: 默认截断超过 6000 字符的 diff，避免 AI 处理超时
4. **异步处理**: PR 审查是异步进行的，不会阻塞 HTTP 请求

## License

MIT
