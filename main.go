package main

import (
	"log"
	"net/http"
	"pr-review/lib"
	"pr-review/router"
	"time"
)

func main() {
	// 加载配置文件
	if err := LoadConfig("config.yaml"); err != nil {
		log.Fatalf("❌ Configuration error: %v", err)
	}

	// 设置路由器的配置
	router.SetConfig(&AppConfig)

	// 注册通用路由
	http.HandleFunc("/", router.HandleIndex)
	http.HandleFunc("/review", router.HandleReview)
	http.HandleFunc("/health", router.HandleHealth)

	// 根据 VCS Provider 注册对应的 webhook 处理器
	switch AppConfig.VCSProvider {
	case "github":
		router.SetWebhookSecret(AppConfig.GetWebhookSecret())
		http.HandleFunc("/webhook", router.HandleWebhook)
		log.Printf("🔧 VCS Provider: GitHub")
	case "gitlab":
		router.SetGitLabWebhookToken(AppConfig.GetGitlabWebhookToken())
		http.HandleFunc("/webhook", router.HandleGitLabWebhook)
		log.Printf("🔧 VCS Provider: GitLab (%s)", AppConfig.GitlabBaseURL)
	default:
		log.Fatalf("❌ Unsupported VCS provider: %s", AppConfig.VCSProvider)
	}

	// 启动定期清理任务（如果使用需要克隆仓库的 CLI 模式）
	if AppConfig.ReviewMode == "claude_cli" || AppConfig.ReviewMode == "codex" {
		startCleanupTask()
	}

	// 启动服务
	log.Printf("🚀 PR Review Service started on :%s", AppConfig.Port)
	log.Printf("   AI Service: %s", AppConfig.AIApiURL)
	log.Printf("   AI Model: %s", AppConfig.AIModel)
	log.Printf("   Review Mode: %s", AppConfig.ReviewMode)

	if err := http.ListenAndServe(":"+AppConfig.Port, nil); err != nil {
		log.Fatalf("❌ Server failed to start: %v", err)
	}
}

// startCleanupTask 启动定期清理任务
func startCleanupTask() {
	repoManager := lib.NewRepoManager(
		AppConfig.RepoClone.TempDir,
		AppConfig.RepoClone.CloneTimeout,
		AppConfig.RepoClone.ShallowClone,
		AppConfig.RepoClone.ShallowDepth,
	)

	// 立即执行一次清理
	go func() {
		log.Printf("🧹 Running initial cleanup task...")
		if err := repoManager.CleanupOldRepos(24 * time.Hour); err != nil {
			log.Printf("⚠️ Cleanup task failed: %v", err)
		}
	}()

	// 启动定期清理（每小时执行一次）
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		log.Printf("🧹 Cleanup task started (runs every 1 hour)")

		for range ticker.C {
			// 清理超过 24 小时的仓库
			if err := repoManager.CleanupOldRepos(24 * time.Hour); err != nil {
				log.Printf("⚠️ Cleanup task failed: %v", err)
			}
		}
	}()
}
