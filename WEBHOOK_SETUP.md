# GitHub Webhook 自动触发配置指南

本文档说明如何配置 GitHub Webhook，使 PR 有新 commit 时自动触发代码审查。

## 功能说明

配置完成后，当以下事件发生时，系统会自动触发 AI 代码审查：
- ✅ PR 被创建（`opened`）
- ✅ PR 有新的 commit 推送（`synchronize`）
- ✅ PR 被重新打开（`reopened`）

## 配置步骤

### 1. 生成 Webhook Secret（可选但推荐）

为了安全验证 webhook 请求，建议生成一个随机 secret：

```bash
# 生成随机 secret
openssl rand -hex 32
```

将生成的 secret 添加到 `config.yaml`：

```yaml
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

### Q1: Webhook 触发失败，返回 401 Unauthorized

**原因**: Webhook secret 配置不一致

**解决**:
1. 检查 `config.yaml` 中的 `webhook_secret` 是否与 GitHub 配置一致
2. 重启服务使配置生效

### Q2: Webhook 触发成功，但没有评论

**原因**: GitHub Token 权限不足或 AI 服务异常

**解决**:
1. 检查服务日志，查看具体错误信息
2. 确认 GitHub Token 有 `repo` 权限
3. 确认 AI 服务可访问

### Q3: Webhook URL 无法访问

**原因**: 网络配置问题

**解决**:
1. 确认服务已正常启动（`kubectl get pods`）
2. 确认 Service/Ingress 配置正确
3. 如果是内网部署，确认 GitHub 可以访问（可能需要反向代理）

### Q4: 不想验证签名怎么办？

**回答**:
将 `config.yaml` 中的 `webhook_secret` 设置为空字符串：

```yaml
webhook_secret: ""
```

GitHub Webhook 配置页面的 Secret 也留空。

**注意**: 不验证签名会降低安全性，任何人都可以伪造请求触发 review。

## 测试 Webhook

可以使用 curl 手动测试 webhook endpoint：

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

## API 端点说明

服务提供以下端点：

| 端点 | 方法 | 说明 |
|------|------|------|
| `/webhook` | POST | GitHub Webhook 接收端点 |
| `/review` | POST | 手动触发 review（需要传 repo 和 pr_number） |
| `/health` | GET | 健康检查 |

## 安全建议

1. ✅ 始终配置 `webhook_secret` 验证请求签名
2. ✅ 使用 HTTPS（通过 Ingress + TLS）
3. ✅ 定期轮换 GitHub Token 和 Webhook Secret
4. ✅ 限制 GitHub Token 权限（只给必要的 repo 访问权限）
5. ✅ 监控服务日志，及时发现异常请求
