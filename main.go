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
	router.SetWebhookSecret(AppConfig.GetWebhookSecret())

	// 注册路由
	http.HandleFunc("/", router.HandleIndex)
	http.HandleFunc("/review", router.HandleReview)
	http.HandleFunc("/webhook", router.HandleWebhook)
	http.HandleFunc("/health", router.HandleHealth)

	// 启动服务
	log.Printf("🚀 PR Review Service started on :%s", AppConfig.Port)
	log.Printf("   AI Service: %s", AppConfig.AIApiURL)
	log.Printf("   AI Model: %s", AppConfig.AIModel)

	if err := http.ListenAndServe(":"+AppConfig.Port, nil); err != nil {
		log.Fatalf("❌ Server failed to start: %v", err)
	}
}
