package lib

// VCSProvider 定义版本控制系统提供商的统一接口
type VCSProvider interface {
	// GetDiff 获取 Pull/Merge Request 的代码变更
	GetDiff(repo string, number int) (string, error)

	// GetHeadSHA 获取 PR/MR 的最新 commit SHA
	GetHeadSHA(repo string, number int) (string, error)

	// PostComment 发布普通评论到 PR/MR
	PostComment(repo string, number int, comment string) error

	// PostInlineComment 发布行内评论到 PR/MR
	PostInlineComment(repo string, number int, commitSHA, path string, position int, body string) error

	// GetProviderType 返回提供商类型（用于日志）
	GetProviderType() string
}

const (
	ProviderTypeGitHub = "github"
	ProviderTypeGitLab = "gitlab"
)
