package contract

// ListFilter 描述卡密库存筛选和分页条件。
type ListFilter struct {
	ProductID uint
	SKUID     uint
	BatchID   uint
	Status    string
	Secret    string
	BatchNo   string
	Country   string
	Brand     string
	CardType  string
	Page      int
	PageSize  int
}

// PickFilter 描述挑卡选卡条件：挑头模式只需 BinPrefix，其余模式国家必填，品牌/种类可多选（空 = 不限）。
type PickFilter struct {
	Country   string
	Brands    []string
	CardTypes []string
	BinPrefix string
}

// Empty 判断是否未启用挑卡（无任何过滤条件即视为无挑卡条件）。
func (f PickFilter) Empty() bool {
	return f.Country == "" && f.BinPrefix == "" && len(f.Brands) == 0 && len(f.CardTypes) == 0
}

// PickAttrCount 是按商品/SKU/国家/品牌/种类聚合的可用卡密数量。
type PickAttrCount struct {
	ProductID uint   `gorm:"column:product_id" json:"product_id"`
	SKUID     uint   `gorm:"column:sku_id" json:"sku_id"`
	Country   string `gorm:"column:country" json:"country"`
	Brand     string `gorm:"column:brand" json:"brand"`
	CardType  string `gorm:"column:card_type" json:"card_type"`
	Total     int64  `gorm:"column:total" json:"total"`
}

// BinHeadCount 是按卡号首位（bin_prefix 第一位）聚合的可用卡密数量，
// 用于 bot「3头/4头/5头/6头」首位挑卡的库存展示。
type BinHeadCount struct {
	Head  string `gorm:"column:head" json:"head"` // 卡号首位（"3".."6"）
	Total int64  `gorm:"column:total" json:"total"`
}

// CardBinFilter 描述 BIN 库列表筛选条件。
type CardBinFilter struct {
	Country string
	Brand   string
	Keyword string
	Offset  int
	Limit   int
}

// BatchStatusCount 是按批次和状态聚合的数量。
type BatchStatusCount struct {
	BatchID uint   `gorm:"column:batch_id"`
	Status  string `gorm:"column:status"`
	Total   int64  `gorm:"column:total"`
}

// SKUStockCount 是按商品、SKU 和状态聚合的库存数量。
type SKUStockCount struct {
	ProductID uint   `gorm:"column:product_id"`
	SKUID     uint   `gorm:"column:sku_id"`
	Status    string `gorm:"column:status"`
	Total     int64  `gorm:"column:total"`
}
