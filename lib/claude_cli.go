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
}

// ReviewResult Claude CLI 审查结果
type ReviewResult struct {
	Content string
	Success bool
	Error   error
}

// NewClaudeCLIClient 创建 Claude CLI 客户端
func NewClaudeCLIClient(binaryPath string, allowedTools []string, timeout int, maxOutputLength int) *ClaudeCLIClient {
	return &ClaudeCLIClient{
		BinaryPath:      binaryPath,
		AllowedTools:    allowedTools,
		Timeout:         time.Duration(timeout) * time.Second,
		MaxOutputLength: maxOutputLength,
	}
}

// ReviewCodeInRepo 在克隆的仓库目录中执行 Claude CLI 审查
func (c *ClaudeCLIClient) ReviewCodeInRepo(workDir string, diffContent string) (*ReviewResult, error) {
	// 1. 构建审查 prompt
	reviewPrompt := fmt.Sprintf(`请对以下 PR/MR 的代码变更进行专业的代码审查。

你可以：
- 使用 Read 工具查看项目中的其他文件以理解上下文
- 使用 Glob 工具查找相关文件
- 使用 Grep 工具搜索代码
- 使用 Bash 工具执行 git 命令

请基于整个项目的上下文进行审查，而不仅仅是 diff 本身。

审查要点：
  1. **逻辑错误与 Bug**：是否存在潜在的逻辑漏洞、边界条件处理不当或空指针风险？
  2. **代码质量与可读性**：是否遵循 Clean Code 原则？变量命名是否清晰？函数是否过长？是否有冗余代码？
  3. **性能优化**：是否存在不必要的循环、内存泄露或可以优化的算法复杂度？
  4. **安全性**：是否存在常见的安全漏洞（如 SQL 注入、XSS、敏感信息泄露、不安全的加密等）？
  5. **可测试性**：代码是否易于编写单元测试？是否实现了关注点分离？
  6. **最佳实践**：是否符合该编程语言/框架的主流社区最佳实践？
  7. **文档与注释**：是否有必要的注释和文档？注释是否准确反映代码意图？

请以以下格式输出审查结果（严格遵守格式,注意括号内容为说明，不要输出）：

## 评分
评分：X（满分 100，严重bug<60，有语法错误=0，轻微问题扣5-10分）

## 修改点
1. [简要描述主要修改]
2. [简要描述主要修改]

## 总结
[一句话评价，是否建议合入（建议合入时打✅标记，否则打❌）]

## 详细问题
如果有具体问题，请使用表格格式：

| 文件名 | 旧行号 | 新行号 | 代码片段 | 严重程度 | 类别 | 问题描述 | 建议修改 |
|--------|--------|--------|----------|----------|------|----------|----------|

代码变更 diff：
%s
`, diffContent)

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
