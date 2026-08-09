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
	service contract.Service
}

// NewUpdateHandler 构造 webhook 处理器。
// secret_token 从 service.CurrentSecret() 实时读取：后台保存 webhook 配置后立即热更新，无需重启。
func NewUpdateHandler(service contract.Service) *UpdateHandler {
	if service == nil {
		panic("telegram webhook handler: service is nil")
	}
	return &UpdateHandler{service: service}
}

// HandleUpdate POST /api/v1/telegram/webhook
func (h *UpdateHandler) HandleUpdate(c *gin.Context) {
	if secret := strings.TrimSpace(h.service.CurrentSecret()); secret != "" {
		got := strings.TrimSpace(c.GetHeader("X-Telegram-Bot-Api-Secret-Token"))
		if got != secret {
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