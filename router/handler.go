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
	Repo     string `json:"repo"`                // owner/repo
	PRNumber int    `json:"pr_number"`           // PR ID
	Provider string `json:"provider,omitempty"`  // 可选，未指定则使用配置
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
	unmatched := make([]reviewIssue, 0)
	for _, issue := range issues {
		fileLines, ok := positionMap[issue.File]
		if !ok {
			log.Printf("⚠️ [%s#%d] File not in diff for inline comment: %s", repo, prNum, issue.File)
			unmatched = append(unmatched, issue)
			continue
		}

		lineInfo, ok := resolveLineInfo(fileLines, issue)
		if !ok {
			log.Printf("⚠️ [%s#%d] Line not in diff for inline comment: %s (old:%d new:%d)", repo, prNum, issue.File, issue.OldLine, issue.NewLine)
			unmatched = append(unmatched, issue)
			continue
		}

		// 根据配置决定是否跳过上下文行（未修改的行）
		// GitLab 始终不允许在上下文行上发布评论
		// 如果开启了 comment_only_changes，GitHub 也跳过上下文行
		commentOnlyChanges := appConfig.GetCommentOnlyChanges()
		if lineInfo.Type == " " {
			if vcsClient.GetProviderType() == lib.ProviderTypeGitLab {
				// GitLab API 不支持在上下文行上评论
				log.Printf("⚠️ [%s#%d] Skipping context line (GitLab limitation): %s line %d", repo, prNum, issue.File, issue.NewLine)
				unmatched = append(unmatched, issue)
				continue
			} else if commentOnlyChanges {
				// GitHub 可以评论上下文行，但用户配置了只评论修改的行
				log.Printf("⚠️ [%s#%d] Skipping context line (comment_only_changes enabled): %s line %d", repo, prNum, issue.File, issue.NewLine)
				unmatched = append(unmatched, issue)
				continue
			}
		}

		body := buildInlineBody(issue)

		// 根据 provider 类型选择合适的参数
		// GitHub 使用 diff position，GitLab 使用实际行号
		var lineParam int
		if vcsClient.GetProviderType() == lib.ProviderTypeGitLab {
			// GitLab 需要实际的文件行号
			// 根据 issue 的 Side 或者有无 newLine/oldLine 来判断

			// 优先使用 Side 字段判断
			if issue.Side == "LEFT" && issue.OldLine > 0 {
				// 明确标记为左侧（删除的行）
				lineParam = -issue.OldLine
			} else if issue.Side == "RIGHT" && issue.NewLine > 0 {
				// 明确标记为右侧（新增的行）
				lineParam = issue.NewLine
			} else if issue.NewLine > 0 {
				// 没有 Side 标记，优先使用 NewLine
				lineParam = issue.NewLine
			} else if issue.OldLine > 0 {
				// 只有 OldLine，表示删除的行
				lineParam = -issue.OldLine
			} else {
				log.Printf("⚠️ [%s#%d] No valid line number for GitLab inline comment: %s", repo, prNum, issue.File)
				unmatched = append(unmatched, issue)
				continue
			}
		} else {
			// GitHub 使用 diff position
			lineParam = lineInfo.Position
		}

		if err := vcsClient.PostInlineComment(repo, prNum, headSHA, issue.File, lineParam, body); err != nil {
			log.Printf("❌ [%s#%d] %v", repo, prNum, err)
			unmatched = append(unmatched, issue)
		}
	}
	return unmatched
}

func resolveLineInfo(fileLines diffPositionLines, issue reviewIssue) (diffLineInfo, bool) {
	if issue.Code != "" && isInvalidSnippet(issue.Code) {
		return diffLineInfo{}, false
	}

	if issue.Side == "RIGHT" && issue.NewLine > 0 {
		if info, ok := fileLines.New[issue.NewLine]; ok && lineMatches(issue.Code, info.Content) {
			return info, true
		}
	}
	if issue.Side == "LEFT" && issue.OldLine > 0 {
		if info, ok := fileLines.Old[issue.OldLine]; ok && lineMatches(issue.Code, info.Content) {
			return info, true
		}
	}

	if issue.NewLine > 0 {
		if info, ok := fileLines.New[issue.NewLine]; ok && lineMatches(issue.Code, info.Content) {
			return info, true
		}
	}
	if issue.OldLine > 0 {
		if info, ok := fileLines.Old[issue.OldLine]; ok && lineMatches(issue.Code, info.Content) {
			return info, true
		}
	}

	if issue.Code != "" {
		if info, ok := findBySnippet(fileLines.New, issue.Code); ok {
			return info, true
		}
		if info, ok := findBySnippet(fileLines.Old, issue.Code); ok {
			return info, true
		}
		return diffLineInfo{}, false
	}

	if issue.NewLine > 0 {
		if info, ok := fileLines.New[issue.NewLine]; ok {
			return info, true
		}
	}
	if issue.OldLine > 0 {
		if info, ok := fileLines.Old[issue.OldLine]; ok {
			return info, true
		}
	}

	return diffLineInfo{}, false
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
	builder.WriteString("### 未定位到行的问题\n")
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
