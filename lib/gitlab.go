package lib

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GitLabClient GitLab API 客户端
type GitLabClient struct {
	Token      string
	BaseURL    string
	HTTPClient *http.Client
}

// MRInfo MR 基本信息
type MRInfo struct {
	SHA        string `json:"sha"`
	DiffRefs   struct {
		BaseSHA  string `json:"base_sha"`
		HeadSHA  string `json:"head_sha"`
		StartSHA string `json:"start_sha"`
	} `json:"diff_refs"`
}

// MRChanges MR 变更信息
type MRChanges struct {
	SHA     string `json:"sha"`
	Changes []struct {
		OldPath string `json:"old_path"`
		NewPath string `json:"new_path"`
		Diff    string `json:"diff"`
	} `json:"changes"`
}

// NewGitLabClient 创建 GitLab 客户端
func NewGitLabClient(token, baseURL string) *GitLabClient {
	if baseURL == "" {
		baseURL = "https://gitlab.com"
	}
	return &GitLabClient{
		Token:      token,
		BaseURL:    strings.TrimSuffix(baseURL, "/"),
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// GetDiff 获取 Merge Request 的代码变更
func (c *GitLabClient) GetDiff(repo string, mrNum int) (string, error) {
	// URL encode the project path
	encodedRepo := url.PathEscape(repo)
	diffURL := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%d/changes", c.BaseURL, encodedRepo, mrNum)

	req, err := http.NewRequest("GET", diffURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.Token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get diff: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitLab API error: %s, body: %s", resp.Status, string(body))
	}

	var mrChanges MRChanges
	if err := json.NewDecoder(resp.Body).Decode(&mrChanges); err != nil {
		return "", fmt.Errorf("failed to decode MR changes: %w", err)
	}

	// 将 GitLab 的 changes 转换为 unified diff 格式
	diffText := c.buildUnifiedDiff(mrChanges.Changes)

	// 截断保护，避免过长的 diff
	const maxDiffLength = 6000
	if len(diffText) > maxDiffLength {
		diffText = diffText[:maxDiffLength] + "\n...(truncated)"
	}

	return diffText, nil
}

// GetHeadSHA 获取 MR 的最新 commit SHA
func (c *GitLabClient) GetHeadSHA(repo string, mrNum int) (string, error) {
	encodedRepo := url.PathEscape(repo)
	infoURL := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%d", c.BaseURL, encodedRepo, mrNum)

	req, err := http.NewRequest("GET", infoURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.Token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get MR info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitLab API error: %s, body: %s", resp.Status, string(body))
	}

	var mrInfo MRInfo
	if err := json.NewDecoder(resp.Body).Decode(&mrInfo); err != nil {
		return "", fmt.Errorf("failed to decode MR info: %w", err)
	}

	headSHA := mrInfo.SHA
	if headSHA == "" && mrInfo.DiffRefs.HeadSHA != "" {
		headSHA = mrInfo.DiffRefs.HeadSHA
	}

	if headSHA == "" {
		return "", fmt.Errorf("MR head sha is empty")
	}

	return headSHA, nil
}

// PostComment 向 MR 发布评论
func (c *GitLabClient) PostComment(repo string, mrNum int, comment string) error {
	encodedRepo := url.PathEscape(repo)
	commentURL := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%d/notes", c.BaseURL, encodedRepo, mrNum)

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

	req.Header.Set("PRIVATE-TOKEN", c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to post comment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("GitLab API response: %s", string(body))
		return fmt.Errorf("failed to post comment, status: %s", resp.Status)
	}

	return nil
}

// PostInlineComment 向 MR 发布行内评论
func (c *GitLabClient) PostInlineComment(repo string, mrNum int, commitSHA, path string, position int, body string) error {
	encodedRepo := url.PathEscape(repo)

	// GitLab 使用 discussions API 来发布行内评论
	// 需要获取 MR 信息来构建 position 对象
	mrInfo, err := c.getMRInfo(repo, mrNum)
	if err != nil {
		return fmt.Errorf("failed to get MR info for inline comment: %w", err)
	}

	discussionURL := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%d/discussions", c.BaseURL, encodedRepo, mrNum)

	// GitLab 的 position 对象比 GitHub 复杂，需要包含 base_sha, head_sha, start_sha 等
	// position 参数在 GitLab 中对应的是 new_line（新文件的行号）
	discussionBody := map[string]interface{}{
		"body": body,
		"position": map[string]interface{}{
			"base_sha":      mrInfo.DiffRefs.BaseSHA,
			"head_sha":      mrInfo.DiffRefs.HeadSHA,
			"start_sha":     mrInfo.DiffRefs.StartSHA,
			"position_type": "text",
			"new_path":      path,
			"old_path":      path,
			"new_line":      position, // 注意：这里使用 position 作为行号的近似
		},
	}

	jsonDiscussion, err := json.Marshal(discussionBody)
	if err != nil {
		return fmt.Errorf("failed to marshal discussion: %w", err)
	}

	req, err := http.NewRequest("POST", discussionURL, bytes.NewBuffer(jsonDiscussion))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to post inline comment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("GitLab API response: %s", string(bodyBytes))
		return fmt.Errorf("failed to post inline comment, status: %s", resp.Status)
	}

	return nil
}

// GetProviderType 实现 VCSProvider 接口
func (c *GitLabClient) GetProviderType() string {
	return ProviderTypeGitLab
}

// === 辅助方法 ===

// getMRInfo 获取 MR 完整信息（包括 diff_refs）
func (c *GitLabClient) getMRInfo(repo string, mrNum int) (*MRInfo, error) {
	encodedRepo := url.PathEscape(repo)
	infoURL := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%d", c.BaseURL, encodedRepo, mrNum)

	req, err := http.NewRequest("GET", infoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.Token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get MR info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitLab API error: %s, body: %s", resp.Status, string(body))
	}

	var mrInfo MRInfo
	if err := json.NewDecoder(resp.Body).Decode(&mrInfo); err != nil {
		return nil, fmt.Errorf("failed to decode MR info: %w", err)
	}

	return &mrInfo, nil
}

// buildUnifiedDiff 将 GitLab changes 数组转换为 unified diff 格式
func (c *GitLabClient) buildUnifiedDiff(changes []struct {
	OldPath string `json:"old_path"`
	NewPath string `json:"new_path"`
	Diff    string `json:"diff"`
}) string {
	var builder strings.Builder

	for _, change := range changes {
		// 写入文件头
		builder.WriteString(fmt.Sprintf("diff --git a/%s b/%s\n", change.OldPath, change.NewPath))

		// 如果是新文件
		if change.OldPath == "/dev/null" || change.OldPath == "" {
			builder.WriteString("new file mode 100644\n")
			builder.WriteString("--- /dev/null\n")
			builder.WriteString(fmt.Sprintf("+++ b/%s\n", change.NewPath))
		} else if change.NewPath == "/dev/null" || change.NewPath == "" {
			// 如果是删除文件
			builder.WriteString("deleted file mode 100644\n")
			builder.WriteString(fmt.Sprintf("--- a/%s\n", change.OldPath))
			builder.WriteString("+++ /dev/null\n")
		} else if change.OldPath != change.NewPath {
			// 如果是重命名
			builder.WriteString(fmt.Sprintf("rename from %s\n", change.OldPath))
			builder.WriteString(fmt.Sprintf("rename to %s\n", change.NewPath))
			builder.WriteString(fmt.Sprintf("--- a/%s\n", change.OldPath))
			builder.WriteString(fmt.Sprintf("+++ b/%s\n", change.NewPath))
		} else {
			// 普通修改
			builder.WriteString(fmt.Sprintf("--- a/%s\n", change.OldPath))
			builder.WriteString(fmt.Sprintf("+++ b/%s\n", change.NewPath))
		}

		// 添加 diff 内容（GitLab 已经提供了 unified diff 格式的片段）
		if change.Diff != "" {
			builder.WriteString(change.Diff)
			// 确保以换行结尾
			if !strings.HasSuffix(change.Diff, "\n") {
				builder.WriteString("\n")
			}
		}
	}

	return builder.String()
}
