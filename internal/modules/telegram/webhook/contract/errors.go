package contract

import "errors"

// 模块内错误定义。
var (
	ErrTokenUnavailable = errors.New("telegram: bot token unavailable")
	ErrInvalidUpdate    = errors.New("telegram: invalid update")
	ErrConfigDisabled   = errors.New("telegram: bot config disabled")
)