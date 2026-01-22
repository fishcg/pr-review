package router

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"pr-review/lib"
	"strings"
)

// WebhookPayload GitHub Webhook 事件载荷
type WebhookPayload struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		Number int `json:"number"`
		Head   struct {
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

var webhookSecret string

// SetWebhookSecret 设置 webhook 密钥
func SetWebhookSecret(secret string) {
	webhookSecret = secret
}

// HandleWebhook 处理 GitHub Webhook 事件
func HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. 读取请求体
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("❌ Failed to read webhook body: %v", err)
		http.Error(w, "Failed to read request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// 2. 验证签名（如果配置了 webhook secret）
	if webhookSecret != "" {
		signature := r.Header.Get("X-Hub-Signature-256")
		if !verifySignature(body, signature, webhookSecret) {
			log.Printf("❌ Invalid webhook signature")
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// 3. 解析事件类型
	eventType := r.Header.Get("X-GitHub-Event")
	log.Printf("📨 Received GitHub webhook: %s", eventType)

	// 4. 只处理 PR 相关事件
	if eventType != "pull_request" {
		log.Printf("⏭️  Ignoring event type: %s", eventType)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Event ignored"))
		return
	}

	// 5. 解析 payload
	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("❌ Failed to parse webhook payload: %v", err)
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	// 6. 检查是否需要触发 review
	// 触发条件: opened（新建PR）, synchronize（新push）, reopened（重新打开）
	shouldReview := payload.Action == "opened" ||
		payload.Action == "synchronize" ||
		payload.Action == "reopened"

	if !shouldReview {
		log.Printf("⏭️  Ignoring PR action: %s", payload.Action)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf("Action '%s' ignored", payload.Action)))
		return
	}

	// 7. 提取信息
	repo := payload.Repository.FullName
	prNumber := payload.PullRequest.Number
	commitSHA := payload.PullRequest.Head.SHA

	log.Printf("🎯 Triggering review for %s #%d (commit: %s)", repo, prNumber, commitSHA[:7])

	// 8. 获取 GitHub Token
	token := appConfig.GetGithubToken()

	// 9. 异步触发 review
	go ProcessReview(repo, prNumber, lib.ProviderTypeGitHub, token)

	// 10. 返回成功响应
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(fmt.Sprintf("Review triggered for %s #%d", repo, prNumber)))
}

// verifySignature 验证 GitHub webhook 签名
func verifySignature(payload []byte, signature, secret string) bool {
	if signature == "" {
		return false
	}

	// GitHub 使用 sha256 签名，格式为 "sha256=<hash>"
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	// 提取十六进制哈希值
	expectedHash := signature[7:]

	// 计算 HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	actualHash := hex.EncodeToString(mac.Sum(nil))

	// 使用恒定时间比较防止时序攻击
	return hmac.Equal([]byte(actualHash), []byte(expectedHash))
}
