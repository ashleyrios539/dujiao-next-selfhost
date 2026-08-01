package domain

import (
	"time"
)

const (
	StatusAvailable = "available"
	StatusReserved  = "reserved"
	StatusUsed      = "used"
	StatusInvalid   = "invalid"
)

// Secret 卡密库存实体。
type Secret struct {
	ID         uint       `gorm:"primarykey" json:"id"`                                                         // 主键
	ProductID  uint       `gorm:"not null;index:idx_card_secret_reserve" json:"product_id"`                     // 商品ID
	SKUID      uint       `gorm:"column:sku_id;not null;default:0;index:idx_card_secret_reserve" json:"sku_id"` // SKU ID
	BatchID    *uint      `gorm:"index" json:"batch_id,omitempty"`                                              // 批次ID
	Secret     string     `gorm:"type:text;not null" json:"secret"`                                             // 卡密内容
	Status     string     `gorm:"not null;index:idx_card_secret_reserve" json:"status"`                         // 状态（available/used）
	Country    string     `gorm:"column:country;type:varchar(2);default:'';index" json:"country"`               // 卡所属国家（两字母）
	Brand      string     `gorm:"column:brand;type:varchar(16);default:'';index" json:"brand"`                  // 归一化挑卡品牌（visa/mastercard/discover/other）
	CardType   string     `gorm:"column:card_type;type:varchar(8);default:'';index" json:"card_type"`           // 挑卡种类（D/PD/C）
	BinPrefix  string     `gorm:"column:bin_prefix;type:varchar(6);default:'';index" json:"bin_prefix"`         // 卡号前6位（挑头过滤用）
	OrderID    *uint      `gorm:"index" json:"order_id,omitempty"`                                              // 关联订单ID
	ReservedAt *time.Time `gorm:"index" json:"reserved_at"`                                                     // 占用时间
	UsedAt     *time.Time `gorm:"index" json:"used_at"`                                                         // 使用时间
	CreatedAt  time.Time  `gorm:"index" json:"created_at"`                                                      // 创建时间
	UpdatedAt  time.Time  `gorm:"index" json:"updated_at"`                                                      // 更新时间
	DeletedAt  *time.Time `gorm:"index" json:"-"`                                                               // 软删除时间

	Batch *Batch `gorm:"foreignKey:BatchID" json:"batch,omitempty"` // 批次信息
}

// TableName 指定表名
func (Secret) TableName() string {
	return "card_secrets"
}
