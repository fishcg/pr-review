# PR Review Service — 代码导读

## 项目概述

这是一个自动化 PR/MR 代码审查服务，监听 GitHub/GitLab 的 Webhook 事件或接收手动触发请求，调用 AI 对代码变更进行审查，并将结果以评论形式发布回对应平台。

---

## 目录结构

```
.
├── main.go                    # 服务入口
├── config.go                  # 配置结构与加载
├── router/
│   ├── handler.go             # 核心处理逻辑（review 流程、行内评论解析）
│   ├── webhook_github.go      # GitHub Webhook 入口
│   └── webhook_gitlab.go      # GitLab Webhook 入口
└── lib/
    ├── provider.go            # VCSProvider 接口定义 + 公共类型
    ├── github.go              # GitHub API 适配层
    ├── gitlab.go              # GitLab API 适配层
    ├── ai.go                  # OpenAI 兼容 API 客户端
    ├── claude_cli.go          # Claude CLI 模式客户端
    ├── codex_cli.go           # Codex CLI 模式客户端
    ├── context_enhancer.go    # Diff 增强 + Claude CLI 引导生成
    ├── code_analyzer.go       # 函数/依赖/测试覆盖静态分析
    └── repo_manager.go        # 仓库克隆、Checkout、清理管理
```

---

## 核心流程

### 触发方式

1. **Webhook 自动触发**（`/webhook`）
   - GitHub：PR 事件 `opened / synchronize / reopened`
   - GitLab：MR 事件 `open / update / reopen`
   - 支持签名验证（GitHub HMAC-SHA256 / GitLab Token）

2. **手动触发**（`POST /review`）
   - 请求体：`{ "repo": "owner/repo", "number": 123, "provider": "github", "engine": "api" }`
   - `engine` 字段可覆盖配置文件中的 `review_mode`

### Review 执行流程（`ProcessReview`）

```
触发请求
  │
  ├─ 创建 VCSProvider（GitHub / GitLab）
  │
  ├─ 选择 Review 引擎（api / claude_cli / codex）
  │     ├─ api:        获取 Diff → 增强 Diff → 调用 AI API → 返回审查内容
  │     ├─ claude_cli: 克隆仓库 → 获取完整 Diff → 依赖分析 → 调用 Claude CLI
  │     └─ codex:      克隆仓库 → 获取完整 Diff → 依赖分析 → 调用 Codex CLI
  │     （CLI 模式失败时自动降级到 api 模式）
  │
  └─ 发布评论
        ├─ inline_issue_comment=true:  解析 AI 输出中的问题表格 → 发布行内评论 + 汇总评论
        └─ inline_issue_comment=false: 发布一条整体评论
```

---

## 三种 Review 模式

| 模式 | 原理 | 适用场景 |
|------|------|----------|
| `api` | 将 Diff 发送给 OpenAI 兼容 API | 轻量，不需要克隆仓库 |
| `claude_cli` | 克隆仓库 + 调用本地 Claude CLI，可读取完整文件 | 需要全量代码上下文 |
| `codex` | 克隆仓库 + 调用本地 Codex CLI | 同上，使用 OpenAI Codex |

CLI 模式额外能力：
- 从本地 Git 获取不受 API 大小限制的完整 Diff
- 通过 `CodeAnalyzer` 提取被修改的函数，扫描调用方，检查测试覆盖
- 将分析结果作为引导信息注入给 AI

---

## VCS 平台抽象

`lib.VCSProvider` 接口统一了 GitHub 和 GitLab 的操作：

```go
type VCSProvider interface {
    GetDiff(repo string, number int) (string, error)
    GetHeadSHA(repo string, number int) (string, error)
    GetPRInfo(repo string, number int) (*PRInfo, error)
    PostComment(repo string, number int, comment string) error
    PostInlineComment(repo string, number int, commitSHA, path string, position int, body string, oldLine, newLine int) error
    GetIssueComments(repo string, number int) ([]Comment, error)
    GetInlineComments(repo string, number int) ([]Comment, error)
    GetBranchInfo(repo string, number int) (*BranchInfo, error)
    GetCloneURL(repo string) (string, error)
    GetCurrentUser() (string, error)
    GetProviderType() string
}
```

- **GitHub** (`GitHubClient`)：使用 `application/vnd.github.v3.diff` 获取 Diff，行内评论通过 `position`（diff 位置）定位
- **GitLab** (`GitLabClient`)：通过 `/changes` 接口获取变更并转换为 unified diff，行内评论使用 Discussions API + `old_line`/`new_line` 定位

---

## 行内评论机制

当 `inline_issue_comment: true` 时，流程如下：

1. AI 输出必须包含结构化问题表格（支持多种列格式：6列/8列/9列）
2. `parseIssuesFromReview` 解析每行问题，提取文件名、行号、代码片段、严重程度等
3. `buildDiffPositionMap` 解析 Diff 文本，建立 `文件 → 行号 → diff position` 的映射
4. `resolveLineInfo` 按以下优先级定位具体行：
   - **优先**：代码片段精确匹配（对 diff 内容做归一化后模糊匹配）
   - **降级**：直接使用行号
5. 未能匹配到位置的问题汇总为"其他问题"表格附在总评论中

两个配置控制行为：
- `comment_only_changes`：只对新增/删除行评论，跳过上下文行
- `line_match_strategy`：`snippet_first`（默认，优先代码片段）或 `line_number_first`

---

## 配置说明

主要配置项（`config.yaml`）：

```yaml
vcs_provider: github           # github | gitlab
review_mode: api               # api | claude_cli | codex
inline_issue_comment: false    # 是否发布行内评论
comment_only_changes: false    # 是否只对变更行评论

ai_api_url: ...                # OpenAI 兼容 API 地址
ai_api_key: ...
ai_model: qwen-plus-latest
system_prompt: ...             # 系统 Prompt
user_prompt_template: ...      # 用户 Prompt 模板，{diff} 为占位符

github_token: ...
webhook_secret: ...            # 可选，GitHub Webhook 签名验证

gitlab_token: ...
gitlab_base_url: https://gitlab.com
gitlab_webhook_token: ...      # 可选，GitLab Webhook Token 验证

claude_cli:
  binary_path: claude
  timeout: 600
  api_key: ...                 # 可选，覆盖 ANTHROPIC_API_KEY 环境变量
  include_others_comments: false

repo_clone:
  temp_dir: /tmp/pr-review-repos
  shallow_clone: false
  cleanup_after_review: false  # false 时依赖定时清理（每小时）
```

---

## 关键文件速查

| 要看什么 | 看哪里 |
|----------|--------|
| 整体 review 流程 | `router/handler.go` → `ProcessReview` |
| 行内评论解析与发布 | `router/handler.go` → `parseIssuesFromReview` / `postInlineIssues` |
| GitHub API 调用 | `lib/github.go` |
| GitLab API 调用 | `lib/gitlab.go` |
| Diff 增强与 Claude 引导 | `lib/context_enhancer.go` |
| 依赖分析与测试覆盖检测 | `lib/code_analyzer.go` |
| 仓库克隆管理 | `lib/repo_manager.go` |
| AI API 调用（OpenAI 格式） | `lib/ai.go` |
