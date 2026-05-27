package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// ───────────────────────────────────────────────
// СТРУКТУРЫ ОТВЕТОВ
// ───────────────────────────────────────────────

// Финальный ответ фронтенду (ключи совпадают с Flutter-кодом!)
type ScanResponse struct {
	Status        string   `json:"status"`         // "safe" | "warning" | "danger"
	SecurityScore int      `json:"security_score"` // 0–100
	TokenSymbol   string   `json:"token_symbol"`
	Message       string   `json:"message"`    // AI вердикт
	Tax           string   `json:"tax"`        // "1.5"
	MarketCap     string   `json:"market_cap"` // "$450K"
	Holders       string   `json:"holders"`    // "1.2K"
	Flags         []string `json:"flags"`      // ["honeypot", "high_tax"]
}

// GoPlusLabs — структура нужных полей
type GoPlusToken struct {
	IsHoneypot  string `json:"is_honeypot"`
	BuyTax      string `json:"buy_tax"`
	SellTax     string `json:"sell_tax"`
	HolderCount string `json:"holder_count"`
	LpHolders   []struct {
		IsLocked int `json:"is_locked"`
	} `json:"lp_holders"`
	OwnerAddress         string `json:"owner_address"`
	CreatorAddress       string `json:"creator_address"`
	IsOpenSource         string `json:"is_open_source"`
	CanTakeBackOwnership string `json:"can_take_back_ownership"`
	HiddenOwner          string `json:"hidden_owner"`
	TokenName            string `json:"token_name"`
	TokenSymbol          string `json:"token_symbol"`
}

// DexScreener
type DexScreenerResponse struct {
	Pairs []struct {
		FDV         float64 `json:"fdv"`
		PairAddress string  `json:"pairAddress"`
		BaseToken   struct {
			Symbol string `json:"symbol"`
			Name   string `json:"name"`
		} `json:"baseToken"`
	} `json:"pairs"`
}

// ───────────────────────────────────────────────
// MAIN
// ───────────────────────────────────────────────

func main() {
	http.HandleFunc("/scan", corsMiddleware(scanHandler))
	http.HandleFunc("/health", corsMiddleware(healthHandler))
	http.HandleFunc("/mentor", corsMiddleware(mentorHandler))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("✅ TrustScan сервер запущен на порту %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

// ───────────────────────────────────────────────
// CORS MIDDLEWARE
// ───────────────────────────────────────────────

func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

// ───────────────────────────────────────────────
// HEALTH CHECK
// ───────────────────────────────────────────────

func healthHandler(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// ───────────────────────────────────────────────
// ОСНОВНОЙ ОБРАБОТЧИК /scan
// ───────────────────────────────────────────────

func scanHandler(w http.ResponseWriter, r *http.Request) {
	address := strings.TrimSpace(r.URL.Query().Get("address"))
	chain := r.URL.Query().Get("chain")
	if chain == "" {
		chain = "1" // Ethereum по умолчанию
	}

	// Базовая валидация
	if len(address) != 42 || !strings.HasPrefix(address, "0x") {
		http.Error(w, `{"error":"invalid address"}`, http.StatusBadRequest)
		return
	}

	log.Printf("🔍 Сканируем: %s (chain %s)", address, chain)

	// ─── 1. GoPlusLabs ───
	gpToken, err := fetchGoPlusData(address, chain)
	if err != nil {
		log.Printf("⚠️  GoPlusLabs ошибка: %v", err)
	}

	// ─── 2. DexScreener ───
	dexData, err := fetchDexScreener(address)
	if err != nil {
		log.Printf("⚠️  DexScreener ошибка: %v", err)
	}

	// ─── 3. Считаем score и флаги ───
	score, flags := calculateScore(gpToken)

	// ─── 4. Формируем метрики ───
	tax := extractTax(gpToken)
	holders := formatHolders(gpToken)
	marketCap := formatMarketCap(dexData)
	symbol := extractSymbol(gpToken, dexData, address)

	// ─── 5. Статус ───
	status := scoreToStatus(score)

	// ─── 6. Claude AI вердикт ───
	verdict := generateVerdict(symbol, score, tax, holders, flags, status)

	// ─── 7. Финальный ответ ───
	resp := ScanResponse{
		Status:        status,
		SecurityScore: score,
		TokenSymbol:   symbol,
		Message:       verdict,
		Tax:           tax,
		MarketCap:     marketCap,
		Holders:       holders,
		Flags:         flags,
	}

	json.NewEncoder(w).Encode(resp)
}

// ───────────────────────────────────────────────
// GOPLUS LABS
// ───────────────────────────────────────────────

func fetchGoPlusData(address, chain string) (*GoPlusToken, error) {
	url := fmt.Sprintf("https://api.gopluslabs.io/api/v1/token_security/%s?contract_addresses=%s", chain, address)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Code   int                     `json:"code"`
		Result map[string]*GoPlusToken `json:"result"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("json parse: %w", err)
	}

	addrLower := strings.ToLower(address)
	for key, token := range result.Result {
		if strings.ToLower(key) == addrLower {
			return token, nil
		}
	}

	return nil, fmt.Errorf("адрес не найден в ответе GoPlusLabs")
}

// ───────────────────────────────────────────────
// DEXSCREENER
// ───────────────────────────────────────────────

func fetchDexScreener(address string) (*DexScreenerResponse, error) {
	url := fmt.Sprintf("https://api.dexscreener.com/latest/dex/tokens/%s", address)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var dex DexScreenerResponse
	if err := json.NewDecoder(resp.Body).Decode(&dex); err != nil {
		return nil, err
	}

	return &dex, nil
}

// ───────────────────────────────────────────────
// ПОДСЧЁТ SCORE И ФЛАГОВ
// ───────────────────────────────────────────────

func calculateScore(t *GoPlusToken) (int, []string) {
	score := 100
	var flags []string

	if t == nil {
		return 50, []string{"data_unavailable"}
	}

	// Honeypot — самый критичный флаг (-50)
	if t.IsHoneypot == "1" {
		score -= 50
		flags = append(flags, "honeypot")
	}

	// Налог на покупку
	if buyTax, err := strconv.ParseFloat(t.BuyTax, 64); err == nil {
		if buyTax > 10 {
			score -= 20
			flags = append(flags, "high_buy_tax")
		} else if buyTax > 5 {
			score -= 10
			flags = append(flags, "moderate_buy_tax")
		}
	}

	// Налог на продажу
	if sellTax, err := strconv.ParseFloat(t.SellTax, 64); err == nil {
		if sellTax > 10 {
			score -= 20
			flags = append(flags, "high_sell_tax")
		} else if sellTax > 5 {
			score -= 10
			flags = append(flags, "moderate_sell_tax")
		}
	}

	// Нет открытого исходного кода (-10)
	if t.IsOpenSource == "0" {
		score -= 10
		flags = append(flags, "closed_source")
	}

	// Скрытый владелец (-15)
	if t.HiddenOwner == "1" {
		score -= 15
		flags = append(flags, "hidden_owner")
	}

	// Владелец может забрать назад ownership (-10)
	if t.CanTakeBackOwnership == "1" {
		score -= 10
		flags = append(flags, "recoverable_ownership")
	}

	// Ликвидность заблокирована? Бонус
	lpLocked := false
	for _, lp := range t.LpHolders {
		if lp.IsLocked == 1 {
			lpLocked = true
			break
		}
	}
	if lpLocked {
		flags = append(flags, "lp_locked") // Положительный флаг
	} else {
		score -= 10
		flags = append(flags, "lp_not_locked")
	}

	// Нет холдеров — подозрительно
	if t.HolderCount == "0" || t.HolderCount == "" {
		score -= 5
		flags = append(flags, "no_holders")
	}

	if score < 0 {
		score = 0
	}

	return score, flags
}

// ───────────────────────────────────────────────
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ
// ───────────────────────────────────────────────

func scoreToStatus(score int) string {
	switch {
	case score >= 70:
		return "safe"
	case score >= 40:
		return "warning"
	default:
		return "danger"
	}
}

func extractTax(t *GoPlusToken) string {
	if t == nil {
		return "N/A"
	}
	buy, _ := strconv.ParseFloat(t.BuyTax, 64)
	sell, _ := strconv.ParseFloat(t.SellTax, 64)
	avg := (buy + sell) / 2
	return fmt.Sprintf("%.1f", avg)
}

func formatHolders(t *GoPlusToken) string {
	if t == nil || t.HolderCount == "" {
		return "N/A"
	}
	count, err := strconv.Atoi(t.HolderCount)
	if err != nil {
		return t.HolderCount
	}
	if count >= 1000 {
		return fmt.Sprintf("%.1fK", float64(count)/1000)
	}
	return strconv.Itoa(count)
}

func formatMarketCap(dex *DexScreenerResponse) string {
	if dex == nil || len(dex.Pairs) == 0 {
		return "N/A"
	}
	fdv := dex.Pairs[0].FDV
	switch {
	case fdv >= 1_000_000_000:
		return fmt.Sprintf("$%.1fB", fdv/1_000_000_000)
	case fdv >= 1_000_000:
		return fmt.Sprintf("$%.1fM", fdv/1_000_000)
	case fdv >= 1_000:
		return fmt.Sprintf("$%.1fK", fdv/1_000)
	default:
		return fmt.Sprintf("$%.0f", fdv)
	}
}

func extractSymbol(gp *GoPlusToken, dex *DexScreenerResponse, address string) string {
	if gp != nil && gp.TokenSymbol != "" {
		return gp.TokenSymbol
	}
	if dex != nil && len(dex.Pairs) > 0 {
		return dex.Pairs[0].BaseToken.Symbol
	}
	// Короткая версия адреса как fallback
	return address[:6] + "..."
}

// ───────────────────────────────────────────────
// CLAUDE AI ВЕРДИКТ
// ───────────────────────────────────────────────

func generateVerdict(symbol string, score int, tax, holders string, flags []string, status string) string {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")

	// Если ключа нет — возвращаем автоматический вердикт
	if apiKey == "" {
		return buildFallbackVerdict(symbol, score, status, flags)
	}

	flagsStr := strings.Join(flags, ", ")
	if flagsStr == "" {
		flagsStr = "нет"
	}

	prompt := fmt.Sprintf(`Ты эксперт по безопасности DeFi токенов. Проанализируй токен и дай краткий вердикт (2-3 предложения) на русском языке.

Токен: $%s
Оценка безопасности: %d/100
Статус: %s
Налог: %s%%
Держатели: %s
Флаги рисков: %s

Вердикт должен быть конкретным, профессиональным и полезным для инвестора.`,
		symbol, score, status, tax, holders, flagsStr)

	payload := map[string]interface{}{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 300,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(body))
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Claude API ошибка: %v", err)
		return buildFallbackVerdict(symbol, score, status, flags)
	}
	defer resp.Body.Close()

	var claudeResp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&claudeResp); err != nil || len(claudeResp.Content) == 0 {
		return buildFallbackVerdict(symbol, score, status, flags)
	}

	return claudeResp.Content[0].Text
}

// Автовердикт без Claude API
func buildFallbackVerdict(symbol string, score int, status string, flags []string) string {
	hasHoneypot := contains(flags, "honeypot")
	hasHiddenOwner := contains(flags, "hidden_owner")
	lpLocked := contains(flags, "lp_locked")

	switch {
	case hasHoneypot:
		return fmt.Sprintf("⚠️ ВНИМАНИЕ: Токен $%s идентифицирован как honeypot. Продажа токена заблокирована контрактом. Инвестиции категорически не рекомендуются.", symbol)
	case score >= 80:
		lpText := ""
		if lpLocked {
			lpText = " Ликвидность заблокирована — дополнительная защита от rug pull."
		}
		return fmt.Sprintf("Токен $%s демонстрирует высокий уровень безопасности (оценка %d/100). Контракт верифицирован, критических уязвимостей не обнаружено.%s", symbol, score, lpText)
	case score >= 50:
		ownerText := ""
		if hasHiddenOwner {
			ownerText = " Обнаружен скрытый владелец — рекомендуется дополнительная проверка."
		}
		return fmt.Sprintf("Токен $%s имеет умеренный профиль риска (оценка %d/100). Обнаружены незначительные предупреждения.%s Инвестируйте осторожно.", symbol, score, ownerText)
	default:
		return fmt.Sprintf("🔴 ВЫСОКИЙ РИСК: Токен $%s получил низкую оценку безопасности (%d/100). Обнаружены серьёзные уязвимости: %s. Инвестиции не рекомендуются.", symbol, score, strings.Join(flags, ", "))
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
