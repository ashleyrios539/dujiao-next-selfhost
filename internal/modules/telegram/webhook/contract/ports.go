package contract

import (
	"context"

	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
	webhookdomain "github.com/dujiao-next/internal/modules/telegram/webhook/domain"
)

// BotConfigReader 读取 Telegram Bot 配置。
type BotConfigReader interface {
	GetTelegramBotConfig() (settingsmessaging.TelegramBotConfigSetting, error)
	GetTelegramBotRuntimeStatus() (settingsmessaging.TelegramBotRuntimeStatusSetting, error)
	UpdateTelegramBotRuntimeStatus(status settingsmessaging.TelegramBotRuntimeStatusSetting) error
}

// BotTokenResolver 解析当前生效的 Bot Token。
type BotTokenResolver interface {
	ResolveActiveBotToken() (string, error)
}

// BotAPIClient 是 Telegram Bot API 客户端端口。
type BotAPIClient interface {
	SendMessage(ctx context.Context, botToken, chatID, message string, options SendMessageOptions) error
	AnswerCallbackQuery(ctx context.Context, botToken, callbackID string, options AnswerCallbackOptions) error
	SetMyCommands(ctx context.Context, botToken string, commands []BotCommand) error
	GetMe(ctx context.Context, botToken string) (*BotInfo, error)
	// SetWebhook 设置 Telegram webhook（secretToken 通过 X-Telegram-Bot-Api-Secret-Token 校验）。
	SetWebhook(ctx context.Context, botToken, webhookURL, secretToken string) error
	// DeleteWebhook 删除 Telegram webhook。
	DeleteWebhook(ctx context.Context, botToken string) error
	// SendPhotoBytes 通过 sendPhoto 发送内存中的图片文件（如收款地址二维码 PNG）。
	SendPhotoBytes(ctx context.Context, botToken, chatID, fileName string, content []byte, caption string, options SendMessageOptions) error
}

// SendMessageOptions 是发送消息的可选参数。
type SendMessageOptions struct {
	ParseMode             string
	DisableWebPagePreview bool
	ReplyMarkup           interface{}
}

// AnswerCallbackOptions 是应答回调的可选参数。
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
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	UserName  string `json:"username"`
	FirstName string `json:"first_name"`
}

// Handler 处理单个 Telegram Update。
type Handler interface {
	HandleUpdate(ctx context.Context, update webhookdomain.Update) error
}

// Service 是 webhook 应用服务端口。
type Service interface {
	Handler
	// ApplyWebhook 在配置启用时设置 Telegram webhook，禁用时删除。
	ApplyWebhook(ctx context.Context, webhookURL, secretToken string) error
	// ApplyConfiguredWebhook 应用当前生效的 webhook 配置（数据库优先，config 兜底），并同步运行时密钥。
	ApplyConfiguredWebhook(ctx context.Context) error
	// CurrentSecret 返回当前生效的 webhook secret_token（用于入站请求校验；空表示不校验）。
	CurrentSecret() string
	// SyncRuntimeStatus 校验 token 并更新运行时状态。
	SyncRuntimeStatus(ctx context.Context) error
}

// PurchasePorts 聚合 bot 内购买流程所需的全部端口。
type PurchasePorts struct {
	Catalog  PurchaseCatalogReader
	Orders   PurchaseOrderGateway
	Payments PurchasePaymentGateway
	Wallet   PurchaseWalletReader
	Identity PurchaseIdentityResolver
	Settings PurchaseSettingReader
	// OrderReader 提供 bot 内订单列表/详情（含卡密）查询；nil 时「我的订单」禁用。
	OrderReader PurchaseOrderReader
	// Recharge 提供 bot 内钱包充值；nil 时充值禁用。
	Recharge PurchaseRechargeGateway
}