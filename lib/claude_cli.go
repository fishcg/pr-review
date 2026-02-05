package lib

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ClaudeCLIClient Claude CLI 客户端
type ClaudeCLIClient struct {
	BinaryPath      string
	AllowedTools    []string
	Timeout         time.Duration
	MaxOutputLength int
	SystemPrompt    string
	UserTemplate    string
	APIKey          string
	APIURL          string
	Model           string
	EnableOutputLog bool
}

// ReviewResult Claude CLI 审查结果
type ReviewResult struct {
	Content string
	Success bool
	Error   error
}

// NewClaudeCLIClient 创建 Claude CLI 客户端
func NewClaudeCLIClient(binaryPath string, allowedTools []string, timeout int, maxOutputLength int, systemPrompt, userTemplate, apiKey, apiURL, model string, enableOutputLog bool) *ClaudeCLIClient {
	return &ClaudeCLIClient{
		BinaryPath:      binaryPath,
		AllowedTools:    allowedTools,
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

// ReviewCodeInRepo 在克隆的仓库目录中执行 Claude CLI 审查
func (c *ClaudeCLIClient) ReviewCodeInRepo(workDir string, diffContent string, commentsContext string) (*ReviewResult, error) {
	// 1. 构建审查 prompt
	// 添加 Claude CLI 工具使用说明
	toolGuidance := `请对以下 PR/MR 的代码变更进行专业的代码审查。

你可以：
- 使用 Read 工具查看项目中的其他文件以理解上下文
- 使用 Glob 工具查找相关文件
- 使用 Grep 工具搜索代码
- 使用 Bash 工具执行 git 命令

必须基于整个项目的上下文进行审查，判断修改的代码是否影响其他地方，而不仅仅是 diff 本身。

`

	// 组合：工具指导 + 系统 prompt + 用户 prompt
	fullPrompt := toolGuidance + c.SystemPrompt + "\n\n"

	// 如果有其他人的评论，添加到 prompt 中
	if commentsContext != "" {
		fullPrompt += commentsContext + "\n\n"
	}

	// 替换用户模板中的 {diff} 占位符
	userPrompt := strings.ReplaceAll(c.UserTemplate, "{diff}", diffContent)
	reviewPrompt := fullPrompt + userPrompt

	allowedToolsStr := strings.Join(c.AllowedTools, ",")

	args := []string{
		"--print",
		"--allowedTools", allowedToolsStr,
	}

	log.Printf("🤖 Starting Claude CLI review...")
	log.Printf("   Timeout: %v", c.Timeout)
	if c.APIKey != "" {
		log.Printf("   Claude API Key: configured (from config file)")
	} else {
		log.Printf("   Claude API Key: using environment variable or global config")
	}
	if c.APIURL != "" {
		log.Printf("   Claude API URL: %s (from config file)", c.APIURL)
	} else {
		log.Printf("   Claude API URL: using default or environment variable")
	}
	if c.Model != "" {
		log.Printf("   Claude Model: %s (from config file)", c.Model)
	} else {
		log.Printf("   Claude Model: using default or environment variable")
	}

	// 2. 创建执行上下文（带超时）
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	// 3. 执行命令
	cmd := exec.CommandContext(ctx, c.BinaryPath, args...)
	cmd.Dir = workDir

	// 设置 Claude API 环境变量
	// 优先级：配置文件 > 环境变量 > Claude CLI 全局配置
	cmd.Env = filterAndSetEnv(os.Environ(), c.APIKey, c.APIURL, c.Model)

	// 使用 stdin 传递 prompt
	cmd.Stdin = strings.NewReader(reviewPrompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	err := cmd.Run()
	duration := time.Since(startTime)

	// 5. 处理结果
	stderrStr := stderr.String()

	if err != nil {
		// 检查是否超时
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("❌ Claude CLI timeout after %v", c.Timeout)
			return &ReviewResult{
				Content: "",
				Success: false,
				Error:   fmt.Errorf("Claude CLI timeout after %v", c.Timeout),
			}, fmt.Errorf("Claude CLI timeout after %v", c.Timeout)
		}

		// 其他错误 - 输出详细的 stderr 信息
		log.Printf("❌ Claude CLI failed: %v", err)
		if stderrStr != "" {
			log.Printf("❌ Claude CLI stderr:\n%s", stderrStr)
		}
		return &ReviewResult{
			Content: "",
			Success: false,
			Error:   fmt.Errorf("Claude CLI execution failed: %w, stderr: %s", err, stderrStr),
		}, fmt.Errorf("Claude CLI execution failed: %w", err)
	}

	// 6. 处理输出
	output := stdout.String()

	// 如果启用了输出日志，打印完整输出
	if c.EnableOutputLog {
		log.Printf("📝 Claude CLI Output:\n%s", output)
	}

	// 截断保护
	if len(output) > c.MaxOutputLength {
		log.Printf("⚠️ Output truncated from %d to %d bytes", len(output), c.MaxOutputLength)
		output = output[:c.MaxOutputLength] + "\n\n...(output truncated)"
	}

	log.Printf("✅ Claude CLI review completed in %.1fs", duration.Seconds())

	return &ReviewResult{
		Content: output,
		Success: true,
		Error:   nil,
	}, nil
}

// filterAndSetEnv 过滤环境变量并设置 Claude API 配置
// 优先级：配置文件 > 环境变量 > Claude CLI 全局配置
// Claude CLI 使用的环境变量：ANTHROPIC_AUTH_TOKEN, ANTHROPIC_BASE_URL, ANTHROPIC_MODEL
func filterAndSetEnv(envVars []string, apiKey, apiURL, model string) []string {
	filtered := make([]string, 0, len(envVars))

	// 过滤掉已存在的 ANTHROPIC_AUTH_TOKEN, ANTHROPIC_BASE_URL 和 ANTHROPIC_MODEL
	for _, env := range envVars {
		if !strings.HasPrefix(env, "ANTHROPIC_AUTH_TOKEN=") &&
			!strings.HasPrefix(env, "ANTHROPIC_BASE_URL=") &&
			!strings.HasPrefix(env, "ANTHROPIC_MODEL=") {
			filtered = append(filtered, env)
		}
	}

	// 如果配置文件中设置了 API Key，添加到环境变量（覆盖原有值）
	if apiKey != "" {
		filtered = append(filtered, fmt.Sprintf("ANTHROPIC_AUTH_TOKEN=%s", apiKey))
	}

	// 如果配置文件中设置了 API URL，添加到环境变量（覆盖原有值）
	if apiURL != "" {
		filtered = append(filtered, fmt.Sprintf("ANTHROPIC_BASE_URL=%s", apiURL))
	}

	// 如果配置文件中设置了 Model，添加到环境变量（覆盖原有值）
	if model != "" {
		filtered = append(filtered, fmt.Sprintf("ANTHROPIC_MODEL=%s", model))
	}

	return filtered
}

// CheckCLIAvailable 检查 Claude CLI 是否可用
func (c *ClaudeCLIClient) CheckCLIAvailable() error {
	cmd := exec.Command(c.BinaryPath, "--version")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Claude CLI not available at %s: %w, stderr: %s", c.BinaryPath, err, stderr.String())
	}

	version := strings.TrimSpace(stdout.String())
	log.Printf("✅ Claude CLI available: %s", version)
	return nil
}
