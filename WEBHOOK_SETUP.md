# Webhook 自动触发配置指南

本文档说明如何配置 GitHub 和 GitLab Webhook，使 PR/MR 有新 commit 时自动触发代码审查。

## 功能说明

配置完成后，当以下事件发生时，系统会自动触发 AI 代码审查：

**GitHub PR**:
- ✅ PR 被创建（`opened`）
- ✅ PR 有新的 commit 推送（`synchronize`）
- ✅ PR 被重新打开（`reopened`）

**GitLab MR**:
- ✅ MR 被创建（`open`）
- ✅ MR 有新的 commit 推送（`update`）
- ✅ MR 被重新打开（`reopen`）

## 目录

- [GitHub Webhook 配置](#github-webhook-配置)
- [GitLab Webhook 配置](#gitlab-webhook-配置)
- [Kubernetes 环境配置](#kubernetes-环境配置)
- [常见问题](#常见问题)

---

## GitHub Webhook 配置

### 1. 生成 Webhook Secret（可选但推荐）

为了安全验证 webhook 请求，建议生成一个随机 secret：

```bash
# 生成随机 secret
openssl rand -hex 32
```

将生成的 secret 添加到 `config.yaml`：

```yaml
vcs_provider: "github"
webhook_secret: "your-generated-secret-here"
```

### 2. 在 GitHub 仓库配置 Webhook

1. 进入你的 GitHub 仓库
2. 点击 **Settings** → **Webhooks** → **Add webhook**

3. 填写 Webhook 配置：

   - **Payload URL**: `http://your-service-url/webhook`
     - 例如：`http://pr-review-service.default.svc.cluster.local/webhook`（集群内部）
     - 例如：`http://your-domain.com/webhook`（外部访问）

   - **Content type**: 选择 `application/json`

   - **Secret**: 填写步骤 1 中生成的 secret（与 config.yaml 中一致）

   - **Which events would you like to trigger this webhook?**
     - 选择 **Let me select individual events**
     - 勾选 **Pull requests** ✅
     - 取消勾选其他事件

   - **Active**: 勾选 ✅

4. 点击 **Add webhook**

### 3. 验证配置

#### 方法 1: 查看 Webhook 日志

在 GitHub Webhook 设置页面，点击刚创建的 webhook，查看 **Recent Deliveries** 标签页，可以看到：
- 请求详情
- 响应状态（应该是 `200 OK` 或 `202 Accepted`）

#### 方法 2: 创建测试 PR

1. 在仓库中创建一个测试分支
2. 提交一些改动
3. 创建 Pull Request
4. 查看服务日志，应该看到：

```log
📨 Received GitHub webhook: pull_request
🎯 Triggering review for owner/repo #123 (commit: abc1234)
📥 Received review request for owner/repo #123
🔍 [owner/repo#123] Fetching PR diff...
🤖 [owner/repo#123] Sending to AI for review...
📝 [owner/repo#123] Posting review comment...
✅ [owner/repo#123] Review completed successfully!
```

5. PR 中应该会收到 AI 的 review 评论

---

## GitLab Webhook 配置

### 1. 配置 VCS Provider

在 `config.yaml` 中配置使用 GitLab：

```yaml
vcs_provider: "gitlab"
gitlab_token: "glpat-xxxxxxxxxxxxxxxxxxxxx"
gitlab_base_url: ""  # 留空使用 gitlab.com，私有实例填写完整地址
gitlab_webhook_token: "your-secret-token"  # 可选但推荐
```

### 2. 生成 Webhook Token（可选但推荐）

GitLab 使用简单的 Token 验证（而非 HMAC 签名）：

```bash
# 生成随机 token
openssl rand -hex 32
```

将生成的 token 添加到 `config.yaml` 的 `gitlab_webhook_token` 字段。

### 3. 在 GitLab 项目配置 Webhook

1. 进入你的 GitLab 项目
2. 点击 **Settings** → **Webhooks**

3. 填写 Webhook 配置：

   - **URL**: `http://your-service-url/webhook`
     - 例如：`http://pr-review-service.default.svc.cluster.local/webhook`（集群内部）
     - 例如：`https://pr-review.your-domain.com/webhook`（外部访问）

   - **Secret token**: 填写步骤 2 中生成的 token（与 config.yaml 中一致）

   - **Trigger**: 勾选 **Merge request events** ✅

   - **Enable SSL verification**: 如果使用 HTTPS，建议勾选 ✅

4. 点击 **Add webhook**

### 4. 验证配置

#### 方法 1: 使用 GitLab 的测试功能

在 GitLab Webhook 设置页面，点击刚创建的 webhook 右侧的 **Test** → **Merge request events**，可以立即触发测试请求。

查看响应：
- HTTP 状态码应该是 `202 Accepted`
- 响应体：`Review triggered for group/project !123`

#### 方法 2: 创建测试 MR

1. 在项目中创建一个测试分支
2. 提交一些改动
3. 创建 Merge Request
4. 查看服务日志，应该看到：

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

5. MR 中应该会收到 AI 的 review 评论

### 5. 私有 GitLab 实例配置

如果使用私有部署的 GitLab，需要配置 `gitlab_base_url`：

```yaml
vcs_provider: "gitlab"
gitlab_token: "glpat-xxxxxxxxxxxxxxxxxxxxx"
gitlab_base_url: "https://gitlab.company.com"  # 私有实例地址
gitlab_webhook_token: "your-secret-token"
```

**注意**：
- 确保服务可以访问私有 GitLab 实例的网络
- 如果使用自签名证书，可能需要配置 SSL 证书信任

### 6. GitLab 项目标识说明

GitLab 支持两种方式标识项目：

1. **项目路径**（推荐）：`group/project` 或 `group/subgroup/project`
2. **项目 ID**（数字）：如 `12345`

在 API 调用时，两种方式都可以使用：

```json
{
  "repo": "group/project",
  "pr_number": 45,
  "provider": "gitlab"
}
```

或

```json
{
  "repo": "12345",
  "pr_number": 45,
  "provider": "gitlab"
}
```

---

## Kubernetes 环境配置

如果服务部署在 Kubernetes 中，需要确保：

### 选项 A: 使用 NodePort（外部访问）

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
      nodePort: 30095  # 外部访问端口
```

Webhook URL: `http://<your-node-ip>:30095/webhook`

### 选项 B: 使用 Ingress（推荐）

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

## 常见问题

### GitHub 相关

#### Q1: Webhook 触发失败，返回 401 Unauthorized

**原因**: Webhook secret 配置不一致

**解决**:
1. 检查 `config.yaml` 中的 `webhook_secret` 是否与 GitHub 配置一致
2. 重启服务使配置生效

#### Q2: Webhook 触发成功，但没有评论

**原因**: Token 权限不足或 AI 服务异常

**解决**:
1. 检查服务日志，查看具体错误信息
2. 确认 GitHub Token 有 `repo` 权限
3. 确认 AI 服务可访问

#### Q3: 不想验证签名怎么办？

**回答**:
将 `config.yaml` 中的 `webhook_secret` 设置为空字符串：

```yaml
webhook_secret: ""
```

GitHub Webhook 配置页面的 Secret 也留空。

**注意**: 不验证签名会降低安全性，任何人都可以伪造请求触发 review。

### GitLab 相关

#### Q4: GitLab Webhook 触发失败，返回 401 Unauthorized

**原因**: Webhook token 配置不一致

**解决**:
1. 检查 `config.yaml` 中的 `gitlab_webhook_token` 是否与 GitLab 配置一致
2. 重启服务使配置生效

#### Q5: GitLab MR 没有收到评论

**原因**: GitLab Token 权限不足

**解决**:
1. 检查服务日志，查看具体错误信息
2. 确认 GitLab Token 有以下权限：
   - `api` - 完整的 API 访问权限
   - `read_api` - 读取 API 权限
   - `write_repository` - 写入仓库权限
3. 可以在 GitLab 的 **User Settings** → **Access Tokens** 重新生成 token

#### Q6: 私有 GitLab 实例连接失败

**原因**: 网络不可达或 SSL 证书问题

**解决**:
1. 确认服务可以访问私有 GitLab 实例（`curl https://gitlab.company.com`）
2. 检查 `gitlab_base_url` 配置是否正确（包含 `https://` 前缀）
3. 如果使用自签名证书，需要配置信任（Go 程序可能需要设置 `GODEBUG=x509ignoreCN=0`）

#### Q7: GitLab Webhook 不想验证 Token 怎么办？

**回答**:
将 `config.yaml` 中的 `gitlab_webhook_token` 设置为空字符串：

```yaml
gitlab_webhook_token: ""
```

GitLab Webhook 配置页面的 Secret token 也留空。

**注意**: 不验证 token 会降低安全性。

### 通用问题

#### Q8: Webhook URL 无法访问

**原因**: 网络配置问题

**解决**:
1. 确认服务已正常启动（`kubectl get pods`）
2. 确认 Service/Ingress 配置正确
3. 如果是内网部署，确认 GitHub/GitLab 可以访问（可能需要反向代理）

#### Q9: 如何在同一服务中同时支持 GitHub 和 GitLab？

**回答**:
目前一个服务实例只能配置一个 VCS Provider（通过 `vcs_provider` 配置）。如果需要同时支持两个平台：

1. **方案一**：部署两个服务实例
   - 实例 A 配置 `vcs_provider: github`
   - 实例 B 配置 `vcs_provider: gitlab`

2. **方案二**：手动 API 调用时指定 provider
   - 配置文件设置默认 provider
   - API 调用时通过 `provider` 字段覆盖
   ```json
   {
     "repo": "group/project",
     "pr_number": 45,
     "provider": "gitlab"
   }
   ```

## 测试 Webhook

### 测试 GitHub Webhook

```bash
# 不带签名验证的测试
curl -X POST http://your-service/webhook \
  -H "Content-Type: application/json" \
  -H "X-GitHub-Event: pull_request" \
  -d '{
    "action": "opened",
    "number": 1,
    "pull_request": {
      "number": 1,
      "head": {"sha": "abc123"}
    },
    "repository": {
      "full_name": "owner/repo"
    }
  }'
```

预期响应：`Review triggered for owner/repo #1`

### 测试 GitLab Webhook

```bash
# 不带 token 验证的测试
curl -X POST http://your-service/webhook \
  -H "Content-Type: application/json" \
  -H "X-Gitlab-Event: Merge Request Hook" \
  -d '{
    "object_kind": "merge_request",
    "object_attributes": {
      "iid": 45,
      "action": "open"
    },
    "project": {
      "id": 12345,
      "path_with_namespace": "group/project"
    }
  }'
```

预期响应：`Review triggered for group/project !45`

## API 端点说明

服务提供以下端点：

| 端点 | 方法 | 说明 |
|------|------|------|
| `/webhook` | POST | GitHub/GitLab Webhook 接收端点（根据配置的 vcs_provider） |
| `/review` | POST | 手动触发 review（需要传 repo、pr_number 和可选的 provider） |
| `/health` | GET | 健康检查 |

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

### 通用建议
1. ✅ 使用 Kubernetes Secrets 存储敏感配置
2. ✅ 定期审计 Webhook 触发日志
3. ✅ 限制服务网络访问范围
4. ✅ 配置服务资源限制（CPU/Memory）
5. ✅ 启用日志监控和告警
