package router

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"pr-review/lib"
)

// GitLabWebhookPayload GitLab Webhook 事件载荷
type GitLabWebhookPayload struct {
	ObjectKind       string `json:"object_kind"`
	ObjectAttributes struct {
		IID    int    `json:"iid"`    // Merge Request IID（不是 ID）
		Action string `json:"action"` // open, update, merge, close, reopen
		State  string `json:"state"`  // opened, merged, closed
	} `json:"object_attributes"`
	Project struct {
		ID                int    `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"` // 如 "group/project"
	} `json:"project"`
}

var gitlabWebhookToken string

// SetGitLabWebhookToken 设置 GitLab webhook token
func SetGitLabWebhookToken(token string) {
	gitlabWebhookToken = token
}

// HandleGitLabWebhook 处理 GitLab Webhook 事件
func HandleGitLabWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. 验证 Token（如果配置了）
	if gitlabWebhookToken != "" {
		token := r.Header.Get("X-Gitlab-Token")
		if token != gitlabWebhookToken {
			log.Printf("❌ Invalid GitLab webhook token")
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}
	}

	// 2. 读取请求体
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("❌ Failed to read webhook body: %v", err)
		http.Error(w, "Failed to read request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// 3. 解析事件类型
	eventType := r.Header.Get("X-Gitlab-Event")

	// 4. 只处理 Merge Request 相关事件
	if eventType != "Merge Request Hook" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Event ignored"))
		return
	}

	// 5. 解析 payload
	var payload GitLabWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("❌ Failed to parse webhook payload: %v", err)
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	// 6. 验证 object_kind
	if payload.ObjectKind != "merge_request" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf("Object kind '%s' ignored", payload.ObjectKind)))
		return
	}

	// 7. 检查是否需要触发 review
	// 触发条件: open（新建MR）, update（新push）, reopen（重新打开）
	shouldReview := payload.ObjectAttributes.Action == "open" ||
		payload.ObjectAttributes.Action == "update" ||
		payload.ObjectAttributes.Action == "reopen"

	if !shouldReview {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf("Action '%s' ignored", payload.ObjectAttributes.Action)))
		return
	}

	// 8. 提取信息
	// 优先使用 path_with_namespace，因为它更易读
	repo := payload.Project.PathWithNamespace
	if repo == "" {
		// 降级使用项目 ID
		repo = fmt.Sprintf("%d", payload.Project.ID)
	}
	mrNumber := payload.ObjectAttributes.IID // 注意：使用 IID 而不是 ID

	log.Printf("🎯 Triggering review for %s !%d", repo, mrNumber)

	// 9. 获取 GitLab Token
	token := appConfig.GetGitlabToken()

	// 10. 异步触发 review
	go ProcessReview(repo, mrNumber, lib.ProviderTypeGitLab, token)

	// 11. 返回成功响应
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(fmt.Sprintf("Review triggered for %s !%d", repo, mrNumber)))
}
