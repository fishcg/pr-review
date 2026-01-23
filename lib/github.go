package lib

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// GitHubClient GitHub API 客户端
type GitHubClient struct {
	Token      string
	HTTPClient *http.Client
}

// PRInfo PR 基本信息
type PRInfo struct {
	Head struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

// NewGitHubClient 创建 GitHub 客户端
func NewGitHubClient(token string) *GitHubClient {
	return &GitHubClient{
		Token:      token,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// GetPRDiff 获取 Pull Request 的代码变更
func (c *GitHubClient) GetPRDiff(repo string, prNum int) (string, error) {
	diffURL := fmt.Sprintf("https://api.github.com/repos/%s/pulls/%d", repo, prNum)

	req, err := http.NewRequest("GET", diffURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github.v3.diff")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get diff: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub API error: %s", resp.Status)
	}

	diffBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	diffText := string(diffBytes)

	// 截断保护，避免过长的 diff
	const maxDiffLength = 12000
	if len(diffText) > maxDiffLength {
		diffText = diffText[:maxDiffLength] + "\n...(truncated)"
	}

	return diffText, nil
}

// GetPRHeadSHA 获取 PR 的最新 commit SHA
func (c *GitHubClient) GetPRHeadSHA(repo string, prNum int) (string, error) {
	infoURL := fmt.Sprintf("https://api.github.com/repos/%s/pulls/%d", repo, prNum)

	req, err := http.NewRequest("GET", infoURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get PR info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub API error: %s", resp.Status)
	}

	var prInfo PRInfo
	if err := json.NewDecoder(resp.Body).Decode(&prInfo); err != nil {
		return "", fmt.Errorf("failed to decode PR info: %w", err)
	}

	if prInfo.Head.SHA == "" {
		return "", fmt.Errorf("PR head sha is empty")
	}

	return prInfo.Head.SHA, nil
}

// PostComment 向 PR 发布评论
func (c *GitHubClient) PostComment(repo string, prNum int, comment string) error {
	commentURL := fmt.Sprintf("https://api.github.com/repos/%s/issues/%d/comments", repo, prNum)

	commentBody := map[string]string{
		"body": comment,
	}
	jsonComment, err := json.Marshal(commentBody)
	if err != nil {
		return fmt.Errorf("failed to marshal comment: %w", err)
	}

	req, err := http.NewRequest("POST", commentURL, bytes.NewBuffer(jsonComment))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to post comment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("GitHub API response: %s", string(body))
		return fmt.Errorf("failed to post comment, status: %s", resp.Status)
	}

	return nil
}

// PostInlineComment 向 PR 发布行内评论
func (c *GitHubClient) PostInlineComment(repo string, prNum int, commitSHA, path string, position int, body string) error {
	commentURL := fmt.Sprintf("https://api.github.com/repos/%s/pulls/%d/comments", repo, prNum)

	commentBody := map[string]interface{}{
		"body":      body,
		"commit_id": commitSHA,
		"path":      path,
		"position":  position,
	}
	jsonComment, err := json.Marshal(commentBody)
	if err != nil {
		return fmt.Errorf("failed to marshal inline comment: %w", err)
	}

	req, err := http.NewRequest("POST", commentURL, bytes.NewBuffer(jsonComment))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to post inline comment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("GitHub API response: %s", string(bodyBytes))
		return fmt.Errorf("failed to post inline comment, status: %s", resp.Status)
	}

	return nil
}

// === VCSProvider 接口实现 ===

// GetDiff 实现 VCSProvider 接口
func (c *GitHubClient) GetDiff(repo string, number int) (string, error) {
	return c.GetPRDiff(repo, number)
}

// GetHeadSHA 实现 VCSProvider 接口
func (c *GitHubClient) GetHeadSHA(repo string, number int) (string, error) {
	return c.GetPRHeadSHA(repo, number)
}

// GetProviderType 实现 VCSProvider 接口
func (c *GitHubClient) GetProviderType() string {
	return ProviderTypeGitHub
}
