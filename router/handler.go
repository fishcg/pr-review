package router

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"pr-review/lib"
	"strconv"
	"strings"
)

// ReviewRequest PR 审查请求体结构
type ReviewRequest struct {
	Repo     string `json:"repo"`               // owner/repo
	PRNumber int    `json:"pr_number"`          // PR ID
	Provider string `json:"provider,omitempty"` // 可选，未指定则使用配置
}

// Config 配置接口（避免循环依赖）
type Config interface {
	GetGithubToken() string
	GetGitlabToken() string
	GetGitlabBaseURL() string
	GetVCSProvider() string
	GetAIConfig() (apiURL, apiKey, model, systemPrompt, userTemplate string)
	GetInlineIssueComment() bool
	GetCommentOnlyChanges() bool
	GetLineMatchStrategy() string
}

var appConfig Config

// SetConfig 设置配置
func SetConfig(cfg Config) {
	appConfig = cfg
}

// HandleReview 处理 PR 审查请求
func HandleReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. 解析请求
	var req ReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// 2. 确定使用的 VCS Provider（请求中指定 > 配置文件）
	providerType := req.Provider
	if providerType == "" {
		providerType = appConfig.GetVCSProvider()
	}

	// 3. 获取对应的 Token
	var token string
	switch providerType {
	case lib.ProviderTypeGitHub:
		token = r.Header.Get("X-Github-Token")
		if token == "" {
			token = appConfig.GetGithubToken()
		}
	case lib.ProviderTypeGitLab:
		token = r.Header.Get("PRIVATE-TOKEN")
		if token == "" {
			token = appConfig.GetGitlabToken()
		}
	default:
		http.Error(w, fmt.Sprintf("Unsupported provider: %s", providerType), http.StatusBadRequest)
		return
	}

	log.Printf("📥 Received review request for %s #%d (provider: %s)", req.Repo, req.PRNumber, providerType)

	// 4. 异步处理 Review (防止 CI HTTP 请求超时)
	// 如果你希望 CI 等待结果，可以去掉 go 关键字
	go ProcessReview(req.Repo, req.PRNumber, providerType, token)

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(fmt.Sprintf("Review started for %s #%d", req.Repo, req.PRNumber)))
}

// HandleHealth 健康检查
func HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// HandleIndex 首页处理
func HandleIndex(w http.ResponseWriter, r *http.Request) {
	// 只处理根路径
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, "static/index.html")
}

// ProcessReview 处理 PR 审查的完整流程
func ProcessReview(repo string, prNum int, providerType string, token string) {
	// === A. 创建 VCS Provider ===
	var vcsClient lib.VCSProvider
	switch providerType {
	case lib.ProviderTypeGitHub:
		vcsClient = lib.NewGitHubClient(token)
	case lib.ProviderTypeGitLab:
		baseURL := appConfig.GetGitlabBaseURL()
		vcsClient = lib.NewGitLabClient(token, baseURL)
	default:
		log.Printf("❌ [%s#%d] Unsupported provider: %s", repo, prNum, providerType)
		return
	}

	log.Printf("🔧 [%s#%d] Using VCS provider: %s", repo, prNum, vcsClient.GetProviderType())

	// === B. 获取 Diff ===
	log.Printf("🔍 [%s#%d] Fetching diff...", repo, prNum)

	diffText, err := vcsClient.GetDiff(repo, prNum)
	if err != nil {
		log.Printf("❌ [%s#%d] %v", repo, prNum, err)
		return
	}

	// === C. 调用 AI 审查 ===
	log.Printf("🤖 [%s#%d] Sending to AI for review...", repo, prNum)

	apiURL, apiKey, model, systemPrompt, userTemplate := appConfig.GetAIConfig()
	aiClient := lib.NewAIClient(apiURL, apiKey, model, systemPrompt, userTemplate)
	reviewContent, err := aiClient.ReviewCode(diffText)
	if err != nil {
		log.Printf("❌ [%s#%d] %v", repo, prNum, err)
		return
	}

	// === D. 发布评论 ===
	inlineMode := appConfig.GetInlineIssueComment()
	log.Printf("📝 [%s#%d] Posting review comment... (inline: %v)", repo, prNum, inlineMode)

	comment := fmt.Sprintf("🤖 **AI Code Review**\n\n%s", reviewContent)
	if inlineMode {
		headSHA, err := vcsClient.GetHeadSHA(repo, prNum)
		if err != nil {
			log.Printf("❌ [%s#%d] %v", repo, prNum, err)
			return
		}

		diffPositionMap := buildDiffPositionMap(diffText)
		issues := parseIssuesFromReview(reviewContent)
		unmatched := postInlineIssues(repo, prNum, headSHA, vcsClient, diffPositionMap, issues)

		summary := buildSummaryComment(reviewContent)
		if strings.TrimSpace(summary) == "" {
			summary = "（未能解析评分/修改点/总结）"
		}
		unmatchedSummary := buildUnmatchedIssuesTable(unmatched)
		if unmatchedSummary != "" {
			summary = strings.TrimSpace(summary + "\n\n" + unmatchedSummary)
		}
		comment = fmt.Sprintf("🤖 **AI Code Review**\n\n%s", summary)
	}
	if err := vcsClient.PostComment(repo, prNum, comment); err != nil {
		log.Printf("❌ [%s#%d] %v", repo, prNum, err)
		return
	}

	log.Printf("✅ [%s#%d] Review completed successfully!", repo, prNum)
}

type reviewIssue struct {
	File       string
	Side       string
	OldLine    int
	NewLine    int
	Code       string
	Severity   string
	Category   string
	Problem    string
	Suggestion string
}

func buildSummaryComment(content string) string {
	sections := []string{
		extractMarkdownSection(content, "评分"),
		extractMarkdownSection(content, "修改点"),
		extractMarkdownSection(content, "总结"),
	}

	var parts []string
	for _, section := range sections {
		if strings.TrimSpace(section) != "" {
			parts = append(parts, strings.TrimSpace(section))
		}
	}

	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func extractMarkdownSection(content, title string) string {
	lines := strings.Split(content, "\n")
	var buf []string
	found := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			heading := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			heading = strings.TrimSuffix(heading, ":")
			if found {
				break
			}
			if strings.HasPrefix(heading, title) {
				found = true
				buf = append(buf, line)
				continue
			}
		}

		if found {
			buf = append(buf, line)
		}
	}

	return strings.TrimSpace(strings.Join(buf, "\n"))
}

func parseIssuesFromReview(content string) []reviewIssue {
	lines := strings.Split(content, "\n")
	issues := make([]reviewIssue, 0)

	for _, line := range lines {
		normalized := strings.ReplaceAll(line, "｜", "|")
		if !strings.Contains(normalized, "|") {
			continue
		}

		cells := splitTableRow(normalized)
		if len(cells) < 5 {
			continue
		}

		if strings.Contains(cells[0], "文件名") || strings.Contains(cells[0], "---") {
			continue
		}

		if len(cells) >= 6 {
			file := strings.Trim(cells[0], "` ")
			oldLine := parseLineNumber(cells[1])
			newLine := parseLineNumber(cells[2])
			if file == "" || (oldLine == 0 && newLine == 0) {
				continue
			}
			codeSnippet := ""
			severityIndex := 3
			if len(cells) >= 8 {
				codeSnippet = strings.Trim(cells[3], "` ")
				severityIndex = 4
			}
			issues = append(issues, reviewIssue{
				File:       file,
				OldLine:    oldLine,
				NewLine:    newLine,
				Code:       codeSnippet,
				Severity:   strings.TrimSpace(cells[severityIndex]),
				Category:   strings.TrimSpace(cells[severityIndex+1]),
				Problem:    strings.TrimSpace(cells[severityIndex+2]),
				Suggestion: "",
			})
			if len(cells) > severityIndex+3 {
				issues[len(issues)-1].Suggestion = strings.TrimSpace(cells[severityIndex+3])
			}
			continue
		}

		file, lineNum, side, ok := parseFileLine(cells[0])
		if !ok {
			continue
		}

		issues = append(issues, reviewIssue{
			File:       file,
			Side:       side,
			OldLine:    0,
			NewLine:    lineNum,
			Code:       "",
			Severity:   strings.TrimSpace(cells[1]),
			Category:   strings.TrimSpace(cells[2]),
			Problem:    strings.TrimSpace(cells[3]),
			Suggestion: strings.TrimSpace(cells[4]),
		})
	}

	return issues
}

func splitTableRow(line string) []string {
	raw := strings.Split(line, "|")
	cells := make([]string, 0, len(raw))
	for _, cell := range raw {
		trimmed := strings.TrimSpace(cell)
		if trimmed == "" {
			continue
		}
		cells = append(cells, trimmed)
	}
	return cells
}

func parseFileLine(input string) (string, int, string, bool) {
	trimmed := strings.TrimSpace(input)
	side := ""
	if strings.HasPrefix(trimmed, "+") {
		side = "RIGHT"
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "+"))
	} else if strings.HasPrefix(trimmed, "-") {
		side = "LEFT"
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
	}

	parts := strings.SplitN(trimmed, ":", 2)
	if len(parts) != 2 {
		return "", 0, "", false
	}

	file := strings.Trim(parts[0], "` ")
	lineStr := strings.Trim(parts[1], "` ")
	lineNum, err := strconv.Atoi(lineStr)
	if err != nil || lineNum <= 0 {
		return "", 0, "", false
	}

	return file, lineNum, side, true
}

func parseLineNumber(input string) int {
	trimmed := strings.TrimSpace(strings.Trim(input, "` "))
	if trimmed == "" || trimmed == "-" {
		return 0
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

type diffLineInfo struct {
	Position int
	Content  string
	Type     string // "+", "-", or " " (context)
}

type diffPositionLines struct {
	Old map[int]diffLineInfo
	New map[int]diffLineInfo
}

func buildDiffPositionMap(diffText string) map[string]diffPositionLines {
	lineMap := make(map[string]diffPositionLines)

	var currentFile string
	var oldLine int
	var newLine int
	var inPatch bool
	var inHunk bool
	var position int

	lines := strings.Split(diffText, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			currentFile = ""
			oldLine = 0
			newLine = 0
			inPatch = false
			inHunk = false
			position = 0
			continue
		}

		if strings.HasPrefix(line, "+++ ") && !strings.HasPrefix(line, "+++ b/") {
			currentFile = ""
			oldLine = 0
			newLine = 0
			inPatch = false
			inHunk = false
			position = 0
			continue
		}

		if strings.HasPrefix(line, "+++ b/") {
			currentFile = strings.TrimSpace(strings.TrimPrefix(line, "+++ b/"))
			oldLine = 0
			newLine = 0
			inPatch = true
			inHunk = false
			position = 0
			if currentFile != "" {
				if _, ok := lineMap[currentFile]; !ok {
					lineMap[currentFile] = diffPositionLines{
						Old: make(map[int]diffLineInfo),
						New: make(map[int]diffLineInfo),
					}
				}
			}
			continue
		}

		if !inPatch || currentFile == "" {
			continue
		}

		if strings.HasPrefix(line, "@@") {
			oldLine = parseOldHunkStart(line)
			newLine = parseNewHunkStart(line)
			inHunk = true
			continue
		}

		if !inHunk || (oldLine == 0 && newLine == 0) {
			continue
		}

		if line == "\\ No newline at end of file" {
			continue
		}

		// 跳过空行（通常是 split 的副作用）
		if line == "" {
			continue
		}

		// 只处理有效的 diff 行
		if !strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, " ") {
			// 跳过非标准行（例如：某些 diff 工具的注释）
			log.Printf("⚠️ Skipping non-standard diff line in %s: %q", currentFile, line)
			continue
		}

		position++
		if strings.HasPrefix(line, "+") {
			lineMap[currentFile].New[newLine] = diffLineInfo{
				Position: position,
				Content:  strings.TrimPrefix(line, "+"),
				Type:     "+",
			}
			newLine++
			continue
		}
		if strings.HasPrefix(line, "-") {
			lineMap[currentFile].Old[oldLine] = diffLineInfo{
				Position: position,
				Content:  strings.TrimPrefix(line, "-"),
				Type:     "-",
			}
			oldLine++
			continue
		}
		if strings.HasPrefix(line, " ") {
			trimmed := strings.TrimPrefix(line, " ")
			lineMap[currentFile].Old[oldLine] = diffLineInfo{
				Position: position,
				Content:  trimmed,
				Type:     " ",
			}
			lineMap[currentFile].New[newLine] = diffLineInfo{
				Position: position,
				Content:  trimmed,
				Type:     " ",
			}
			oldLine++
			newLine++
		}
	}

	return lineMap
}

func parseNewHunkStart(hunkLine string) int {
	parts := strings.Split(hunkLine, " ")
	if len(parts) < 3 {
		return 0
	}

	newPart := strings.TrimPrefix(parts[2], "+")
	newPart = strings.SplitN(newPart, ",", 2)[0]
	newLine, err := strconv.Atoi(newPart)
	if err != nil {
		return 0
	}

	return newLine
}

func parseOldHunkStart(hunkLine string) int {
	parts := strings.Split(hunkLine, " ")
	if len(parts) < 2 {
		return 0
	}

	oldPart := strings.TrimPrefix(parts[1], "-")
	oldPart = strings.SplitN(oldPart, ",", 2)[0]
	oldLine, err := strconv.Atoi(oldPart)
	if err != nil {
		return 0
	}

	return oldLine
}

func postInlineIssues(repo string, prNum int, headSHA string, vcsClient lib.VCSProvider, positionMap map[string]diffPositionLines, issues []reviewIssue) []reviewIssue {
	// 调试: 输出 position map 的统计信息
	log.Printf("📊 [%s#%d] Diff position map statistics:", repo, prNum)
	for file, lines := range positionMap {
		log.Printf("  File: %s - Old lines: %d, New lines: %d", file, len(lines.Old), len(lines.New))
		// 输出前10个新行的映射，用于调试
		log.Printf("  New line mappings (first 10):")
		for i := 1; i <= 10 && i <= len(lines.New); i++ {
			if info, ok := lines.New[i]; ok {
				log.Printf("    NewLine[%d] -> Position=%d, Type=%q, Content=%q", i, info.Position, info.Type, truncateString(info.Content, 50))
			}
		}
	}

	unmatched := make([]reviewIssue, 0)
	for _, issue := range issues {
		fileLines, ok := positionMap[issue.File]
		if !ok {
			log.Printf("⚠️ [%s#%d] File not in diff for inline comment: %s", repo, prNum, issue.File)
			unmatched = append(unmatched, issue)
			continue
		}

		log.Printf("🔍 [%s#%d] Attempting to match issue: File=%s, OldLine=%d, NewLine=%d, Side=%s, Code=%q",
			repo, prNum, issue.File, issue.OldLine, issue.NewLine, issue.Side, issue.Code)

		lineInfo, ok := resolveLineInfo(fileLines, issue)
		if !ok {
			log.Printf("⚠️ [%s#%d] Line not in diff for inline comment: %s (old:%d new:%d)", repo, prNum, issue.File, issue.OldLine, issue.NewLine)
			unmatched = append(unmatched, issue)
			continue
		}

		// 根据配置决定是否跳过上下文行（未修改的行）
		commentOnlyChanges := appConfig.GetCommentOnlyChanges()
		if lineInfo.Type == " " {
			if commentOnlyChanges {
				// 用户配置了只评论修改的行，完全忽略上下文行的问题
				// 不添加到 unmatched，也不会出现在大评论中
				log.Printf("⚠️ [%s#%d] Ignoring context line issue (comment_only_changes enabled): %s line %d", repo, prNum, issue.File, issue.NewLine)
				continue
			} else if vcsClient.GetProviderType() == lib.ProviderTypeGitLab {
				// GitLab API 不支持在上下文行上评论
				// 但用户没有开启 comment_only_changes，所以添加到大评论的未定位问题表格中
				log.Printf("⚠️ [%s#%d] Skipping context line (GitLab limitation): %s line %d", repo, prNum, issue.File, issue.NewLine)
				unmatched = append(unmatched, issue)
				continue
			}
			// GitHub 且 comment_only_changes=false，可以正常评论上下文行
		}

		body := buildInlineBody(issue)

		// 从 lineInfo 中提取实际的行号（通过 position 反查）
		var actualOldLine, actualNewLine int
		if lineInfo.Type == "+" {
			// 新增行：只有 new_line
			actualNewLine = findLineNumberByPosition(fileLines.New, lineInfo.Position)
			actualOldLine = 0
			log.Printf("📍 Resolved to: NewLine=%d (added line)", actualNewLine)
		} else if lineInfo.Type == "-" {
			// 删除行：只有 old_line
			actualOldLine = findLineNumberByPosition(fileLines.Old, lineInfo.Position)
			actualNewLine = 0
			log.Printf("📍 Resolved to: OldLine=%d (deleted line)", actualOldLine)
		} else {
			// 上下文行或修改行：同时有 old_line 和 new_line
			actualOldLine = findLineNumberByPosition(fileLines.Old, lineInfo.Position)
			actualNewLine = findLineNumberByPosition(fileLines.New, lineInfo.Position)
			log.Printf("📍 Resolved to: OldLine=%d, NewLine=%d (context/modified line)", actualOldLine, actualNewLine)
		}

		// 根据 provider 类型选择合适的参数
		var lineParam int
		if vcsClient.GetProviderType() == lib.ProviderTypeGitLab {
			// GitLab 会使用 actualOldLine 和 actualNewLine 参数，lineParam 被忽略
			lineParam = 0
		} else {
			// GitHub 使用 diff position
			lineParam = lineInfo.Position
		}

		// 调用 PostInlineComment，传递实际的行号信息
		if err := vcsClient.PostInlineComment(repo, prNum, headSHA, issue.File, lineParam, body, actualOldLine, actualNewLine); err != nil {
			log.Printf("❌ [%s#%d] %v", repo, prNum, err)
			unmatched = append(unmatched, issue)
		}
	}
	return unmatched
}

func resolveLineInfo(fileLines diffPositionLines, issue reviewIssue) (diffLineInfo, bool) {
	// 清理代码片段：去掉 AI 可能添加的 diff 前缀（+ 或 -）
	cleanCode := issue.Code
	if len(cleanCode) > 0 && (cleanCode[0] == '+' || cleanCode[0] == '-') {
		cleanCode = strings.TrimSpace(cleanCode[1:])
	}

	if cleanCode != "" && isInvalidSnippet(cleanCode) {
		log.Printf("⚠️ Invalid snippet: %q", cleanCode)
		return diffLineInfo{}, false
	}

	// 策略 1: 优先使用代码片段精确匹配（最可靠，只要 AI 提供了代码）
	if cleanCode != "" {
		// 先在新行中搜索
		if info, ok := findBySnippet(fileLines.New, cleanCode); ok {
			actualLine := findLineNumberByPosition(fileLines.New, info.Position)
			if actualLine != issue.NewLine && issue.NewLine > 0 {
				log.Printf("⚠️ 行号修正: AI报告NewLine=%d, 实际NewLine=%d (代码片段定位)", issue.NewLine, actualLine)
			} else {
				log.Printf("✅ Matched by snippet in New lines, NewLine=%d, Position=%d", actualLine, info.Position)
			}
			return info, true
		}
		// 再在旧行中搜索
		if info, ok := findBySnippet(fileLines.Old, cleanCode); ok {
			actualLine := findLineNumberByPosition(fileLines.Old, info.Position)
			if actualLine != issue.OldLine && issue.OldLine > 0 {
				log.Printf("⚠️ 行号修正: AI报告OldLine=%d, 实际OldLine=%d (代码片段定位)", issue.OldLine, actualLine)
			} else {
				log.Printf("✅ Matched by snippet in Old lines, OldLine=%d, Position=%d", actualLine, info.Position)
			}
			return info, true
		}
		log.Printf("❌ Code snippet not found: %q", cleanCode)
		// 注意：代码片段未找到时，不fallback到行号匹配，直接返回失败
		// 因为AI提供了代码但找不到，说明可能是错误的问题
		return diffLineInfo{}, false
	}

	// 策略 2: 如果没有代码片段，尝试使用行号（但要谨慎）
	log.Printf("⚠️ No code snippet provided, trying line number matching (less reliable)")

	// 优先尝试 Side 字段匹配
	if issue.Side == "RIGHT" && issue.NewLine > 0 {
		if info, ok := fileLines.New[issue.NewLine]; ok {
			log.Printf("✅ Matched by Side=RIGHT, NewLine=%d, Position=%d", issue.NewLine, info.Position)
			return info, true
		}
		log.Printf("⚠️ Side=RIGHT, NewLine=%d not in diff", issue.NewLine)
	}

	if issue.Side == "LEFT" && issue.OldLine > 0 {
		if info, ok := fileLines.Old[issue.OldLine]; ok {
			log.Printf("✅ Matched by Side=LEFT, OldLine=%d, Position=%d", issue.OldLine, info.Position)
			return info, true
		}
		log.Printf("⚠️ Side=LEFT, OldLine=%d not in diff", issue.OldLine)
	}

	// 直接行号匹配
	if issue.NewLine > 0 {
		if info, ok := fileLines.New[issue.NewLine]; ok {
			log.Printf("✅ Matched by NewLine=%d, Position=%d", issue.NewLine, info.Position)
			return info, true
		}
	}

	if issue.OldLine > 0 {
		if info, ok := fileLines.Old[issue.OldLine]; ok {
			log.Printf("✅ Matched by OldLine=%d, Position=%d", issue.OldLine, info.Position)
			return info, true
		}
	}

	log.Printf("❌ Failed to resolve: OldLine=%d, NewLine=%d, Code=%q", issue.OldLine, issue.NewLine, cleanCode)
	return diffLineInfo{}, false
}

// 辅助函数：通过 position 查找行号
func findLineNumberByPosition(lines map[int]diffLineInfo, position int) int {
	for lineNum, info := range lines {
		if info.Position == position {
			return lineNum
		}
	}
	return 0
}

func lineMatches(snippet, content string) bool {
	normalizedSnippet := normalizeSnippet(snippet)
	if normalizedSnippet == "" {
		return true
	}
	normalizedContent := normalizeSnippet(content)
	return strings.Contains(normalizedContent, normalizedSnippet)
}

func normalizeSnippet(input string) string {
	trimmed := strings.TrimSpace(strings.Trim(input, "`"))
	if trimmed == "" {
		return ""
	}
	return strings.Join(strings.Fields(trimmed), " ")
}

func isInvalidSnippet(snippet string) bool {
	normalized := normalizeSnippet(snippet)
	if normalized == "" {
		return true
	}
	if strings.Contains(normalized, "...") || strings.Contains(normalized, "…") {
		return true
	}
	return false
}

func findBySnippet(lines map[int]diffLineInfo, snippet string) (diffLineInfo, bool) {
	normalized := normalizeSnippet(snippet)
	if normalized == "" {
		return diffLineInfo{}, false
	}
	var match diffLineInfo
	matchCount := 0
	for _, info := range lines {
		if strings.Contains(normalizeSnippet(info.Content), normalized) {
			match = info
			matchCount++
			if matchCount > 1 {
				return diffLineInfo{}, false
			}
		}
	}
	if matchCount == 1 {
		return match, true
	}
	return diffLineInfo{}, false
}

func buildInlineBody(issue reviewIssue) string {
	var builder strings.Builder

	// 严重程度
	builder.WriteString(fmt.Sprintf("**严重程度**: %s\n\n", issue.Severity))

	// 类别
	builder.WriteString(fmt.Sprintf("**类别**: %s\n\n", issue.Category))

	// 问题描述
	builder.WriteString(fmt.Sprintf("**问题**: %s\n", issue.Problem))

	// 建议修复（如果有）
	if issue.Suggestion != "" {
		builder.WriteString("\n**建议**: ")

		// 检查建议中是否包含代码片段（简单判断：包含代码相关关键词）
		suggestion := issue.Suggestion
		if containsCodeSuggestion(suggestion) {
			// 尝试提取并格式化代码建议
			formatted := formatCodeSuggestion(suggestion)
			builder.WriteString(formatted)
		} else {
			builder.WriteString(suggestion)
		}
	}

	return builder.String()
}

// containsCodeSuggestion 检查建议中是否包含代码修复
func containsCodeSuggestion(text string) bool {
	// 如果建议中包含这些关键词，可能包含代码建议
	keywords := []string{"改为", "修改为", "替换为", "应该是", "建议使用"}
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

// formatCodeSuggestion 格式化代码建议，如果可能的话提取为 diff 格式
func formatCodeSuggestion(text string) string {
	// 简单处理：如果文本中包含代码片段，尝试格式化为 diff
	// 例如："将 app.listen(8981) 改为 app.listen(8982)"

	// 如果已经包含代码块标记，直接返回
	if strings.Contains(text, "```") {
		return text
	}

	// 尝试识别 "将 X 改为 Y" 或 "X 改为 Y" 的模式
	patterns := []string{
		"将 ", " 改为 ", "替换为 ", "修改为 ", "应该是 ", "建议使用 ",
	}

	hasPattern := false
	for _, p := range patterns {
		if strings.Contains(text, p) {
			hasPattern = true
			break
		}
	}

	if !hasPattern {
		return text
	}

	// 尝试提取修改建议并格式化为 diff
	var builder strings.Builder
	builder.WriteString(text)
	builder.WriteString("\n\n")

	// 如果文本中有清晰的代码片段（用反引号包裹），提取并显示为 diff
	if extractDiffSuggestion(text, &builder) {
		return builder.String()
	}

	return text
}

// extractDiffSuggestion 尝试从建议中提取代码并格式化为 diff
func extractDiffSuggestion(text string, builder *strings.Builder) bool {
	// 查找反引号包裹的代码片段
	parts := strings.Split(text, "`")
	if len(parts) < 3 {
		return false
	}

	var oldCode, newCode string
	codeCount := 0

	for i := 1; i < len(parts); i += 2 {
		code := strings.TrimSpace(parts[i])
		if code != "" {
			if codeCount == 0 {
				oldCode = code
			} else if codeCount == 1 {
				newCode = code
			}
			codeCount++
		}
	}

	// 如果找到了两段代码（旧代码和新代码），格式化为 diff
	if oldCode != "" && newCode != "" && oldCode != newCode {
		builder.WriteString("```diff\n")
		builder.WriteString(fmt.Sprintf("- %s\n", oldCode))
		builder.WriteString(fmt.Sprintf("+ %s\n", newCode))
		builder.WriteString("```\n")
		return true
	}

	return false
}

func buildUnmatchedIssuesTable(issues []reviewIssue) string {
	if len(issues) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("### 其他问题\n")
	builder.WriteString("| 文件名 | 代码片段 | 严重程度 | 类别 | 问题描述 | 建议修改 |\n")
	builder.WriteString("|---|---|---|---|---|---|\n")
	for _, issue := range issues {
		builder.WriteString(fmt.Sprintf("| %s:%s | %s | %s | %s | %s | %s |\n",
			escapeTable(issue.File),
			formatLineValue(issue.NewLine),
			escapeTable(issue.Code),
			escapeTable(issue.Severity),
			escapeTable(issue.Category),
			escapeTable(issue.Problem),
			escapeTable(issue.Suggestion),
		))
	}
	return strings.TrimSpace(builder.String())
}

func formatLineValue(value int) string {
	if value <= 0 {
		return "-"
	}
	return strconv.Itoa(value)
}

func escapeTable(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "-"
	}
	trimmed = strings.ReplaceAll(trimmed, "\n", " ")
	trimmed = strings.ReplaceAll(trimmed, "|", "\\|")
	return trimmed
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
