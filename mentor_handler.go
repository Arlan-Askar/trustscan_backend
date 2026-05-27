package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

func mentorHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
		http.Error(w, `{"error":"empty message"}`, http.StatusBadRequest)
		return
	}

	log.Printf("🤖 Mentor: %s", req.Message)

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		json.NewEncoder(w).Encode(map[string]string{
			"reply": "Бэкенд работает! GEMINI_API_KEY не настроен — добавь ключ в переменные окружения.",
		})
		return
	}

	ctx := context.Background()

	// 1. Создаем клиент через стандартный endpoint, но БЕЗ указания v1 в конце URL
	client, err := genai.NewClient(ctx,
		option.WithAPIKey(apiKey),
		option.WithEndpoint("https://generativelanguage.googleapis.com"),
	)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"reply": "Ошибка ИИ: " + err.Error()})
		return
	}
	defer client.Close()

	// 2. Создаем модель без лишних аргументов (клиент сам подставит нужные пути)
	model := client.GenerativeModel("gemini-1.5-flash")
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{
			genai.Text("Ты — AI Ментор TrustScan. Объясняй риски смарт-контрактов простым языком, давай краткие советы. Отвечай на русском, дружелюбно и ёмко."),
		},
	}

	resp, err := model.GenerateContent(ctx, genai.Text(req.Message))
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"reply": "Ошибка генерации: " + err.Error()})
		return
	}

	reply := "Не удалось получить ответ."
	if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
		if txt, ok := resp.Candidates[0].Content.Parts[0].(genai.Text); ok {
			reply = string(txt)
		}
	}

	json.NewEncoder(w).Encode(map[string]string{"reply": reply})
}
