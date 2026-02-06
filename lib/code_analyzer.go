package lib

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// CodeAnalyzer 代码分析器
type CodeAnalyzer struct {
	workDir       string
	modifiedFiles []string
	diffText      string
}

// NewCodeAnalyzer 创建代码分析器
func NewCodeAnalyzer(workDir string, modifiedFiles []string, diffText string) *CodeAnalyzer {
	return &CodeAnalyzer{
		workDir:       workDir,
		modifiedFiles: modifiedFiles,
		diffText:      diffText,
	}
}

// FunctionInfo 函数信息
type FunctionInfo struct {
	Name     string
	File     string
	Language string
	Type     string // "function", "method", "class", "interface"
}

// DependencyAnalysisResult 依赖分析结果
type DependencyAnalysisResult struct {
	ModifiedFunctions []FunctionInfo
	CallSites         map[string][]string // function name -> file paths
	TestCoverage      map[string][]string // source file -> test files
	MissingTests      []string            // files without tests
}

// AnalyzeDependencies 分析依赖影响和测试覆盖
func (a *CodeAnalyzer) AnalyzeDependencies() *DependencyAnalysisResult {
	result := &DependencyAnalysisResult{
		ModifiedFunctions: []FunctionInfo{},
		CallSites:         make(map[string][]string),
		TestCoverage:      make(map[string][]string),
		MissingTests:      []string{},
	}

	// 1. 提取修改的函数/方法/类
	for _, file := range a.modifiedFiles {
		functions := a.extractModifiedFunctions(file)
		result.ModifiedFunctions = append(result.ModifiedFunctions, functions...)
	}

	// 2. 查找函数调用位置
	for _, fn := range result.ModifiedFunctions {
		callSites := a.findCallSites(fn.Name, fn.File)
		if len(callSites) > 0 {
			result.CallSites[fn.Name] = callSites
		}
	}

	// 3. 检查测试覆盖
	for _, file := range a.modifiedFiles {
		testFiles := a.findTestFiles(file)
		if len(testFiles) > 0 {
			result.TestCoverage[file] = testFiles
		} else {
			result.MissingTests = append(result.MissingTests, file)
		}
	}

	return result
}

// extractModifiedFunctions 从 diff 中提取修改的函数
func (a *CodeAnalyzer) extractModifiedFunctions(file string) []FunctionInfo {
	functions := []FunctionInfo{}
	language := detectLanguage(file)

	// 根据语言选择不同的函数提取策略
	patterns := getFunctionPatterns(language)
	if len(patterns) == 0 {
		return functions
	}

	// 从 diff 中提取修改的代码行
	addedLines := a.extractAddedOrModifiedLines(file)

	for _, line := range addedLines {
		for _, pattern := range patterns {
			re := regexp.MustCompile(pattern.Regex)
			if matches := re.FindStringSubmatch(line); matches != nil && len(matches) > 1 {
				funcName := matches[1]
				// 过滤掉一些明显的误报
				if !isValidFunctionName(funcName) {
					continue
				}
				functions = append(functions, FunctionInfo{
					Name:     funcName,
					File:     file,
					Language: language,
					Type:     pattern.Type,
				})
			}
		}
	}

	return functions
}

// extractAddedOrModifiedLines 从 diff 中提取新增或修改的代码行
func (a *CodeAnalyzer) extractAddedOrModifiedLines(file string) []string {
	lines := []string{}
	diffLines := strings.Split(a.diffText, "\n")

	inTargetFile := false
	for _, line := range diffLines {
		// 检查是否进入目标文件
		if strings.HasPrefix(line, "+++ b/") {
			currentFile := strings.TrimPrefix(line, "+++ b/")
			inTargetFile = (currentFile == file)
			continue
		}

		if !inTargetFile {
			continue
		}

		// 提取新增的代码行（以 + 开头，但不是 +++）
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			lines = append(lines, strings.TrimPrefix(line, "+"))
		}
	}

	return lines
}

// FunctionPattern 函数匹配模式
type FunctionPattern struct {
	Regex string
	Type  string
}

// getFunctionPatterns 获取不同语言的函数匹配模式
func getFunctionPatterns(language string) []FunctionPattern {
	patterns := make([]FunctionPattern, 0)

	switch language {
	case "Go":
		patterns = append(patterns,
			FunctionPattern{Regex: `^\s*func\s+(\w+)\s*\(`, Type: "function"},
			FunctionPattern{Regex: `^\s*func\s+\([^)]+\)\s+(\w+)\s*\(`, Type: "method"},
			FunctionPattern{Regex: `^\s*type\s+(\w+)\s+struct`, Type: "struct"},
			FunctionPattern{Regex: `^\s*type\s+(\w+)\s+interface`, Type: "interface"},
		)
	case "JavaScript", "TypeScript", "React", "React/TypeScript":
		patterns = append(patterns,
			FunctionPattern{Regex: `^\s*function\s+(\w+)\s*\(`, Type: "function"},
			FunctionPattern{Regex: `^\s*const\s+(\w+)\s*=\s*\([^)]*\)\s*=>`, Type: "function"},
			FunctionPattern{Regex: `^\s*export\s+function\s+(\w+)\s*\(`, Type: "function"},
			FunctionPattern{Regex: `^\s*class\s+(\w+)`, Type: "class"},
			FunctionPattern{Regex: `^\s*(\w+)\s*:\s*function\s*\(`, Type: "method"},
			FunctionPattern{Regex: `^\s*(\w+)\s*\([^)]*\)\s*{`, Type: "method"},
		)
	case "Python":
		patterns = append(patterns,
			FunctionPattern{Regex: `^\s*def\s+(\w+)\s*\(`, Type: "function"},
			FunctionPattern{Regex: `^\s*class\s+(\w+)`, Type: "class"},
			FunctionPattern{Regex: `^\s*async\s+def\s+(\w+)\s*\(`, Type: "function"},
		)
	case "Java":
		patterns = append(patterns,
			FunctionPattern{Regex: `^\s*(?:public|private|protected)?\s*(?:static)?\s*\w+\s+(\w+)\s*\(`, Type: "method"},
			FunctionPattern{Regex: `^\s*(?:public|private|protected)?\s*class\s+(\w+)`, Type: "class"},
			FunctionPattern{Regex: `^\s*(?:public|private|protected)?\s*interface\s+(\w+)`, Type: "interface"},
		)
	case "Rust":
		patterns = append(patterns,
			FunctionPattern{Regex: `^\s*fn\s+(\w+)\s*\(`, Type: "function"},
			FunctionPattern{Regex: `^\s*pub\s+fn\s+(\w+)\s*\(`, Type: "function"},
			FunctionPattern{Regex: `^\s*struct\s+(\w+)`, Type: "struct"},
			FunctionPattern{Regex: `^\s*trait\s+(\w+)`, Type: "trait"},
		)
	case "C++", "C":
		patterns = append(patterns,
			FunctionPattern{Regex: `^\s*\w+\s+(\w+)\s*\([^)]*\)\s*{`, Type: "function"},
			FunctionPattern{Regex: `^\s*class\s+(\w+)`, Type: "class"},
		)
	}

	return patterns
}

// isValidFunctionName 检查是否是有效的函数名
func isValidFunctionName(name string) bool {
	// 过滤掉关键字和常见误报
	keywords := map[string]bool{
		"if": true, "for": true, "while": true, "switch": true,
		"return": true, "break": true, "continue": true,
		"true": true, "false": true, "null": true, "nil": true,
	}

	if keywords[name] {
		return false
	}

	// 函数名至少3个字符
	if len(name) < 3 {
		return false
	}

	return true
}

// findCallSites 查找函数的调用位置
func (a *CodeAnalyzer) findCallSites(functionName, sourceFile string) []string {
	callSites := []string{}

	// 使用 grep 在整个仓库中搜索函数名
	// 使用 -l 只返回文件名，避免输出过多
	cmd := exec.Command("grep", "-r", "-l", "--include=*.go", "--include=*.js", "--include=*.ts",
		"--include=*.py", "--include=*.java", "--include=*.rs", functionName, ".")
	cmd.Dir = a.workDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		// grep 返回非0状态码时（未找到）也会有 err，但这是正常的
		return callSites
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == sourceFile || strings.HasPrefix(line, "./"+sourceFile) {
			continue // 跳过定义文件本身
		}
		// 去掉 ./ 前缀
		line = strings.TrimPrefix(line, "./")
		callSites = append(callSites, line)
	}

	// 去重
	callSites = uniqueStrings(callSites)

	return callSites
}

// findTestFiles 查找对应的测试文件
func (a *CodeAnalyzer) findTestFiles(sourceFile string) []string {
	testFiles := []string{}
	language := detectLanguage(sourceFile)

	// 生成可能的测试文件名
	possibleTests := generateTestFileNames(sourceFile, language)

	for _, testFile := range possibleTests {
		testPath := filepath.Join(a.workDir, testFile)
		// 使用 ls 检查文件是否存在（跨平台兼容）
		cmd := exec.Command("ls", testPath)
		if err := cmd.Run(); err == nil {
			testFiles = append(testFiles, testFile)
		}
	}

	return testFiles
}

// generateTestFileNames 生成可能的测试文件名
func generateTestFileNames(sourceFile, language string) []string {
	testNames := []string{}
	dir := filepath.Dir(sourceFile)
	base := filepath.Base(sourceFile)
	ext := filepath.Ext(base)
	nameWithoutExt := strings.TrimSuffix(base, ext)

	switch language {
	case "Go":
		// Go: foo.go -> foo_test.go
		testNames = append(testNames, filepath.Join(dir, nameWithoutExt+"_test.go"))

	case "JavaScript", "TypeScript":
		// JS/TS: foo.js -> foo.test.js, foo.spec.js, __tests__/foo.js
		testNames = append(testNames,
			filepath.Join(dir, nameWithoutExt+".test"+ext),
			filepath.Join(dir, nameWithoutExt+".spec"+ext),
			filepath.Join(dir, "__tests__", base),
			filepath.Join(dir, "__tests__", nameWithoutExt+".test"+ext),
		)

	case "Python":
		// Python: foo.py -> test_foo.py, foo_test.py, tests/test_foo.py
		testNames = append(testNames,
			filepath.Join(dir, "test_"+base),
			filepath.Join(dir, nameWithoutExt+"_test.py"),
			filepath.Join("tests", "test_"+base),
			filepath.Join("tests", dir, "test_"+base),
		)

	case "Java":
		// Java: Foo.java -> FooTest.java, tests/FooTest.java
		testNames = append(testNames,
			filepath.Join(dir, nameWithoutExt+"Test.java"),
			filepath.Join("test", dir, nameWithoutExt+"Test.java"),
			strings.Replace(filepath.Join(dir, nameWithoutExt+"Test.java"), "/main/", "/test/", 1),
		)

	case "Rust":
		// Rust: 通常在同文件内的 #[cfg(test)] mod tests
		// 或者在 tests/ 目录下
		testNames = append(testNames,
			filepath.Join("tests", nameWithoutExt+".rs"),
			filepath.Join("tests", base),
		)
	}

	return testNames
}

// BuildAnalysisGuidance 构建分析引导（用于 Claude CLI）
func (result *DependencyAnalysisResult) BuildAnalysisGuidance() string {
	var builder strings.Builder

	builder.WriteString("\n═══════════════════════════════════════════════════════════\n")
	builder.WriteString("         DEPENDENCY IMPACT & TEST COVERAGE ANALYSIS        \n")
	builder.WriteString("═══════════════════════════════════════════════════════════\n\n")

	// 1. 修改的函数/方法
	if len(result.ModifiedFunctions) > 0 {
		builder.WriteString("## 🔧 检测到以下函数/方法被修改:\n\n")

		// 按文件分组显示
		fileGroups := make(map[string][]FunctionInfo)
		for _, fn := range result.ModifiedFunctions {
			fileGroups[fn.File] = append(fileGroups[fn.File], fn)
		}

		for file, functions := range fileGroups {
			builder.WriteString(fmt.Sprintf("**%s**:\n", file))
			for _, fn := range functions {
				builder.WriteString(fmt.Sprintf("  - `%s` (%s)\n", fn.Name, fn.Type))
			}
			builder.WriteString("\n")
		}
	}

	// 2. 依赖影响分析
	if len(result.CallSites) > 0 {
		builder.WriteString("## 🔍 依赖影响分析 (检测到的调用位置):\n\n")
		builder.WriteString("**重要**: 以下函数在其他文件中被调用，请验证调用方是否需要更新:\n\n")

		for fnName, sites := range result.CallSites {
			if len(sites) > 0 {
				builder.WriteString(fmt.Sprintf("### `%s` 被以下文件调用:\n", fnName))
				for _, site := range sites {
					builder.WriteString(fmt.Sprintf("  - %s\n", site))
				}
				builder.WriteString("\n**审查建议**:\n")
				builder.WriteString(fmt.Sprintf("1. 使用 `Read(\"%s\")` 查看调用方代码\n", sites[0]))
				if len(sites) > 1 {
					builder.WriteString("2. 检查所有调用方是否适配了新的函数签名或行为\n")
				}
				builder.WriteString(fmt.Sprintf("3. 使用 `Grep(\"%s\", output_mode=\"content\")` 查看具体调用上下文\n", fnName))
				builder.WriteString("4. 评估是否存在破坏性变更 (Breaking Change)\n\n")
			}
		}
	} else if len(result.ModifiedFunctions) > 0 {
		builder.WriteString("## ℹ️ 依赖影响分析:\n\n")
		builder.WriteString("未检测到修改的函数在其他文件中被调用（可能是内部实现或新增函数）\n\n")
	}

	// 3. 测试覆盖分析
	builder.WriteString("## 🧪 测试覆盖检测:\n\n")

	if len(result.TestCoverage) > 0 {
		builder.WriteString("**✅ 以下文件有对应的测试文件**:\n\n")
		for sourceFile, testFiles := range result.TestCoverage {
			builder.WriteString(fmt.Sprintf("- `%s`\n", sourceFile))
			for _, testFile := range testFiles {
				builder.WriteString(fmt.Sprintf("  - 测试文件: `%s`\n", testFile))
			}
		}
		builder.WriteString("\n**审查建议**: 使用 `Read` 工具查看测试文件，确认:\n")
		builder.WriteString("1. 测试用例是否已更新以覆盖新逻辑\n")
		builder.WriteString("2. 是否需要添加新的测试用例\n")
		builder.WriteString("3. 边界条件和异常情况是否充分测试\n\n")
	}

	if len(result.MissingTests) > 0 {
		builder.WriteString("**⚠️ 以下文件缺少测试覆盖**:\n\n")
		for _, file := range result.MissingTests {
			builder.WriteString(fmt.Sprintf("- `%s` ❌ 未找到对应的测试文件\n", file))
		}
		builder.WriteString("\n**严重警告**: 这些文件的修改缺少自动化测试，存在回归风险!\n")
		builder.WriteString("建议作者补充单元测试覆盖关键逻辑。\n\n")
	}

	// 4. 审查流程建议
	builder.WriteString("## 📋 建议的审查流程:\n\n")
	builder.WriteString("1. **理解变更意图**: 阅读 PR 描述和修改的完整文件\n")
	builder.WriteString("2. **验证影响范围**: 检查所有调用方是否已适配\n")
	builder.WriteString("3. **测试覆盖检查**: 确认测试用例是否充分\n")
	builder.WriteString("4. **边界条件审查**: 空值、零值、最大/最小值等\n")
	builder.WriteString("5. **安全性检查**: SQL注入、XSS、认证授权等\n")
	builder.WriteString("6. **性能评估**: 循环复杂度、数据库查询效率等\n\n")

	return builder.String()
}

// uniqueStrings 字符串数组去重
func uniqueStrings(input []string) []string {
	seen := make(map[string]bool)
	result := []string{}

	for _, str := range input {
		if !seen[str] {
			seen[str] = true
			result = append(result, str)
		}
	}

	return result
}
