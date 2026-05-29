package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testConfig struct{}

func (testConfig) GetGithubToken() string   { return "gh-token" }
func (testConfig) GetGitlabToken() string   { return "gl-token" }
func (testConfig) GetGitlabBaseURL() string { return "https://gitlab.example.com" }
func (testConfig) GetVCSProvider() string   { return "github" }
func (testConfig) GetAIConfig() (string, string, string, string, string) {
	return "http://ai.example.com", "key", "model", "system", "{diff}"
}
func (testConfig) GetInlineIssueComment() bool             { return false }
func (testConfig) GetCommentOnlyChanges() bool             { return false }
func (testConfig) GetLineMatchStrategy() string            { return "snippet_first" }
func (testConfig) GetReviewMode() string                   { return "api" }
func (testConfig) GetClaudeCLIBinaryPath() string          { return "claude" }
func (testConfig) GetClaudeCLIAllowedTools() []string      { return nil }
func (testConfig) GetClaudeCLITimeout() int                { return 60 }
func (testConfig) GetClaudeCLIMaxOutputLength() int        { return 1000 }
func (testConfig) GetClaudeCLIAPIKey() string              { return "" }
func (testConfig) GetClaudeCLIAPIURL() string              { return "" }
func (testConfig) GetClaudeCLIModel() string               { return "" }
func (testConfig) GetClaudeCLIIncludeOthersComments() bool { return false }
func (testConfig) GetClaudeCLIEnableOutputLog() bool       { return false }
func (testConfig) GetCodexCLIBinaryPath() string           { return "codex" }
func (testConfig) GetCodexCLIAllowedTools() []string       { return nil }
func (testConfig) GetCodexCLITimeout() int                 { return 60 }
func (testConfig) GetCodexCLIMaxOutputLength() int         { return 1000 }
func (testConfig) GetCodexCLIAPIKey() string               { return "" }
func (testConfig) GetCodexCLIAPIURL() string               { return "" }
func (testConfig) GetCodexCLIModel() string                { return "" }
func (testConfig) GetCodexCLIIncludeOthersComments() bool  { return false }
func (testConfig) GetCodexCLIEnableOutputLog() bool        { return false }
func (testConfig) GetRepoCloneTempDir() string             { return "/tmp" }
func (testConfig) GetRepoCloneTimeout() int                { return 60 }
func (testConfig) GetRepoCloneShallowClone() bool          { return true }
func (testConfig) GetRepoCloneShallowDepth() int           { return 1 }
func (testConfig) GetRepoCloneCleanupAfterReview() bool    { return true }
func (testConfig) GetCodeGraphEnabled() bool               { return false }
func (testConfig) GetCodeGraphBinaryPath() string          { return "codegraph" }
func (testConfig) GetCodeGraphIndexTimeout() int           { return 600 }

func init() {
	SetConfig(testConfig{})
}

func TestHandleReview_NumberMismatch(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/review", strings.NewReader(`{"repo":"org/repo","pr_number":1,"number":2}`))
	rr := httptest.NewRecorder()

	HandleReview(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "mismatch") {
		t.Fatalf("expected mismatch error, got: %s", rr.Body.String())
	}
}

func TestHandleReview_InvalidEngine(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/review", strings.NewReader(`{"repo":"org/repo","number":1,"engine":"invalid_engine"}`))
	rr := httptest.NewRecorder()

	HandleReview(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Invalid engine") {
		t.Fatalf("expected invalid engine error, got: %s", rr.Body.String())
	}
}

func TestHandleHealth_DefaultPlainText(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	HandleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if body := strings.TrimSpace(rr.Body.String()); body != "ok" {
		t.Fatalf("expected body 'ok', got %q", body)
	}
	if !strings.Contains(rr.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("expected text/plain content-type, got %q", rr.Header().Get("Content-Type"))
	}
}

func TestHandleHealth_JSONWhenRequested(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Accept", "application/json")
	rr := httptest.NewRecorder()

	HandleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"status":"ok"`) {
		t.Fatalf("expected json status, got: %s", body)
	}
	if !strings.Contains(rr.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("expected application/json content-type, got %q", rr.Header().Get("Content-Type"))
	}
}

func TestParseIssuesFromReview_EscapedPipeInSnippet(t *testing.T) {
	content := strings.Join([]string{
		"### 问题:",
		"| 文件名 | 旧行号 | 新行号 | Side | 代码片段 | 严重程度 | 类别 | 问题描述 | 建议修改 |",
		"|--------|--------|--------|------|----------|----------|------|----------|----------|",
		`| controllers/earphone/prompttone.go | - | 366 | RIGHT | ` + "`len(data) >= 3 && (string(data[0:3]) == \"ID3\" \\|\\| data[0] == 0xff)`" + ` | 低 | lint | 内层 len 检查冗余 | 删除冗余检查 |`,
	}, "\n")

	issues := parseIssuesFromReview(content)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	got := issues[0]
	if got.File != "controllers/earphone/prompttone.go" {
		t.Errorf("file = %q", got.File)
	}
	if got.NewLine != 366 {
		t.Errorf("newLine = %d, want 366", got.NewLine)
	}
	if got.Side != "RIGHT" {
		t.Errorf("side = %q, want RIGHT", got.Side)
	}
	wantCode := `len(data) >= 3 && (string(data[0:3]) == "ID3" || data[0] == 0xff)`
	if got.Code != wantCode {
		t.Errorf("code = %q, want %q", got.Code, wantCode)
	}
	if got.Severity != "低" {
		t.Errorf("severity = %q, want 低", got.Severity)
	}
	if got.Category != "lint" {
		t.Errorf("category = %q, want lint", got.Category)
	}
	if got.Problem != "内层 len 检查冗余" {
		t.Errorf("problem = %q, want 内层 len 检查冗余", got.Problem)
	}
	if got.Suggestion != "删除冗余检查" {
		t.Errorf("suggestion = %q, want 删除冗余检查", got.Suggestion)
	}
}

func TestParseIssuesFromReview_RangeLineNumberNotDropped(t *testing.T) {
	content := strings.Join([]string{
		"### 问题:",
		"| 文件名 | 旧行号 | 新行号 | Side | 代码片段 | 严重程度 | 类别 | 问题描述 | 建议修改 |",
		"|--------|--------|--------|------|----------|----------|------|----------|----------|",
		"| a/single.go | - | 211 | RIGHT | foo() | 高 | bug | 单行号 | 修一下 |",
		"| a/range.go | - | 100-107 | RIGHT | Updates(map) | 高 | bug | 范围行号 | 改成 struct |",
		"| a/range2.go | - | 9-31 | RIGHT | enabled: true | 低 | lint | 配置范围 | 用占位符 |",
	}, "\n")

	issues := parseIssuesFromReview(content)
	if len(issues) != 3 {
		t.Fatalf("expected 3 issues (range rows must not be dropped), got %d", len(issues))
	}
	if issues[1].NewLine != 100 {
		t.Errorf("range 100-107 newLine = %d, want 100", issues[1].NewLine)
	}
	if issues[1].File != "a/range.go" || issues[1].Problem != "范围行号" {
		t.Errorf("range row mismatched: file=%q problem=%q", issues[1].File, issues[1].Problem)
	}
	if issues[2].NewLine != 9 {
		t.Errorf("range 9-31 newLine = %d, want 9", issues[2].NewLine)
	}
}
