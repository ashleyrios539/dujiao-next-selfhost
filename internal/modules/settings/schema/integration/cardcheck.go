package settingsintegration

import (
	"strings"

	"github.com/dujiao-next/internal/constants"
	settingsvalue "github.com/dujiao-next/internal/modules/settings/schema/value"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

const (
	cardCheckBufferDefault    = 5
	cardCheckBufferMin        = 0
	cardCheckBufferMax        = 100
	cardCheckTimeoutDef       = 60
	cardCheckTimeoutMin       = 10
	cardCheckTimeoutMax       = 300
	cardCheckPollMillisDef    = 2000
	cardCheckPollMillisMin    = 500
	cardCheckPollMillisMax    = 10000
	cardCheckInterfaceDefault = ""
)

// CardCheckConfig 是接入 CheckDx 测活接口的 typed 设置。
type CardCheckConfig struct {
	Enabled        bool   `json:"enabled"`
	Kami           string `json:"kami"`
	Interface      string `json:"interface"`
	Buffer         int    `json:"buffer"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	PollMillis     int    `json:"poll_interval_millis"`
}

// DefaultCardCheckConfig 返回稳定的测活默认设置。
func DefaultCardCheckConfig() CardCheckConfig {
	return CardCheckConfig{
		Enabled:        false,
		Kami:           "",
		Interface:      cardCheckInterfaceDefault,
		Buffer:         cardCheckBufferDefault,
		TimeoutSeconds: cardCheckTimeoutDef,
		PollMillis:     cardCheckPollMillisDef,
	}
}

// NormalizeCardCheckConfig 归一化测活设置范围。
func NormalizeCardCheckConfig(config CardCheckConfig) CardCheckConfig {
	config.Kami = strings.TrimSpace(config.Kami)
	config.Interface = strings.TrimSpace(config.Interface)
	if config.Buffer < cardCheckBufferMin {
		config.Buffer = cardCheckBufferDefault
	}
	if config.Buffer > cardCheckBufferMax {
		config.Buffer = cardCheckBufferMax
	}
	if config.TimeoutSeconds < cardCheckTimeoutMin {
		config.TimeoutSeconds = cardCheckTimeoutDef
	}
	if config.TimeoutSeconds > cardCheckTimeoutMax {
		config.TimeoutSeconds = cardCheckTimeoutMax
	}
	if config.PollMillis < cardCheckPollMillisMin {
		config.PollMillis = cardCheckPollMillisDef
	}
	if config.PollMillis > cardCheckPollMillisMax {
		config.PollMillis = cardCheckPollMillisMax
	}
	return config
}

// DecodeCardCheckConfig 从持久化 JSON 解码，缺失字段使用 fallback。
func DecodeCardCheckConfig(raw jsonmap.JSON, fallback CardCheckConfig) CardCheckConfig {
	result := NormalizeCardCheckConfig(fallback)
	if value, exists := raw[constants.SettingFieldCardCheckEnabled]; exists {
		result.Enabled = settingsvalue.ParseBool(value)
	}
	if text, ok := raw[constants.SettingFieldCardCheckKami].(string); ok {
		result.Kami = text
	}
	if text, ok := raw[constants.SettingFieldCardCheckInterface].(string); ok {
		result.Interface = text
	}
	if parsed, err := settingsvalue.ParseInt(raw[constants.SettingFieldCardCheckBuffer]); err == nil {
		result.Buffer = parsed
	}
	if parsed, err := settingsvalue.ParseInt(raw[constants.SettingFieldCardCheckTimeout]); err == nil {
		result.TimeoutSeconds = parsed
	}
	if parsed, err := settingsvalue.ParseInt(raw[constants.SettingFieldCardCheckPollMillis]); err == nil {
		result.PollMillis = parsed
	}
	return NormalizeCardCheckConfig(result)
}

// EncodeCardCheckConfig 把 typed 设置编码为稳定的持久化 JSON。
func EncodeCardCheckConfig(config CardCheckConfig) jsonmap.JSON {
	normalized := NormalizeCardCheckConfig(config)
	return jsonmap.JSON{
		constants.SettingFieldCardCheckEnabled:    normalized.Enabled,
		constants.SettingFieldCardCheckKami:       normalized.Kami,
		constants.SettingFieldCardCheckInterface:  normalized.Interface,
		constants.SettingFieldCardCheckBuffer:     normalized.Buffer,
		constants.SettingFieldCardCheckTimeout:    normalized.TimeoutSeconds,
		constants.SettingFieldCardCheckPollMillis: normalized.PollMillis,
	}
}

// NormalizeCardCheckConfigJSON 是 Registry 使用的原始 JSON 写入策略。
func NormalizeCardCheckConfigJSON(raw jsonmap.JSON) jsonmap.JSON {
	return EncodeCardCheckConfig(DecodeCardCheckConfig(raw, DefaultCardCheckConfig()))
}
