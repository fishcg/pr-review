package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 配置结构
type Config struct {
	AIApiURL           string `yaml:"ai_api_url"`
	AIApiKey           string `yaml:"ai_api_key"`
	AIModel            string `yaml:"ai_model"`
	Port               string `yaml:"port"`
	SystemPrompt       string `yaml:"system_prompt"`
	UserPromptTemplate string `yaml:"user_prompt_template"`
	InlineIssueComment bool   `yaml:"inline_issue_comment"`

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
