package lib

// Comment 代表一条评论
type Comment struct {
	ID        int64  // 评论 ID
	Body      string // 评论内容
	Path      string // 文件路径（行内评论）
	Line      int    // 行号（行内评论）
	Position  int    // Diff 位置（行内评论，GitHub）
	CreatedAt string // 创建时间
	UserID    int64  // 用户 ID
	UserLogin string // 用户登录名
}

// VCSProvider 定义版本控制系统提供商的统一接口
type VCSProvider interface {
	// GetDiff 获取 Pull/Merge Request 的代码变更
	GetDiff(repo string, number int) (string, error)

	// GetHeadSHA 获取 PR/MR 的最新 commit SHA
	GetHeadSHA(repo string, number int) (string, error)

	// PostComment 发布普通评论到 PR/MR
	PostComment(repo string, number int, comment string) error

	// PostInlineComment 发布行内评论到 PR/MR
	// position: GitHub 使用 diff position, GitLab 使用实际行号
	// oldLine, newLine: GitLab 需要这两个参数来标识修改的行
	PostInlineComment(repo string, number int, commitSHA, path string, position int, body string, oldLine, newLine int) error

	// GetIssueComments 获取 PR/MR 的普通评论列表
	GetIssueComments(repo string, number int) ([]Comment, error)

	// GetInlineComments 获取 PR/MR 的行内评论列表
	GetInlineComments(repo string, number int) ([]Comment, error)

	// GetBranchInfo 获取 PR/MR 的分支信息
	GetBranchInfo(repo string, number int) (*BranchInfo, error)

	// GetCloneURL 获取仓库克隆 URL
	GetCloneURL(repo string) (string, error)

	// GetCurrentUser 获取当前认证用户的登录名
	GetCurrentUser() (string, error)

	// GetProviderType 返回提供商类型（用于日志）
	GetProviderType() string
}

const (
	ProviderTypeGitHub = "github"
	ProviderTypeGitLab = "gitlab"
)
