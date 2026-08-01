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

// PickFilter 描述挑卡选卡条件：国家必填，品牌/种类可多选（空 = 不限）。
type PickFilter struct {
	Country   string
	Brands    []string
	CardTypes []string
}

// Empty 判断是否未启用挑卡（国家为空即视为无挑卡条件）。
func (f PickFilter) Empty() bool {
	return f.Country == ""
}

// PickAttrCount 是按商品/SKU/国家/品牌/种类聚合的可用卡密数量。
type PickAttrCount struct {
	ProductID uint   `gorm:"column:product_id"`
	SKUID     uint   `gorm:"column:sku_id"`
	Country   string `gorm:"column:country"`
	Brand     string `gorm:"column:brand"`
	CardType  string `gorm:"column:card_type"`
	Total     int64  `gorm:"column:total"`
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
