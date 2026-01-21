package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// 配置结构
type Config struct {
	AIApiURL           string `yaml:"ai_api_url"`
	AIApiKey           string `yaml:"ai_api_key"`
	AIModel            string `yaml:"ai_model"`
	Port               string `yaml:"port"`
	GithubToken        string `yaml:"github_token"`
	SystemPrompt       string `yaml:"system_prompt"`
	UserPromptTemplate string `yaml:"user_prompt_template"`
}

// 全局配置
var config Config

// 请求体结构
type ReviewRequest struct {
	Repo     string `json:"repo"`      // owner/repo
	PRNumber int    `json:"pr_number"` // PR ID
}

// OpenAI 格式的消息结构
type AIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OpenAI 格式的请求
type AIRequest struct {
	Model    string      `json:"model"`
	Messages []AIMessage `json:"messages"`
	Stream   bool        `json:"stream"`
}

// OpenAI 格式的响应
type AIResponse struct {
	Choices []struct {
		Message AIMessage `json:"message"`
	} `json:"choices"`
}

// 加载配置文件
func loadConfig(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	// 验证必需字段
	if config.AIApiURL == "" {
		return fmt.Errorf("ai_api_url is required in config")
	}
	if config.AIApiKey == "" {
		return fmt.Errorf("ai_api_key is required in config")
	}
	if config.AIModel == "" {
		config.AIModel = "qwen-plus-latest" // 默认模型
	}
	if config.Port == "" {
		config.Port = "7995" // 默认端口
	}
	if config.GithubToken == "" {
		return fmt.Errorf("github_token is required in config")
	}
	if config.SystemPrompt == "" {
		return fmt.Errorf("system_prompt is required in config")
	}
	if config.UserPromptTemplate == "" {
		return fmt.Errorf("user_prompt_template is required in config")
	}

	return nil
}

func main() {
	// 加载配置文件
	if err := loadConfig("config.yaml"); err != nil {
		log.Fatalf("❌ Configuration error: %v", err)
	}

	http.HandleFunc("/review", handleReview)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	log.Printf("🚀 PR Review Service started on :%s, AI URL: %s", config.Port, config.AIApiURL)
	log.Fatal(http.ListenAndServe(":"+config.Port, nil))
}

func handleReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. 获取 GitHub Token (优先使用请求头，否则使用配置文件中的)
	token := r.Header.Get("X-Github-Token")
	if token == "" {
		token = config.GithubToken
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
	go processReview(req.Repo, req.PRNumber, token)

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(fmt.Sprintf("Review started for %s #%d", req.Repo, req.PRNumber)))
}

func processReview(repo string, prNum int, token string) {
	// === A. 获取 Diff ===
	client := &http.Client{Timeout: 30 * time.Second}
	diffUrl := fmt.Sprintf("https://api.github.com/repos/%s/pulls/%d", repo, prNum)

	ghReq, _ := http.NewRequest("GET", diffUrl, nil)
	ghReq.Header.Set("Authorization", "Bearer "+token)
	ghReq.Header.Set("Accept", "application/vnd.github.v3.diff")

	resp, err := client.Do(ghReq)
	if err != nil {
		log.Printf("❌ [%s#%d] Failed to get diff: %v", repo, prNum, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("❌ [%s#%d] GitHub API Error: %s", repo, prNum, resp.Status)
		return
	}

	diffBytes, _ := io.ReadAll(resp.Body)
	diffText := string(diffBytes)

	// 截断保护
	if len(diffText) > 6000 {
		diffText = diffText[:6000] + "\n...(truncated)"
	}

	// === B. 调用 AI ===
	log.Printf("🤖 [%s#%d] Sending to AI...", repo, prNum)

	// 使用配置的 prompt 模板，替换 {diff} 占位符
	userPrompt := strings.ReplaceAll(config.UserPromptTemplate, "{diff}", diffText)

	// 构建 OpenAI 格式的请求
	aiPayload := AIRequest{
		Model: config.AIModel,
		Messages: []AIMessage{
			{
				Role:    "system",
				Content: config.SystemPrompt,
			},
			{
				Role:    "user",
				Content: userPrompt,
			},
		},
		Stream: false,
	}
	jsonPayload, _ := json.Marshal(aiPayload)

	// 创建带 Authorization 的请求
	aiReq, _ := http.NewRequest("POST", config.AIApiURL, bytes.NewBuffer(jsonPayload))
	aiReq.Header.Set("Authorization", "Bearer "+config.AIApiKey)
	aiReq.Header.Set("Content-Type", "application/json")

	aiResp, err := client.Do(aiReq)
	if err != nil {
		log.Printf("❌ [%s#%d] AI Service call failed: %v", repo, prNum, err)
		return
	}
	defer aiResp.Body.Close()

	aiBody, _ := io.ReadAll(aiResp.Body)

	// 解析 OpenAI 格式的响应
	var aiResult AIResponse
	if err := json.Unmarshal(aiBody, &aiResult); err != nil {
		log.Printf("❌ [%s#%d] Failed to parse AI response: %v", repo, prNum, err)
		log.Printf("Response body: %s", string(aiBody))
		return
	}

	reviewContent := ""
	if len(aiResult.Choices) > 0 {
		reviewContent = aiResult.Choices[0].Message.Content
	} else {
		log.Printf("⚠️ [%s#%d] AI returned empty response", repo, prNum)
		reviewContent = "AI 服务未返回审查结果"
	}

	// === C. 回复 GitHub ===
	log.Printf("📝 [%s#%d] Posting comment...", repo, prNum)
	commentBody := map[string]string{
		"body": fmt.Sprintf("🤖 **AI Code Review**\n\n%s", reviewContent),
	}
	jsonComment, _ := json.Marshal(commentBody)

	commentUrl := fmt.Sprintf("https://api.github.com/repos/%s/issues/%d/comments", repo, prNum)
	commentReq, _ := http.NewRequest("POST", commentUrl, bytes.NewBuffer(jsonComment))
	commentReq.Header.Set("Authorization", "Bearer "+token)
	commentReq.Header.Set("Content-Type", "application/json")

	cResp, err := client.Do(commentReq)
	if err != nil {
		log.Printf("❌ [%s#%d] Failed to post comment: %v", repo, prNum, err)
		return
	}
	defer cResp.Body.Close()

	if cResp.StatusCode == 201 {
		log.Printf("✅ [%s#%d] Review done!", repo, prNum)
	} else {
		log.Printf("⚠️ [%s#%d] Comment failed status: %s", repo, prNum, cResp.Status)
	}
}
