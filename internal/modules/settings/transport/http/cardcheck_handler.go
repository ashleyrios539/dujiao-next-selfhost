package settingshttp

import (
	"context"
	"strings"

	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

// CardCheckTester 校验 CheckDx 卡密并返回剩余点数。
type CardCheckTester interface {
	Verify(ctx context.Context, kami string) (float64, error)
}

// CardCheckTestHandler 处理后台测活连接测试请求。
type CardCheckTestHandler struct {
	tester CardCheckTester
}

func NewCardCheckTestHandler(tester CardCheckTester) *CardCheckTestHandler {
	if tester == nil {
		panic("settings card check handler: tester is nil")
	}
	return &CardCheckTestHandler{tester: tester}
}

type cardCheckTestRequest struct {
	Kami string `json:"kami" binding:"required"`
}

// TestCardCheck 测试 CheckDx 卡密是否有效并返回剩余点数。
func (h *CardCheckTestHandler) TestCardCheck(c *gin.Context) {
	var req cardCheckTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	kami := strings.TrimSpace(req.Kami)
	if kami == "" {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
		return
	}

	balance, err := h.tester.Verify(c.Request.Context(), kami)
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.card_check_test_failed", err)
		return
	}
	response.Success(c, gin.H{"balance": balance})
}
