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

🔧 可用工具：
- Read: 查看项目中的任何文件以理解上下文
- Glob: 查找相关文件
- Grep: 搜索代码中的函数、类型、变量等
- Bash: 执行 git 命令查看历史、分支等

📝 审查方法：
1. 先使用工具充分理解项目结构和修改的代码上下文
2. 基于完整的项目理解进行审查，不要只看 diff 表面
3. 评估修改对其他文件的影响（使用 Grep 查找调用位置）
4. 在问题表格中，只填写被修改文件的问题（文件名和代码片段必须来自 diff）
5. 如果发现相关文件的问题，在问题描述中说明，不要在表格中列出未修改的文件

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

	err := cmd.Run()

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

		// 其他错误 - 输出详细的调试信息
		log.Printf("❌ Claude CLI failed: %v", err)
		// 输出 stderr（如果有）
		if stderrStr != "" {
			log.Printf("❌ Claude CLI stderr:\n%s", stderrStr)
		}

		// 输出 stdout（如果有且不太长）
		stdoutStr := stdout.String()
		if stdoutStr != "" {
			if len(stdoutStr) > 500 {
				log.Printf("❌ Claude CLI stdout (first 500 bytes):\n%s\n... (truncated)", stdoutStr[:500])
			} else {
				log.Printf("❌ Claude CLI stdout:\n%s", stdoutStr)
			}
		} else {
			log.Printf("❌ Claude CLI stdout: (empty)")
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
	return nil
}
