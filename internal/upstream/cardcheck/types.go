package cardcheck

import "time"

// DefaultBaseURL 是 CheckDx 测活 API 根地址。
const DefaultBaseURL = "https://dxchecklive.com/api"

// DefaultFrontendVersion 与 CheckDx 前端约定的上送版本号。
const DefaultFrontendVersion = "1.1.0"

// Status 单卡检测状态。
type Status string

const (
	StatusLive    Status = "live"
	StatusDead    Status = "dead"
	StatusUnknown Status = "unknown"
)

// Card 礼品卡信息。
type Card struct {
	Number string // 卡号
	Month  string // 月（1-12）
	Year   string // 年（2 或 4 位）
	CVV    string // 3 位安全码
}

// Format 返回 CheckDx 期望的「卡号|月|年|cvv」行格式。
func (c Card) Format() string {
	return c.Number + "|" + c.Month + "|" + c.Year + "|" + c.CVV
}

// Result 单卡检测结果。
type Result struct {
	Card     Card
	Status   Status
	Raw      string // 原始结果行
	Refunded bool   // 是否因未检测到而退点
}

// InterfaceOption 可用的测活接口（站点）。
type InterfaceOption struct {
	Label string
	Value string
}

// Options 一次批量检测的可调参数。
type Options struct {
	PollInterval time.Duration // 结果轮询间隔
	Timeout      time.Duration // 单次检测最长等待
}

// DefaultOptions 返回推荐的检测参数。
func DefaultOptions() Options {
	return Options{
		PollInterval: 2 * time.Second,
		Timeout:      60 * time.Second,
	}
}
