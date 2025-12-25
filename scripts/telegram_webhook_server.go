package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

// Telegram Update 结构
type Update struct {
	UpdateID int     `json:"update_id"`
	Message  Message `json:"message"`
}

type Message struct {
	MessageID int    `json:"message_id"`
	From      User   `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// 发送消息到 Telegram
func sendMessage(chatID int64, text string, botToken string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	payload := map[string]interface{}{
		"chat_id": chatID,
		"text":    text,
		"parse_mode": "Markdown",
	}

	jsonData, _ := json.Marshal(payload)

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// Webhook 处理函数
func handleWebhook(w http.ResponseWriter, r *http.Request) {
	botToken := os.Getenv("TOFI_TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Println("Error: TOFI_TELEGRAM_BOT_TOKEN not set")
		http.Error(w, "Bot not configured", http.StatusInternalServerError)
		return
	}

	// 解析请求
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading body: %v", err)
		return
	}

	var update Update
	if err := json.Unmarshal(body, &update); err != nil {
		log.Printf("Error unmarshaling: %v", err)
		return
	}

	// 处理命令
	chatID := update.Message.Chat.ID
	text := update.Message.Text

	log.Printf("Received message from %d: %s", chatID, text)

	// 自动回复 Chat ID
	if text == "/start" || text == "/getid" {
		message := fmt.Sprintf(`✅ *Your Chat ID*

📋 Your Telegram Chat ID is: *%d*

🔧 *How to use:*
Add this to your Tofi workflow:

\`\`\`yaml
input:
  chat_id: "%d"
  message: "Your message here"
\`\`\`

💡 *Tip:* You don't need to provide bot_token when using the Tofi official bot!`, chatID, chatID)

		sendMessage(chatID, message, botToken)
	}

	w.WriteHeader(http.StatusOK)
}

func main() {
	http.HandleFunc("/webhook", handleWebhook)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 Telegram Webhook Server starting on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
