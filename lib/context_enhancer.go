package lib

import (
	"fmt"
	"path/filepath"
	"strings"
)

// PRContextInfo PR 上下文信息
type PRContextInfo struct {
	Title       string
	Description string
	Author      string
	SourceBranch string
	TargetBranch string
	Labels      []string
	IsDraft     bool
	CreatedAt   string
	UpdatedAt   string
}

// FileSummary 文件变更摘要
type FileSummary struct {
	Path         string
	ChangeType   string // "added", "modified", "deleted", "renamed"
	AddedLines   int
	DeletedLines int
	Language     string
	IsTestFile   bool
	IsMigration  bool
	IsConfig     bool
	IsGenerated  bool
}

// DiffEnhancer diff 增强器
type DiffEnhancer struct {
	prInfo    PRContextInfo
	summaries []FileSummary
}

// NewDiffEnhancer 创建 diff 增强器
func NewDiffEnhancer(prInfo PRContextInfo, diff string) *DiffEnhancer {
	summaries := ParseFileSummaries(diff)
	return &DiffEnhancer{
		prInfo:    prInfo,
		summaries: summaries,
	}
}

// EnhanceDiff 增强 diff，添加上下文信息
func (e *DiffEnhancer) EnhanceDiff(diff string) string {
	var builder strings.Builder

	// 添加 PR 上下文
	builder.WriteString("═══════════════════════════════════════════════════════════\n")
	builder.WriteString("                    PR CONTEXT INFORMATION                  \n")
	builder.WriteString("═══════════════════════════════════════════════════════════\n\n")

	builder.WriteString(fmt.Sprintf("📋 Title: %s\n", e.prInfo.Title))
	if e.prInfo.Description != "" {
		// 截断过长的描述
		description := e.prInfo.Description
		if len(description) > 500 {
			description = description[:500] + "...(truncated)"
		}
		builder.WriteString(fmt.Sprintf("📝 Description:\n%s\n\n", description))
	}
	builder.WriteString(fmt.Sprintf("👤 Author: %s\n", e.prInfo.Author))
	builder.WriteString(fmt.Sprintf("🌿 Branch: %s → %s\n", e.prInfo.SourceBranch, e.prInfo.TargetBranch))

	if len(e.prInfo.Labels) > 0 {
		builder.WriteString(fmt.Sprintf("🏷️  Labels: %s\n", strings.Join(e.prInfo.Labels, ", ")))
	}
	if e.prInfo.IsDraft {
		builder.WriteString("⚠️  Status: Draft (Work in Progress)\n")
	}
	builder.WriteString(fmt.Sprintf("📅 Created: %s | Updated: %s\n", e.prInfo.CreatedAt, e.prInfo.UpdatedAt))

	// 添加文件变更统计
	builder.WriteString("\n═══════════════════════════════════════════════════════════\n")
	builder.WriteString("                   FILES CHANGED SUMMARY                    \n")
	builder.WriteString("═══════════════════════════════════════════════════════════\n\n")

	totalAdded, totalDeleted := 0, 0
	filesByType := make(map[string]int)
	specialFiles := []string{}

	for _, summary := range e.summaries {
		totalAdded += summary.AddedLines
		totalDeleted += summary.DeletedLines
		filesByType[summary.Language]++

		flags := getFileFlags(summary)
		changeIndicator := getChangeIndicator(summary.ChangeType)

		builder.WriteString(fmt.Sprintf("%s %s (%s) [%s]",
			changeIndicator,
			summary.Path,
			summary.Language,
			getChangeStats(summary.AddedLines, summary.DeletedLines)))

		if flags != "" {
			builder.WriteString(fmt.Sprintf(" %s", flags))
			specialFiles = append(specialFiles, fmt.Sprintf("%s (%s)", summary.Path, flags))
		}
		builder.WriteString("\n")
	}

	// 统计信息
	builder.WriteString(fmt.Sprintf("\n📊 Total: %d files changed, +%d additions, -%d deletions\n",
		len(e.summaries), totalAdded, totalDeleted))

	if len(filesByType) > 0 {
		builder.WriteString("📂 Languages: ")
		langs := []string{}
		for lang, count := range filesByType {
			langs = append(langs, fmt.Sprintf("%s (%d)", lang, count))
		}
		builder.WriteString(strings.Join(langs, ", "))
		builder.WriteString("\n")
	}

	// 特殊文件提示
	if len(specialFiles) > 0 {
		builder.WriteString("\n⚠️  Special Files Requiring Extra Attention:\n")
		for _, file := range specialFiles {
			builder.WriteString(fmt.Sprintf("   - %s\n", file))
		}
	}

	// 原始 diff
	builder.WriteString("\n═══════════════════════════════════════════════════════════\n")
	builder.WriteString("                      CODE CHANGES                          \n")
	builder.WriteString("═══════════════════════════════════════════════════════════\n\n")
	builder.WriteString(diff)

	return builder.String()
}

// GetModifiedFilePaths 获取所有被修改的文件路径（用于引导 Claude CLI）
func (e *DiffEnhancer) GetModifiedFilePaths() []string {
	paths := make([]string, 0, len(e.summaries))
	for _, summary := range e.summaries {
		// 只包含新增和修改的文件，跳过删除的文件
		if summary.ChangeType != "deleted" {
			paths = append(paths, summary.Path)
		}
	}
	return paths
}

// BuildClaudeCLIGuidance 构建 Claude CLI 引导提示
func (e *DiffEnhancer) BuildClaudeCLIGuidance() string {
	modifiedFiles := e.GetModifiedFilePaths()
	if len(modifiedFiles) == 0 {
		return ""
	}

	var builder strings.Builder

	builder.WriteString("═══════════════════════════════════════════════════════════\n")
	builder.WriteString("              REVIEW GUIDANCE FOR CLAUDE CLI               \n")
	builder.WriteString("═══════════════════════════════════════════════════════════\n\n")

	builder.WriteString("在开始审查前，建议你执行以下步骤以获得更全面的理解：\n\n")

	// 步骤 1: 阅读完整文件
	builder.WriteString("**步骤 1: 阅读被修改文件的完整内容**\n")
	builder.WriteString("使用 Read 工具查看以下文件，理解完整的代码上下文：\n\n")
	for i, path := range modifiedFiles {
		if i >= 5 {
			builder.WriteString(fmt.Sprintf("   ... 以及其他 %d 个文件\n", len(modifiedFiles)-5))
			break
		}
		builder.WriteString(fmt.Sprintf("   - Read(\"%s\")\n", path))
	}

	// 步骤 2: 查找相关代码
	builder.WriteString("\n**步骤 2: 查找相关的函数、类型、接口**\n")
	builder.WriteString("使用 Grep 工具搜索关键函数/类型的调用位置，判断变更影响范围：\n\n")
	builder.WriteString("   - 例如: Grep(\"functionName\", output_mode=\"files_with_matches\")\n")
	builder.WriteString("   - 检查是否所有调用方都已更新\n")

	// 步骤 3: 检查测试覆盖
	builder.WriteString("\n**步骤 3: 检查测试覆盖**\n")
	builder.WriteString("查看是否有对应的测试文件，评估测试覆盖是否充分：\n\n")
	builder.WriteString("   - 使用 Glob 查找测试文件: Glob(\"**/*test*\")\n")
	builder.WriteString("   - 检查是否新增/更新了测试用例\n")

	// 步骤 4: 理解依赖关系
	builder.WriteString("\n**步骤 4: 理解依赖关系**\n")
	builder.WriteString("查看 import/require/include 的相关文件，理解模块间依赖：\n\n")
	builder.WriteString("   - 使用 Read 查看被导入的模块\n")
	builder.WriteString("   - 评估变更对依赖方的影响\n")

	// 重点关注项
	builder.WriteString("\n**重点关注**:\n")
	for _, summary := range e.summaries {
		if summary.IsTestFile {
			builder.WriteString("   ⚠️  包含测试文件变更，请验证测试用例是否充分\n")
			break
		}
	}
	for _, summary := range e.summaries {
		if summary.IsMigration {
			builder.WriteString("   ⚠️  包含数据库迁移，请检查数据安全性和可回滚性\n")
			break
		}
	}
	for _, summary := range e.summaries {
		if summary.IsConfig {
			builder.WriteString("   ⚠️  包含配置文件变更，请检查是否有破坏性变更\n")
			break
		}
	}

	builder.WriteString("\n完成上述步骤后，再基于完整理解进行全面的代码审查。\n\n")

	return builder.String()
}

// ParseFileSummaries 从 diff 中解析文件摘要
func ParseFileSummaries(diff string) []FileSummary {
	summaries := []FileSummary{}
	lines := strings.Split(diff, "\n")

	var currentFile *FileSummary
	for _, line := range lines {
		// 新文件开始
		if strings.HasPrefix(line, "diff --git") {
			if currentFile != nil {
				summaries = append(summaries, *currentFile)
			}
			currentFile = &FileSummary{}
			continue
		}

		if currentFile == nil {
			continue
		}

		// 解析文件路径
		if strings.HasPrefix(line, "--- /dev/null") {
			currentFile.ChangeType = "added"
		} else if strings.HasPrefix(line, "+++ /dev/null") {
			currentFile.ChangeType = "deleted"
		} else if strings.HasPrefix(line, "+++ b/") {
			currentFile.Path = strings.TrimPrefix(line, "+++ b/")
			if currentFile.ChangeType == "" {
				currentFile.ChangeType = "modified"
			}

			// 推断文件属性
			currentFile.Language = detectLanguage(currentFile.Path)
			currentFile.IsTestFile = isTestFile(currentFile.Path)
			currentFile.IsMigration = isMigrationFile(currentFile.Path)
			currentFile.IsConfig = isConfigFile(currentFile.Path)
			currentFile.IsGenerated = isGeneratedFile(currentFile.Path)
		}

		// 统计增删行
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			currentFile.AddedLines++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			currentFile.DeletedLines++
		}
	}

	// 添加最后一个文件
	if currentFile != nil && currentFile.Path != "" {
		summaries = append(summaries, *currentFile)
	}

	return summaries
}

// 辅助函数

func getFileFlags(summary FileSummary) string {
	flags := []string{}
	if summary.IsTestFile {
		flags = append(flags, "🧪test")
	}
	if summary.IsMigration {
		flags = append(flags, "🗃️migration")
	}
	if summary.IsConfig {
		flags = append(flags, "⚙️config")
	}
	if summary.IsGenerated {
		flags = append(flags, "🤖generated")
	}
	if len(flags) > 0 {
		return strings.Join(flags, " ")
	}
	return ""
}

func getChangeIndicator(changeType string) string {
	switch changeType {
	case "added":
		return "✨"
	case "deleted":
		return "🗑️"
	case "modified":
		return "📝"
	case "renamed":
		return "📛"
	default:
		return "📄"
	}
}

func getChangeStats(added, deleted int) string {
	return fmt.Sprintf("+%d -%d", added, deleted)
}

func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	langMap := map[string]string{
		".go":    "Go",
		".js":    "JavaScript",
		".ts":    "TypeScript",
		".jsx":   "React",
		".tsx":   "React/TypeScript",
		".py":    "Python",
		".java":  "Java",
		".cpp":   "C++",
		".c":     "C",
		".h":     "C/C++ Header",
		".rs":    "Rust",
		".rb":    "Ruby",
		".php":   "PHP",
		".swift": "Swift",
		".kt":    "Kotlin",
		".sql":   "SQL",
		".sh":    "Shell",
		".yaml":  "YAML",
		".yml":   "YAML",
		".json":  "JSON",
		".xml":   "XML",
		".html":  "HTML",
		".css":   "CSS",
		".scss":  "SCSS",
		".md":    "Markdown",
		".toml":  "TOML",
	}

	if lang, ok := langMap[ext]; ok {
		return lang
	}
	if ext != "" {
		return ext[1:] // 去掉点号
	}
	return "Unknown"
}

func isTestFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, "_test.") ||
		strings.Contains(lower, ".test.") ||
		strings.Contains(lower, "/test/") ||
		strings.Contains(lower, "/tests/") ||
		strings.Contains(lower, "/__tests__/") ||
		strings.HasSuffix(lower, "_spec.js") ||
		strings.HasSuffix(lower, "_spec.rb") ||
		strings.HasSuffix(lower, ".spec.ts")
}

func isMigrationFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, "migration") ||
		strings.Contains(lower, "/migrate/") ||
		strings.Contains(lower, "/migrations/")
}

func isConfigFile(path string) bool {
	lower := strings.ToLower(path)
	configNames := []string{
		"config.", "dockerfile", ".env", ".yaml", ".yml",
		"package.json", "go.mod", "requirements.txt",
		"cargo.toml", "composer.json", "tsconfig.json",
	}
	for _, name := range configNames {
		if strings.Contains(lower, name) {
			return true
		}
	}
	return false
}

func isGeneratedFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, ".generated.") ||
		strings.Contains(lower, ".gen.") ||
		strings.Contains(lower, "_gen.") ||
		strings.Contains(lower, ".pb.go") ||
		strings.Contains(lower, "_pb2.py") ||
		strings.Contains(lower, "/generated/")
}
