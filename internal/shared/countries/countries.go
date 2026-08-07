// Package countries 提供 ISO 3166-1 alpha-2 两字母国家代码到中文名的静态字典。
package countries

import (
	"sort"
	"strings"
)

var chineseNames = map[string]string{
	"US": "美国",
	"CA": "加拿大",
	"GB": "英国",
	"AU": "澳大利亚",
	"DE": "德国",
	"FR": "法国",
	"IT": "意大利",
	"ES": "西班牙",
	"NL": "荷兰",
	"SE": "瑞典",
	"NO": "挪威",
	"DK": "丹麦",
	"FI": "芬兰",
	"CH": "瑞士",
	"AT": "奥地利",
	"BE": "比利时",
	"PT": "葡萄牙",
	"IE": "爱尔兰",
	"PL": "波兰",
	"JP": "日本",
	"KR": "韩国",
	"SG": "新加坡",
	"HK": "中国香港",
	"TW": "中国台湾",
	"CN": "中国大陆",
	"MX": "墨西哥",
	"BR": "巴西",
	"AR": "阿根廷",
	"CL": "智利",
	"CO": "哥伦比亚",
	"PE": "秘鲁",
	"NZ": "新西兰",
	"IN": "印度",
	"ID": "印度尼西亚",
	"MY": "马来西亚",
	"TH": "泰国",
	"VN": "越南",
	"PH": "菲律宾",
	"TR": "土耳其",
	"SA": "沙特阿拉伯",
	"AE": "阿联酋",
	"IL": "以色列",
	"ZA": "南非",
	"EG": "埃及",
	"NG": "尼日利亚",
	"RU": "俄罗斯",
	"UA": "乌克兰",
	"GR": "希腊",
	"CZ": "捷克",
	"HU": "匈牙利",
	"RO": "罗马尼亚",
	"HR": "克罗地亚",
	"SK": "斯洛伐克",
	"BG": "保加利亚",
	"LT": "立陶宛",
	"LV": "拉脱维亚",
	"EE": "爱沙尼亚",
	"SI": "斯洛文尼亚",
	"IS": "冰岛",
	"LU": "卢森堡",
	"MT": "马耳他",
	"CY": "塞浦路斯",
}

// ChineseName 返回国家代码对应的中文名；未知代码返回空串。
func ChineseName(code string) string {
	return chineseNames[code]
}

// Item 国家字典条目。
type Item struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// List 返回按代码排序的国家字典。
func List() []Item {
	codes := make([]string, 0, len(chineseNames))
	for code := range chineseNames {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	items := make([]Item, 0, len(codes))
	for _, code := range codes {
		items = append(items, Item{Code: code, Name: chineseNames[code]})
	}
	return items
}

// EmojiFlag 返回 ISO 3166-1 alpha-2 国家代码对应的旗帜 emoji（regional indicator 拼接）。
// 非两字母代码返回空串。纯计算，无需数据表。
func EmojiFlag(code string) string {
	if len(code) != 2 {
		return ""
	}
	var b strings.Builder
	for _, r := range code {
		if r >= 'A' && r <= 'Z' {
			b.WriteRune(0x1F1E6 + (r - 'A'))
		} else if r >= 'a' && r <= 'z' {
			b.WriteRune(0x1F1E6 + (r - 'a'))
		} else {
			return ""
		}
	}
	return b.String()
}
