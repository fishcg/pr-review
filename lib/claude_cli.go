package lib

import (
	"bytes"
	"context"
	"fmt"
	"log"
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
}

// ReviewResult Claude CLI 审查结果
type ReviewResult struct {
	Content string
	Success bool
	Error   error
}

// NewClaudeCLIClient 创建 Claude CLI 客户端
func NewClaudeCLIClient(binaryPath string, allowedTools []string, timeout int, maxOutputLength int, systemPrompt, userTemplate string) *ClaudeCLIClient {
	return &ClaudeCLIClient{
		BinaryPath:      binaryPath,
		AllowedTools:    allowedTools,
		Timeout:         time.Duration(timeout) * time.Second,
		MaxOutputLength: maxOutputLength,
		SystemPrompt:    systemPrompt,
		UserTemplate:    userTemplate,
	}
}

// ReviewCodeInRepo 在克隆的仓库目录中执行 Claude CLI 审查
func (c *ClaudeCLIClient) ReviewCodeInRepo(workDir string, diffContent string) (*ReviewResult, error) {
	// 1. 构建审查 prompt
	// 添加 Claude CLI 工具使用说明
	toolGuidance := `请对以下 PR/MR 的代码变更进行专业的代码审查。

你可以：
- 使用 Read 工具查看项目中的其他文件以理解上下文
- 使用 Glob 工具查找相关文件
- 使用 Grep 工具搜索代码
- 使用 Bash 工具执行 git 命令

请基于整个项目的上下文进行审查，而不仅仅是 diff 本身。

`

	// 组合：工具指导 + 系统 prompt + 用户 prompt
	fullPrompt := toolGuidance + c.SystemPrompt + "\n\n"

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

	// 2. 创建执行上下文（带超时）
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	// 3. 执行命令
	cmd := exec.CommandContext(ctx, c.BinaryPath, args...)
	cmd.Dir = workDir

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

		// 其他错误
		log.Printf("❌ Claude CLI failed: %v", err)
		return &ReviewResult{
			Content: "",
			Success: false,
			Error:   fmt.Errorf("Claude CLI execution failed: %w, stderr: %s", err, stderrStr),
		}, fmt.Errorf("Claude CLI execution failed: %w", err)
	}

	// 6. 处理输出
	output := stdout.String()

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
