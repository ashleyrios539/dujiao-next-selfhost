package cardcheck

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

var (
	cardNumberRe = regexp.MustCompile(`(?i)(?:card|cc|card_number|cardno)\s*[=:]\s*([\d*X]{13,19})`)
	expiryRe     = regexp.MustCompile(`(?i)(?:exp(?:iration)?|date)\s*[=:]\s*(\d{1,2}\s*[/\\-]\s*\d{2,4}|\d{4,6})`)
	cvvRe        = regexp.MustCompile(`(?i)(?:cvv|cvc)\s*[=:]\s*(\d{3,4})`)

	numberOnlyRe  = regexp.MustCompile(`\d{13,19}`)
	expiryPairRe  = regexp.MustCompile(`(\d{1,2})\s*[/\\-]\s*(\d{2,4})`)
	expiryJoinedRe = regexp.MustCompile(`\b(\d{2})(\d{2})\b`)
)

// ParseCard 把卡密文本解析为礼品卡结构。
// 支持 JSON、card=/exp=/cvv= 键值、label 换行格式以及 | / \ 空格等分隔格式。
func ParseCard(raw string) (Card, bool) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return Card{}, false
	}

	if strings.HasPrefix(text, "{") {
		if card, ok := parseJSONCard(text); ok {
			return card, true
		}
	}
	if card, ok := parseKeyValueCard(text); ok {
		return card, true
	}
	if card, ok := parseDelimitedCard(text); ok {
		return card, true
	}
	return Card{}, false
}

// ExtractCardNumber 宽松提取卡密文本中的卡号（首个 13~19 位数字串），
// 用于 BIN 标注等只需要卡号、不需要完整卡信息（到期/安全码）的场景。
// 它不要求整卡可解析，因此可以覆盖 card|mm|yy|cvv|姓名|地址|...|邮箱 等
// 末尾字段不含数字而完整 ParseCard 会失败的格式。
func ExtractCardNumber(raw string) (string, bool) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return "", false
	}
	number := numberOnlyRe.FindString(text)
	if len(number) < 13 || len(number) > 19 {
		return "", false
	}
	return number, true
}

func parseJSONCard(text string) (Card, bool) {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		return Card{}, false
	}
	number := firstString(data, "card_number", "card", "cc", "cardno")
	exp := firstString(data, "expiration", "exp", "date", "expiry")
	cvv := firstString(data, "cvv", "cvc", "security_code")
	if number == "" || exp == "" {
		return Card{}, false
	}
	return buildCard(number, exp, cvv)
}

func parseKeyValueCard(text string) (Card, bool) {
	numMatch := cardNumberRe.FindStringSubmatch(text)
	if len(numMatch) < 2 {
		return Card{}, false
	}
	expMatch := expiryRe.FindStringSubmatch(text)
	cvvMatch := cvvRe.FindStringSubmatch(text)
	exp := ""
	if len(expMatch) == 2 {
		exp = expMatch[1]
	}
	cvv := ""
	if len(cvvMatch) == 2 {
		cvv = cvvMatch[1]
	}
	return buildCard(numMatch[1], exp, cvv)
}

func parseDelimitedCard(text string) (Card, bool) {
	fields := splitFields(text)
	if len(fields) < 3 {
		return Card{}, false
	}
	number := numberOnlyRe.FindString(fields[0])
	if len(number) < 13 || len(number) > 19 {
		return Card{}, false
	}
	cvv := digitsOnly(fields[len(fields)-1])
	if len(cvv) < 3 || len(cvv) > 4 {
		return Card{}, false
	}
	switch {
	case len(fields) == 3:
		month, year, ok := parseExpiry(fields[1])
		if !ok {
			return Card{}, false
		}
		return Card{Number: number, Month: month, Year: year, CVV: cvv}, true
	case len(fields) >= 4:
		month, year, ok := normalizeMonthYear(fields[1], fields[2])
		if !ok {
			return Card{}, false
		}
		return Card{Number: number, Month: month, Year: year, CVV: cvv}, true
	}
	return Card{}, false
}

// buildCard 组装并校验卡号/到期/安全码。
func buildCard(number, exp, cvv string) (Card, bool) {
	number = numberOnlyRe.FindString(number)
	if len(number) < 13 || len(number) > 19 {
		return Card{}, false
	}
	month, year, ok := parseExpiry(exp)
	if !ok {
		return Card{}, false
	}
	cvvDigits := digitsOnly(cvv)
	if len(cvvDigits) < 3 || len(cvvDigits) > 4 {
		return Card{}, false
	}
	return Card{
		Number: number,
		Month:  month,
		Year:   year,
		CVV:    cvvDigits,
	}, true
}

// parseExpiry 解析到期日期，返回月、年（4 位）与是否合法。
func parseExpiry(exp string) (string, string, bool) {
	exp = strings.TrimSpace(exp)
	if exp == "" {
		return "", "", false
	}
	if pair := expiryPairRe.FindStringSubmatch(exp); len(pair) == 3 {
		month := pair[1]
		year := pair[2]
		if len(year) == 2 {
			year = normalizeShortYear(year)
		}
		return normalizeMonthYear(month, year)
	}
	if joined := expiryJoinedRe.FindStringSubmatch(exp); len(joined) == 3 {
		month := joined[1]
		year := normalizeShortYear(joined[2])
		return normalizeMonthYear(month, year)
	}
	// 4 位数字的 mmyy
	if len(exp) == 4 && isAllDigits(exp) {
		return normalizeMonthYear(exp[0:2], normalizeShortYear(exp[2:4]))
	}
	return "", "", false
}

func normalizeMonthYear(month, year string) (string, string, bool) {
	month = digitsOnly(month)
	if month == "" || len(month) > 2 {
		return "", "", false
	}
	monthNum := 0
	for _, ch := range month {
		monthNum = monthNum*10 + int(ch-'0')
	}
	if monthNum < 1 || monthNum > 12 {
		return "", "", false
	}
	yearDigits := digitsOnly(year)
	switch len(yearDigits) {
	case 2:
		yearDigits = normalizeShortYear(yearDigits)
		if yearDigits == "" {
			return "", "", false
		}
	case 4:
	default:
		return "", "", false
	}
	return month, yearDigits, true
}

func normalizeShortYear(year string) string {
	digits := digitsOnly(year)
	switch len(digits) {
	case 2:
		prefix := "20"
		if digits >= "30" {
			prefix = "19"
		}
		return prefix + digits
	case 4:
		return digits
	default:
		return ""
	}
}

func splitFields(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		switch r {
		case '|', '/', '\\', ',', '\t', '\n', '\r', ' ':
			return true
		}
		return false
	})
}

func digitsOnly(text string) string {
	var builder strings.Builder
	for _, ch := range text {
		if ch >= '0' && ch <= '9' {
			builder.WriteRune(ch)
		}
	}
	return builder.String()
}

func isAllDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, ch := range text {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func firstString(data map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		for rawKey, value := range data {
			if !strings.EqualFold(rawKey, key) {
				continue
			}
			switch typed := value.(type) {
			case string:
				return typed
			case float64:
				return strconv.FormatFloat(typed, 'f', -1, 64)
			case json.Number:
				return typed.String()
			}
		}
	}
	return ""
}
