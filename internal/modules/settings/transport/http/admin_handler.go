package settingshttp

import (
	"strings"

	"github.com/dujiao-next/internal/cache"
	"github.com/dujiao-next/internal/constants"
	settingsapp "github.com/dujiao-next/internal/modules/settings/application"
	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
	ginutil "github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"
	"github.com/dujiao-next/internal/shared/jsonmap"

	"github.com/gin-gonic/gin"
)

// AdminService 是后台通用设置端口。
type AdminService interface {
	GetByKey(key string) (jsonmap.JSON, error)
	UpdateWithEffects(key string, value map[string]interface{}) (settingsapp.UpdateResult, error)
	InvalidateCallbackRoutesCache()
}

// AdminHandler 处理后台通用设置请求。
type AdminHandler struct {
	settings AdminService
}

func NewAdminHandler(settings AdminService) *AdminHandler {
	if settings == nil {
		panic("settings admin handler: settings is nil")
	}
	return &AdminHandler{settings: settings}
}

type updateRequest struct {
	Key   string                 `json:"key" binding:"required"`
	Value map[string]interface{} `json:"value" binding:"required"`
}

// Get 获取设置。
func (h *AdminHandler) Get(c *gin.Context) {
	key := c.DefaultQuery("key", constants.SettingKeySiteConfig)

	value, err := h.settings.GetByKey(key)
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.settings_fetch_failed", err)
		return
	}
	if value == nil {
		response.Success(c, gin.H{})
		return
	}

	// telegram_bot_config 含 webhook.secret_token，通用读取必须打码（专用路由已 Mask）。
	if key == constants.SettingKeyTelegramBotConfig {
		value = settingsmessaging.SanitizeTelegramBotConfigForGenericRead(value)
	}

	response.Success(c, value)
}

// Update 更新设置。
func (h *AdminHandler) Update(c *gin.Context) {
	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	if strings.TrimSpace(req.Key) == constants.SettingKeyGoogleAuthConfig {
		ginutil.RespondErrorWithMsg(
			c,
			response.CodeBadRequest,
			"google_auth_config must be updated through /admin/settings/google-auth",
			nil,
		)
		return
	}
	// telegram_bot_config 含 webhook secret，通用写入会绕过专用路由的「留空保留原值」与打码，
	// 禁止走通用路径（改为 /admin/settings/telegram-bot）。
	if strings.TrimSpace(req.Key) == constants.SettingKeyTelegramBotConfig {
		ginutil.RespondErrorWithMsg(
			c,
			response.CodeBadRequest,
			"telegram_bot_config must be updated through /admin/settings/telegram-bot",
			nil,
		)
		return
	}

	result, err := h.settings.UpdateWithEffects(req.Key, req.Value)
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.settings_save_failed", err)
		return
	}

	if result.HasEffect(settingsapp.EffectInvalidatePublicConfigCache) {
		_ = cache.DelAllPublicConfig(c.Request.Context())
	}
	if result.HasEffect(settingsapp.EffectInvalidateCallbackRoutesCache) {
		h.settings.InvalidateCallbackRoutesCache()
	}
	response.Success(c, result.Value)
}
