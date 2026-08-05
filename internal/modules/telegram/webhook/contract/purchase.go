package contract

import (
	"context"
	"errors"
)

// --- 购买流程展示用 DTO（与业务模块解耦，由 container 适配器转换） ---

// ShopCategory 分类展示项。
type ShopCategory struct {
	ID   uint
	Slug string
	Name string // 已按 locale 解析
	Icon string
}

// ShopSKU SKU 展示项。
type ShopSKU struct {
	ID          uint
	Code        string
	PriceAmount string // 格式化金额
	IsActive    bool
}

// ShopProduct 商品展示项。
type ShopProduct struct {
	ID               uint
	Slug             string
	Title            string // 已按 locale 解析
	Currency         string
	PriceAmount      string
	FulfillmentType  string
	CardCheckEnabled bool
	CardCheckFee     string
	PickEnabled      bool
	PickPrices       map[string]string // key -> 单价字符串
	StockAvailable   int64             // 当前可发数（auto=可用库存，manual=剩余库存；-1 表示无限）
	SKUs             []ShopSKU
}

// PickAttrCount 挑卡可用库存聚合（单 SKU 维度，与 cardsecret 契约字段对齐）。
type PickAttrCount struct {
	ProductID uint
	SKUID     uint
	Country   string
	Brand     string
	CardType  string
	Total     int64
}

// PickMode 挑卡模式。
type PickMode string

const (
	PickModeRandom PickMode = "random" // 随机（仅选国家）
	PickModeBin    PickMode = "bin"    // 挑头（BIN，不需国家）
	PickModeType   PickMode = "type"   // 挑卡种类（国家+品牌+卡类型）
)

// ShopPickBrand 可选品牌。
type ShopPickBrand struct {
	Key  string // random/visa/mastercard/discover/amex/jcb
	Name string
}

// ShopPickCardType 可选卡类型。
type ShopPickCardType struct {
	Key  string // random/D/PD/C
	Name string
}

// ShopPickCountry 可选国家（含该国家可用库存，用于按库存排序）。
type ShopPickCountry struct {
	Code     string
	Name     string
	Stock    int64 // 该国家下全部 SKU 的可用库存合计
}

// ShopPickStock 商品挑卡可选维度（供 bot 端构建选择 UI 与计算可发数）。
type ShopPickStock struct {
	Countries []ShopPickCountry // 按库存降序
	Brands    []ShopPickBrand
	CardTypes []ShopPickCardType
}
// --- 下单/支付输入输出 DTO ---

// 下单/支付错误分类。container 适配层负责把业务模块错误翻译成这些哨兵，
// bot 端据此显示本地化文案，避免把 Go 内部错误原文展示给用户。
var (
	ErrOrderStockInsufficient = errors.New("purchase: stock insufficient")
	ErrOrderIdentityRequired  = errors.New("purchase: identity required")
	ErrOrderInsufficient      = errors.New("purchase: insufficient")
	ErrPaymentCreateFailed    = errors.New("purchase: payment create failed")
	ErrOrderCreateFailed      = errors.New("purchase: order create failed")
)

// TranslatePurchaseError 把一次下单/支付调用返回的错误归类为可展示的分类错误。
// 归类失败时返回原错误（调用方回退到通用文案且不外泄内部细节）。
func TranslatePurchaseError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrOrderStockInsufficient) ||
		errors.Is(err, ErrOrderIdentityRequired) ||
		errors.Is(err, ErrOrderInsufficient) ||
		errors.Is(err, ErrPaymentCreateFailed) ||
		errors.Is(err, ErrOrderCreateFailed) {
		return err
	}
	return err
}

// ClassifyPurchaseError 判断错误属于哪一分类，用于本地化文案选择。
func ClassifyPurchaseError(err error) string {
	switch {
	case errors.Is(err, ErrOrderStockInsufficient):
		return "purchase.stock_insufficient"
	case errors.Is(err, ErrOrderIdentityRequired):
		return "purchase.identity_required"
	case errors.Is(err, ErrOrderInsufficient):
		return "purchase.insufficient"
	case errors.Is(err, ErrPaymentCreateFailed):
		return "purchase.payment_failed"
	case errors.Is(err, ErrOrderCreateFailed):
		return "purchase.order_failed"
	}
	return ""
}

// PurchaseItem 单个订单项（含挑头/测活）。
type PurchaseItem struct {
	ProductID        uint
	SKUID            uint
	Quantity         int
	FulfillmentType  string
	CardCheckEnabled bool
	PickCountry      string
	PickBrands       []string
	PickCardTypes    []string
	PickBin          string
}

// PurchasePreviewInput 预下单输入。
type PurchasePreviewInput struct {
	UserID uint
	IP     string
	Items  []PurchaseItem
}

// PurchasePreviewItem 预下单单项结果。
type PurchasePreviewItem struct {
	ProductID          uint
	SKUID              uint
	Title              string
	Quantity           int
	UnitPrice          string
	TotalPrice         string
	CardCheckEnabled   bool
	PickCountry        string
	PickBrands         []string
	PickCardTypes      []string
	PickBin            string
}

// PurchasePreview 预下单结果。
type PurchasePreview struct {
	Currency       string
	OriginalAmount string
	DiscountAmount string
	TotalAmount    string
	Items          []PurchasePreviewItem
}

// PurchaseCreateInput 下单输入。
type PurchaseCreateInput struct {
	UserID uint
	IP     string
	Items  []PurchaseItem
}

// PurchaseCreated 下单结果。
type PurchaseCreated struct {
	OrderID     uint
	OrderNo     string
	Currency    string
	TotalAmount string
}

// PurchasePaymentInput 发起支付输入。
type PurchasePaymentInput struct {
	OrderID    uint
	ChannelID  uint
	UseBalance bool
	IP         string
}

// PurchasePaymentResult 支付发起结果。
type PurchasePaymentResult struct {
	OrderPaid        bool
	WalletPaidAmount string
	OnlinePayAmount  string
	PayURL           string
	QRCode           string
	ProviderType     string
	ChannelType      string
	InteractionMode  string
	ExpiresAt        string // 格式化时间或空
}

// PurchaseUser 已解析的商城用户（供下单使用）。
type PurchaseUser struct {
	ID          uint
	DisplayName string
}

// --- 端口接口 ---

// PurchaseCatalogReader 提供分类与商品浏览查询。
type PurchaseCatalogReader interface {
	ListActiveCategories(ctx context.Context) ([]ShopCategory, error)
	ListProducts(ctx context.Context, categoryID string, page, pageSize int) ([]ShopProduct, int64, error)
	GetProductBySlug(ctx context.Context, slug string) (*ShopProduct, error)
	CountPickAttrs(ctx context.Context, productID uint) ([]PickAttrCount, error)
	CountAvailableByBinPrefix(ctx context.Context, productID uint, bin string) (int64, error)
	// GetPickStock 返回商品挑卡可选维度（国家按库存降序、品牌、卡类型）。
	GetPickStock(ctx context.Context, productID uint) (*ShopPickStock, error)
}

// PurchaseOrderGateway 提供预下单与下单能力（由 container 适配到 OrderService）。
type PurchaseOrderGateway interface {
	Preview(ctx context.Context, input PurchasePreviewInput) (*PurchasePreview, error)
	Create(ctx context.Context, input PurchaseCreateInput) (*PurchaseCreated, error)
}

// ShopPaymentChannel 支付渠道展示项（bot 在线支付选择用）。
type ShopPaymentChannel struct {
	ID          uint
	Name        string
	ChannelType string
}

// PurchasePaymentGateway 提供支付发起能力（由 container 适配到 PaymentService）。
type PurchasePaymentGateway interface {
	CreatePayment(ctx context.Context, input PurchasePaymentInput) (*PurchasePaymentResult, error)
	// ListPaymentChannels 返回可用的在线支付渠道（活跃渠道，供 bot 内选择）。
	ListPaymentChannels(ctx context.Context) ([]ShopPaymentChannel, error)
}

// PurchaseWalletReader 提供余额查询。
type PurchaseWalletReader interface {
	GetBalance(ctx context.Context, userID uint) (string, error)
}

// PurchaseIdentityResolver 解析 Telegram 渠道身份到商城用户；未绑定则自动建号绑定。
type PurchaseIdentityResolver interface {
	ResolveOrProvision(ctx context.Context, channelUserID, username, firstName, lastName string) (*PurchaseUser, error)
}

// PurchaseSettingReader 提供站点级展示配置。
type PurchaseSettingReader interface {
	GetCurrency(ctx context.Context) (string, error)
	GetSiteName(ctx context.Context) (string, error)
}
