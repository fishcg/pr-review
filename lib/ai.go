package lib

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// AIMessage OpenAI 格式的消息结构
type AIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AIRequest OpenAI 格式的请求
type AIRequest struct {
	Model    string      `json:"model"`
	Messages []AIMessage `json:"messages"`
	Stream   bool        `json:"stream"`
}

// AIResponse OpenAI 格式的响应
type AIResponse struct {
	Choices []struct {
		Message AIMessage `json:"message"`
	} `json:"choices"`
}

// AIClient AI 服务客户端
type AIClient struct {
	APIUrl       string
	APIKey       string
	Model        string
	SystemPrompt string
	UserTemplate string
	HTTPClient   *http.Client
}

// NewAIClient 创建 AI 客户端
func NewAIClient(apiURL, apiKey, model, systemPrompt, userTemplate string) *AIClient {
	return &AIClient{
		APIUrl:       apiURL,
		APIKey:       apiKey,
		Model:        model,
		SystemPrompt: systemPrompt,
		UserTemplate: userTemplate,
		HTTPClient:   &http.Client{Timeout: 60 * time.Second},
	}
}

// ReviewCode 调用 AI 服务审查代码
func (c *AIClient) ReviewCode(diffText string) (string, error) {
	// 使用配置的 prompt 模板，替换 {diff} 占位符
	userPrompt := strings.ReplaceAll(c.UserTemplate, "{diff}", diffText)

	// 构建 OpenAI 格式的请求
	aiPayload := AIRequest{
		Model: c.Model,
		Messages: []AIMessage{
			{
				Role:    "system",
				Content: c.SystemPrompt,
			},
			{
				Role:    "user",
				Content: userPrompt,
			},
		},
		Stream: false,
	}

	jsonPayload, err := json.Marshal(aiPayload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal AI request: %w", err)
	}

	// 创建带 Authorization 的请求
	req, err := http.NewRequest("POST", c.APIUrl, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("AI service call failed: %w", err)
	}
	defer resp.Body.Close()

	aiBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read AI response: %w", err)
	}

	// 解析 OpenAI 格式的响应
	var aiResult AIResponse
	if err := json.Unmarshal(aiBody, &aiResult); err != nil {
		log.Printf("Failed to parse AI response: %v", err)
		log.Printf("Response body: %s", string(aiBody))
		return "", fmt.Errorf("failed to parse AI response: %w", err)
	}

	if len(aiResult.Choices) == 0 {
		return "", fmt.Errorf("AI returned empty response")
	}

	reviewContent := aiResult.Choices[0].Message.Content
	if reviewContent == "" {
		return "", fmt.Errorf("AI returned empty review content")
	}

	return reviewContent, nil
}
