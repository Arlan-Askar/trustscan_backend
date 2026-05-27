package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// Структура для входящего запроса от Flutter
type MentorRequest struct {
	Message string `json:"message"`
}

// Структура для ответа пользователю
type MentorResponse struct {
	Reply string `json:"reply"`
}

func mentorHandler(w http.ResponseWriter, r *http.Request) {
	// Проверяем, что запрос именно POST
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Декодируем входящий JSON
	var req MentorRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Достаем API ключ из системы
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		http.Error(w, "GEMINI_API_KEY is not set on server", http.StatusInternalServerError)
		return
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer client.Close()

	model := client.GenerativeModel("gemini-1.5-flash")

	// Задаем характер нашего ментора
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{
			genai.Text("Ты — AI Ментор в приложении TrustScan. Твоя цель — оценивать безопасность смарт-контрактов и отвечать на вопросы о крипте. Пиши в уверенном, экспертном, но простом стиле, используя понятный язык. Общайся дружелюбно, как опытный наставник."),
		},
	}

	// Отправляем запрос в Gemini
	resp, err := model.GenerateContent(ctx, genai.Text(req.Message))
	if err != nil {
		http.Error(w, "AI generation failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Извлекаем ответ
	var replyText string = "Не удалось получить ответ от AI."
	if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
		if txt, ok := resp.Candidates[0].Content.Parts[0].(genai.Text); ok {
			replyText = string(txt)
		}
	}

	// Отправляем JSON обратно во Flutter
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MentorResponse{Reply: replyText})
}
