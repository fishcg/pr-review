package router

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"pr-review/lib"
)

// ReviewRequest PR 审查请求体结构
type ReviewRequest struct {
	Repo     string `json:"repo"`      // owner/repo
	PRNumber int    `json:"pr_number"` // PR ID
}

// Config 配置接口（避免循环依赖）
type Config interface {
	GetGithubToken() string
	GetAIConfig() (apiURL, apiKey, model, systemPrompt, userTemplate string)
}

var appConfig Config

// SetConfig 设置配置
func SetConfig(cfg Config) {
	appConfig = cfg
}

// HandleReview 处理 PR 审查请求
func HandleReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. 获取 GitHub Token (优先使用请求头，否则使用配置文件中的)
	token := r.Header.Get("X-Github-Token")
	if token == "" {
		token = appConfig.GetGithubToken()
	}

	// 2. 解析请求
	var req ReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("📥 Received review request for %s #%d", req.Repo, req.PRNumber)

	// 3. 异步处理 Review (防止 CI HTTP 请求超时)
	// 如果你希望 CI 等待结果，可以去掉 go 关键字
	go ProcessReview(req.Repo, req.PRNumber, token)

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(fmt.Sprintf("Review started for %s #%d", req.Repo, req.PRNumber)))
}

// HandleHealth 健康检查
func HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// ProcessReview 处理 PR 审查的完整流程
func ProcessReview(repo string, prNum int, token string) {
	// === A. 获取 Diff ===
	log.Printf("🔍 [%s#%d] Fetching PR diff...", repo, prNum)

	ghClient := lib.NewGitHubClient(token)
	diffText, err := ghClient.GetPRDiff(repo, prNum)
	if err != nil {
		log.Printf("❌ [%s#%d] %v", repo, prNum, err)
		return
	}

	// === B. 调用 AI 审查 ===
	log.Printf("🤖 [%s#%d] Sending to AI for review...", repo, prNum)

	apiURL, apiKey, model, systemPrompt, userTemplate := appConfig.GetAIConfig()
	aiClient := lib.NewAIClient(apiURL, apiKey, model, systemPrompt, userTemplate)
	reviewContent, err := aiClient.ReviewCode(diffText)
	if err != nil {
		log.Printf("❌ [%s#%d] %v", repo, prNum, err)
		return
	}

	// === C. 发布评论到 GitHub ===
	log.Printf("📝 [%s#%d] Posting review comment...", repo, prNum)

	comment := fmt.Sprintf("🤖 **AI Code Review**\n\n%s", reviewContent)
	if err := ghClient.PostComment(repo, prNum, comment); err != nil {
		log.Printf("❌ [%s#%d] %v", repo, prNum, err)
		return
	}

	log.Printf("✅ [%s#%d] Review completed successfully!", repo, prNum)
}
