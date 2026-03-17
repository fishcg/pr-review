package lib

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// CodexCLIClient Codex CLI 客户端
type CodexCLIClient struct {
	BinaryPath      string
	Timeout         time.Duration
	MaxOutputLength int
	SystemPrompt    string
	UserTemplate    string
	APIKey          string
	APIURL          string
	Model           string
	EnableOutputLog bool
}

// NewCodexCLIClient 创建 Codex CLI 客户端
func NewCodexCLIClient(binaryPath string, timeout int, maxOutputLength int, systemPrompt, userTemplate, apiKey, apiURL, model string, enableOutputLog bool) *CodexCLIClient {
	return &CodexCLIClient{
		BinaryPath:      binaryPath,
		Timeout:         time.Duration(timeout) * time.Second,
		MaxOutputLength: maxOutputLength,
		SystemPrompt:    systemPrompt,
		UserTemplate:    userTemplate,
		APIKey:          apiKey,
		APIURL:          apiURL,
		Model:           model,
		EnableOutputLog: enableOutputLog,
	}
}

// ReviewCodeInRepo 在克隆的仓库目录中执行 Codex CLI 审查
func (c *CodexCLIClient) ReviewCodeInRepo(workDir string, baseBranch string, diffContent string) (*ReviewResult, error) {
	fullPrompt := c.SystemPrompt + "\n\n" + strings.ReplaceAll(c.UserTemplate, "{diff}", diffContent)

	args := []string{"review"}
	if c.Model != "" {
		args = append(args, "-m", c.Model)
	}
	if baseBranch != "" {
		args = append(args, "--base", baseBranch)
	}
	args = append(args, "-")

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.BinaryPath, args...)
	cmd.Dir = workDir
	cmd.Env = filterAndSetCodexEnv(os.Environ(), c.APIKey, c.APIURL, c.Model)
	cmd.Stdin = strings.NewReader(fullPrompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	stderrStr := stderr.String()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return &ReviewResult{
				Content: "",
				Success: false,
				Error:   fmt.Errorf("Codex CLI timeout after %v", c.Timeout),
			}, fmt.Errorf("Codex CLI timeout after %v", c.Timeout)
		}

		log.Printf("❌ Codex CLI failed: %v", err)
		if stderrStr != "" {
			log.Printf("❌ Codex CLI stderr:\n%s", stderrStr)
		}

		return &ReviewResult{
			Content: "",
			Success: false,
			Error:   fmt.Errorf("Codex CLI execution failed: %w, stderr: %s", err, stderrStr),
		}, fmt.Errorf("Codex CLI execution failed: %w", err)
	}

	output := strings.TrimSpace(stdout.String())
	if c.EnableOutputLog {
		log.Printf("📝 Codex CLI Output:\n%s", output)
	}

	if len(output) > c.MaxOutputLength {
		output = output[:c.MaxOutputLength] + "\n\n...(output truncated)"
	}

	if output == "" {
		return &ReviewResult{
			Content: "",
			Success: false,
			Error:   fmt.Errorf("Codex CLI output is empty"),
		}, fmt.Errorf("Codex CLI output is empty")
	}

	return &ReviewResult{
		Content: output,
		Success: true,
		Error:   nil,
	}, nil
}

func filterAndSetCodexEnv(envVars []string, apiKey, apiURL, model string) []string {
	filtered := make([]string, 0, len(envVars)+3)

	overrideAPIKey := apiKey != ""
	overrideAPIURL := apiURL != ""
	overrideModel := model != ""

	for _, env := range envVars {
		if overrideAPIKey && strings.HasPrefix(env, "OPENAI_API_KEY=") {
			continue
		}
		if overrideAPIURL && strings.HasPrefix(env, "OPENAI_BASE_URL=") {
			continue
		}
		if overrideModel && strings.HasPrefix(env, "OPENAI_MODEL=") {
			continue
		}
		filtered = append(filtered, env)
	}

	if overrideAPIKey {
		filtered = append(filtered, fmt.Sprintf("OPENAI_API_KEY=%s", apiKey))
	}
	if overrideAPIURL {
		filtered = append(filtered, fmt.Sprintf("OPENAI_BASE_URL=%s", apiURL))
	}
	if overrideModel {
		filtered = append(filtered, fmt.Sprintf("OPENAI_MODEL=%s", model))
	}

	return filtered
}

// CheckCLIAvailable 检查 Codex CLI 是否可用
func (c *CodexCLIClient) CheckCLIAvailable() error {
	cmd := exec.Command(c.BinaryPath, "--version")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Codex CLI not available at %s: %w, stderr: %s", c.BinaryPath, err, stderr.String())
	}
	return nil
}

// BuildCodexGuidance 构建简要引导，帮助 Codex 输出稳定的结构化审查
func BuildCodexGuidance() string {
	return strings.Join([]string{
		"请基于仓库上下文进行代码审查，优先关注正确性、安全性、并发与性能。",
		"输出请使用 Markdown，并包含：评分、修改点、总结。",
		"如果发现可定位问题，请尽量提供文件与行号信息。",
	}, "\n")
}

// FormatCodexTimeoutLog 统一超时日志格式
func FormatCodexTimeoutLog(timeout time.Duration) string {
	return "codex timeout: " + strconv.FormatInt(int64(timeout/time.Second), 10) + "s"
}
