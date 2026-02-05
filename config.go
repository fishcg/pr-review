package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ClaudeCLIConfig Claude CLI 配置
type ClaudeCLIConfig struct {
	BinaryPath           string   `yaml:"binary_path"`             // Claude CLI 路径
	AllowedTools         []string `yaml:"allowed_tools"`           // 允许使用的工具
	Timeout              int      `yaml:"timeout"`                 // 超时秒数
	MaxOutputLength      int      `yaml:"max_output_length"`       // 最大输出长度
	APIKey               string   `yaml:"api_key"`                 // Anthropic API Key
	APIURL               string   `yaml:"api_url"`                 // Anthropic API URL (可选)
	Model                string   `yaml:"model"`                   // Claude Model (可选)
	IncludeOthersComments bool     `yaml:"include_others_comments"` // 是否包含其他人的评论
	EnableOutputLog      bool     `yaml:"enable_output_log"`       // 是否启用输出日志
}

// RepoCloneConfig 仓库克隆配置
type RepoCloneConfig struct {
	TempDir            string `yaml:"temp_dir"`              // 临时目录
	CloneTimeout       int    `yaml:"clone_timeout"`         // 克隆超时秒数
	ShallowClone       bool   `yaml:"shallow_clone"`         // 是否浅克隆
	ShallowDepth       int    `yaml:"shallow_depth"`         // 浅克隆深度
	CleanupAfterReview bool   `yaml:"cleanup_after_review"`  // Review 后是否清理
}

// Config 配置结构
type Config struct {
	AIApiURL           string `yaml:"ai_api_url"`
	AIApiKey           string `yaml:"ai_api_key"`
	AIModel            string `yaml:"ai_model"`
	Port               string `yaml:"port"`
	SystemPrompt       string `yaml:"system_prompt"`
	UserPromptTemplate string `yaml:"user_prompt_template"`
	InlineIssueComment bool   `yaml:"inline_issue_comment"`
	CommentOnlyChanges bool   `yaml:"comment_only_changes"` // 只对修改的代码行评论，不对上下文行评论

	// 行号匹配策略配置
	LineMatchStrategy string `yaml:"line_match_strategy"` // "snippet_first"(默认) 或 "line_number_first"

	// Review 模式配置
	ReviewMode string `yaml:"review_mode"` // "api" 或 "claude_cli"

	// Claude CLI 配置
	ClaudeCLI ClaudeCLIConfig `yaml:"claude_cli"`

	// 仓库克隆配置
	RepoClone RepoCloneConfig `yaml:"repo_clone"`

	// VCS Provider 配置
	VCSProvider string `yaml:"vcs_provider"` // "github" 或 "gitlab"

	// GitHub 配置
	GithubToken   string `yaml:"github_token"`
	WebhookSecret string `yaml:"webhook_secret"`

	// GitLab 配置
	GitlabToken        string `yaml:"gitlab_token"`
	GitlabBaseURL      string `yaml:"gitlab_base_url"`
	GitlabWebhookToken string `yaml:"gitlab_webhook_token"`
}

// 全局配置实例
var AppConfig Config

// LoadConfig 加载配置文件
func LoadConfig(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, &AppConfig); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	// 验证必需字段
	if AppConfig.AIApiURL == "" {
		return fmt.Errorf("ai_api_url is required in config")
	}
	if AppConfig.AIApiKey == "" {
		return fmt.Errorf("ai_api_key is required in config")
	}
	if AppConfig.AIModel == "" {
		AppConfig.AIModel = "qwen-plus-latest" // 默认模型
	}
	if AppConfig.Port == "" {
		AppConfig.Port = "7995" // 默认端口
	}
	if AppConfig.SystemPrompt == "" {
		return fmt.Errorf("system_prompt is required in config")
	}
	if AppConfig.UserPromptTemplate == "" {
		return fmt.Errorf("user_prompt_template is required in config")
	}

	// VCS Provider 默认值和验证
	if AppConfig.VCSProvider == "" {
		AppConfig.VCSProvider = "github" // 默认使用 GitHub（向后兼容）
	}

	// 根据 VCS Provider 验证对应的 token
	switch AppConfig.VCSProvider {
	case "github":
		if AppConfig.GithubToken == "" {
			return fmt.Errorf("github_token is required when vcs_provider is 'github'")
		}
	case "gitlab":
		if AppConfig.GitlabToken == "" {
			return fmt.Errorf("gitlab_token is required when vcs_provider is 'gitlab'")
		}
		if AppConfig.GitlabBaseURL == "" {
			AppConfig.GitlabBaseURL = "https://gitlab.com" // 默认 GitLab 地址
		}
	default:
		return fmt.Errorf("vcs_provider must be either 'github' or 'gitlab', got: %s", AppConfig.VCSProvider)
	}

	// 行号匹配策略默认值
	if AppConfig.LineMatchStrategy == "" {
		AppConfig.LineMatchStrategy = "snippet_first" // 默认：优先使用代码片段匹配
	}

	// Review 模式默认值和验证
	if AppConfig.ReviewMode == "" {
		AppConfig.ReviewMode = "api" // 默认使用 API 模式
	}
	if AppConfig.ReviewMode != "api" && AppConfig.ReviewMode != "claude_cli" {
		return fmt.Errorf("review_mode must be either 'api' or 'claude_cli', got: %s", AppConfig.ReviewMode)
	}

	// Claude CLI 配置默认值
	if AppConfig.ClaudeCLI.BinaryPath == "" {
		AppConfig.ClaudeCLI.BinaryPath = "claude" // 默认假设 claude 在 PATH 中
	}
	if len(AppConfig.ClaudeCLI.AllowedTools) == 0 {
		AppConfig.ClaudeCLI.AllowedTools = []string{"Read", "Glob", "Grep", "Bash"}
	}
	if AppConfig.ClaudeCLI.Timeout == 0 {
		AppConfig.ClaudeCLI.Timeout = 600 // 默认 10 分钟
	}
	if AppConfig.ClaudeCLI.MaxOutputLength == 0 {
		AppConfig.ClaudeCLI.MaxOutputLength = 100000 // 默认 100KB
	}

	// 仓库克隆配置默认值
	if AppConfig.RepoClone.TempDir == "" {
		AppConfig.RepoClone.TempDir = "/tmp/pr-review-repos"
	}
	if AppConfig.RepoClone.CloneTimeout == 0 {
		AppConfig.RepoClone.CloneTimeout = 180 // 默认 3 分钟
	}
	if AppConfig.RepoClone.ShallowDepth == 0 {
		AppConfig.RepoClone.ShallowDepth = 100 // 默认深度 100
	}
	// ShallowClone 和 CleanupAfterReview 默认为 false，不需要显式设置

	return nil
}

// GetGithubToken 获取 GitHub Token
func (c *Config) GetGithubToken() string {
	return c.GithubToken
}

// GetAIConfig 获取 AI 配置
func (c *Config) GetAIConfig() (apiURL, apiKey, model, systemPrompt, userTemplate string) {
	return c.AIApiURL, c.AIApiKey, c.AIModel, c.SystemPrompt, c.UserPromptTemplate
}

// GetWebhookSecret 获取 Webhook Secret
func (c *Config) GetWebhookSecret() string {
	return c.WebhookSecret
}

// GetInlineIssueComment 是否开启行内问题评论
func (c *Config) GetInlineIssueComment() bool {
	return c.InlineIssueComment
}

// GetCommentOnlyChanges 是否只对修改的代码行评论
func (c *Config) GetCommentOnlyChanges() bool {
	return c.CommentOnlyChanges
}

// GetVCSProvider 获取 VCS Provider 类型
func (c *Config) GetVCSProvider() string {
	return c.VCSProvider
}

// GetGitlabToken 获取 GitLab Token
func (c *Config) GetGitlabToken() string {
	return c.GitlabToken
}

// GetGitlabBaseURL 获取 GitLab 实例地址
func (c *Config) GetGitlabBaseURL() string {
	return c.GitlabBaseURL
}

// GetGitlabWebhookToken 获取 GitLab Webhook Token
func (c *Config) GetGitlabWebhookToken() string {
	return c.GitlabWebhookToken
}

// GetLineMatchStrategy 获取行号匹配策略
func (c *Config) GetLineMatchStrategy() string {
	return c.LineMatchStrategy
}

// GetReviewMode 获取 Review 模式
func (c *Config) GetReviewMode() string {
	return c.ReviewMode
}

// GetClaudeCLIConfig 获取 Claude CLI 配置
func (c *Config) GetClaudeCLIConfig() ClaudeCLIConfig {
	return c.ClaudeCLI
}

// GetRepoCloneConfig 获取仓库克隆配置
func (c *Config) GetRepoCloneConfig() RepoCloneConfig {
	return c.RepoClone
}

// Claude CLI 配置的单独 getter 方法
func (c *Config) GetClaudeCLIBinaryPath() string {
	return c.ClaudeCLI.BinaryPath
}

func (c *Config) GetClaudeCLIAllowedTools() []string {
	return c.ClaudeCLI.AllowedTools
}

func (c *Config) GetClaudeCLITimeout() int {
	return c.ClaudeCLI.Timeout
}

func (c *Config) GetClaudeCLIMaxOutputLength() int {
	return c.ClaudeCLI.MaxOutputLength
}

func (c *Config) GetClaudeCLIAPIKey() string {
	return c.ClaudeCLI.APIKey
}

func (c *Config) GetClaudeCLIAPIURL() string {
	return c.ClaudeCLI.APIURL
}

func (c *Config) GetClaudeCLIModel() string {
	return c.ClaudeCLI.Model
}

func (c *Config) GetClaudeCLIIncludeOthersComments() bool {
	return c.ClaudeCLI.IncludeOthersComments
}

func (c *Config) GetClaudeCLIEnableOutputLog() bool {
	return c.ClaudeCLI.EnableOutputLog
}

// 仓库克隆配置的单独 getter 方法
func (c *Config) GetRepoCloneTempDir() string {
	return c.RepoClone.TempDir
}

func (c *Config) GetRepoCloneTimeout() int {
	return c.RepoClone.CloneTimeout
}

func (c *Config) GetRepoCloneShallowClone() bool {
	return c.RepoClone.ShallowClone
}

func (c *Config) GetRepoCloneShallowDepth() int {
	return c.RepoClone.ShallowDepth
}

func (c *Config) GetRepoCloneCleanupAfterReview() bool {
	return c.RepoClone.CleanupAfterReview
}
