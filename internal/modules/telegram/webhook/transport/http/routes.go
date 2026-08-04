package webhookhttp

import "github.com/gin-gonic/gin"

// RegisterWebhookRoutes 注册 Telegram 原生 webhook 路由（公开端点，由 secret_token 校验）。
func RegisterWebhookRoutes(public gin.IRoutes, handler *UpdateHandler) {
	public.POST("/telegram/webhook", handler.HandleUpdate)
}