package settingshttp

import (
	"context"
	"time"

	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
	ginutil "github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

// TelegramBotAdminService is the admin Telegram Bot settings port.
type TelegramBotAdminService interface {
	GetTelegramBotConfig() (settingsmessaging.TelegramBotConfigSetting, error)
	UpdateTelegramBotConfig(cfg settingsmessaging.TelegramBotConfigSetting) (settingsmessaging.TelegramBotConfigSetting, error)
	GetTelegramBotRuntimeStatus() (settingsmessaging.TelegramBotRuntimeStatusSetting, error)
}

// WebhookReconciler optionally re-applies the Telegram webhook after config changes.
// Implementations (e.g. the native webhook service) may be nil to disable auto-sync.
type WebhookReconciler interface {
	ApplyWebhook(ctx context.Context, webhookURL, secretToken string) error
	SyncRuntimeStatus(ctx context.Context) error
}

// TelegramBotHandler handles admin Telegram Bot settings requests.
type TelegramBotHandler struct {
	bot         TelegramBotAdminService
	webhook     WebhookReconciler
	webhookURL  string
	secretToken string
}

// TelegramBotHandlerOption configures a TelegramBotHandler.
type TelegramBotHandlerOption func(*TelegramBotHandler)

// WithWebhookReconciler injects a native webhook reconciler and its public URL/secret.
func WithWebhookReconciler(reconciler WebhookReconciler, webhookURL, secretToken string) TelegramBotHandlerOption {
	return func(h *TelegramBotHandler) {
		h.webhook = reconciler
		h.webhookURL = webhookURL
		h.secretToken = secretToken
	}
}

func NewTelegramBotHandler(bot TelegramBotAdminService, options ...TelegramBotHandlerOption) *TelegramBotHandler {
	if bot == nil {
		panic("settings telegram-bot handler: bot is nil")
	}
	h := &TelegramBotHandler{bot: bot}
	for _, opt := range options {
		opt(h)
	}
	return h
}

// GetTelegramBotConfig fetches the Telegram Bot config.
func (h *TelegramBotHandler) GetTelegramBotConfig(c *gin.Context) {
	setting, err := h.bot.GetTelegramBotConfig()
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.settings_fetch_failed", err)
		return
	}
	response.Success(c, settingsmessaging.MaskTelegramBotConfigForAdmin(setting))
}

// UpdateTelegramBotConfig updates the Telegram Bot config (object overwrite).
func (h *TelegramBotHandler) UpdateTelegramBotConfig(c *gin.Context) {
	var req settingsmessaging.TelegramBotConfigSetting
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}

	setting, err := h.bot.UpdateTelegramBotConfig(req)
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.settings_save_failed", err)
		return
	}

	if h.webhook != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = h.webhook.ApplyWebhook(ctx, h.webhookURL, h.secretToken)
			_ = h.webhook.SyncRuntimeStatus(ctx)
		}()
	}

	response.Success(c, settingsmessaging.MaskTelegramBotConfigForAdmin(setting))
}

// GetTelegramBotRuntimeStatus fetches the Telegram Bot runtime status.
func (h *TelegramBotHandler) GetTelegramBotRuntimeStatus(c *gin.Context) {
	status, err := h.bot.GetTelegramBotRuntimeStatus()
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.settings_fetch_failed", err)
		return
	}
	response.Success(c, status)
}
