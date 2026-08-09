package settingshttp

import (
	"context"
	"strings"
	"time"

	"github.com/dujiao-next/internal/logger"
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
	// ApplyConfiguredWebhook 应用当前生效的 webhook 配置（数据库优先、config 兜底），
	// 由实现方内部解析，调用方无需再传 URL/secret。
	ApplyConfiguredWebhook(ctx context.Context) error
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

// effectiveWebhookURL 返回当前生效的 webhook 地址（数据库优先、config 兜底）。
func (h *TelegramBotHandler) effectiveWebhookURL(setting settingsmessaging.TelegramBotConfigSetting) string {
	if u := strings.TrimSpace(setting.Webhook.URL); u != "" {
		return u
	}
	return h.webhookURL
}

// effectiveSecretSet 返回当前生效的 webhook secret 是否已设置（数据库优先、config 兜底）。
func (h *TelegramBotHandler) effectiveSecretSet(setting settingsmessaging.TelegramBotConfigSetting) bool {
	if s := strings.TrimSpace(setting.Webhook.SecretToken); s != "" {
		return true
	}
	return strings.TrimSpace(h.secretToken) != ""
}

// GetTelegramBotConfig fetches the Telegram Bot config.
func (h *TelegramBotHandler) GetTelegramBotConfig(c *gin.Context) {
	setting, err := h.bot.GetTelegramBotConfig()
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.settings_fetch_failed", err)
		return
	}
	// webhook.url 只反映数据库值（表单编辑对象），避免把 config 兜底值误当作「已配置」；
	// effective_url / secret_set 反映当前生效值，供前端展示提示。
	response.Success(c, settingsmessaging.MaskTelegramBotConfigForAdmin(setting, h.effectiveWebhookURL(setting), h.effectiveSecretSet(setting)))
}

// UpdateTelegramBotConfig updates the Telegram Bot config (object overwrite).
func (h *TelegramBotHandler) UpdateTelegramBotConfig(c *gin.Context) {
	var req settingsmessaging.TelegramBotConfigSetting
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}

	// 网页不回显 secret_token（GET 只给 secret_set）：留空表示保留原值，避免误清空。
	// URL 同理：留空保留原值；数据库为空时继续用 config 兜底，不会把兜底值误提升进 DB
	// （否则 config.yml/.env 的后续修改会被静默忽略）。
	current, err := h.bot.GetTelegramBotConfig()
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.settings_fetch_failed", err)
		return
	}
	if strings.TrimSpace(req.Webhook.SecretToken) == "" {
		req.Webhook.SecretToken = current.Webhook.SecretToken
	}
	if strings.TrimSpace(req.Webhook.URL) == "" {
		req.Webhook.URL = current.Webhook.URL
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
			// 应用失败时记录日志并保留 set_webhook_failed 运行时状态（不被 SyncRuntimeStatus 覆盖），
			// 让管理员在「连接状态」页看到失败原因而不是误以为 active。
			if err := h.webhook.ApplyConfiguredWebhook(ctx); err != nil {
				logger.Warnw("telegram_webhook_apply_failed", "error", err)
				return
			}
			_ = h.webhook.SyncRuntimeStatus(ctx)
		}()
	}

	response.Success(c, settingsmessaging.MaskTelegramBotConfigForAdmin(setting, h.effectiveWebhookURL(setting), h.effectiveSecretSet(setting)))
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
