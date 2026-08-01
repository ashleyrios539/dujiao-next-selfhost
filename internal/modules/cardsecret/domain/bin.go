package domain

import (
	"strings"
	"time"
)

// 卡密挑卡属性常量。
const (
	// CardTypeD D卡（含预付）
	CardTypeD = "D"
	// CardTypePD 纯D（不含预付）
	CardTypePD = "PD"
	// CardTypeC 纯C
	CardTypeC = "C"

	// PickBrandVisa Visa
	PickBrandVisa = "visa"
	// PickBrandMastercard Mastercard
	PickBrandMastercard = "mastercard"
	// PickBrandDiscover Discover
	PickBrandDiscover = "discover"
	// PickBrandOther 其他（含无法识别/未命中）
	PickBrandOther = "other"
)

// ValidCardType 判断是否为合法的挑卡种类。
func ValidCardType(value string) bool {
	switch value {
	case CardTypeD, CardTypePD, CardTypeC:
		return true
	}
	return false
}

// ValidPickBrand 判断是否为合法的挑卡品牌。
func ValidPickBrand(value string) bool {
	switch value {
	case PickBrandVisa, PickBrandMastercard, PickBrandDiscover, PickBrandOther:
		return true
	}
	return false
}

// NormalizePickBrand 把 BIN 库品牌文本归一化为挑卡品牌（未识别/空归为 other）。
func NormalizePickBrand(raw string) string {
	text := strings.ToUpper(strings.TrimSpace(raw))
	switch {
	case strings.Contains(text, "VISA"):
		return PickBrandVisa
	case strings.Contains(text, "MASTERCARD"), strings.HasPrefix(text, "MC"):
		return PickBrandMastercard
	case strings.Contains(text, "DISCOVER"):
		return PickBrandDiscover
	}
	return PickBrandOther
}

// NormalizeCardType 把 BIN 库卡片分类到挑卡种类三值：
//   - typeValue 命中 rules 显式映射（如 CREDIT→C）时直接返回该值；
//   - 否则按「是否含 PREPAID 属性」区分 D 家族：prepaidValue 或 typeValue 命中 prepaidKeywords → D（含预付），
//     否则 → PD（纯D）。
func NormalizeCardType(typeValue, prepaidValue string, rules map[string]string, prepaidKeywords []string) string {
	value := strings.ToUpper(strings.TrimSpace(typeValue))
	if value == "" {
		return ""
	}
	if len(rules) > 0 {
		if mapped, ok := rules[value]; ok && ValidCardType(mapped) {
			return mapped
		}
	}
	haystack := strings.ToUpper(strings.TrimSpace(prepaidValue)) + "|" + value
	for _, keyword := range prepaidKeywords {
		normalized := strings.ToUpper(strings.TrimSpace(keyword))
		if normalized == "" {
			continue
		}
		if strings.Contains(haystack, normalized) {
			return CardTypeD
		}
	}
	return CardTypePD
}

// CardBin BIN 库条目：前 6 位卡号 → 国家/品牌/种类标注。
type CardBin struct {
	BIN       string    `gorm:"column:bin;primaryKey;type:varchar(6)" json:"bin"` // 前 6 位卡号
	Country   string    `gorm:"column:country;type:varchar(2);default:'';index" json:"country"`
	Brand     string    `gorm:"column:brand;type:varchar(16);default:'';index" json:"brand"`      // 归一化挑卡品牌
	RawBrand  string    `gorm:"column:raw_brand;type:varchar(64);default:''" json:"raw_brand"`    // BIN 库原始品牌文本
	CardType  string    `gorm:"column:card_type;type:varchar(8);default:'';index" json:"card_type"` // D/PD/C
	Issuer    string    `gorm:"column:issuer;type:varchar(128);default:''" json:"issuer"`           // 发卡行名称
	UpdatedAt time.Time `gorm:"index" json:"updated_at"`                                            // 更新时间
}

// TableName 指定表名
func (CardBin) TableName() string {
	return "card_bins"
}
