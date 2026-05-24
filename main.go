package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

func scanHandler(w http.ResponseWriter, r *http.Request) {
	// Разрешаем CORS, чтобы Flutter не ругался
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	// Тестовые данные для оживления интерфейса
	response := map[string]interface{}{
		"score": 85,
		"metrics": map[string]string{
			"tax":        "0%",
			"market_cap": "$1.5M",
			"holders":    "1,240",
		},
		"verdict": "Тестовый запуск прошел успешно! Бэкенд на Go подключен и готов к работе. Ждем интеграции с Claude.",
	}

	json.NewEncoder(w).Encode(response)
}

func main() {
	http.HandleFunc("/scan", scanHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Сервер TrustScan запущен на порту %s", port)
	
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}