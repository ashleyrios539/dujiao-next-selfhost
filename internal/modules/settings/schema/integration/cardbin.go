package settingsintegration

import (
	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

// CardBinConfig 是接入 BIN 库导入的 typed 设置。
// ColumnMap 描述 BIN CSV 列名映射，TypeRules 描述「种类列值 → 挑卡种类三值」规则。
type CardBinConfig struct {
	ColumnMap jsonmap.JSON `json:"column_map"`
	TypeRules jsonmap.JSON `json:"type_rules"`
}

// DefaultCardBinConfig 返回稳定的 BIN 库导入默认设置。
func DefaultCardBinConfig() CardBinConfig {
	return CardBinConfig{
		ColumnMap: jsonmap.JSON{
			"bin":     "BIN",
			"country": "isoCode2",
			"brand":   "Brand",
			"type":    "Type",
		},
		TypeRules: jsonmap.JSON{
			"PREPAID": "D",
			"GIFT":    "D",
			"DEBIT":   "PD",
			"CREDIT":  "C",
			"CHARGE":  "C",
		},
	}
}

// DecodeCardBinConfig 从持久化 JSON 解码，缺失字段使用 fallback。
func DecodeCardBinConfig(raw jsonmap.JSON, fallback CardBinConfig) CardBinConfig {
	result := fallback
	if columnMap, ok := raw[constants.SettingFieldCardBinColumnMap].(map[string]interface{}); ok {
		result.ColumnMap = jsonmap.JSON(columnMap)
	}
	if typeRules, ok := raw[constants.SettingFieldCardBinTypeRules].(map[string]interface{}); ok {
		result.TypeRules = jsonmap.JSON(typeRules)
	}
	return result
}

// EncodeCardBinConfig 把 typed 设置编码为稳定的持久化 JSON。
func EncodeCardBinConfig(config CardBinConfig) jsonmap.JSON {
	return jsonmap.JSON{
		constants.SettingFieldCardBinColumnMap: config.ColumnMap,
		constants.SettingFieldCardBinTypeRules: config.TypeRules,
	}
}

// NormalizeCardBinConfigJSON 是 Registry 使用的原始 JSON 写入策略。
func NormalizeCardBinConfigJSON(raw jsonmap.JSON) jsonmap.JSON {
	return EncodeCardBinConfig(DecodeCardBinConfig(raw, DefaultCardBinConfig()))
}
