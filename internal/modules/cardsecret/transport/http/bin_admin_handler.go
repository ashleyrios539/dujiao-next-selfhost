package cardsecrethttp

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	cardsecretapp "github.com/dujiao-next/internal/modules/cardsecret/application"
	cardsecretcontract "github.com/dujiao-next/internal/modules/cardsecret/contract"
	cardsecretdomain "github.com/dujiao-next/internal/modules/cardsecret/domain"
	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

// BinService BIN 库管理端口。
type BinService interface {
	ImportCardBins(cardsecretapp.ImportCardBinsInput) (*cardsecretapp.ImportCardBinsResult, error)
	GetBinStats() (*cardsecretapp.BinStats, error)
	ListCardBins(cardsecretcontract.CardBinFilter) ([]cardsecretdomain.CardBin, int64, error)
	ClearCardBins() error
}

// BinAdminHandler 管理 BIN 库。
type BinAdminHandler struct {
	service BinService
}

// NewBinAdminHandler 创建 BIN 库管理处理器。
func NewBinAdminHandler(service BinService) *BinAdminHandler {
	if service == nil {
		panic("card bin admin handler: required dependency is nil")
	}
	return &BinAdminHandler{service: service}
}

// RegisterBinRoutes 注册 BIN 库管理路由。
func RegisterBinRoutes(admin gin.IRoutes, handler *BinAdminHandler) {
	if admin == nil || handler == nil {
		panic("card bin admin routes: required dependency is nil")
	}
	admin.POST("/card-bins/import", handler.ImportCardBins)
	admin.GET("/card-bins/stats", handler.GetBinStats)
	admin.GET("/card-bins", handler.ListCardBins)
	admin.POST("/card-bins/clear", handler.ClearCardBins)
}

// ImportCardBins 上传 BIN 库 CSV（可选携带列映射与种类规则）。
func (h *BinAdminHandler) ImportCardBins(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.card_bin_invalid", nil)
		return
	}
	input := cardsecretapp.ImportCardBinsInput{File: file}

	if raw := strings.TrimSpace(c.PostForm("column_map")); raw != "" {
		var columnMap cardsecretapp.BinColumnMap
		if err := json.Unmarshal([]byte(raw), &columnMap); err != nil {
			ginutil.RespondError(c, response.CodeBadRequest, "error.card_bin_invalid", nil)
			return
		}
		input.ColumnMap = &columnMap
	}
	if raw := strings.TrimSpace(c.PostForm("type_rules")); raw != "" {
		var typeRules cardsecretapp.BinTypeRule
		if err := json.Unmarshal([]byte(raw), &typeRules); err != nil {
			ginutil.RespondError(c, response.CodeBadRequest, "error.card_bin_invalid", nil)
			return
		}
		input.TypeRules = &typeRules
	}
	if raw := strings.TrimSpace(c.PostForm("prepaid_keywords")); raw != "" {
		var keywords []string
		if err := json.Unmarshal([]byte(raw), &keywords); err != nil {
			ginutil.RespondError(c, response.CodeBadRequest, "error.card_bin_invalid", nil)
			return
		}
		input.PrepaidKeywords = keywords
	}

	result, err := h.service.ImportCardBins(input)
	if err != nil {
		switch {
		case errors.Is(err, cardsecretapp.ErrInvalid):
			ginutil.RespondError(c, response.CodeBadRequest, "error.card_bin_invalid", nil)
		default:
			ginutil.RespondError(c, response.CodeInternal, "error.card_bin_import_failed", err)
		}
		return
	}
	response.Success(c, result)
}

// GetBinStats 获取 BIN 库统计。
func (h *BinAdminHandler) GetBinStats(c *gin.Context) {
	stats, err := h.service.GetBinStats()
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.card_bin_fetch_failed", err)
		return
	}
	response.Success(c, stats)
}

// ListCardBins 查询 BIN 库列表。
func (h *BinAdminHandler) ListCardBins(c *gin.Context) {
	filter := cardsecretcontract.CardBinFilter{
		Country: strings.TrimSpace(c.Query("country")),
		Brand:   strings.TrimSpace(c.Query("brand")),
		Keyword: strings.TrimSpace(c.Query("keyword")),
	}
	if raw := strings.TrimSpace(c.Query("offset")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			filter.Offset = parsed
		}
	}
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			filter.Limit = parsed
		}
	}

	items, total, err := h.service.ListCardBins(filter)
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.card_bin_fetch_failed", err)
		return
	}
	response.Success(c, gin.H{
		"items": items,
		"total": total,
	})
}

// ClearCardBins 清空 BIN 库。
func (h *BinAdminHandler) ClearCardBins(c *gin.Context) {
	if err := h.service.ClearCardBins(); err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.card_bin_delete_failed", err)
		return
	}
	response.Success(c, gin.H{"cleared": true})
}
