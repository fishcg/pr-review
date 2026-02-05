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
		var fetchArgs []string
		if rm.ShallowClone {
			fetchArgs = []string{"fetch", "--depth", fmt.Sprintf("%d", rm.ShallowDepth), "origin", branchInfo.SourceBranch}
		} else {
			fetchArgs = []string{"fetch", "origin", branchInfo.SourceBranch}
		}

		fetchCmd := exec.Command("git", fetchArgs...)
		fetchCmd.Dir = workDir

		var fetchStderr strings.Builder
		fetchCmd.Stderr = &fetchStderr

		if err := fetchCmd.Run(); err != nil {
			log.Printf("⚠️ Failed to fetch source branch: %v, stderr: %s", err, fetchStderr.String())
			// 不返回错误，继续尝试 checkout
		}

		// 5. Checkout 到源分支
		checkoutCmd := exec.Command("git", "checkout", branchInfo.SourceBranch)
		checkoutCmd.Dir = workDir

		var checkoutStderr strings.Builder
		checkoutCmd.Stderr = &checkoutStderr

		if err := checkoutCmd.Run(); err != nil {
			// 尝试使用 SHA 来 checkout（如果提供了 SHA）
			if branchInfo.SourceSHA != "" {
				checkoutSHACmd := exec.Command("git", "checkout", branchInfo.SourceSHA)
				checkoutSHACmd.Dir = workDir

				if err := checkoutSHACmd.Run(); err != nil {
					return "", fmt.Errorf("checkout failed: %w, stderr: %s", err, checkoutStderr.String())
				}
			} else {
				return "", fmt.Errorf("checkout failed: %w, stderr: %s", err, checkoutStderr.String())
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
