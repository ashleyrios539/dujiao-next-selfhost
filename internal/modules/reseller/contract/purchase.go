package contract

import (
	"context"

	"github.com/shopspring/decimal"
)

// WholesaleItemInput 批发采购订单项（主站代理中心按渠道价购买）。
type WholesaleItemInput struct {
	ProductID        uint   `json:"product_id"`
	SKUID            uint   `json:"sku_id"`
	Quantity         int    `json:"quantity"`
	FulfillmentType  string `json:"fulfillment_type"`
	CardCheckEnabled bool   `json:"card_check_enabled"`
}

// WholesaleOrderInput 批发采购用例输入。
type WholesaleOrderInput struct {
	UserID   uint
	Tenant   TenantContext
	Items    []WholesaleItemInput
	ClientIP string
}

// WholesalePreviewItem 批发采购预订单行。
type WholesalePreviewItem struct {
	ProductID        uint
	SKUID            uint
	Quantity         int
	UnitPrice        decimal.Decimal
	TotalPrice       decimal.Decimal
	CardCheckEnabled bool
}

// WholesalePreview 批发采购预览结果。
type WholesalePreview struct {
	Currency       string
	TotalAmount    decimal.Decimal
	OriginalAmount decimal.Decimal
	Items          []WholesalePreviewItem
}

// WholesaleCreated 批发采购下单结果。
type WholesaleCreated struct {
	OrderID     uint
	OrderNo     string
	Currency    string
	TotalAmount decimal.Decimal
}

// WholesaleOrderGateway 由 bootstrap 接线到订单用例的批发采购端口。
// 该端口隔离分销模块与订单模块，分销应用层只依赖这里的接口签名。
type WholesaleOrderGateway interface {
	Preview(ctx context.Context, input WholesaleOrderInput) (*WholesalePreview, error)
	Create(ctx context.Context, input WholesaleOrderInput) (*WholesaleCreated, error)
}
