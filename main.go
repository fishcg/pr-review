package main

import (
	"log"
	"net/http"
	"pr-review/router"
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

	// 启动服务
	log.Printf("🚀 PR Review Service started on :%s", AppConfig.Port)
	log.Printf("   AI Service: %s", AppConfig.AIApiURL)
	log.Printf("   AI Model: %s", AppConfig.AIModel)

	if err := http.ListenAndServe(":"+AppConfig.Port, nil); err != nil {
		log.Fatalf("❌ Server failed to start: %v", err)
	}
}
