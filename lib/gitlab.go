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

// gitlabMRResponse GitLab MR 响应结构
type gitlabMRResponse struct {
	Title          string `json:"title"`
	Description    string `json:"description"`
	State          string `json:"state"`
	Draft          bool   `json:"draft"`
	WorkInProgress bool   `json:"work_in_progress"`
	Author         struct {
		Username string `json:"username"`
	} `json:"author"`
	SHA      string `json:"sha"`
	DiffRefs struct {
		BaseSHA  string `json:"base_sha"`
		HeadSHA  string `json:"head_sha"`
		StartSHA string `json:"start_sha"`
	} `json:"diff_refs"`
	SourceBranch string   `json:"source_branch"`
	TargetBranch string   `json:"target_branch"`
	Labels       []string `json:"labels"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
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
	const maxDiffLength = 24000
	if len(diffText) > maxDiffLength {
		diffText = diffText[:maxDiffLength] + "\n...(truncated)"
	}

	return diffText, nil
}

// getMRResponse 获取 GitLab MR 响应（内部方法）
func (c *GitLabClient) getMRResponse(repo string, mrNum int) (*gitlabMRResponse, error) {
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

	var mrResp gitlabMRResponse
	if err := json.NewDecoder(resp.Body).Decode(&mrResp); err != nil {
		return nil, fmt.Errorf("failed to decode MR info: %w", err)
	}

	return &mrResp, nil
}

// GetHeadSHA 获取 MR 的最新 commit SHA
func (c *GitLabClient) GetHeadSHA(repo string, mrNum int) (string, error) {
	mrResp, err := c.getMRResponse(repo, mrNum)
	if err != nil {
		return "", err
	}

	headSHA := mrResp.SHA
	if headSHA == "" && mrResp.DiffRefs.HeadSHA != "" {
		headSHA = mrResp.DiffRefs.HeadSHA
	}

	if headSHA == "" {
		return "", fmt.Errorf("MR head sha is empty")
	}

	return headSHA, nil
}

// GetPRInfo 获取 MR 的详细信息
func (c *GitLabClient) GetPRInfo(repo string, mrNum int) (*PRInfo, error) {
	mrResp, err := c.getMRResponse(repo, mrNum)
	if err != nil {
		return nil, err
	}

	// GitLab 的 draft 状态可能通过 draft 字段或 work_in_progress 字段表示
	isDraft := mrResp.Draft || mrResp.WorkInProgress

	return &PRInfo{
		Title:        mrResp.Title,
		Description:  mrResp.Description,
		Author:       mrResp.Author.Username,
		SourceBranch: mrResp.SourceBranch,
		TargetBranch: mrResp.TargetBranch,
		Labels:       mrResp.Labels,
		IsDraft:      isDraft,
		CreatedAt:    mrResp.CreatedAt,
		UpdatedAt:    mrResp.UpdatedAt,
	}, nil
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
// position: 对于 GitLab 忽略该参数
// oldLine, newLine: 用于标识评论的具体行位置
func (c *GitLabClient) PostInlineComment(repo string, mrNum int, commitSHA, path string, position int, body string, oldLine, newLine int) error {
	encodedRepo := url.PathEscape(repo)

	// GitLab 使用 discussions API 来发布行内评论
	// 需要获取 MR 信息来构建 position 对象
	mrResp, err := c.getMRResponse(repo, mrNum)
	if err != nil {
		return fmt.Errorf("failed to get MR info for inline comment: %w", err)
	}

	discussionURL := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%d/discussions", c.BaseURL, encodedRepo, mrNum)

	// 构建 position 对象
	positionObj := map[string]interface{}{
		"base_sha":      mrResp.DiffRefs.BaseSHA,
		"head_sha":      mrResp.DiffRefs.HeadSHA,
		"start_sha":     mrResp.DiffRefs.StartSHA,
		"position_type": "text",
		"new_path":      path,
		"old_path":      path,
	}

	// 根据 oldLine 和 newLine 设置行位置
	// GitLab API 的限制：每次只能指定 old_line 或 new_line 中的一个
	// 对于修改的行（同时有 old_line 和 new_line），优先使用 new_line
	if newLine > 0 {
		// 新增的行或修改的行：只设置 new_line
		positionObj["new_line"] = newLine
	} else if oldLine > 0 {
		// 删除的行：只设置 old_line
		positionObj["old_line"] = oldLine
	} else {
		return fmt.Errorf("invalid line numbers: oldLine=%d, newLine=%d", oldLine, newLine)
	}
	discussionBody := map[string]interface{}{
		"body":     body,
		"position": positionObj,
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
		log.Printf("❌ GitLab API response (status %d): %s", resp.StatusCode, string(bodyBytes))
		return fmt.Errorf("failed to post inline comment, status: %s", resp.Status)
	}
	return nil
}

// GetIssueComments 获取 MR 的普通评论列表
func (c *GitLabClient) GetIssueComments(repo string, mrNum int) ([]Comment, error) {
	encodedRepo := url.PathEscape(repo)
	notesURL := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%d/notes", c.BaseURL, encodedRepo, mrNum)

	req, err := http.NewRequest("GET", notesURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.Token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get notes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitLab API error: %s, body: %s", resp.Status, string(body))
	}

	var gitlabNotes []struct {
		ID        int64  `json:"id"`
		Body      string `json:"body"`
		CreatedAt string `json:"created_at"`
		System    bool   `json:"system"`
		Author    struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		} `json:"author"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&gitlabNotes); err != nil {
		return nil, fmt.Errorf("failed to decode notes: %w", err)
	}

	comments := make([]Comment, 0, len(gitlabNotes))
	for _, note := range gitlabNotes {
		// 跳过系统评论
		if note.System {
			continue
		}
		comments = append(comments, Comment{
			ID:        note.ID,
			Body:      note.Body,
			CreatedAt: note.CreatedAt,
			UserID:    note.Author.ID,
			UserLogin: note.Author.Username,
		})
	}

	return comments, nil
}

// GetInlineComments 获取 MR 的行内评论列表
func (c *GitLabClient) GetInlineComments(repo string, mrNum int) ([]Comment, error) {
	encodedRepo := url.PathEscape(repo)
	discussionsURL := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%d/discussions", c.BaseURL, encodedRepo, mrNum)

	req, err := http.NewRequest("GET", discussionsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.Token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get discussions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitLab API error: %s, body: %s", resp.Status, string(body))
	}

	var gitlabDiscussions []struct {
		ID    string `json:"id"`
		Notes []struct {
			ID        int64  `json:"id"`
			Body      string `json:"body"`
			CreatedAt string `json:"created_at"`
			System    bool   `json:"system"`
			Author    struct {
				ID       int64  `json:"id"`
				Username string `json:"username"`
			} `json:"author"`
			Position struct {
				NewPath string `json:"new_path"`
				OldPath string `json:"old_path"`
				NewLine int    `json:"new_line"`
				OldLine int    `json:"old_line"`
			} `json:"position"`
		} `json:"notes"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&gitlabDiscussions); err != nil {
		return nil, fmt.Errorf("failed to decode discussions: %w", err)
	}

	comments := make([]Comment, 0)
	for _, discussion := range gitlabDiscussions {
		for _, note := range discussion.Notes {
			// 跳过系统评论
			if note.System {
				continue
			}

			// 只处理有位置信息的评论（行内评论）
			if note.Position.NewPath == "" && note.Position.OldPath == "" {
				continue
			}

			path := note.Position.NewPath
			if path == "" {
				path = note.Position.OldPath
			}

			line := note.Position.NewLine
			if line == 0 {
				line = note.Position.OldLine
			}

			comments = append(comments, Comment{
				ID:        note.ID,
				Body:      note.Body,
				Path:      path,
				Line:      line,
				CreatedAt: note.CreatedAt,
				UserID:    note.Author.ID,
				UserLogin: note.Author.Username,
			})
		}
	}

	return comments, nil
}

// GetBranchInfo 实现 VCSProvider 接口 - 获取分支信息
func (c *GitLabClient) GetBranchInfo(repo string, mrNum int) (*BranchInfo, error) {
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

	var mrInfo struct {
		SHA          string `json:"sha"`
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
		DiffRefs     struct {
			HeadSHA string `json:"head_sha"`
		} `json:"diff_refs"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&mrInfo); err != nil {
		return nil, fmt.Errorf("failed to decode MR info: %w", err)
	}

	sourceSHA := mrInfo.SHA
	if sourceSHA == "" && mrInfo.DiffRefs.HeadSHA != "" {
		sourceSHA = mrInfo.DiffRefs.HeadSHA
	}

	return &BranchInfo{
		SourceBranch: mrInfo.SourceBranch,
		TargetBranch: mrInfo.TargetBranch,
		SourceSHA:    sourceSHA,
	}, nil
}

// GetCloneURL 实现 VCSProvider 接口 - 获取克隆 URL
func (c *GitLabClient) GetCloneURL(repo string) (string, error) {
	// GitLab repo format: group/project 或 namespace/group/project
	// Clone URL: https://gitlab.com/group/project.git 或自托管地址

	// 解析 BaseURL
	baseURLParsed, err := url.Parse(c.BaseURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse base URL: %w", err)
	}

	// 构建克隆 URL
	cloneURL := fmt.Sprintf("%s://%s/%s.git", baseURLParsed.Scheme, baseURLParsed.Host, repo)
	return cloneURL, nil
}

// GetCurrentUser 实现 VCSProvider 接口 - 获取当前认证用户
func (c *GitLabClient) GetCurrentUser() (string, error) {
	userURL := fmt.Sprintf("%s/api/v4/user", c.BaseURL)

	req, err := http.NewRequest("GET", userURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("PRIVATE-TOKEN", c.Token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitLab API error: %s, body: %s", resp.Status, string(body))
	}

	var user struct {
		Username string `json:"username"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", fmt.Errorf("failed to decode user: %w", err)
	}

	return user.Username, nil
}

// GetProviderType 实现 VCSProvider 接口
func (c *GitLabClient) GetProviderType() string {
	return ProviderTypeGitLab
}

// === 辅助方法 ===

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
