# PR Review Service

基于 AI 的自动代码审查服务，支持 GitHub Pull Request 和 GitLab Merge Request 自动审查。

**项目说明主页**: [https://ci.acgay.cn](https://ci.acgay.cn)

![审查效果示例](https://acgay.oss-cn-hangzhou.aliyuncs.com/ai/images/cr.png)

## 功能特性

- ✅ 支持 GitHub 和 GitLab 双平台
- ✅ **双模式审查**：API 模式（快速）+ Claude CLI 模式（深度理解项目上下文）
- ✅ 自动获取 PR/MR 的代码变更
- ✅ 调用 AI 服务进行代码审查
- ✅ 自动将审查结果评论到 PR/MR
- ✅ 支持代码质量评分（满分 100 分）
- ✅ 全面的安全检查（SQL 注入、XSS、权限等）
- ✅ 性能和代码规范建议
- ✅ 可配置的 Prompt 模板
- ✅ 支持私有 GitLab 实例
- ✅ Webhook 自动触发审查

## 目录

- [快速开始](#快速开始)
- [配置说明](#配置说明)
  - [Review 模式](#review-模式)
  - [VCS Provider 配置](#vcs-provider-配置)
  - [AI 服务配置](#ai-服务配置)
  - [Claude CLI 配置](#claude-cli-配置)
  - [仓库克隆配置](#仓库克隆配置)
  - [Prompt 配置](#prompt-配置)
- [API 使用](#api-使用)
- [Webhook 自动触发配置](#webhook-自动触发配置)
  - [GitHub Webhook 配置](#github-webhook-配置)
  - [GitLab Webhook 配置](#gitlab-webhook-配置)
- [部署](#部署)
  - [Docker 部署](#docker-部署)
  - [Kubernetes 部署](#kubernetes-部署)
- [开发](#开发)
- [常见问题](#常见问题)
- [安全建议](#安全建议)

---

## 快速开始

### 1. 配置文件

复制示例配置文件并填写你的配置：

```bash
cp config.yaml.example config.yaml
```

编辑 `config.yaml`，填写以下信息：

```yaml
# Review 模式: "api" 或 "claude_cli"
review_mode: "api"  # 推荐先使用 api 模式测试

# AI 服务配置（Open AI 接口兼容）
ai_api_url: "https://dashscope.aliyuncs.com/compatible-mode/v1"
ai_api_key: "your-api-key"
ai_model: "qwen-plus-latest"

# VCS Provider 配置
vcs_provider: "github"  # 或 "gitlab"

# GitHub 配置
github_token: "ghp_xxxxxxxxxxxx"  # 需要 repo 权限

# 或 GitLab 配置
gitlab_token: "glpat-xxxxxxxxxxxx"  # 需要 api, read_api, write_repository 权限
gitlab_base_url: ""  # 留空使用 gitlab.com

# Claude CLI 配置（仅当 review_mode: "claude_cli" 时需要）
claude_cli:
  binary_path: "claude"
  allowed_tools: ["Read", "Glob", "Grep", "Bash"]
  timeout: 600
  max_output_length: 100000
  api_key: "sk-ant-xxxxxxxxxxxxx"  # Anthropic API Key
  api_url: ""  # 可选
```

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

---

## 配置说明

### Review 模式

服务支持两种审查模式：

#### API 模式（推荐用于快速审查）

```yaml
review_mode: "api"
```

**特点**:
- ✅ 速度快（5-15 秒）
- ✅ 仅基于 PR/MR 的 diff 进行审查
- ✅ 适合简单的代码变更
- ⚠️ 缺乏项目整体上下文

#### Claude CLI 模式（推荐用于深度审查）

```yaml
review_mode: "claude_cli"

# Claude CLI 配置
claude_cli:
  binary_path: "claude"           # Claude CLI 二进制路径
  allowed_tools: ["Read", "Glob", "Grep", "Bash"]
  timeout: 600                    # 超时秒数（10分钟）
  max_output_length: 100000       # 最大输出长度

# 仓库克隆配置
repo_clone:
  temp_dir: "/tmp/pr-review-repos"
  clone_timeout: 180              # 克隆超时秒数
  shallow_clone: true             # 使用浅克隆
  shallow_depth: 100              # 浅克隆深度
  cleanup_after_review: true      # 审查后自动清理
```

**特点**:
- ✅ **深度理解项目上下文**：可读取项目中的其他文件
- ✅ **智能探索代码**：使用 Read、Glob、Grep 工具理解代码结构
- ✅ **更准确的审查**：基于整个项目而非单独的 diff
- ⚠️ 速度较慢（1-5 分钟）
- ⚠️ 需要安装 Claude CLI（`npm install -g @anthropic-ai/claude-code`）

**工作流程**:
1. Clone 仓库到临时目录（使用 commit SHA 命名避免冲突）
2. Checkout 到 PR/MR 的源分支
3. 执行 Claude CLI，Claude 可以：
   - 使用 Read 工具查看项目文件
   - 使用 Glob 工具查找相关文件
   - 使用 Grep 工具搜索代码
   - 使用 Bash 工具执行 git 命令
4. 基于整个项目上下文生成审查报告
5. 自动清理临时目录（每小时清理超过 24 小时的仓库）

### VCS Provider 配置

- `vcs_provider`: 版本控制系统类型（`github` 或 `gitlab`，默认 `github`）

#### GitHub 配置

```yaml
vcs_provider: "github"
github_token: "ghp_xxxxxxxxxxxx"
webhook_secret: ""  # 可选，建议配置用于验证 webhook 请求
```

**Token 权限要求**:
- `repo` - 完整仓库访问权限（私有仓库）
- 或 `public_repo` - 公开仓库访问权限（仅公开仓库）

#### GitLab 配置

```yaml
vcs_provider: "gitlab"
gitlab_token: "glpat-xxxxxxxxxxxx"
gitlab_base_url: ""  # 留空使用 gitlab.com，私有实例填写完整地址
gitlab_webhook_token: ""  # 可选，建议配置用于验证 webhook 请求
```

**Token 权限要求**:
- `api` - 完整的 API 访问权限
- `read_api` - 读取 API 权限
- `write_repository` - 写入仓库权限

**私有 GitLab 实例**:
```yaml
gitlab_base_url: "https://gitlab.company.com"
```

### AI 服务配置

```yaml
ai_api_url: "https://dashscope.aliyuncs.com/compatible-mode/v1"
ai_api_key: "your-api-key"
ai_model: "qwen-plus-latest"
inline_issue_comment: true      # 行内评论模式
comment_only_changes: true      # 仅对修改行发布评论
```

**配置项说明**:

- `inline_issue_comment`: 开启后，问题拆分为行内评论，PR/MR 大评论仅保留评分/修改点/总结
- `comment_only_changes`: 开启后，只对修改的代码行（+/-）发布评论
  - `true`: 上下文行的问题不会出现在任何评论中
  - `false` (GitHub): 可以对上下文行发布行内评论
  - `false` (GitLab): 上下文行无法发布行内评论（API 限制），但会在主评论中列出

### Claude CLI 配置

仅在 `review_mode: "claude_cli"` 时需要配置：

```yaml
claude_cli:
  binary_path: "claude"           # Claude CLI 二进制路径
  allowed_tools:                  # 允许 Claude 使用的工具
    - "Read"                      # 读取文件
    - "Glob"                      # 查找文件
    - "Grep"                      # 搜索代码
    - "Bash"                      # 执行 git 命令
  timeout: 600                    # 超时秒数（10分钟）
  max_output_length: 100000       # 最大输出长度
  api_key: "sk-ant-xxxxxxxxxxxxx" # Anthropic API Key（必填）
  api_url: ""                     # Anthropic API URL（可选，默认官方 API）
```

**安装 Claude CLI**:
```bash
npm install -g @anthropic-ai/claude-code
```

**获取 Anthropic API Key**:
1. 访问 https://console.anthropic.com/
2. 登录并创建 API Key
3. 将 API Key 配置到 `claude_cli.api_key`

**配置说明**:
- `api_key`: Anthropic API Key/Token，用于调用 Claude API
  - 如果配置了值，会使用配置的 key（覆盖环境变量）
  - 如果留空（`""`），会使用环境变量 `ANTHROPIC_AUTH_TOKEN`
  - 如果环境变量也没有，会使用 Claude CLI 的全局配置
- `api_url`: 自定义 API Base URL（可选）
  - 留空使用默认 API 地址
  - 如果配置了值，会覆盖环境变量 `ANTHROPIC_BASE_URL`

**Claude CLI 环境变量**:
- `ANTHROPIC_AUTH_TOKEN`: 认证令牌（不是 `ANTHROPIC_API_KEY`）
- `ANTHROPIC_BASE_URL`: API 基础地址（不是 `ANTHROPIC_API_URL`）

**优先级（从高到低）**:
1. 配置文件（`config.yaml` 中的 `claude_cli.api_key` 和 `claude_cli.api_url`）
2. 环境变量（`ANTHROPIC_AUTH_TOKEN` 和 `ANTHROPIC_BASE_URL`）
3. Claude CLI 全局配置（`~/.config/claude/config.json`）

### 仓库克隆配置

仅在 `review_mode: "claude_cli"` 时需要配置：

```yaml
repo_clone:
  temp_dir: "/tmp/pr-review-repos"  # 临时目录
  clone_timeout: 180                # 克隆超时秒数（3分钟）
  shallow_clone: true               # 使用浅克隆（推荐）
  shallow_depth: 100                # 浅克隆深度
  cleanup_after_review: true        # 审查后自动清理
```

**目录命名规则**:
- 使用 commit SHA 前 8 位命名：`repo-name-abc12345`
- 避免并发审查时的目录冲突
- 如果目录已存在，自动删除并重新 clone

**自动清理**:
- 每小时自动清理超过 24 小时的仓库目录
- 审查完成后立即清理（如果 `cleanup_after_review: true`）

### Prompt 配置

自定义 AI 审查的 Prompt：

```yaml
system_prompt: |
  你是一个专业的代码审查助手。请对提供的代码变更进行全面的审查，关注以下方面：
  1. **逻辑错误与 Bug**：是否存在潜在的逻辑漏洞、边界条件处理不当或空指针风险？
  2. **代码质量与可读性**：是否遵循 Clean Code 原则？变量命名是否清晰？
  3. **性能优化**：是否存在不必要的循环、内存泄露或可以优化的算法？
  4. **安全性**：是否存在常见的安全漏洞（SQL 注入、XSS、敏感信息泄露等）？
  5. **可测试性**：代码是否易于编写单元测试？
  6. **最佳实践**：是否符合该编程语言/框架的主流社区最佳实践？

user_prompt_template: |
  请审查以下代码变更：

  {diff}

  请以以下格式输出审查结果：

  ## 评分
  评分：X/100（满分 100，严重bug<60，有语法错误=0，轻微问题扣5-10分）

  ## 修改点
  1. [简要描述主要修改]
  2. [简要描述主要修改]

  ## 总结
  [一句话评价，是否建议合入（建议合入时打✅标记，否则打❌）]

  ## 详细问题
  如果有具体问题，请使用表格格式：

  | 文件名 | 旧行号 | 新行号 | 代码片段 | 严重程度 | 类别 | 问题描述 | 建议修改 |
  |--------|--------|--------|----------|----------|------|----------|----------|
```

**注意事项**:
- `user_prompt_template` 中必须包含 `{diff}` 占位符
- 在 Claude CLI 模式下，会在 prompt 前添加工具使用指导
- 如果需要行内评论功能，必须在 prompt 中要求 Claude 输出表格格式

---

## API 使用

### 触发 PR/MR 审查

**端点**: `POST /review`

**请求头**:
- `Content-Type: application/json`
- `X-Github-Token: <github_token>` (GitHub，可选)
- `PRIVATE-TOKEN: <gitlab_token>` (GitLab，可选)

**请求体**:

GitHub PR:
```json
{
  "repo": "owner/repo-name",
  "pr_number": 123,
  "provider": "github"
}
```

GitLab MR:
```json
{
  "repo": "group/project",
  "pr_number": 45,
  "provider": "gitlab"
}
```

> **注意**：`provider` 字段可选，未指定时使用配置文件中的 `vcs_provider` 设置

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

---

## Webhook 自动触发配置

配置 Webhook 后，当以下事件发生时，系统会自动触发 AI 代码审查：

**GitHub PR**:
- ✅ PR 被创建（`opened`）
- ✅ PR 有新的 commit 推送（`synchronize`）
- ✅ PR 被重新打开（`reopened`）

**GitLab MR**:
- ✅ MR 被创建（`open`）
- ✅ MR 有新的 commit 推送（`update`）
- ✅ MR 被重新打开（`reopen`）

### GitHub Webhook 配置

#### 1. 生成 Webhook Secret（可选但推荐）

```bash
# 生成随机 secret
openssl rand -hex 32
```

将生成的 secret 添加到 `config.yaml`：

```yaml
vcs_provider: "github"
webhook_secret: "your-generated-secret-here"
```

#### 2. 在 GitHub 仓库配置 Webhook

1. 进入你的 GitHub 仓库
2. 点击 **Settings** → **Webhooks** → **Add webhook**
3. 填写 Webhook 配置：

   - **Payload URL**: `http://your-service-url/webhook`
   - **Content type**: 选择 `application/json`
   - **Secret**: 填写步骤 1 中生成的 secret
   - **Which events would you like to trigger this webhook?**
     - 选择 **Let me select individual events**
     - 勾选 **Pull requests** ✅
   - **Active**: 勾选 ✅

4. 点击 **Add webhook**

#### 3. 验证配置

**方法 1: 查看 Webhook 日志**

在 GitHub Webhook 设置页面，点击刚创建的 webhook，查看 **Recent Deliveries** 标签页。

**方法 2: 创建测试 PR**

1. 创建测试分支并提交改动
2. 创建 Pull Request
3. 查看服务日志：

```log
📨 Received GitHub webhook: pull_request
🎯 Triggering review for owner/repo #123 (commit: abc1234)
📥 Received review request for owner/repo #123
🔍 [owner/repo#123] Fetching PR diff...
🤖 [owner/repo#123] Sending to AI for review...
📝 [owner/repo#123] Posting review comment...
✅ [owner/repo#123] Review completed successfully!
```

### GitLab Webhook 配置

#### 1. 生成 Webhook Token（可选但推荐）

```bash
# 生成随机 token
openssl rand -hex 32
```

将生成的 token 添加到 `config.yaml`：

```yaml
vcs_provider: "gitlab"
gitlab_webhook_token: "your-generated-token-here"
```

#### 2. 在 GitLab 项目配置 Webhook

1. 进入你的 GitLab 项目
2. 点击 **Settings** → **Webhooks**
3. 填写 Webhook 配置：

   - **URL**: `http://your-service-url/webhook`
   - **Secret token**: 填写步骤 1 中生成的 token
   - **Trigger**: 勾选 **Merge request events** ✅
   - **Enable SSL verification**: 如果使用 HTTPS，建议勾选 ✅

4. 点击 **Add webhook**

#### 3. 验证配置

**方法 1: 使用 GitLab 的测试功能**

在 GitLab Webhook 设置页面，点击刚创建的 webhook 右侧的 **Test** → **Merge request events**。

查看响应：
- HTTP 状态码应该是 `202 Accepted`
- 响应体：`Review triggered for group/project !123`

**方法 2: 创建测试 MR**

1. 创建测试分支并提交改动
2. 创建 Merge Request
3. 查看服务日志：

```log
📨 Received GitLab webhook: Merge Request Hook
🎯 Triggering review for group/project !45
📥 Received review request for group/project #45 (provider: gitlab)
🔧 [group/project#45] Using VCS provider: gitlab
🔍 [group/project#45] Fetching diff...
🤖 [group/project#45] Sending to AI for review...
📝 [group/project#45] Posting review comment...
✅ [group/project#45] Review completed successfully!
```

#### 4. 私有 GitLab 实例配置

```yaml
vcs_provider: "gitlab"
gitlab_token: "glpat-xxxxxxxxxxxxxxxxxxxxx"
gitlab_base_url: "https://gitlab.company.com"
gitlab_webhook_token: "your-secret-token"
```

**注意**：
- 确保服务可以访问私有 GitLab 实例的网络
- 如果使用自签名证书，可能需要配置 SSL 证书信任

---

## 部署

### Docker 部署

Docker 镜像已内置 Claude CLI 支持，包含以下组件：
- ✅ Git（用于仓库克隆）
- ✅ Node.js 和 npm
- ✅ Claude CLI (`@anthropic-ai/claude-code`)

```bash
# 构建镜像
docker build -t pr-review-service:v1 .

# 运行容器（API 模式）
docker run -d \
  -p 7995:7995 \
  -v $(pwd)/config.yaml:/app/config.yaml \
  pr-review-service:v1

# 运行容器（Claude CLI 模式）
docker run -d \
  -p 7995:7995 \
  -v $(pwd)/config.yaml:/app/config.yaml \
  -v /tmp/pr-review-repos:/tmp/pr-review-repos \
  pr-review-service:v1

# 或通过环境变量传递 API Key（推荐，更安全）
docker run -d \
  -p 7995:7995 \
  -v $(pwd)/config.yaml:/app/config.yaml \
  -v /tmp/pr-review-repos:/tmp/pr-review-repos \
  -e ANTHROPIC_AUTH_TOKEN="sk-ant-xxxxxxxxxxxxx" \
  -e ANTHROPIC_BASE_URL="https://api.anthropic.com" \
  pr-review-service:v1
```

**注意**：
- Claude CLI 模式需要挂载临时目录（`/tmp/pr-review-repos`）用于仓库克隆
- 确保 `config.yaml` 中的 `repo_clone.temp_dir` 与挂载路径一致
- **必须配置 Anthropic API Key**（在 config.yaml 或通过环境变量 `ANTHROPIC_AUTH_TOKEN`）
- ⚠️ Claude CLI 使用 `ANTHROPIC_AUTH_TOKEN`，不是 `ANTHROPIC_API_KEY`

### Kubernetes 部署

参考 `k8s.yaml` 文件进行部署：

```bash
kubectl apply -f k8s.yaml
```

**配置说明**:

1. **资源配置**（`k8s.yaml` 已针对 Claude CLI 模式优化）:
   - API 模式：100Mi 内存 + 100m CPU（requests）
   - Claude CLI 模式：1Gi 内存 + 1000m CPU（limits）

2. **存储配置**:
   - 使用 `emptyDir` 挂载 `/tmp/pr-review-repos`
   - 限制大小为 5Gi
   - 每个 Pod 独立的临时存储

3. **启用 Claude CLI 模式**:
   - 编辑 ConfigMap 中的 `review_mode: "claude_cli"`
   - 配置已包含 `claude_cli` 和 `repo_clone` 相关参数
   - 重新应用配置：`kubectl apply -f k8s.yaml`
   - 重启 Pod：`kubectl rollout restart deployment/pr-review-service`

**注意**:
- Claude CLI 需要更多资源，建议在生产环境中调整资源限制
- 使用 `emptyDir` 意味着 Pod 重启会丢失临时数据（这是预期行为）
- 如果需要持久化，可以使用 PVC 替代 `emptyDir`

#### 使用 NodePort（外部访问）

```yaml
apiVersion: v1
kind: Service
metadata:
  name: pr-review-service
spec:
  type: NodePort
  ports:
    - port: 7995
      targetPort: 7995
      nodePort: 30095
```

Webhook URL: `http://<your-node-ip>:30095/webhook`

#### 使用 Ingress（推荐）

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: pr-review-ingress
spec:
  rules:
    - host: pr-review.your-domain.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: pr-review-service
                port:
                  number: 7995
```

Webhook URL: `https://pr-review.your-domain.com/webhook`

### CI/CD 集成示例

#### GitHub Actions 集成

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

#### GitLab CI 集成

在你的仓库中创建 `.gitlab-ci.yml`：

```yaml
ai-review:
  stage: review
  only:
    - merge_requests
  script:
    - |
      curl -X POST http://your-pr-review-service:7995/review \
        -H "Content-Type: application/json" \
        -H "PRIVATE-TOKEN: $GITLAB_TOKEN" \
        -d "{
          \"repo\": \"$CI_PROJECT_PATH\",
          \"pr_number\": $CI_MERGE_REQUEST_IID,
          \"provider\": \"gitlab\"
        }"
```

---

## 开发

### 项目结构

```
.
├── main.go              # 程序入口，启动逻辑
├── config.go            # 配置管理
├── lib/                 # 第三方服务集成库
│   ├── ai.go           # AI 服务客户端
│   ├── claude_cli.go   # Claude CLI 客户端
│   ├── repo_manager.go # 仓库克隆和管理
│   ├── provider.go     # VCS Provider 接口定义
│   ├── github.go       # GitHub API 客户端
│   └── gitlab.go       # GitLab API 客户端
├── router/              # HTTP 路由处理
│   ├── handler.go      # 请求处理器
│   ├── webhook_github.go  # GitHub Webhook 处理
│   └── webhook_gitlab.go  # GitLab Webhook 处理
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
- **claude_cli.go** - Claude CLI 客户端，在克隆的仓库中执行深度审查
- **repo_manager.go** - 仓库克隆、checkout 和清理管理
- **provider.go** - VCS Provider 接口定义
- **github.go** - GitHub API 客户端实现
- **gitlab.go** - GitLab API 客户端实现

**router/** - HTTP 路由和业务逻辑
- **handler.go** - HTTP 路由处理器，协调整个审查流程
- **webhook_github.go** - GitHub Webhook 事件处理
- **webhook_gitlab.go** - GitLab Webhook 事件处理

### 技术栈

- Go 1.21+
- GitHub API / GitLab API
- OpenAI 格式 API（兼容通义千问等）
- Claude CLI (optional)
- Provider 接口抽象设计

### 审查输出格式

AI 将按照以下结构输出审查结果：

1. **📊 代码质量评分** - 多维度评分（总分、规范、功能、安全、性能、可维护性）
2. **✅ 做得好的地方** - 正面反馈
3. **⚠️ 需要注意的问题** - 分为严重问题和建议优化
4. **🔒 安全检查** - 安全漏洞检测
5. **⚡ 性能建议** - 性能优化建议
6. **📝 代码规范** - 命名、注释等规范建议
7. **💡 总体建议** - 总结和改进方向

---

## 常见问题

### Review 模式相关

#### Q: API 模式和 Claude CLI 模式有什么区别？

**API 模式**:
- 只基于 diff 文本进行审查
- 速度快（5-15 秒）
- 适合简单的代码变更

**Claude CLI 模式**:
- 克隆完整仓库，可以读取任何项目文件
- Claude 可以使用 Read/Glob/Grep/Bash 工具探索代码
- 基于整个项目上下文进行审查
- 速度较慢（1-5 分钟）
- 需要安装 Claude CLI

#### Q: Claude CLI 模式需要哪些准备？

1. 安装 Claude CLI：`npm install -g @anthropic-ai/claude-code`
2. 获取 Anthropic API Key：https://console.anthropic.com/
3. 配置 `review_mode: "claude_cli"`
4. 配置 `claude_cli` 相关参数（包括 `api_key`）
5. 配置 `repo_clone` 相关参数
6. 确保 VCS Token 有克隆仓库的权限（HTTPS + Token 认证）

#### Q: 临时目录占用磁盘空间怎么办？

系统会自动清理：
- 每小时清理超过 24 小时的仓库
- 审查完成后立即清理（如果 `cleanup_after_review: true`）
- 可以手动清理：`rm -rf /tmp/pr-review-repos/*`

#### Q: 如何配置 Anthropic API Key？

有多种配置方式，按优先级从高到低排列：

**方法 1: 配置文件（最高优先级）**
```yaml
claude_cli:
  api_key: "sk-ant-xxxxxxxxxxxxx"  # 会覆盖环境变量
  api_url: ""                      # 可选，留空使用默认
```
- ✅ 推荐用于开发环境
- ✅ 明确知道使用的是哪个 key
- ⚠️ 不要提交到 git

**方法 2: 环境变量（第二优先级）**
```bash
export ANTHROPIC_AUTH_TOKEN="sk-ant-xxxxxxxxxxxxx"
export ANTHROPIC_BASE_URL="https://api.anthropic.com"  # 可选
```
- ✅ 推荐用于生产环境
- ✅ 更安全，不会泄露到代码仓库
- 💡 配置文件中 `api_key` 留空（`""`）时才会生效
- ⚠️ 注意：Claude CLI 使用 `ANTHROPIC_AUTH_TOKEN`，不是 `ANTHROPIC_API_KEY`

**方法 3: Docker 环境变量**
```bash
docker run \
  -e ANTHROPIC_AUTH_TOKEN="sk-ant-xxxxxxxxxxxxx" \
  -e ANTHROPIC_BASE_URL="https://api.anthropic.com" \
  ...
```

**方法 4: Kubernetes Secret**
```yaml
containers:
  - name: server
    env:
      - name: ANTHROPIC_AUTH_TOKEN
        valueFrom:
          secretKeyRef:
            name: claude-api-secret
            key: auth-token
      - name: ANTHROPIC_BASE_URL
        value: "https://api.anthropic.com"  # 可选
```

**方法 5: Claude CLI 全局配置（最低优先级）**
```bash
# Claude CLI 会自动使用 ~/.config/claude/config.json
# 仅当配置文件和环境变量都没有设置时才会使用
```

**优先级总结**:
```
配置文件 > 环境变量 > Claude CLI 全局配置
```

**实际场景示例**:

场景 1: 只配置了 api_key，没有配置 api_url
```yaml
claude_cli:
  api_key: "sk-ant-xxxxxxxxxxxxx"
  api_url: ""  # 留空
```
→ 使用配置的 api_key + Claude CLI 默认 URL

场景 2: 什么都没配置
```yaml
claude_cli:
  api_key: ""  # 留空
  api_url: ""  # 留空
```
→ 使用环境变量 `ANTHROPIC_AUTH_TOKEN` 或 Claude CLI 全局配置

场景 3: 配置文件和环境变量都设置了
```yaml
claude_cli:
  api_key: "sk-ant-config-key"
```
```bash
export ANTHROPIC_AUTH_TOKEN="sk-ant-env-key"
```
→ 使用配置文件中的 `sk-ant-config-key`（配置文件优先级更高）

### GitHub 相关

#### Q: Webhook 触发失败，返回 401 Unauthorized

**原因**: Webhook secret 配置不一致

**解决**:
1. 检查 `config.yaml` 中的 `webhook_secret` 是否与 GitHub 配置一致
2. 重启服务使配置生效

#### Q: Webhook 触发成功，但没有评论

**原因**: Token 权限不足或 AI 服务异常

**解决**:
1. 检查服务日志，查看具体错误信息
2. 确认 GitHub Token 有 `repo` 权限
3. 确认 AI 服务可访问

#### Q: 不想验证签名怎么办？

将 `config.yaml` 中的 `webhook_secret` 设置为空字符串：

```yaml
webhook_secret: ""
```

GitHub Webhook 配置页面的 Secret 也留空。

**注意**: 不验证签名会降低安全性。

### GitLab 相关

#### Q: GitLab Webhook 触发失败，返回 401 Unauthorized

**原因**: Webhook token 配置不一致

**解决**:
1. 检查 `config.yaml` 中的 `gitlab_webhook_token` 是否与 GitLab 配置一致
2. 重启服务使配置生效

#### Q: GitLab MR 没有收到评论

**原因**: GitLab Token 权限不足

**解决**:
1. 检查服务日志，查看具体错误信息
2. 确认 GitLab Token 有以下权限：
   - `api` - 完整的 API 访问权限
   - `read_api` - 读取 API 权限
   - `write_repository` - 写入仓库权限

#### Q: 私有 GitLab 实例连接失败

**原因**: 网络不可达或 SSL 证书问题

**解决**:
1. 确认服务可以访问私有 GitLab 实例
2. 检查 `gitlab_base_url` 配置是否正确（包含 `https://` 前缀）
3. 如果使用自签名证书，需要配置信任

### 通用问题

#### Q: Webhook URL 无法访问

**原因**: 网络配置问题

**解决**:
1. 确认服务已正常启动
2. 确认 Service/Ingress 配置正确
3. 如果是内网部署，确认 GitHub/GitLab 可以访问

#### Q: 如何在同一服务中同时支持 GitHub 和 GitLab？

**方案一**：部署两个服务实例
- 实例 A 配置 `vcs_provider: github`
- 实例 B 配置 `vcs_provider: gitlab`

**方案二**：手动 API 调用时指定 provider
- 配置文件设置默认 provider
- API 调用时通过 `provider` 字段覆盖

---

## 安全建议

### GitHub

1. ✅ 始终配置 `webhook_secret` 验证请求签名
2. ✅ 使用 HTTPS（通过 Ingress + TLS）
3. ✅ 定期轮换 GitHub Token 和 Webhook Secret
4. ✅ 限制 GitHub Token 权限（只给必要的 repo 访问权限）
5. ✅ 监控服务日志，及时发现异常请求

### GitLab

1. ✅ 始终配置 `gitlab_webhook_token` 验证请求
2. ✅ 使用 HTTPS 并启用 SSL verification
3. ✅ 定期轮换 GitLab Token 和 Webhook Token
4. ✅ 限制 GitLab Token 权限和作用域
5. ✅ 对于私有实例，确保网络隔离和访问控制
6. ✅ 监控服务日志，及时发现异常请求

### Claude CLI 模式

1. ✅ 使用 Kubernetes Secrets 存储敏感配置
2. ✅ 限制临时目录访问权限
3. ✅ 配置磁盘空间监控和告警
4. ✅ 定期审计克隆日志
5. ✅ 使用浅克隆减少网络和磁盘开销

### 通用建议

1. ✅ 不要将 `config.yaml` 提交到 git（包含敏感信息）
2. ✅ 使用 Kubernetes Secrets 存储敏感配置
3. ✅ 定期审计 Webhook 触发日志
4. ✅ 限制服务网络访问范围
5. ✅ 配置服务资源限制（CPU/Memory）
6. ✅ 启用日志监控和告警

---

## API 端点说明

| 端点 | 方法 | 说明 |
|------|------|------|
| `/webhook` | POST | GitHub/GitLab Webhook 接收端点（根据配置的 vcs_provider） |
| `/review` | POST | 手动触发 review（需要传 repo、pr_number 和可选的 provider） |
| `/health` | GET | 健康检查 |

---

## License

MIT
