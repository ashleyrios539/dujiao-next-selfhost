package settingsintegration

import (
	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

// CardBinConfig 是接入 BIN 库导入的 typed 设置。
// ColumnMap 描述 BIN CSV 列名映射，TypeRules 描述「种类列值 → 挑卡种类三值」的显式映射，
// PrepaidKeywords 描述「含预付」标记（命中该列的卡归入 D 含预付，否则为纯D）。
type CardBinConfig struct {
	ColumnMap        jsonmap.JSON `json:"column_map"`
	TypeRules        jsonmap.JSON `json:"type_rules"`
	PrepaidKeywords  []string     `json:"prepaid_keywords"`
}

// DefaultCardBinConfig 返回稳定的 BIN 库导入默认设置。
func DefaultCardBinConfig() CardBinConfig {
	return CardBinConfig{
		ColumnMap: jsonmap.JSON{
			"bin":     "BIN",
			"country": "isoCode2",
			"brand":   "Brand",
			"type":    "Type",
			"prepaid": "Category",
		},
		TypeRules: jsonmap.JSON{
			"CREDIT": "C",
			"CHARGE": "C",
		},
		PrepaidKeywords: []string{"PREPAID"},
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
	if keywords, ok := raw[constants.SettingFieldCardBinPrepaidKeywords].([]interface{}); ok {
		result.PrepaidKeywords = toStringSlice(keywords)
	}
	return result
}

// EncodeCardBinConfig 把 typed 设置编码为稳定的持久化 JSON。
func EncodeCardBinConfig(config CardBinConfig) jsonmap.JSON {
	return jsonmap.JSON{
		constants.SettingFieldCardBinColumnMap:       config.ColumnMap,
		constants.SettingFieldCardBinTypeRules:       config.TypeRules,
		constants.SettingFieldCardBinPrepaidKeywords: config.PrepaidKeywords,
	}
}

// NormalizeCardBinConfigJSON 是 Registry 使用的原始 JSON 写入策略。
func NormalizeCardBinConfigJSON(raw jsonmap.JSON) jsonmap.JSON {
	return EncodeCardBinConfig(DecodeCardBinConfig(raw, DefaultCardBinConfig()))
}

func toStringSlice(values []interface{}) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok && text != "" {
			result = append(result, text)
		}
	}
	return result
}
