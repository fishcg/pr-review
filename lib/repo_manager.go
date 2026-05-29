package lib

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// RepoManager 仓库管理器
type RepoManager struct {
	TempDir      string
	CloneTimeout time.Duration
	ShallowClone bool
	ShallowDepth int
}

// BranchInfo 分支信息
type BranchInfo struct {
	SourceBranch string // PR/MR 的源分支
	TargetBranch string // PR/MR 的目标分支
	SourceSHA    string // 源分支的 SHA
}

// NewRepoManager 创建仓库管理器
func NewRepoManager(tempDir string, cloneTimeout int, shallowClone bool, shallowDepth int) *RepoManager {
	return &RepoManager{
		TempDir:      tempDir,
		CloneTimeout: time.Duration(cloneTimeout) * time.Second,
		ShallowClone: shallowClone,
		ShallowDepth: shallowDepth,
	}
}

// CloneAndCheckout 克隆仓库并检出到指定分支
func (rm *RepoManager) CloneAndCheckout(cloneURL string, branchInfo BranchInfo) (string, error) {
	// 1. 确保临时目录存在
	if err := os.MkdirAll(rm.TempDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	// 2. 创建工作目录（使用 SHA 避免并发冲突）
	repoName := extractRepoName(cloneURL)
	shortSHA := branchInfo.SourceSHA
	if len(shortSHA) > 8 {
		shortSHA = shortSHA[:8]
	}
	// 如果没有 SHA，使用时间戳作为后备
	if shortSHA == "" {
		shortSHA = time.Now().Format("20060102-150405")
	}
	workDir := filepath.Join(rm.TempDir, fmt.Sprintf("%s-%s", repoName, shortSHA))

	// 如果目录已存在，先删除（可能是之前失败的 review）
	if _, err := os.Stat(workDir); err == nil {
		if err := os.RemoveAll(workDir); err != nil {
			return "", fmt.Errorf("failed to remove existing work directory: %w", err)
		}
	}

	// 3. 克隆仓库
	ctx, cancel := context.WithTimeout(context.Background(), rm.CloneTimeout)
	defer cancel()

	var cloneArgs []string
	if rm.ShallowClone {
		// 浅克隆目标分支
		cloneArgs = []string{
			"clone",
			"--depth", fmt.Sprintf("%d", rm.ShallowDepth),
			"--branch", branchInfo.TargetBranch,
			cloneURL,
			workDir,
		}
	} else {
		// 完整克隆
		cloneArgs = []string{
			"clone",
			cloneURL,
			workDir,
		}
	}

	cmd := exec.CommandContext(ctx, "git", cloneArgs...)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// startTime := time.Now()
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("clone timeout after %v", rm.CloneTimeout)
		}
		return "", fmt.Errorf("git clone failed: %w, stderr: %s", err, stderr.String())
	}

	// 4. Fetch 源分支（如果与目标分支不同）
	if branchInfo.SourceBranch != branchInfo.TargetBranch {
		// 显式把远端分支映射到本地分支引用，避免 fetch 只写 FETCH_HEAD
		// 导致后续 `git checkout <branch>` 找不到本地分支而失败。
		refspec := fmt.Sprintf("refs/heads/%s:refs/remotes/origin/%s",
			branchInfo.SourceBranch, branchInfo.SourceBranch)
		var fetchArgs []string
		if rm.ShallowClone {
			fetchArgs = []string{"fetch", "--depth", fmt.Sprintf("%d", rm.ShallowDepth), "origin", refspec}
		} else {
			fetchArgs = []string{"fetch", "origin", refspec}
		}

		fetchCmd := exec.Command("git", fetchArgs...)
		fetchCmd.Dir = workDir

		var fetchStderr strings.Builder
		fetchCmd.Stderr = &fetchStderr

		if err := fetchCmd.Run(); err != nil {
			log.Printf("⚠️ Failed to fetch source branch: %v, stderr: %s", err, fetchStderr.String())
			// 不返回错误，继续尝试 checkout
		}

		// 5. Checkout 到源分支的提交。
		// 优先用 SourceSHA（最精确，不依赖本地分支名）；
		// 没有 SHA 时回退到 origin/<source> 远端跟踪分支。
		checkoutTarget := branchInfo.SourceSHA
		if checkoutTarget == "" {
			checkoutTarget = fmt.Sprintf("origin/%s", branchInfo.SourceBranch)
		}

		checkoutCmd := exec.Command("git", "checkout", "--detach", checkoutTarget)
		checkoutCmd.Dir = workDir

		var checkoutStderr strings.Builder
		checkoutCmd.Stderr = &checkoutStderr

		if err := checkoutCmd.Run(); err != nil {
			// 回退：尝试 origin/<source> 远端跟踪分支
			fallback := fmt.Sprintf("origin/%s", branchInfo.SourceBranch)
			if checkoutTarget != fallback {
				retryCmd := exec.Command("git", "checkout", "--detach", fallback)
				retryCmd.Dir = workDir
				if retryErr := retryCmd.Run(); retryErr != nil {
					return "", fmt.Errorf("checkout failed for %s and %s: %w, stderr: %s",
						checkoutTarget, fallback, err, checkoutStderr.String())
				}
			} else {
				return "", fmt.Errorf("checkout failed for %s: %w, stderr: %s",
					checkoutTarget, err, checkoutStderr.String())
			}
		}

		// 6. 校验 HEAD 确实落在源分支提交上，避免静默停留在目标分支
		// 导致 diff 为空、模型凭空臆测（幻觉）。
		if branchInfo.SourceSHA != "" {
			headCmd := exec.Command("git", "rev-parse", "HEAD")
			headCmd.Dir = workDir
			var headOut strings.Builder
			headCmd.Stdout = &headOut
			if err := headCmd.Run(); err == nil {
				gotHead := strings.TrimSpace(headOut.String())
				if gotHead != branchInfo.SourceSHA {
					log.Printf("⚠️ HEAD=%s 与源分支 SHA=%s 不一致（diff 可能不准确）",
						gotHead, branchInfo.SourceSHA)
				}
			}
		}
	}

	return workDir, nil
}

// Cleanup 清理工作目录
func (rm *RepoManager) Cleanup(workDir string) error {
	// 安全检查：确保要删除的目录在临时目录下
	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	absTempDir, err := filepath.Abs(rm.TempDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute temp path: %w", err)
	}

	if !strings.HasPrefix(absWorkDir, absTempDir) {
		return fmt.Errorf("security: refusing to delete directory outside temp dir: %s", workDir)
	}

	if err := os.RemoveAll(workDir); err != nil {
		return fmt.Errorf("failed to remove work directory: %w", err)
	}
	return nil
}

// CleanupOldRepos 清理过期的仓库目录（超过指定时间）
func (rm *RepoManager) CleanupOldRepos(maxAge time.Duration) error {
	entries, err := os.ReadDir(rm.TempDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 目录不存在，无需清理
		}
		return fmt.Errorf("failed to read temp directory: %w", err)
	}

	now := time.Now()
	cleaned := 0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirPath := filepath.Join(rm.TempDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		age := now.Sub(info.ModTime())
		if age > maxAge {
			if err := os.RemoveAll(dirPath); err != nil {
				log.Printf("⚠️ Failed to remove old repo: %v", err)
			} else {
				cleaned++
			}
		}
	}

	return nil
}

// BuildCloneURL 构建克隆 URL（带认证）
func BuildCloneURL(baseURL, token, providerType string) (string, error) {
	// 验证 URL
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	// 安全检查：只允许 https:// 或 git@ (SSH)
	if parsedURL.Scheme != "https" && !strings.HasPrefix(baseURL, "git@") {
		return "", fmt.Errorf("only https:// or git@ URLs are allowed, got: %s", baseURL)
	}

	// 如果已经包含认证信息，直接返回
	if parsedURL.User != nil {
		return baseURL, nil
	}

	// 根据 provider 类型添加认证
	switch providerType {
	case ProviderTypeGitHub:
		// GitHub: https://oauth2:TOKEN@github.com/owner/repo.git
		parsedURL.User = url.UserPassword("oauth2", token)
	case ProviderTypeGitLab:
		// GitLab: https://oauth2:TOKEN@gitlab.com/owner/repo.git
		parsedURL.User = url.UserPassword("oauth2", token)
	default:
		return "", fmt.Errorf("unsupported provider type: %s", providerType)
	}

	return parsedURL.String(), nil
}

// GetDiffFromLocalRepo 从本地仓库获取 PR/MR 的完整 diff
// （即源分支相对目标分支自分叉点起的全部变更，等价于 PR/MR Files Changed 视图）。
// 通过显式计算 merge-base 来获取 diff，避免浅克隆下 `git diff A...B` 退化为
// 浅克隆边界处的"伪 base"，导致结果只包含源分支最近若干 commit 的变更。
func (rm *RepoManager) GetDiffFromLocalRepo(workDir, sourceBranch, targetBranch string) (string, error) {
	targetRef := fmt.Sprintf("origin/%s", targetBranch)

	mergeBase, err := rm.ensureMergeBase(workDir, targetRef, "HEAD", sourceBranch, targetBranch)
	if err != nil {
		return "", fmt.Errorf("failed to find merge-base between %s and HEAD: %w", targetRef, err)
	}

	log.Printf("🔗 Diff range: %s..HEAD (merge-base of source=%s vs target=%s)",
		shortSHA(mergeBase), sourceBranch, targetBranch)

	diffCmd := exec.Command("git", "diff", fmt.Sprintf("%s..HEAD", mergeBase))
	diffCmd.Dir = workDir

	var stdout, stderr strings.Builder
	diffCmd.Stdout = &stdout
	diffCmd.Stderr = &stderr

	if err := diffCmd.Run(); err != nil {
		return "", fmt.Errorf("git diff %s..HEAD failed: %w, stderr: %s", mergeBase, err, stderr.String())
	}

	return stdout.String(), nil
}

// ensureMergeBase 查找 merge-base；浅克隆下若 merge-base 不可达，会逐步加深
// 直至可达或转为 unshallow。
func (rm *RepoManager) ensureMergeBase(workDir, ref1, ref2, sourceBranch, targetBranch string) (string, error) {
	if base, ok := tryMergeBase(workDir, ref1, ref2); ok {
		return base, nil
	}

	if !rm.ShallowClone {
		return "", fmt.Errorf("no merge-base reachable between %s and %s", ref1, ref2)
	}

	base := rm.ShallowDepth
	if base <= 0 {
		base = 100
	}
	deepenSteps := []int{base * 5, base * 20, base * 100}
	for _, depth := range deepenSteps {
		log.Printf("⚠️ merge-base not found in shallow clone, deepening fetch to depth=%d", depth)
		deepenFetch(workDir, targetBranch, depth)
		if sourceBranch != "" && sourceBranch != targetBranch {
			deepenFetch(workDir, sourceBranch, depth)
		}
		if b, ok := tryMergeBase(workDir, ref1, ref2); ok {
			return b, nil
		}
	}

	log.Printf("⚠️ merge-base still missing, attempting unshallow fetch")
	unshallowCmd := exec.Command("git", "fetch", "--unshallow", "origin")
	unshallowCmd.Dir = workDir
	if err := unshallowCmd.Run(); err != nil {
		log.Printf("⚠️ unshallow fetch failed (continuing): %v", err)
	}
	if b, ok := tryMergeBase(workDir, ref1, ref2); ok {
		return b, nil
	}

	return "", fmt.Errorf("merge-base unreachable between %s and %s after deepening and unshallow", ref1, ref2)
}

func tryMergeBase(workDir, a, b string) (string, bool) {
	cmd := exec.Command("git", "merge-base", a, b)
	cmd.Dir = workDir
	var out strings.Builder
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", false
	}
	base := strings.TrimSpace(out.String())
	return base, base != ""
}

func deepenFetch(workDir, branch string, depth int) {
	if branch == "" {
		return
	}
	cmd := exec.Command("git", "fetch", fmt.Sprintf("--depth=%d", depth), "origin", branch)
	cmd.Dir = workDir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		log.Printf("⚠️ deepen fetch origin/%s depth=%d failed: %v, stderr: %s", branch, depth, err, stderr.String())
	}
}

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// extractRepoName 从 URL 中提取仓库名称
func extractRepoName(cloneURL string) string {
	// 移除 .git 后缀
	cleaned := strings.TrimSuffix(cloneURL, ".git")

	// 提取最后一段路径
	parts := strings.Split(cleaned, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}

	return "repo"
}
