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

// githubPRResponse GitHub PR 响应结构
type githubPRResponse struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	Draft  bool   `json:"draft"`
	User   struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
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

// getPRResponse 获取 GitHub PR 响应（内部方法）
func (c *GitHubClient) getPRResponse(repo string, prNum int) (*githubPRResponse, error) {
	infoURL := fmt.Sprintf("https://api.github.com/repos/%s/pulls/%d", repo, prNum)

	req, err := http.NewRequest("GET", infoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API error: %s", resp.Status)
	}

	var prResp githubPRResponse
	if err := json.NewDecoder(resp.Body).Decode(&prResp); err != nil {
		return nil, fmt.Errorf("failed to decode PR info: %w", err)
	}

	return &prResp, nil
}

// GetPRHeadSHA 获取 PR 的最新 commit SHA
func (c *GitHubClient) GetPRHeadSHA(repo string, prNum int) (string, error) {
	prResp, err := c.getPRResponse(repo, prNum)
	if err != nil {
		return "", err
	}

	if prResp.Head.SHA == "" {
		return "", fmt.Errorf("PR head sha is empty")
	}

	return prResp.Head.SHA, nil
}

// GetPRInfo 获取 PR 的详细信息
func (c *GitHubClient) GetPRInfo(repo string, prNum int) (*PRInfo, error) {
	prResp, err := c.getPRResponse(repo, prNum)
	if err != nil {
		return nil, err
	}

	// 提取标签
	labels := make([]string, 0, len(prResp.Labels))
	for _, label := range prResp.Labels {
		labels = append(labels, label.Name)
	}

	return &PRInfo{
		Title:        prResp.Title,
		Description:  prResp.Body,
		Author:       prResp.User.Login,
		SourceBranch: prResp.Head.Ref,
		TargetBranch: prResp.Base.Ref,
		Labels:       labels,
		IsDraft:      prResp.Draft,
		CreatedAt:    prResp.CreatedAt,
		UpdatedAt:    prResp.UpdatedAt,
	}, nil
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
func (c *GitHubClient) PostInlineComment(repo string, prNum int, commitSHA, path string, position int, body string, oldLine, newLine int) error {
	// GitHub 只使用 position 参数，忽略 oldLine 和 newLine
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

// GetIssueComments 获取 PR 的普通评论列表
func (c *GitHubClient) GetIssueComments(repo string, prNum int) ([]Comment, error) {
	commentsURL := fmt.Sprintf("https://api.github.com/repos/%s/issues/%d/comments", repo, prNum)

	req, err := http.NewRequest("GET", commentsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get comments: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error: %s, body: %s", resp.Status, string(body))
	}

	var githubComments []struct {
		ID        int64  `json:"id"`
		Body      string `json:"body"`
		CreatedAt string `json:"created_at"`
		User      struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
		} `json:"user"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&githubComments); err != nil {
		return nil, fmt.Errorf("failed to decode comments: %w", err)
	}

	comments := make([]Comment, len(githubComments))
	for i, gc := range githubComments {
		comments[i] = Comment{
			ID:        gc.ID,
			Body:      gc.Body,
			CreatedAt: gc.CreatedAt,
			UserID:    gc.User.ID,
			UserLogin: gc.User.Login,
		}
	}

	return comments, nil
}

// GetInlineComments 获取 PR 的行内评论列表
func (c *GitHubClient) GetInlineComments(repo string, prNum int) ([]Comment, error) {
	commentsURL := fmt.Sprintf("https://api.github.com/repos/%s/pulls/%d/comments", repo, prNum)

	req, err := http.NewRequest("GET", commentsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get inline comments: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error: %s, body: %s", resp.Status, string(body))
	}

	var githubComments []struct {
		ID        int64  `json:"id"`
		Body      string `json:"body"`
		Path      string `json:"path"`
		Line      int    `json:"line"`
		Position  int    `json:"position"`
		CreatedAt string `json:"created_at"`
		User      struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
		} `json:"user"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&githubComments); err != nil {
		return nil, fmt.Errorf("failed to decode inline comments: %w", err)
	}

	comments := make([]Comment, len(githubComments))
	for i, gc := range githubComments {
		comments[i] = Comment{
			ID:        gc.ID,
			Body:      gc.Body,
			Path:      gc.Path,
			Line:      gc.Line,
			Position:  gc.Position,
			CreatedAt: gc.CreatedAt,
			UserID:    gc.User.ID,
			UserLogin: gc.User.Login,
		}
	}

	return comments, nil
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

// GetBranchInfo 实现 VCSProvider 接口 - 获取分支信息
func (c *GitHubClient) GetBranchInfo(repo string, prNum int) (*BranchInfo, error) {
	infoURL := fmt.Sprintf("https://api.github.com/repos/%s/pulls/%d", repo, prNum)

	req, err := http.NewRequest("GET", infoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API error: %s", resp.Status)
	}

	var prInfo struct {
		Head struct {
			Ref string `json:"ref"` // source branch
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"` // target branch
		} `json:"base"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&prInfo); err != nil {
		return nil, fmt.Errorf("failed to decode PR info: %w", err)
	}

	return &BranchInfo{
		SourceBranch: prInfo.Head.Ref,
		TargetBranch: prInfo.Base.Ref,
		SourceSHA:    prInfo.Head.SHA,
	}, nil
}

// GetCloneURL 实现 VCSProvider 接口 - 获取克隆 URL
func (c *GitHubClient) GetCloneURL(repo string) (string, error) {
	// GitHub repo format: owner/repo
	// Clone URL: https://github.com/owner/repo.git
	return fmt.Sprintf("https://github.com/%s.git", repo), nil
}

// GetCurrentUser 实现 VCSProvider 接口 - 获取当前认证用户
func (c *GitHubClient) GetCurrentUser() (string, error) {
	userURL := "https://api.github.com/user"

	req, err := http.NewRequest("GET", userURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API error: %s, body: %s", resp.Status, string(body))
	}

	var user struct {
		Login string `json:"login"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", fmt.Errorf("failed to decode user: %w", err)
	}

	return user.Login, nil
}

// GetProviderType 实现 VCSProvider 接口
func (c *GitHubClient) GetProviderType() string {
	return ProviderTypeGitHub
}
