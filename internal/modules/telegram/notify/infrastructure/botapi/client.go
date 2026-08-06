package botapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	notifycontract "github.com/dujiao-next/internal/modules/telegram/notify/contract"
)

type telegramSendMessageResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
}

// Client é€šè¿‡ Telegram Bot API å‘é€æ¶ˆæ¯ã€‚
type Client struct {
	httpClient *http.Client
}

var _ notifycontract.Sender = (*Client)(nil)

// New åˆ›å»º Telegram Bot API å®¢æˆ·ç«¯ã€‚
func New() *Client {
	return NewWithHTTPClient(&http.Client{Timeout: 6 * time.Second})
}

// NewWithHTTPClient åˆ›å»ºä½¿ç”¨æŒ‡å®š HTTP å®¢æˆ·ç«¯çš„ Bot API å®¢æˆ·ç«¯ã€‚
func NewWithHTTPClient(client *http.Client) *Client {
	if client == nil {
		panic("telegram bot api: http client is nil")
	}
	return &Client{httpClient: client}
}

// SendWithBotToken ä½¿ç”¨æ˜¾å¼ bot token å‘é€ Telegram æ¶ˆæ¯ã€‚
func (s *Client) SendWithBotToken(ctx context.Context, botToken string, options notifycontract.SendOptions) error {
	chatID := strings.TrimSpace(options.ChatID)
	message := strings.TrimSpace(options.Message)
	botToken = strings.TrimSpace(botToken)
	if chatID == "" || message == "" || botToken == "" {
		return notifycontract.ErrNotifySendFailed
	}

	if strings.TrimSpace(options.AttachmentURL) != "" {
		attachmentURL := strings.TrimSpace(options.AttachmentURL)
		if isTelegramPhotoAttachment(attachmentURL, options.AttachmentDisplayName) {
			if filePath, ok := resolveTelegramAttachmentPath(attachmentURL); ok {
				return s.sendMultipartMedia(ctx, botToken, "sendPhoto", "photo", filePath, options)
			}
			payload := map[string]interface{}{
				"chat_id": chatID,
				"photo":   attachmentURL,
				"caption": message,
			}
			if parseMode := strings.TrimSpace(options.ParseMode); parseMode != "" {
				payload["parse_mode"] = parseMode
			}
			return s.sendJSONRequest(ctx, botToken, "sendPhoto", payload)
		}
		if filePath, ok := resolveTelegramAttachmentPath(attachmentURL); ok {
			return s.sendMultipartMedia(ctx, botToken, "sendDocument", "document", filePath, options)
		}
		payload := map[string]interface{}{
			"chat_id":  chatID,
			"document": attachmentURL,
			"caption":  message,
		}
		if parseMode := strings.TrimSpace(options.ParseMode); parseMode != "" {
			payload["parse_mode"] = parseMode
		}
		return s.sendJSONRequest(ctx, botToken, "sendDocument", payload)
	}

	payload := map[string]interface{}{
		"chat_id":                  chatID,
		"text":                     message,
		"disable_web_page_preview": options.DisableWebPagePreview,
	}
	if parseMode := strings.TrimSpace(options.ParseMode); parseMode != "" {
		payload["parse_mode"] = parseMode
	}
	return s.sendJSONRequest(ctx, botToken, "sendMessage", payload)
}

func (s *Client) sendMultipartMedia(ctx context.Context, botToken, method, fieldName, filePath string, options notifycontract.SendOptions) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("%w: open attachment failed: %v", notifycontract.ErrNotifySendFailed, err)
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("chat_id", strings.TrimSpace(options.ChatID)); err != nil {
		return err
	}
	if caption := strings.TrimSpace(options.Message); caption != "" {
		if err := writer.WriteField("caption", caption); err != nil {
			return err
		}
	}
	if parseMode := strings.TrimSpace(options.ParseMode); parseMode != "" {
		if err := writer.WriteField("parse_mode", parseMode); err != nil {
			return err
		}
	}
	part, err := writer.CreateFormFile(fieldName, filepath.Base(filePath))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	requestURL := fmt.Sprintf("https://api.telegram.org/bot%s/%s", botToken, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return s.doRequest(req)
}

// SendDocumentBytes 通过 sendDocument 发送内存中的文件内容（如卡密 txt）。
// 用于订单发货后把卡密以 txt 文件推送到用户私聊，避免超长文本消息被截断。
func (s *Client) SendDocumentBytes(ctx context.Context, botToken, chatID, fileName string, content []byte, caption string, options SendMessageOptions) error {
	chatID = strings.TrimSpace(chatID)
	botToken = strings.TrimSpace(botToken)
	fileName = strings.TrimSpace(fileName)
	if chatID == "" || botToken == "" || len(content) == 0 {
		return notifycontract.ErrNotifySendFailed
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("chat_id", chatID); err != nil {
		return err
	}
	if caption = strings.TrimSpace(caption); caption != "" {
		if err := writer.WriteField("caption", caption); err != nil {
			return err
		}
	}
	if parseMode := strings.TrimSpace(options.ParseMode); parseMode != "" {
		if err := writer.WriteField("parse_mode", parseMode); err != nil {
			return err
		}
	}
	part, err := writer.CreateFormFile("document", fileName)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, bytes.NewReader(content)); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	requestURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendDocument", botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return s.doRequest(req)
}

// SendPhotoBytes 通过 sendPhoto 发送内存中的图片文件（如收款地址二维码 PNG）。
func (s *Client) SendPhotoBytes(ctx context.Context, botToken, chatID, fileName string, content []byte, caption string, options SendMessageOptions) error {
	chatID = strings.TrimSpace(chatID)
	botToken = strings.TrimSpace(botToken)
	fileName = strings.TrimSpace(fileName)
	if chatID == "" || botToken == "" || len(content) == 0 {
		return notifycontract.ErrNotifySendFailed
	}
	if fileName == "" {
		fileName = "photo.png"
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("chat_id", chatID); err != nil {
		return err
	}
	if caption = strings.TrimSpace(caption); caption != "" {
		if err := writer.WriteField("caption", caption); err != nil {
			return err
		}
	}
	if parseMode := strings.TrimSpace(options.ParseMode); parseMode != "" {
		if err := writer.WriteField("parse_mode", parseMode); err != nil {
			return err
		}
	}
	part, err := writer.CreateFormFile("photo", fileName)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, bytes.NewReader(content)); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	requestURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendPhoto", botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return s.doRequest(req)
}

func (s *Client) sendJSONRequest(ctx context.Context, botToken, method string, payload map[string]interface{}) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	requestURL := fmt.Sprintf("https://api.telegram.org/bot%s/%s", botToken, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return s.doRequest(req)
}

func (s *Client) doRequest(req *http.Request) error {
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", notifycontract.ErrNotifySendFailed, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%w: %v", notifycontract.ErrNotifySendFailed, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: telegram status=%d body=%s", notifycontract.ErrNotifySendFailed, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed telegramSendMessageResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("%w: parse telegram response failed", notifycontract.ErrNotifySendFailed)
	}
	if !parsed.OK {
		return fmt.Errorf("%w: %s", notifycontract.ErrNotifySendFailed, strings.TrimSpace(parsed.Description))
	}
	return nil
}

func resolveTelegramAttachmentPath(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", false
	}
	parsed, err := url.Parse(value)
	if err == nil && parsed != nil && parsed.Scheme != "" {
		return "", false
	}

	normalized := strings.TrimPrefix(value, "/")
	normalized = filepath.Clean(normalized)
	if normalized == "." || normalized == "" {
		return "", false
	}
	if normalized == "uploads" || strings.HasPrefix(normalized, "uploads"+string(filepath.Separator)) {
		return normalized, true
	}
	return "", false
}

func isTelegramPhotoAttachment(rawURL, displayName string) bool {
	candidates := []string{
		strings.TrimSpace(displayName),
		strings.TrimSpace(rawURL),
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		value := candidate
		if parsed, err := url.Parse(candidate); err == nil && parsed != nil {
			if parsed.Path != "" {
				value = parsed.Path
			}
		}
		ext := strings.ToLower(strings.TrimSpace(filepath.Ext(value)))
		switch ext {
		case ".jpg", ".jpeg", ".png", ".webp":
			return true
		}
		if ext == ".gif" {
			return true
		}
		if detected := mime.TypeByExtension(ext); strings.HasPrefix(strings.ToLower(detected), "image/") {
			return true
		}
	}

	return false
}



// --- 原生 Webhook 支持（不依赖外部 licensed 客户端） ---

// SetWebhook 设置 Telegram webhook。
func (s *Client) SetWebhook(ctx context.Context, botToken, webhookURL string, secretToken string) error {
	botToken = strings.TrimSpace(botToken)
	whURL := strings.TrimSpace(webhookURL)
	if botToken == "" || whURL == "" {
		return notifycontract.ErrNotifySendFailed
	}
	payload := map[string]interface{}{
		"url":            whURL,
		"allowed_updates": []string{"message", "callback_query", "inline_query"},
		"drop_pending_updates": true,
	}
	if strings.TrimSpace(secretToken) != "" {
		payload["secret_token"] = strings.TrimSpace(secretToken)
	}
	return s.sendJSONRequest(ctx, botToken, "setWebhook", payload)
}

// DeleteWebhook 删除 Telegram webhook。
func (s *Client) DeleteWebhook(ctx context.Context, botToken string) error {
	botToken = strings.TrimSpace(botToken)
	if botToken == "" {
		return notifycontract.ErrNotifySendFailed
	}
	return s.sendJSONRequest(ctx, botToken, "deleteWebhook", map[string]interface{}{
		"drop_pending_updates": true,
	})
}

// AnswerCallbackQuery 应答回调查询（菜单按钮点击）。
func (s *Client) AnswerCallbackQuery(ctx context.Context, botToken, callbackID string, options AnswerCallbackOptions) error {
	botToken = strings.TrimSpace(botToken)
	callbackID = strings.TrimSpace(callbackID)
	if botToken == "" || callbackID == "" {
		return notifycontract.ErrNotifySendFailed
	}
	payload := map[string]interface{}{
		"callback_query_id": callbackID,
	}
	text := strings.TrimSpace(options.Text)
	if text != "" {
		payload["text"] = text
	}
	if options.ShowAlert {
		payload["show_alert"] = true
	}
	if url := strings.TrimSpace(options.URL); url != "" {
		payload["url"] = url
	}
	return s.sendJSONRequest(ctx, botToken, "answerCallbackQuery", payload)
}

// SetMyCommands 设置 Bot 菜单命令。
func (s *Client) SetMyCommands(ctx context.Context, botToken string, commands []BotCommand) error {
	botToken = strings.TrimSpace(botToken)
	if botToken == "" {
		return notifycontract.ErrNotifySendFailed
	}
	if len(commands) == 0 {
		return nil
	}
	payload := map[string]interface{}{
		"commands": commands,
	}
	return s.sendJSONRequest(ctx, botToken, "setMyCommands", payload)
}

// GetMe 获取 Bot 信息，用于启动时校验 token 是否有效。
func (s *Client) GetMe(ctx context.Context, botToken string) (*BotInfo, error) {
	botToken = strings.TrimSpace(botToken)
	if botToken == "" {
		return nil, notifycontract.ErrNotifySendFailed
	}
	requestURL := fmt.Sprintf("https://api.telegram.org/bot%s/getMe", botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", notifycontract.ErrNotifySendFailed, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", notifycontract.ErrNotifySendFailed, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: telegram status=%d body=%s", notifycontract.ErrNotifySendFailed, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed struct {
		OK     bool     `json:"ok"`
		Result BotInfo  `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("%w: parse getMe response failed", notifycontract.ErrNotifySendFailed)
	}
	if !parsed.OK {
		return nil, notifycontract.ErrNotifySendFailed
	}
	return &parsed.Result, nil
}

// SendMessage 发送纯文本消息（带可选内联键盘）。
func (s *Client) SendMessage(ctx context.Context, botToken, chatID, message string, options SendMessageOptions) error {
	chatID = strings.TrimSpace(chatID)
	message = strings.TrimSpace(message)
	botToken = strings.TrimSpace(botToken)
	if chatID == "" || message == "" || botToken == "" {
		return notifycontract.ErrNotifySendFailed
	}
	payload := map[string]interface{}{
		"chat_id":                  chatID,
		"text":                     message,
		"disable_web_page_preview": options.DisableWebPagePreview,
	}
	if parseMode := strings.TrimSpace(options.ParseMode); parseMode != "" {
		payload["parse_mode"] = parseMode
	}
	if options.ReplyMarkup != nil {
		markupBytes, err := json.Marshal(options.ReplyMarkup)
		if err != nil {
			return fmt.Errorf("%w: marshal reply markup failed: %v", notifycontract.ErrNotifySendFailed, err)
		}
		var raw interface{}
		if err := json.Unmarshal(markupBytes, &raw); err == nil {
			payload["reply_markup"] = raw
		}
	}
	return s.sendJSONRequest(ctx, botToken, "sendMessage", payload)
}

// AnswerCallbackOptions 是 answerCallbackQuery 的可选参数。
type AnswerCallbackOptions struct {
	Text      string
	ShowAlert bool
	URL       string
}

// BotCommand 对应 Telegram setMyCommands 的单个命令。
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// BotInfo 对应 getMe 返回的 Bot 基本信息。
type BotInfo struct {
	ID       int64  `json:"id"`
	IsBot    bool   `json:"is_bot"`
	UserName string `json:"username"`
	FirstName string `json:"first_name"`
}

// SendMessageOptions 是 SendMessage 的可选参数。
type SendMessageOptions struct {
	ParseMode             string
	DisableWebPagePreview bool
	ReplyMarkup           interface{}
}