package contract

import (
	"context"
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
// --- 下单/支付输入输出 DTO ---

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
}

// PurchaseOrderGateway 提供预下单与下单能力（由 container 适配到 OrderService）。
type PurchaseOrderGateway interface {
	Preview(ctx context.Context, input PurchasePreviewInput) (*PurchasePreview, error)
	Create(ctx context.Context, input PurchaseCreateInput) (*PurchaseCreated, error)
}

// PurchasePaymentGateway 提供支付发起能力（由 container 适配到 PaymentService）。
type PurchasePaymentGateway interface {
	CreatePayment(ctx context.Context, input PurchasePaymentInput) (*PurchasePaymentResult, error)
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
