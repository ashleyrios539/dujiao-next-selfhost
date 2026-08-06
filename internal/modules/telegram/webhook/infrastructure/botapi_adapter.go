package webhookinfra

import (
	"context"

	notifybotapi "github.com/dujiao-next/internal/modules/telegram/notify/infrastructure/botapi"
	"github.com/dujiao-next/internal/modules/telegram/webhook/contract"
)

// BotAPIAdapter 将 notify 模块的 botapi.Client 适配为 webhook 模块所需的 BotAPIClient 端口。
type BotAPIAdapter struct {
	client *notifybotapi.Client
}

// NewBotAPIAdapter 构造适配器。
func NewBotAPIAdapter(client *notifybotapi.Client) *BotAPIAdapter {
	if client == nil {
		panic("webhook bot api adapter: client is nil")
	}
	return &BotAPIAdapter{client: client}
}

// SendMessage 发送纯文本消息。
func (a *BotAPIAdapter) SendMessage(ctx context.Context, botToken, chatID, message string, options contract.SendMessageOptions) error {
	return a.client.SendMessage(ctx, botToken, chatID, message, notifybotapi.SendMessageOptions{
		ParseMode:             options.ParseMode,
		DisableWebPagePreview: options.DisableWebPagePreview,
		ReplyMarkup:           options.ReplyMarkup,
	})
}

// AnswerCallbackQuery 应答回调查询。
func (a *BotAPIAdapter) AnswerCallbackQuery(ctx context.Context, botToken, callbackID string, options contract.AnswerCallbackOptions) error {
	return a.client.AnswerCallbackQuery(ctx, botToken, callbackID, notifybotapi.AnswerCallbackOptions{
		Text:      options.Text,
		ShowAlert: options.ShowAlert,
		URL:       options.URL,
	})
}

// SetMyCommands 设置 Bot 菜单命令。
func (a *BotAPIAdapter) SetMyCommands(ctx context.Context, botToken string, commands []contract.BotCommand) error {
	converted := make([]notifybotapi.BotCommand, 0, len(commands))
	for _, cmd := range commands {
		converted = append(converted, notifybotapi.BotCommand{
			Command:     cmd.Command,
			Description: cmd.Description,
		})
	}
	return a.client.SetMyCommands(ctx, botToken, converted)
}

// GetMe 获取 Bot 信息。
func (a *BotAPIAdapter) GetMe(ctx context.Context, botToken string) (*contract.BotInfo, error) {
	info, err := a.client.GetMe(ctx, botToken)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, contract.ErrTokenUnavailable
	}
	return &contract.BotInfo{
		ID:        info.ID,
		IsBot:     info.IsBot,
		UserName:  info.UserName,
		FirstName: info.FirstName,
	}, nil
}

// SetWebhook 设置 Telegram webhook。
func (a *BotAPIAdapter) SetWebhook(ctx context.Context, botToken, webhookURL, secretToken string) error {
	return a.client.SetWebhook(ctx, botToken, webhookURL, secretToken)
}

// DeleteWebhook 删除 Telegram webhook。
func (a *BotAPIAdapter) DeleteWebhook(ctx context.Context, botToken string) error {
	return a.client.DeleteWebhook(ctx, botToken)
}

// SendPhotoBytes 通过 sendPhoto 发送内存中的图片文件。
func (a *BotAPIAdapter) SendPhotoBytes(ctx context.Context, botToken, chatID, fileName string, content []byte, caption string, options contract.SendMessageOptions) error {
	return a.client.SendPhotoBytes(ctx, botToken, chatID, fileName, content, caption, notifybotapi.SendMessageOptions{
		ParseMode:             options.ParseMode,
		DisableWebPagePreview: options.DisableWebPagePreview,
		ReplyMarkup:           options.ReplyMarkup,
	})
}