package domain

// Update 是 Telegram webhook 推送的 Update 对象（仅保留本项目需要的字段）。
type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

// Message 是 Telegram 文本消息。
type Message struct {
	MessageID int64    `json:"message_id"`
	Chat      Chat     `json:"chat"`
	From      *User    `json:"from"`
	Text      string   `json:"text"`
	Date      int64    `json:"date"`
}

// CallbackQuery 是菜单按钮点击回调。
type CallbackQuery struct {
	ID      string  `json:"id"`
	From    User    `json:"from"`
	Message Message `json:"message"`
	Data    string  `json:"data"`
}

// Chat 是 Telegram 会话。
type Chat struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	UserName  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// User 是 Telegram 用户。
type User struct {
	ID           int64  `json:"id"`
	IsBot        bool   `json:"is_bot"`
	UserName     string `json:"username"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	LanguageCode string `json:"language_code"`
}

// IsPrivateChat 判断是否私聊。
func (m *Message) IsPrivateChat() bool {
	return m != nil && m.Chat.Type == "private"
}

// IsGroupChat 判断是否群组会话。
func (m *Message) IsGroupChat() bool {
	if m == nil {
		return false
	}
	return m.Chat.Type == "group" || m.Chat.Type == "supergroup"
}