package webhookhttp

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/modules/telegram/webhook/contract"
	webhookdomain "github.com/dujiao-next/internal/modules/telegram/webhook/domain"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

// UpdateHandler 处理 Telegram webhook 推送。
type UpdateHandler struct {
	service     contract.Service
	secretToken string
}

// NewUpdateHandler 构造 webhook 处理器。
// secretToken 为可选的 Telegram webhook secret_token，非空时校验 X-Telegram-Bot-Api-Secret-Token。
func NewUpdateHandler(service contract.Service, secretToken string) *UpdateHandler {
	if service == nil {
		panic("telegram webhook handler: service is nil")
	}
	return &UpdateHandler{service: service, secretToken: strings.TrimSpace(secretToken)}
}

// HandleUpdate POST /api/v1/telegram/webhook
func (h *UpdateHandler) HandleUpdate(c *gin.Context) {
	if h.secretToken != "" {
		got := strings.TrimSpace(c.GetHeader("X-Telegram-Bot-Api-Secret-Token"))
		if got != h.secretToken {
			response.ErrorWithHTTPStatus(c, http.StatusUnauthorized, response.CodeUnauthorized, "error.telegram_webhook_unauthorized")
			return
		}
	}

	var update webhookdomain.Update
	if err := c.ShouldBindJSON(&update); err != nil {
		response.ErrorWithHTTPStatus(c, http.StatusBadRequest, response.CodeBadRequest, "error.telegram_webhook_invalid_payload")
		return
	}

	// 异步处理，立即返回 200 给 Telegram（避免重复推送）。
	// 注意：不能用 c.Request.Context()——它随 HTTP 请求返回而取消，异步 goroutine
	// 拿到的将是已取消的 context，导致后续 SendMessage 等 Bot API 调用全部失败。
	// 这里改用独立的 context（带超时），保证后台处理有完整生命周期。
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				logger.Errorw("telegram_webhook_panic", "recover", r, "update_id", update.UpdateID)
			}
		}()
		if err := h.service.HandleUpdate(ctx, update); err != nil {
			logger.Warnw("telegram_webhook_handle_error", "error", err, "update_id", update.UpdateID)
		}
	}()

	response.Success(c, gin.H{"ok": true})
}

// SetSecretToken 更新 secret_token（配置热更新时调用）。
func (h *UpdateHandler) SetSecretToken(secretToken string) {
	h.secretToken = strings.TrimSpace(secretToken)
}