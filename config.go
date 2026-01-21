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
	GithubToken        string `yaml:"github_token"`
	WebhookSecret      string `yaml:"webhook_secret"`
	SystemPrompt       string `yaml:"system_prompt"`
	UserPromptTemplate string `yaml:"user_prompt_template"`
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
	if AppConfig.GithubToken == "" {
		return fmt.Errorf("github_token is required in config")
	}
	if AppConfig.SystemPrompt == "" {
		return fmt.Errorf("system_prompt is required in config")
	}
	if AppConfig.UserPromptTemplate == "" {
		return fmt.Errorf("user_prompt_template is required in config")
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
