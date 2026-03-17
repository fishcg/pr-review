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
