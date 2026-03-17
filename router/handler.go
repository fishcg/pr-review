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
	PRNumber int    `json:"pr_number"`          // 兼容旧字段
	Number   int    `json:"number"`             // 新字段：PR/MR 编号
	Provider string `json:"provider,omitempty"` // 可选，未指定则使用配置
	Engine   string `json:"engine,omitempty"`   // 可选：api/claude_cli/codex
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
	GetReviewMode() string
	// Claude CLI 配置
	GetClaudeCLIBinaryPath() string
	GetClaudeCLIAllowedTools() []string
	GetClaudeCLITimeout() int
	GetClaudeCLIMaxOutputLength() int
	GetClaudeCLIAPIKey() string
	GetClaudeCLIAPIURL() string
	GetClaudeCLIModel() string
	GetClaudeCLIIncludeOthersComments() bool
	GetClaudeCLIEnableOutputLog() bool
	// Codex CLI 配置
	GetCodexCLIBinaryPath() string
	GetCodexCLIAllowedTools() []string
	GetCodexCLITimeout() int
	GetCodexCLIMaxOutputLength() int
	GetCodexCLIAPIKey() string
	GetCodexCLIAPIURL() string
	GetCodexCLIModel() string
	GetCodexCLIIncludeOthersComments() bool
	GetCodexCLIEnableOutputLog() bool
	// 仓库克隆配置
	GetRepoCloneTempDir() string
	GetRepoCloneTimeout() int
	GetRepoCloneShallowClone() bool
	GetRepoCloneShallowDepth() int
	GetRepoCloneCleanupAfterReview() bool
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

	// 2.1 兼容 pr_number 与 number
	prNumber := req.PRNumber
	if req.Number > 0 {
		if req.PRNumber > 0 && req.PRNumber != req.Number {
			http.Error(w, "pr_number and number mismatch", http.StatusBadRequest)
			return
		}
		prNumber = req.Number
	}
	if prNumber <= 0 {
		http.Error(w, "Invalid PR/MR number", http.StatusBadRequest)
		return
	}

	// 2.2 可选覆盖 review engine
	reviewEngine := strings.TrimSpace(req.Engine)
	if reviewEngine != "" && reviewEngine != "api" && reviewEngine != "claude_cli" && reviewEngine != "codex" {
		http.Error(w, "Invalid engine, must be one of: api, claude_cli, codex", http.StatusBadRequest)
		return
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

	log.Printf("📥 Received review request for %s #%d (provider: %s, engine: %s)", req.Repo, prNumber, providerType, chooseEngineLabel(reviewEngine))

	// 4. 异步处理 Review (防止 CI HTTP 请求超时)
	// 如果你希望 CI 等待结果，可以去掉 go 关键字
	go ProcessReview(req.Repo, prNumber, providerType, token, reviewEngine)

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(fmt.Sprintf("Review started for %s #%d", req.Repo, prNumber)))
}

// HandleHealth 健康检查
func HandleHealth(w http.ResponseWriter, r *http.Request) {
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/json") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":        "ok",
			"review_mode":   appConfig.GetReviewMode(),
			"review_modes":  []string{"api", "claude_cli", "codex"},
			"vcs_provider":  appConfig.GetVCSProvider(),
			"inline_review": appConfig.GetInlineIssueComment(),
		})
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
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
func ProcessReview(repo string, prNum int, providerType string, token string, reviewModeOverride string) {
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

	// === B. 根据 ReviewMode 选择处理策略 ===
	reviewMode := appConfig.GetReviewMode()
	if reviewModeOverride != "" {
		reviewMode = reviewModeOverride
	}
	var reviewContent string
	var diffText string
	var err error

	if reviewMode == "claude_cli" {
		// Claude CLI 模式
		reviewContent, diffText, err = processWithClaudeCLI(vcsClient, repo, prNum, token, providerType)
		if err != nil {
			log.Printf("❌ [%s#%d] Claude CLI mode failed: %v", repo, prNum, err)
			log.Printf("⚠️ [%s#%d] Attempting fallback to API mode...", repo, prNum)

			// 降级到 API 模式
			reviewContent, diffText, err = processWithAPI(vcsClient, repo, prNum)
			if err != nil {
				log.Printf("❌ [%s#%d] API fallback also failed: %v", repo, prNum, err)
				log.Printf("💥 [%s#%d] Review completely failed - both Claude CLI and API modes unsuccessful", repo, prNum)
				return
			}
		}
	} else if reviewMode == "codex" {
		// Codex CLI 模式
		reviewContent, diffText, err = processWithCodexCLI(vcsClient, repo, prNum, token, providerType)
		if err != nil {
			log.Printf("❌ [%s#%d] Codex mode failed: %v", repo, prNum, err)
			log.Printf("⚠️ [%s#%d] Attempting fallback to API mode...", repo, prNum)

			// 降级到 API 模式
			reviewContent, diffText, err = processWithAPI(vcsClient, repo, prNum)
			if err != nil {
				log.Printf("❌ [%s#%d] API fallback also failed: %v", repo, prNum, err)
				log.Printf("💥 [%s#%d] Review completely failed - both Codex and API modes unsuccessful", repo, prNum)
				return
			}
		}
	} else {
		// API 模式
		log.Printf("🔧 [%s#%d] Using API mode (diff-based review)", repo, prNum)
		reviewContent, diffText, err = processWithAPI(vcsClient, repo, prNum)
		if err != nil {
			log.Printf("❌ [%s#%d] API review failed: %v", repo, prNum, err)
			return
		}
	}

	// === D. 发布评论 ===
	inlineMode := appConfig.GetInlineIssueComment()

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

	// 删除当前 bot 账号的旧评论（避免重复）
	deleteOldBotComments(vcsClient, repo, prNum)

	// 发布总评论（每次都发布）
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

			// 检测表格格式：是否包含 Side 列
			// 格式1（9列）: 文件名 | 旧行号 | 新行号 | Side | 代码片段 | 严重程度 | 类别 | 问题描述 | 建议修改
			// 格式2（8列）: 文件名 | 旧行号 | 新行号 | 代码片段 | 严重程度 | 类别 | 问题描述 | 建议修改
			// 格式3（6列）: 文件名 | 旧行号 | 新行号 | 严重程度 | 类别 | 问题描述
			var side string
			var codeSnippet string
			var severityIndex int

			if len(cells) >= 9 {
				// 9列格式：包含 Side 列
				side = strings.TrimSpace(cells[3])
				codeSnippet = strings.Trim(cells[4], "` ")
				severityIndex = 5
			} else if len(cells) >= 8 {
				// 8列格式：不包含 Side 列，但有代码片段
				codeSnippet = strings.Trim(cells[3], "` ")
				severityIndex = 4
			} else {
				// 6列格式：没有代码片段
				severityIndex = 3
			}

			issues = append(issues, reviewIssue{
				File:       file,
				Side:       side,
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
	// 获取现有的行内评论用于去重
	existingComments, err := vcsClient.GetInlineComments(repo, prNum)
	if err != nil {
		log.Printf("⚠️ [%s#%d] Failed to get existing inline comments: %v", repo, prNum, err)
		existingComments = []lib.Comment{}
	}

	unmatched := make([]reviewIssue, 0)
	posted := 0

	for _, issue := range issues {
		fileLines, ok := positionMap[issue.File]
		if !ok {
			unmatched = append(unmatched, issue)
			continue
		}

		lineInfo, ok := resolveLineInfo(fileLines, issue)
		if !ok {
			unmatched = append(unmatched, issue)
			continue
		}

		// 根据配置决定是否跳过上下文行（未修改的行）
		commentOnlyChanges := appConfig.GetCommentOnlyChanges()
		if lineInfo.Type == " " {
			if commentOnlyChanges {
				continue
			} else if vcsClient.GetProviderType() == lib.ProviderTypeGitLab {
				unmatched = append(unmatched, issue)
				continue
			}
		}

		body := buildInlineBody(issue)

		// 从 lineInfo 中提取实际的行号（通过 position 反查）
		var actualOldLine, actualNewLine int
		if lineInfo.Type == "+" {
			actualNewLine = findLineNumberByPosition(fileLines.New, lineInfo.Position)
			actualOldLine = 0
		} else if lineInfo.Type == "-" {
			actualOldLine = findLineNumberByPosition(fileLines.Old, lineInfo.Position)
			actualNewLine = 0
		} else {
			actualOldLine = findLineNumberByPosition(fileLines.Old, lineInfo.Position)
			actualNewLine = findLineNumberByPosition(fileLines.New, lineInfo.Position)
		}

		// 检查是否已存在相同的评论（去重）
		targetLine := actualNewLine
		if targetLine == 0 {
			targetLine = actualOldLine
		}
		if isDuplicateComment(existingComments, issue.File, targetLine) {
			continue
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
			log.Printf("❌ [%s#%d] Failed to post inline comment: %v", repo, prNum, err)
			unmatched = append(unmatched, issue)
		} else {
			posted++
		}
	}

	log.Printf("✅ [%s#%d] Posted %d inline comments, %d unmatched", repo, prNum, posted, len(unmatched))
	return unmatched
}

func resolveLineInfo(fileLines diffPositionLines, issue reviewIssue) (diffLineInfo, bool) {
	// 清理代码片段：去掉 AI 可能添加的 diff 前缀（+ 或 -）
	cleanCode := issue.Code
	if len(cleanCode) > 0 && (cleanCode[0] == '+' || cleanCode[0] == '-') {
		cleanCode = strings.TrimSpace(cleanCode[1:])
	}

	if cleanCode != "" && isInvalidSnippet(cleanCode) {
		return diffLineInfo{}, false
	}

	// 策略 1: 优先使用代码片段精确匹配
	if cleanCode != "" {
		var searchNew, searchOld bool
		if issue.Side == "LEFT" {
			searchOld = true
			searchNew = true
		} else if issue.Side == "RIGHT" {
			searchNew = true
			searchOld = true
		} else {
			searchNew = true
			searchOld = true
		}

		// 在新行中搜索
		if searchNew && issue.Side != "LEFT" {
			if info, ok := findBySnippet(fileLines.New, cleanCode); ok {
				return info, true
			}
		}

		// 在旧行中搜索
		if searchOld && issue.Side != "RIGHT" {
			if info, ok := findBySnippet(fileLines.Old, cleanCode); ok {
				return info, true
			}
		}

		// 如果 Side 限制了搜索范围但没找到，尝试在另一侧搜索
		if issue.Side == "LEFT" && searchNew {
			if info, ok := findBySnippet(fileLines.New, cleanCode); ok {
				return info, true
			}
		} else if issue.Side == "RIGHT" && searchOld {
			if info, ok := findBySnippet(fileLines.Old, cleanCode); ok {
				return info, true
			}
		}

		return diffLineInfo{}, false
	}

	// 策略 2: 如果没有代码片段，尝试使用行号
	if issue.Side == "RIGHT" && issue.NewLine > 0 {
		if info, ok := fileLines.New[issue.NewLine]; ok {
			return info, true
		}
	}

	if issue.Side == "LEFT" && issue.OldLine > 0 {
		if info, ok := fileLines.Old[issue.OldLine]; ok {
			return info, true
		}
	}

	// 直接行号匹配
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
	builder.WriteString("|  代码片段 | 严重程度 | 类别 | 问题描述 | 建议修改 | 文件名 |\n")
	builder.WriteString("|---|---|---|---|---|---|\n")
	for _, issue := range issues {
		builder.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |  %s:%s |\n",
			escapeTable(issue.Code),
			escapeTable(issue.Severity),
			escapeTable(issue.Category),
			escapeTable(issue.Problem),
			escapeTable(issue.Suggestion),
			escapeTable(issue.File),
			formatLineValue(issue.NewLine),
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

// isDuplicateComment 检查该行是否已有评论（用于去重）
func isDuplicateComment(existingComments []lib.Comment, filePath string, line int) bool {
	for _, comment := range existingComments {
		if comment.Path == filePath && comment.Line == line {
			return true
		}
	}
	return false
}

// processWithAPI 使用 API 模式处理审查
func processWithAPI(vcsClient lib.VCSProvider, repo string, prNum int) (reviewContent string, diffText string, err error) {
	// 1. 获取 PR 详细信息
	prInfo, err := vcsClient.GetPRInfo(repo, prNum)
	if err != nil {
		prInfo = &lib.PRInfo{
			Title:  fmt.Sprintf("PR #%d", prNum),
			Author: "unknown",
		}
	}

	// 2. 获取 Diff
	diffText, err = vcsClient.GetDiff(repo, prNum)
	if err != nil {
		log.Printf("❌ [%s#%d] Failed to get diff: %v", repo, prNum, err)
		return "", "", fmt.Errorf("failed to get diff: %w", err)
	}

	// 3. 增强 diff（添加 PR 上下文信息）
	enhancer := lib.NewDiffEnhancer(lib.PRContextInfo{
		Title:        prInfo.Title,
		Description:  prInfo.Description,
		Author:       prInfo.Author,
		SourceBranch: prInfo.SourceBranch,
		TargetBranch: prInfo.TargetBranch,
		Labels:       prInfo.Labels,
		IsDraft:      prInfo.IsDraft,
		CreatedAt:    prInfo.CreatedAt,
		UpdatedAt:    prInfo.UpdatedAt,
	}, diffText)
	enhancedDiff := enhancer.EnhanceDiff(diffText)

	// 4. 调用 AI 审查（使用增强后的 diff）
	log.Printf("🤖 [%s#%d] Starting AI review...", repo, prNum)
	apiURL, apiKey, model, systemPrompt, userTemplate := appConfig.GetAIConfig()
	aiClient := lib.NewAIClient(apiURL, apiKey, model, systemPrompt, userTemplate)
	reviewContent, err = aiClient.ReviewCode(enhancedDiff)
	if err != nil {
		log.Printf("❌ [%s#%d] AI API call failed: %v", repo, prNum, err)
		return "", "", fmt.Errorf("AI review failed: %w", err)
	}

	log.Printf("✅ [%s#%d] AI review completed", repo, prNum)
	return reviewContent, diffText, nil
}

// processWithClaudeCLI 使用 Claude CLI 模式处理审查
func processWithClaudeCLI(vcsClient lib.VCSProvider, repo string, prNum int, token, providerType string) (reviewContent string, diffText string, err error) {
	// 获取 PR 详细信息
	prInfo, err := vcsClient.GetPRInfo(repo, prNum)
	if err != nil {
		prInfo = &lib.PRInfo{
			Title:  fmt.Sprintf("PR #%d", prNum),
			Author: "unknown",
		}
	}

	// 获取分支信息
	branchInfo, err := vcsClient.GetBranchInfo(repo, prNum)
	if err != nil {
		log.Printf("❌ [%s#%d] Failed to get branch info: %v", repo, prNum, err)
		return "", "", fmt.Errorf("failed to get branch info: %w", err)
	}

	// 获取克隆 URL
	cloneURL, err := vcsClient.GetCloneURL(repo)
	if err != nil {
		log.Printf("❌ [%s#%d] Failed to get clone URL: %v", repo, prNum, err)
		return "", "", fmt.Errorf("failed to get clone URL: %w", err)
	}

	// 构建带认证的克隆 URL
	authenticatedURL, err := lib.BuildCloneURL(cloneURL, token, providerType)
	if err != nil {
		log.Printf("❌ [%s#%d] Failed to build clone URL: %v", repo, prNum, err)
		return "", "", fmt.Errorf("failed to build clone URL: %w", err)
	}

	// 克隆仓库
	repoManager := lib.NewRepoManager(
		appConfig.GetRepoCloneTempDir(),
		appConfig.GetRepoCloneTimeout(),
		appConfig.GetRepoCloneShallowClone(),
		appConfig.GetRepoCloneShallowDepth(),
	)

	workDir, err := repoManager.CloneAndCheckout(authenticatedURL, *branchInfo)
	if err != nil {
		log.Printf("❌ [%s#%d] Clone failed: %v", repo, prNum, err)
		return "", "", fmt.Errorf("failed to clone repository: %w", err)
	}

	// 清理工作目录（defer）
	if appConfig.GetRepoCloneCleanupAfterReview() {
		defer func() {
			if cleanupErr := repoManager.Cleanup(workDir); cleanupErr != nil {
				log.Printf("⚠️ [%s#%d] Cleanup failed: %v", repo, prNum, cleanupErr)
			}
		}()
	}

	// 从本地仓库获取完整 diff（不受 API 限制）
	log.Printf("🔍 [%s#%d] Getting full diff from local repository...", repo, prNum)
	diffText, err = repoManager.GetDiffFromLocalRepo(workDir, branchInfo.TargetBranch)
	if err != nil {
		log.Printf("⚠️ [%s#%d] Failed to get local diff: %v, falling back to API", repo, prNum, err)
		// 降级到 API 方式
		diffText, err = vcsClient.GetDiff(repo, prNum)
		if err != nil {
			log.Printf("❌ [%s#%d] Failed to get diff from API: %v", repo, prNum, err)
			return "", "", fmt.Errorf("failed to get diff: %w", err)
		}
	}

	// 构建上下文增强和引导信息
	enhancer := lib.NewDiffEnhancer(lib.PRContextInfo{
		Title:        prInfo.Title,
		Description:  prInfo.Description,
		Author:       prInfo.Author,
		SourceBranch: prInfo.SourceBranch,
		TargetBranch: prInfo.TargetBranch,
		Labels:       prInfo.Labels,
		IsDraft:      prInfo.IsDraft,
		CreatedAt:    prInfo.CreatedAt,
		UpdatedAt:    prInfo.UpdatedAt,
	}, diffText)

	claudeGuidance := enhancer.BuildClaudeCLIGuidance()
	enhancedDiff := enhancer.EnhanceDiff(diffText)

	// 执行依赖影响分析和测试覆盖检测
	modifiedFiles := enhancer.GetModifiedFilePaths()
	analyzer := lib.NewCodeAnalyzer(workDir, modifiedFiles, diffText)
	analysisResult := analyzer.AnalyzeDependencies()
	analysisGuidance := analysisResult.BuildAnalysisGuidance()
	log.Printf("✅ [%s#%d] Analysis completed: %d functions, %d call sites, %d files with tests, %d missing tests",
		repo, prNum, len(analysisResult.ModifiedFunctions), len(analysisResult.CallSites),
		len(analysisResult.TestCoverage), len(analysisResult.MissingTests))

	// 获取其他人的评论
	var commentsContext string
	if appConfig.GetClaudeCLIIncludeOthersComments() {
		commentsContext, _ = fetchOthersComments(vcsClient, repo, prNum)
	}

	// 使用 Claude CLI 审查
	log.Printf("🤖 [%s#%d] Starting Claude review...", repo, prNum)
	apiURL, apiKey, model, systemPrompt, userTemplate := appConfig.GetAIConfig()
	_ = apiURL // 不使用，但需要接收
	_ = apiKey // 不使用，但需要接收
	_ = model  // 不使用，但需要接收

	cliClient := lib.NewClaudeCLIClient(
		appConfig.GetClaudeCLIBinaryPath(),
		appConfig.GetClaudeCLIAllowedTools(),
		appConfig.GetClaudeCLITimeout(),
		appConfig.GetClaudeCLIMaxOutputLength(),
		systemPrompt,
		userTemplate,
		appConfig.GetClaudeCLIAPIKey(),
		appConfig.GetClaudeCLIAPIURL(),
		appConfig.GetClaudeCLIModel(),
		appConfig.GetClaudeCLIEnableOutputLog(),
	)

	// 组合：引导信息 + 依赖分析 + 其他人的评论 + 增强的 diff
	fullContext := claudeGuidance + "\n\n" + analysisGuidance
	if commentsContext != "" {
		fullContext += "\n\n" + commentsContext
	}
	fullContext += "\n\n" + enhancedDiff

	result, err := cliClient.ReviewCodeInRepo(workDir, fullContext, "")
	if err != nil {
		log.Printf("❌ [%s#%d] Claude review failed: %v", repo, prNum, err)
		return "", "", fmt.Errorf("Claude CLI review failed: %w", err)
	}

	if !result.Success {
		log.Printf("❌ [%s#%d] Claude review unsuccessful: %v", repo, prNum, result.Error)
		return "", "", fmt.Errorf("Claude CLI review unsuccessful: %v", result.Error)
	}

	return result.Content, diffText, nil
}

// processWithCodexCLI 使用 Codex CLI 模式处理审查
func processWithCodexCLI(vcsClient lib.VCSProvider, repo string, prNum int, token, providerType string) (reviewContent string, diffText string, err error) {
	// 获取 PR 详细信息
	prInfo, err := vcsClient.GetPRInfo(repo, prNum)
	if err != nil {
		prInfo = &lib.PRInfo{
			Title:  fmt.Sprintf("PR #%d", prNum),
			Author: "unknown",
		}
	}

	// 获取分支信息
	branchInfo, err := vcsClient.GetBranchInfo(repo, prNum)
	if err != nil {
		log.Printf("❌ [%s#%d] Failed to get branch info: %v", repo, prNum, err)
		return "", "", fmt.Errorf("failed to get branch info: %w", err)
	}

	// 获取克隆 URL
	cloneURL, err := vcsClient.GetCloneURL(repo)
	if err != nil {
		log.Printf("❌ [%s#%d] Failed to get clone URL: %v", repo, prNum, err)
		return "", "", fmt.Errorf("failed to get clone URL: %w", err)
	}

	// 构建带认证的克隆 URL
	authenticatedURL, err := lib.BuildCloneURL(cloneURL, token, providerType)
	if err != nil {
		log.Printf("❌ [%s#%d] Failed to build clone URL: %v", repo, prNum, err)
		return "", "", fmt.Errorf("failed to build clone URL: %w", err)
	}

	// 克隆仓库
	repoManager := lib.NewRepoManager(
		appConfig.GetRepoCloneTempDir(),
		appConfig.GetRepoCloneTimeout(),
		appConfig.GetRepoCloneShallowClone(),
		appConfig.GetRepoCloneShallowDepth(),
	)

	workDir, err := repoManager.CloneAndCheckout(authenticatedURL, *branchInfo)
	if err != nil {
		log.Printf("❌ [%s#%d] Clone failed: %v", repo, prNum, err)
		return "", "", fmt.Errorf("failed to clone repository: %w", err)
	}

	// 清理工作目录（defer）
	if appConfig.GetRepoCloneCleanupAfterReview() {
		defer func() {
			if cleanupErr := repoManager.Cleanup(workDir); cleanupErr != nil {
				log.Printf("⚠️ [%s#%d] Cleanup failed: %v", repo, prNum, cleanupErr)
			}
		}()
	}

	// 从本地仓库获取完整 diff（不受 API 限制）
	log.Printf("🔍 [%s#%d] Getting full diff from local repository...", repo, prNum)
	diffText, err = repoManager.GetDiffFromLocalRepo(workDir, branchInfo.TargetBranch)
	if err != nil {
		log.Printf("⚠️ [%s#%d] Failed to get local diff: %v, falling back to API", repo, prNum, err)
		// 降级到 API 方式
		diffText, err = vcsClient.GetDiff(repo, prNum)
		if err != nil {
			log.Printf("❌ [%s#%d] Failed to get diff from API: %v", repo, prNum, err)
			return "", "", fmt.Errorf("failed to get diff: %w", err)
		}
	}

	// 构建上下文增强和引导信息
	enhancer := lib.NewDiffEnhancer(lib.PRContextInfo{
		Title:        prInfo.Title,
		Description:  prInfo.Description,
		Author:       prInfo.Author,
		SourceBranch: prInfo.SourceBranch,
		TargetBranch: prInfo.TargetBranch,
		Labels:       prInfo.Labels,
		IsDraft:      prInfo.IsDraft,
		CreatedAt:    prInfo.CreatedAt,
		UpdatedAt:    prInfo.UpdatedAt,
	}, diffText)

	enhancedDiff := enhancer.EnhanceDiff(diffText)

	// 执行依赖影响分析和测试覆盖检测
	modifiedFiles := enhancer.GetModifiedFilePaths()
	analyzer := lib.NewCodeAnalyzer(workDir, modifiedFiles, diffText)
	analysisResult := analyzer.AnalyzeDependencies()
	analysisGuidance := analysisResult.BuildAnalysisGuidance()
	log.Printf("✅ [%s#%d] Analysis completed: %d functions, %d call sites, %d files with tests, %d missing tests",
		repo, prNum, len(analysisResult.ModifiedFunctions), len(analysisResult.CallSites),
		len(analysisResult.TestCoverage), len(analysisResult.MissingTests))

	// 获取其他人的评论
	var commentsContext string
	if appConfig.GetCodexCLIIncludeOthersComments() {
		commentsContext, _ = fetchOthersComments(vcsClient, repo, prNum)
	}

	// 使用 Codex CLI 审查
	log.Printf("🤖 [%s#%d] Starting Codex review...", repo, prNum)
	apiURL, apiKey, model, systemPrompt, userTemplate := appConfig.GetAIConfig()
	_ = apiURL // 不使用，但需要接收
	_ = apiKey // 不使用，但需要接收
	_ = model  // 不使用，但需要接收

	cliClient := lib.NewCodexCLIClient(
		appConfig.GetCodexCLIBinaryPath(),
		appConfig.GetCodexCLITimeout(),
		appConfig.GetCodexCLIMaxOutputLength(),
		systemPrompt,
		userTemplate,
		appConfig.GetCodexCLIAPIKey(),
		appConfig.GetCodexCLIAPIURL(),
		appConfig.GetCodexCLIModel(),
		appConfig.GetCodexCLIEnableOutputLog(),
	)

	// 组合：引导信息 + 依赖分析 + 其他人的评论 + 增强的 diff
	fullContext := lib.BuildCodexGuidance() + "\n\n" + analysisGuidance
	if commentsContext != "" {
		fullContext += "\n\n" + commentsContext
	}
	fullContext += "\n\n" + enhancedDiff

	result, err := cliClient.ReviewCodeInRepo(workDir, branchInfo.TargetBranch, fullContext)
	if err != nil {
		log.Printf("❌ [%s#%d] Codex review failed: %v", repo, prNum, err)
		return "", "", fmt.Errorf("Codex CLI review failed: %w", err)
	}

	if !result.Success {
		log.Printf("❌ [%s#%d] Codex review unsuccessful: %v", repo, prNum, result.Error)
		return "", "", fmt.Errorf("Codex CLI review unsuccessful: %v", result.Error)
	}

	return result.Content, diffText, nil
}

// fetchOthersComments 获取其他人（非当前认证用户）的评论
func fetchOthersComments(vcsClient lib.VCSProvider, repo string, prNum int) (string, error) {
	// 获取当前认证用户
	currentUser, err := vcsClient.GetCurrentUser()
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}

	// 获取普通评论和行内评论
	issueComments, err := vcsClient.GetIssueComments(repo, prNum)
	if err != nil {
		return "", fmt.Errorf("failed to get issue comments: %w", err)
	}

	inlineComments, err := vcsClient.GetInlineComments(repo, prNum)
	if err != nil {
		return "", fmt.Errorf("failed to get inline comments: %w", err)
	}

	// 过滤掉当前用户的评论
	var othersComments []lib.Comment
	for _, comment := range issueComments {
		if comment.UserLogin != currentUser {
			othersComments = append(othersComments, comment)
		}
	}
	for _, comment := range inlineComments {
		if comment.UserLogin != currentUser {
			othersComments = append(othersComments, comment)
		}
	}

	// 如果没有其他人的评论，返回空字符串
	if len(othersComments) == 0 {
		return "", nil
	}

	// 构建评论上下文字符串
	var sb strings.Builder
	sb.WriteString("=== 已有评论（来自其他审查者）===\n\n")
	sb.WriteString("以下是其他审查者在此 PR/MR 中提出的评论，可以结合这些评论做审查，比如代码还未修复这些问题时进行回复。回复时需要注意如果和其他审查者相关，带上审查者@名称和评论内容(截取关键部分)：\n\n")

	for i, comment := range othersComments {
		sb.WriteString(fmt.Sprintf("**评论 %d** (来自 @%s, %s)\n", i+1, comment.UserLogin, comment.CreatedAt))
		if comment.Path != "" {
			sb.WriteString(fmt.Sprintf("位置: %s:%d\n", comment.Path, comment.Line))
		}
		sb.WriteString(fmt.Sprintf("内容:\n%s\n\n", comment.Body))
		sb.WriteString("---\n\n")
	}
	return sb.String(), nil
}

func chooseEngineLabel(engine string) string {
	if engine == "" {
		return "default"
	}
	return engine
}

// deleteOldBotComments 删除当前 bot 账号在该 PR/MR 上发布的所有评论
func deleteOldBotComments(vcsClient lib.VCSProvider, repo string, prNum int) {
	currentUser, err := vcsClient.GetCurrentUser()
	if err != nil {
		log.Printf("⚠️ [%s#%d] Failed to get current user for cleanup: %v", repo, prNum, err)
		return
	}

	deleted := 0

	// 删除普通评论
	issueComments, err := vcsClient.GetIssueComments(repo, prNum)
	if err != nil {
		log.Printf("⚠️ [%s#%d] Failed to get issue comments for cleanup: %v", repo, prNum, err)
	} else {
		for _, c := range issueComments {
			if c.UserLogin == currentUser {
				if err := vcsClient.DeleteComment(repo, prNum, c.ID); err != nil {
					log.Printf("⚠️ [%s#%d] Failed to delete comment %d: %v", repo, prNum, c.ID, err)
				} else {
					deleted++
				}
			}
		}
	}

	// 删除行内评论
	inlineComments, err := vcsClient.GetInlineComments(repo, prNum)
	if err != nil {
		log.Printf("⚠️ [%s#%d] Failed to get inline comments for cleanup: %v", repo, prNum, err)
	} else {
		for _, c := range inlineComments {
			if c.UserLogin == currentUser {
				if err := vcsClient.DeleteInlineComment(repo, prNum, c.ID); err != nil {
					log.Printf("⚠️ [%s#%d] Failed to delete inline comment %d: %v", repo, prNum, c.ID, err)
				} else {
					deleted++
				}
			}
		}
	}

	if deleted > 0 {
		log.Printf("🧹 [%s#%d] Deleted %d old bot comments", repo, prNum, deleted)
	}
}
