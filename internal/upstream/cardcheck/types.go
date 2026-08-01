package cardcheck

import "time"

// DefaultBaseURL 是 CheckDx /v1 测活 API 根地址。
const DefaultBaseURL = "https://dxchecklive.com"

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
	Month  string // 月（两位数，01-12）
	Year   string // 年（4 位）
	CVV    string // 3 位安全码
}

// Format 返回 CheckDx /v1/submit 期望的「卡号|MM|YY|cvv」行格式（年取后两位）。
func (c Card) Format() string {
	year := c.Year
	if len(year) >= 2 {
		year = year[len(year)-2:]
	}
	return c.Number + "|" + c.Month + "|" + year + "|" + c.CVV
}

// Result 单卡检测结果。
type Result struct {
	Card   Card
	Status Status
	Raw    string // 原始结果文本
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
