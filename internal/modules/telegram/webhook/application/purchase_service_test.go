package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dujiao-next/internal/modules/telegram/webhook/contract"
	webhookdomain "github.com/dujiao-next/internal/modules/telegram/webhook/domain"
)

// --- stub ports ---

type stubCatalog struct {
	cats       []contract.ShopCategory
	products   []contract.ShopProduct
	bySlug     map[string]*contract.ShopProduct
	binCount   map[uint]int64
	binCountBy map[string]int64
	pickStock  *contract.ShopPickStock
	heads      []contract.ShopBinHead
}

func (c *stubCatalog) ListActiveCategories(context.Context) ([]contract.ShopCategory, error) {
	return c.cats, nil
}
func (c *stubCatalog) ListProducts(_ context.Context, _ string, _ int, _ int) ([]contract.ShopProduct, int64, error) {
	return c.products, int64(len(c.products)), nil
}
func (c *stubCatalog) GetProductBySlug(_ context.Context, slug string) (*contract.ShopProduct, error) {
	return c.bySlug[slug], nil
}
func (c *stubCatalog) CountPickAttrs(context.Context, uint) ([]contract.PickAttrCount, error) {
	return nil, nil
}
func (c *stubCatalog) CountAvailableByBinPrefix(_ context.Context, productID uint, bin string) (int64, error) {
	if c.binCountBy != nil {
		if v, ok := c.binCountBy[bin]; ok {
			return v, nil
		}
	}
	if c.binCount != nil {
		return c.binCount[productID], nil
	}
	return 0, nil
}
func (c *stubCatalog) CountByBinHead(context.Context, uint) ([]contract.ShopBinHead, error) {
	return c.heads, nil
}
func (c *stubCatalog) CountAvailableByBinHead(_ context.Context, productID uint, head string) (int64, error) {
	for _, h := range c.heads {
		if h.Head == head {
			return h.Stock, nil
		}
	}
	return 0, nil
}
func (c *stubCatalog) GetPickStock(context.Context, uint) (*contract.ShopPickStock, error) {
	if c.pickStock != nil {
		return c.pickStock, nil
	}
	return &contract.ShopPickStock{}, nil
}

type stubOrders struct {
	preview *contract.PurchasePreview
	created *contract.PurchaseCreated
}

func (o *stubOrders) Preview(context.Context, contract.PurchasePreviewInput) (*contract.PurchasePreview, error) {
	return o.preview, nil
}
func (o *stubOrders) Create(context.Context, contract.PurchaseCreateInput) (*contract.PurchaseCreated, error) {
	return o.created, nil
}

type stubPayments struct {
	result   *contract.PurchasePaymentResult
	channels []contract.ShopPaymentChannel
}

func (p *stubPayments) CreatePayment(context.Context, contract.PurchasePaymentInput) (*contract.PurchasePaymentResult, error) {
	return p.result, nil
}

func (p *stubPayments) ListPaymentChannels(context.Context) ([]contract.ShopPaymentChannel, error) {
	if p.channels == nil {
		return nil, nil
	}
	return p.channels, nil
}

type stubWallet struct{ bal string }

func (w *stubWallet) GetBalance(context.Context, uint) (string, error) { return w.bal, nil }

type stubIdentity struct{ user *contract.PurchaseUser }

func (i *stubIdentity) ResolveOrProvision(context.Context, string, string, string, string) (*contract.PurchaseUser, error) {
	return i.user, nil
}

func (i *stubIdentity) SetLocale(context.Context, uint, string) error { return nil }

type stubSettings struct{ cur, name string }

func (s *stubSettings) GetCurrency(context.Context) (string, error) { return s.cur, nil }
func (s *stubSettings) GetSiteName(context.Context) (string, error) { return s.name, nil }

// --- fake botapi ---

type fakeBotAPI struct {
	sent         []string
	markups      []inlineKeyboard
	replyMarkups []any // 回复键盘/移除键盘等非 inline 标记
	photos       []string // SendPhotoBytes 的 caption 记录
}

func (f *fakeBotAPI) SendMessage(_ context.Context, _, _ string, message string, opts contract.SendMessageOptions) error {
	f.sent = append(f.sent, message)
	if mk, ok := opts.ReplyMarkup.(inlineKeyboard); ok {
		f.markups = append(f.markups, mk)
	} else if opts.ReplyMarkup != nil {
		f.replyMarkups = append(f.replyMarkups, opts.ReplyMarkup)
	}
	return nil
}
func (f *fakeBotAPI) SendPhotoBytes(_ context.Context, _, _, _ string, _ []byte, caption string, opts contract.SendMessageOptions) error {
	f.photos = append(f.photos, caption)
	if mk, ok := opts.ReplyMarkup.(inlineKeyboard); ok {
		f.markups = append(f.markups, mk)
	} else if opts.ReplyMarkup != nil {
		f.replyMarkups = append(f.replyMarkups, opts.ReplyMarkup)
	}
	return nil
}
func (f *fakeBotAPI) AnswerCallbackQuery(context.Context, string, string, contract.AnswerCallbackOptions) error {
	return nil
}
func (f *fakeBotAPI) SetMyCommands(context.Context, string, []contract.BotCommand) error { return nil }
func (f *fakeBotAPI) GetMe(context.Context, string) (*contract.BotInfo, error)            { return &contract.BotInfo{}, nil }
func (f *fakeBotAPI) SetWebhook(context.Context, string, string, string) error            { return nil }
func (f *fakeBotAPI) DeleteWebhook(context.Context, string) error                          { return nil }

func TestPurchaseServiceCategoryBrowse(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newPurchaseService(contract.PurchasePorts{
		Catalog: &stubCatalog{
			cats: []contract.ShopCategory{{ID: 1, Name: "卡密"}},
		},
		Orders:   &stubOrders{},
		Payments: &stubPayments{},
		Wallet:   &stubWallet{},
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u"}},
		Settings: &stubSettings{cur: "CNY", name: "商店"},
	}, bot, func() string { return "zh-CN" })

	handled, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		Message: &webhookdomain.Message{
			Chat: webhookdomain.Chat{ID: 100, Type: "private"},
			From: &webhookdomain.User{ID: 200, UserName: "alice"},
			Text: "/shop",
		},
	})
	if err != nil {
		t.Fatalf("handle err: %v", err)
	}
	if !handled {
		t.Fatalf("expected handled")
	}
	if len(bot.sent) == 0 {
		t.Fatalf("expected a message")
	}
	if !containsStr(bot.sent[0], "请选择分类") {
		t.Fatalf("expected category prompt, got: %q", bot.sent[0])
	}
	if len(bot.markups) == 0 || !keyboardContains(bot.markups[0], "卡密") {
		t.Fatalf("expected category keyboard with 卡密, got markups: %+v", bot.markups)
	}
}

func TestPurchaseServicePickBinAndOrder(t *testing.T) {
	product := &contract.ShopProduct{
		ID: 10, Slug: "dx", Title: "迪士尼卡", Currency: "CNY",
		PriceAmount: "50.00", FulfillmentType: "auto",
		PickEnabled: true, PickPrices: map[string]string{"bin": "5.00"},
	}
	bot := &fakeBotAPI{}
	svc := newPurchaseService(contract.PurchasePorts{
		Catalog: &stubCatalog{
			cats:       []contract.ShopCategory{{ID: 1, Name: "卡密"}},
			products:   []contract.ShopProduct{*product},
			bySlug:     map[string]*contract.ShopProduct{"dx": product},
			binCountBy: map[string]int64{"412345": 3},
		},
		Orders: &stubOrders{
			preview: &contract.PurchasePreview{
				Currency: "CNY", TotalAmount: "55.00", OriginalAmount: "50.00",
				Items: []contract.PurchasePreviewItem{{
					ProductID: 10, Quantity: 1, UnitPrice: "55.00", TotalPrice: "55.00",
					CardCheckEnabled: false, PickBin: "412345", Title: "迪士尼卡",
				}},
			},
			created: &contract.PurchaseCreated{OrderID: 1, OrderNo: "O123", Currency: "CNY", TotalAmount: "55.00"},
		},
		Payments: &stubPayments{result: &contract.PurchasePaymentResult{OrderPaid: true}},
		Wallet:   &stubWallet{},
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u"}},
		Settings: &stubSettings{cur: "CNY", name: "商店"},
	}, bot, func() string { return "zh-CN" })

	// 进入 /shop
	if _, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		Message: &webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}, From: &webhookdomain.User{ID: 200}, Text: "/shop"},
	}); err != nil {
		t.Fatalf("enter err: %v", err)
	}

	// 选分类
	if _, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{ID: "c1", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbCatPrefix + "1"},
	}); err != nil {
		t.Fatalf("cat err: %v", err)
	}
	// 选商品
	if _, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{ID: "c2", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbProdPrefix + "dx"},
	}); err != nil {
		t.Fatalf("prod err: %v", err)
	}
	// 进入挑卡模式选择（配置面板的"挑卡"按钮 → cbBackPick）
	if _, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{ID: "c3", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbBackPick},
	}); err != nil {
		t.Fatalf("pick err: %v", err)
	}
	// 选挑头(BIN)模式
	if _, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{ID: "c4", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbPickModePrefix + "bin"},
	}); err != nil {
		t.Fatalf("pickmode err: %v", err)
	}
	// 输入 BIN
	if _, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		Message: &webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}, Text: "412345"},
	}); err != nil {
		t.Fatalf("bin err: %v", err)
	}
	// 确认下单
	if _, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{ID: "c5", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbConfirm},
	}); err != nil {
		t.Fatalf("confirm err: %v", err)
	}
	// 选择余额支付
	if _, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{ID: "c6", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbPayBalance},
	}); err != nil {
		t.Fatalf("pay err: %v", err)
	}

	hasAmount := false
	hasOrderNo := false
	for _, m := range bot.sent {
		if containsStr(m, "55.00") {
			hasAmount = true
		}
		if containsStr(m, "O123") {
			hasOrderNo = true
		}
	}
	if !hasAmount || !hasOrderNo {
		t.Fatalf("expected amount + order no across messages, got: %v", bot.sent)
	}
}

func keyboardContains(kb inlineKeyboard, text string) bool {
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if containsStr(btn.Text, text) {
				return true
			}
		}
	}
	return false
}

// findKeyboardButton 返回键盘里第一个文本包含 substr 的按钮完整文本，未找到返回空串。
func findKeyboardButton(kb inlineKeyboard, substr string) string {
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if containsStr(btn.Text, substr) {
				return btn.Text
			}
		}
	}
	return ""
}

func TestPurchaseServiceTypeModeWithCountryReply(t *testing.T) {
	product := &contract.ShopProduct{
		ID: 10, Slug: "dx", Title: "迪士尼卡", Currency: "CNY",
		PriceAmount: "50.00", FulfillmentType: "auto",
		PickEnabled: true, PickPrices: map[string]string{"visa": "3.00", "D": "2.00"},
	}
	bot := &fakeBotAPI{}
	svc := newPurchaseService(contract.PurchasePorts{
		Catalog: &stubCatalog{
			cats:     []contract.ShopCategory{{ID: 1, Name: "卡密"}},
			products: []contract.ShopProduct{*product},
			bySlug:   map[string]*contract.ShopProduct{"dx": product},
			pickStock: &contract.ShopPickStock{
				Countries: []contract.ShopPickCountry{
					{Code: "US", Name: "美国", Stock: 5},
					{Code: "DE", Name: "德国", Stock: 2},
				},
				Brands: []contract.ShopPickBrand{
					{Key: "random", Name: "随机"},
					{Key: "visa", Name: "Visa"},
					{Key: "mastercard", Name: "Mastercard"},
				},
				CardTypes: []contract.ShopPickCardType{
					{Key: "random", Name: "随机"},
					{Key: "D", Name: "D"},
					{Key: "PD", Name: "PD"},
				},
			},
		},
		Orders: &stubOrders{
			preview: &contract.PurchasePreview{
				Currency: "CNY", TotalAmount: "55.00",
				Items: []contract.PurchasePreviewItem{{
					ProductID: 10, Quantity: 1, UnitPrice: "55.00", TotalPrice: "55.00",
					PickCountry: "US", PickBrands: []string{"visa"}, PickCardTypes: []string{"D"}, Title: "迪士尼卡",
				}},
			},
			created: &contract.PurchaseCreated{OrderID: 1, OrderNo: "O9", Currency: "CNY", TotalAmount: "55.00"},
		},
		Payments: &stubPayments{result: &contract.PurchasePaymentResult{OrderPaid: true}},
		Wallet:   &stubWallet{},
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u"}},
		Settings: &stubSettings{cur: "CNY", name: "商店"},
	}, bot, func() string { return "zh-CN" })

	handle := func(u webhookdomain.Update) {
		if _, err := svc.handle(context.Background(), "tok", u); err != nil {
			t.Fatalf("handle err: %v", err)
		}
	}

	// /shop
	handle(webhookdomain.Update{Message: &webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}, From: &webhookdomain.User{ID: 200}, Text: "/shop"}})
	// 分类
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "1", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbCatPrefix + "1"}})
	// 商品
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "2", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbProdPrefix + "dx"}})
	// 挑卡 → 选 type
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "3", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbBackPick}})
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "4", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbPickModePrefix + "type"}})
	// 回复国家双字母 US
	handle(webhookdomain.Update{Message: &webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}, Text: "US"}})
	// 选品牌 visa
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "5", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbBrandPrefix + "visa"}})
	// 选卡类型 D
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "6", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbCTypePrefix + "D"}})

	// 确认下单 → 余额支付
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "7", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbConfirm}})
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "8", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbPayBalance}})

	// 验证国家选项键盘含库存排序（US(5) 应在 DE(2) 前）
	if len(bot.markups) == 0 {
		t.Fatalf("expected markups")
	}
	foundCountryKeyboard := false
	for _, mk := range bot.markups {
		if keyboardContains(mk, "US") && keyboardContains(mk, "DE") {
			foundCountryKeyboard = true
		}
	}
	if !foundCountryKeyboard {
		t.Fatalf("expected country keyboard with US/DE")
	}

	hasOrderNo := false
	for _, m := range bot.sent {
		if containsStr(m, "O9") {
			hasOrderNo = true
		}
	}
	if !hasOrderNo {
		t.Fatalf("expected order no O9, got: %v", bot.sent)
	}
}

func containsStr(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestPurchaseServiceGroupShopNotSupported(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newPurchaseService(contract.PurchasePorts{
		Catalog:  &stubCatalog{},
		Orders:   &stubOrders{},
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u"}},
	}, bot, func() string { return "zh-CN" })

	handled, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		Message: &webhookdomain.Message{
			Chat: webhookdomain.Chat{ID: 100, Type: "supergroup"},
			From: &webhookdomain.User{ID: 200, UserName: "alice"},
			Text: "/shop",
		},
	})
	if err != nil {
		t.Fatalf("handle err: %v", err)
	}
	if !handled {
		t.Fatalf("expected handled")
	}
	if len(bot.sent) == 0 || !containsStr(bot.sent[0], "私聊") {
		t.Fatalf("expected private-chat hint, got: %v", bot.sent)
	}
}

func TestPurchaseServiceStartFromMenu(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newPurchaseService(contract.PurchasePorts{
		Catalog: &stubCatalog{
			cats: []contract.ShopCategory{{ID: 1, Name: "卡密"}},
		},
		Orders:   &stubOrders{},
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u"}},
	}, bot, func() string { return "zh-CN" })

	if err := svc.StartFromMenu(context.Background(), "tok", 100, webhookdomain.User{ID: 200, UserName: "alice"}); err != nil {
		t.Fatalf("StartFromMenu err: %v", err)
	}
	if len(bot.sent) == 0 || !containsStr(bot.sent[0], "请选择分类") {
		t.Fatalf("expected category prompt, got: %v", bot.sent)
	}
}

func TestPurchaseServiceShowWallet(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newPurchaseService(contract.PurchasePorts{
		Catalog:  &stubCatalog{},
		Orders:   &stubOrders{},
		Wallet:   &stubWallet{bal: "88.50"},
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u"}},
		Settings: &stubSettings{cur: "CNY"},
	}, bot, func() string { return "zh-CN" })

	if err := svc.ShowWallet(context.Background(), "tok", 100, webhookdomain.User{ID: 200, UserName: "alice"}); err != nil {
		t.Fatalf("ShowWallet err: %v", err)
	}
	if len(bot.sent) == 0 || !containsStr(bot.sent[0], "88.50") {
		t.Fatalf("expected balance 88.50, got: %v", bot.sent)
	}
}

func TestPurchaseServiceSessionExpires(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newPurchaseService(contract.PurchasePorts{
		Catalog: &stubCatalog{
			cats: []contract.ShopCategory{{ID: 1, Name: "卡密"}},
		},
		Orders:   &stubOrders{},
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u"}},
	}, bot, func() string { return "zh-CN" })

	if err := svc.StartFromMenu(context.Background(), "tok", 100, webhookdomain.User{ID: 200, UserName: "alice"}); err != nil {
		t.Fatalf("StartFromMenu err: %v", err)
	}
	if svc.snapshot(100) == nil {
		t.Fatalf("expected active session")
	}

	// 人为把会话时间拨到 TTL 之前，验证 snapshot 自动清理。
	svc.mu.Lock()
	if sess := svc.sessions[100]; sess != nil {
		sess.lastUpdatedAt = sess.lastUpdatedAt.Add(-(purchaseSessionTTL + time.Minute))
	}
	svc.mu.Unlock()

	if svc.snapshot(100) != nil {
		t.Fatalf("expected expired session to be cleaned up")
	}
}

func TestPurchaseServicePayOnlinePicksChannel(t *testing.T) {
	product := &contract.ShopProduct{
		ID: 10, Slug: "dx", Title: "迪士尼卡", Currency: "CNY",
		PriceAmount: "50.00", FulfillmentType: "auto",
	}
	bot := &fakeBotAPI{}
	svc := newPurchaseService(contract.PurchasePorts{
		Catalog: &stubCatalog{
			bySlug: map[string]*contract.ShopProduct{"dx": product},
		},
		Orders: &stubOrders{
			created: &contract.PurchaseCreated{OrderID: 3, OrderNo: "O3", Currency: "CNY", TotalAmount: "50.00"},
		},
		Payments: &stubPayments{
			channels: []contract.ShopPaymentChannel{{ID: 1, Name: "EPUSDT", ChannelType: "epusdt"}},
			result: &contract.PurchasePaymentResult{
				OrderPaid:       false,
				OnlinePayAmount: "50.00",
				PayURL:          "https://pay.example.com/3",
			},
		},
		Wallet:   &stubWallet{bal: "0.00"},
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u"}},
	}, bot, func() string { return "zh-CN" })

	// 进入购买并选商品
	handled, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		Message: &webhookdomain.Message{
			Chat: webhookdomain.Chat{ID: 100, Type: "private"},
			From: &webhookdomain.User{ID: 200, UserName: "alice"},
			Text: "/shop",
		},
	})
	if err != nil || !handled {
		t.Fatalf("enter shop failed: handled=%v err=%v", handled, err)
	}
	// 选商品
	handled, err = svc.handle(context.Background(), "tok", webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{
			ID: "cb1", From: webhookdomain.User{ID: 200}, Data: cbProdPrefix + "dx",
			Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}},
		},
	})
	if err != nil || !handled {
		t.Fatalf("select product failed: handled=%v err=%v", handled, err)
	}
	// 点在线支付（单渠道应直接发起支付）
	handled, err = svc.handle(context.Background(), "tok", webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{
			ID: "cb2", From: webhookdomain.User{ID: 200}, Data: cbPayOnline,
			Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}},
		},
	})
	if err != nil || !handled {
		t.Fatalf("pay online failed: handled=%v err=%v", handled, err)
	}
	// 应显示订单号 + 支付链接
	joined := ""
	for _, m := range bot.sent {
		joined += m + "\n"
	}
	if !containsStr(joined, "O3") || !containsStr(joined, "https://pay.example.com/3") {
		t.Fatalf("expected order no and pay url, got: %v", bot.sent)
	}
}

func TestPurchaseServicePayOnlineEpusdtShowsAddress(t *testing.T) {
	product := &contract.ShopProduct{
		ID: 10, Slug: "dx", Title: "迪士尼卡", Description: "迪士尼正版卡密，秒发。",
		Currency: "CNY", PriceAmount: "50.00", FulfillmentType: "auto",
	}
	bot := &fakeBotAPI{}
	svc := newPurchaseService(contract.PurchasePorts{
		Catalog: &stubCatalog{
			bySlug: map[string]*contract.ShopProduct{"dx": product},
		},
		Orders: &stubOrders{
			created: &contract.PurchaseCreated{OrderID: 3, OrderNo: "O3", Currency: "CNY", TotalAmount: "50.00"},
		},
		Payments: &stubPayments{
			channels: []contract.ShopPaymentChannel{{ID: 1, Name: "EPUSDT", ChannelType: "epusdt"}},
			result: &contract.PurchasePaymentResult{
				OrderPaid:       false,
				OnlinePayAmount: "50.00",
				ReceiveAddress:  "TLq32V4saMHPS71juNAZ6KBmhTTSLCRkp6",
				PayAmount:       "6.9800",
				Token:           "usdt",
				Network:         "tron",
				ExpiresAt:       "2026-08-06 23:30",
				PayURL:          "https://pay.example.com/counter",
			},
		},
		Wallet:   &stubWallet{bal: "0.00"},
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u"}},
	}, bot, func() string { return "zh-CN" })

	svc.handle(context.Background(), "tok", webhookdomain.Update{
		Message: &webhookdomain.Message{
			Chat: webhookdomain.Chat{ID: 100, Type: "private"},
			From: &webhookdomain.User{ID: 200, UserName: "alice"},
			Text: "/shop",
		},
	})
	svc.handle(context.Background(), "tok", webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{
			ID: "cb1", From: webhookdomain.User{ID: 200}, Data: cbProdPrefix + "dx",
			Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}},
		},
	})
	if _, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{
			ID: "cb2", From: webhookdomain.User{ID: 200}, Data: cbPayOnline,
			Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}},
		},
	}); err != nil {
		t.Fatalf("pay online err: %v", err)
	}
	// epusdt 付款信息现在作为图片消息的 caption 发送（二维码为主图，一条消息）。
	if len(bot.photos) != 1 {
		t.Fatalf("expected 1 photo message with payment caption, got: %d", len(bot.photos))
	}
	last := bot.photos[0]
	for _, want := range []string{"TLq32V4saMHPS71juNAZ6KBmhTTSLCRkp6", "6.98", "USDT", "TRC20", "O3"} {
		if !containsStr(last, want) {
			t.Fatalf("expected %q in epusdt payment caption, got: %v", want, last)
		}
	}
	// 地址应为代码块（可点按复制），且不再出现 store 币种金额行
	if !containsStr(last, "```") {
		t.Fatalf("expected address in code block, got: %v", last)
	}
	if containsStr(last, "50.00 CNY") {
		t.Fatalf("store currency line should be removed, got: %v", last)
	}
	markup := bot.markups[len(bot.markups)-1]
	if !keyboardContains(markup, "刷新支付状态") {
		t.Fatalf("expected refresh payment status button, got: %+v", markup)
	}
	if !keyboardContains(markup, "返回主页") {
		t.Fatalf("expected exit home button, got: %+v", markup)
	}
}

func TestPurchaseServiceProductDescriptionShown(t *testing.T) {
	product := &contract.ShopProduct{
		ID: 10, Slug: "dx", Title: "迪士尼卡", Description: "迪士尼正版卡密，秒发。",
		Currency: "CNY", PriceAmount: "50.00", FulfillmentType: "auto",
	}
	bot := &fakeBotAPI{}
	svc := newPurchaseService(contract.PurchasePorts{
		Catalog: &stubCatalog{
			bySlug: map[string]*contract.ShopProduct{"dx": product},
		},
		Orders:   &stubOrders{},
		Wallet:   &stubWallet{bal: "0.00"},
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u"}},
	}, bot, func() string { return "zh-CN" })

	svc.handle(context.Background(), "tok", webhookdomain.Update{
		Message: &webhookdomain.Message{
			Chat: webhookdomain.Chat{ID: 100, Type: "private"},
			From: &webhookdomain.User{ID: 200, UserName: "alice"},
			Text: "/shop",
		},
	})
	svc.handle(context.Background(), "tok", webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{
			ID: "cb1", From: webhookdomain.User{ID: 200}, Data: cbProdPrefix + "dx",
			Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}},
		},
	})
	last := bot.sent[len(bot.sent)-1]
	if !containsStr(last, "迪士尼正版卡密") {
		t.Fatalf("expected product description, got: %v", last)
	}
}

func TestPurchaseServicePayOnlineMultiChannel(t *testing.T) {
	product := &contract.ShopProduct{
		ID: 10, Slug: "dx", Title: "迪士尼卡", Currency: "CNY",
		PriceAmount: "50.00", FulfillmentType: "auto",
	}
	bot := &fakeBotAPI{}
	svc := newPurchaseService(contract.PurchasePorts{
		Catalog: &stubCatalog{
			bySlug: map[string]*contract.ShopProduct{"dx": product},
		},
		Orders: &stubOrders{
			created: &contract.PurchaseCreated{OrderID: 3, OrderNo: "O3", Currency: "CNY", TotalAmount: "50.00"},
		},
		Payments: &stubPayments{
			channels: []contract.ShopPaymentChannel{
				{ID: 1, Name: "EPUSDT", ChannelType: "epusdt"},
				{ID: 2, Name: "USDT 二", ChannelType: "usdt-trc20"},
			},
			result: &contract.PurchasePaymentResult{
				OrderPaid: false, OnlinePayAmount: "50.00", PayURL: "https://pay.example.com/3",
			},
		},
		Wallet:   &stubWallet{bal: "0.00"},
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u"}},
	}, bot, func() string { return "zh-CN" })

	svc.handle(context.Background(), "tok", webhookdomain.Update{
		Message: &webhookdomain.Message{
			Chat: webhookdomain.Chat{ID: 100, Type: "private"},
			From: &webhookdomain.User{ID: 200, UserName: "alice"},
			Text: "/shop",
		},
	})
	svc.handle(context.Background(), "tok", webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{
			ID: "cb1", From: webhookdomain.User{ID: 200}, Data: cbProdPrefix + "dx",
			Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}},
		},
	})
	// 点在线支付：应出现渠道选择键盘
	if _, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{
			ID: "cb2", From: webhookdomain.User{ID: 200}, Data: cbPayOnline,
			Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}},
		},
	}); err != nil {
		t.Fatalf("pay online err: %v", err)
	}
	last := bot.markups[len(bot.markups)-1]
	if !keyboardContains(last, "USDT 二") {
		t.Fatalf("expected channel keyboard with USDT 二, got: %+v", last)
	}
	// 选渠道 2 → 发起支付
	if _, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{
			ID: "cb3", From: webhookdomain.User{ID: 200}, Data: cbPayChannel + "2",
			Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}},
		},
	}); err != nil {
		t.Fatalf("select channel err: %v", err)
	}
	joined := ""
	for _, m := range bot.sent {
		joined += m + "\n"
	}
	if !containsStr(joined, "O3") || !containsStr(joined, "https://pay.example.com/3") {
		t.Fatalf("expected order no and pay url, got: %v", bot.sent)
	}
}

type stubOrderReader struct {
	orders []contract.ShopOrder
	detail *contract.ShopOrderDetail
}

func (o *stubOrderReader) ListOrders(context.Context, uint, int, int) ([]contract.ShopOrder, int64, error) {
	return o.orders, int64(len(o.orders)), nil
}
func (o *stubOrderReader) GetOrderByOrderNo(context.Context, uint, string) (*contract.ShopOrderDetail, error) {
	return o.detail, nil
}

type stubRecharge struct {
	recharge *contract.ShopRecharge
	calls    int
}

func (r *stubRecharge) CreateRecharge(context.Context, contract.PurchaseRechargeInput) (*contract.ShopRecharge, error) {
	r.calls++
	return r.recharge, nil
}
func (r *stubRecharge) GetRechargeStatus(context.Context, uint, string) (*contract.ShopRecharge, error) {
	return r.recharge, nil
}

func TestPurchaseServiceOrdersAndDetail(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newPurchaseService(contract.PurchasePorts{
		Catalog:     &stubCatalog{},
		Orders:      &stubOrders{},
		Identity:    &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u", Locale: "zh-CN"}},
		OrderReader: &stubOrderReader{
			orders: []contract.ShopOrder{{OrderNo: "O1", Status: "completed", Currency: "CNY", TotalAmount: "10.00", Title: "迪士尼卡"}},
			detail: &contract.ShopOrderDetail{
				OrderNo: "O1", Status: "completed", Currency: "CNY", TotalAmount: "10.00",
				Items:      []contract.ShopOrderItem{{Title: "迪士尼卡", Quantity: 1, UnitPrice: "10.00"}},
				Fulfillment: &contract.ShopFulfillment{Type: "auto", Status: "delivered", Payload: "CARD-1\nCARD-2"},
			},
		},
	}, bot, func() string { return "zh-CN" })

	// 我的订单
	if err := svc.renderOrders(context.Background(), "tok", 100, 1, webhookdomain.User{ID: 200, UserName: "alice"}); err != nil {
		t.Fatalf("renderOrders err: %v", err)
	}
	if len(bot.sent) == 0 || !containsStr(bot.sent[0], "我的订单") {
		t.Fatalf("expected order list title, got: %v", bot.sent)
	}
	if len(bot.markups) == 0 || !keyboardContains(bot.markups[len(bot.markups)-1], "迪士尼卡") {
		t.Fatalf("expected order button with 迪士尼卡, got: %+v", bot.markups)
	}
	// 订单详情（含卡密）
	if err := svc.showOrderDetail(context.Background(), "tok", 100, "O1", webhookdomain.User{ID: 200, UserName: "alice"}); err != nil {
		t.Fatalf("showOrderDetail err: %v", err)
	}
	last := bot.sent[len(bot.sent)-1]
	if !containsStr(last, "CARD-1") || !containsStr(last, "CARD-2") {
		t.Fatalf("expected cards in detail, got: %v", last)
	}
}

func TestPurchaseServiceLanguageToggle(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newPurchaseService(contract.PurchasePorts{
		Catalog:  &stubCatalog{},
		Orders:   &stubOrders{},
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u", Locale: "zh-CN"}},
	}, bot, func() string { return "zh-CN" })

	if err := svc.toggleLanguage(context.Background(), "tok", 100, webhookdomain.User{ID: 200, UserName: "alice"}); err != nil {
		t.Fatalf("toggleLanguage err: %v", err)
	}
	if len(bot.sent) == 0 || !containsStr(bot.sent[0], "English") {
		t.Fatalf("expected English switch message, got: %v", bot.sent)
	}
}

func TestPurchaseServiceRecharge(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newPurchaseService(contract.PurchasePorts{
		Catalog:  &stubCatalog{},
		Orders:   &stubOrders{},
		Payments: &stubPayments{channels: []contract.ShopPaymentChannel{{ID: 1, Name: "USDT", ChannelType: "epusdt"}}},
		Recharge: &stubRecharge{recharge: &contract.ShopRecharge{
			RechargeNo: "WR1", PayableAmount: "100.00", Currency: "CNY", Status: "pending",
			PayURL: "https://pay.example.com/wr1",
		}},
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u", Locale: "zh-CN"}},
		Settings: &stubSettings{cur: "CNY"},
	}, bot, func() string { return "zh-CN" })

	// 进入充值
	if err := svc.enterRecharge(context.Background(), "tok", 100, &webhookdomain.User{ID: 200, UserName: "alice"}); err != nil {
		t.Fatalf("enterRecharge err: %v", err)
	}
	// 输入金额
	view := svc.snapshot(100)
	if err := svc.handleRechargeAmount(context.Background(), "tok", view, "100", &webhookdomain.User{ID: 200, UserName: "alice"}); err != nil {
		t.Fatalf("handleRechargeAmount err: %v", err)
	}
	last := bot.sent[len(bot.sent)-1]
	if !containsStr(last, "WR1") || !containsStr(last, "https://pay.example.com/wr1") {
		t.Fatalf("expected recharge order + pay url, got: %v", last)
	}
}

func TestPurchaseServiceRechargeEpusdtShowsAddressAndQR(t *testing.T) {
	bot := &fakeBotAPI{}
	rechargeStub := &stubRecharge{recharge: &contract.ShopRecharge{
		RechargeNo: "WR2", PayableAmount: "100.00", Currency: "CNY", Status: "pending",
		ReceiveAddress: "TLq32V4saMHPS71juNAZ6KBmhTTSLCRkp6",
		PayAmount:      "6.98",
		Token:          "usdt",
		Network:        "tron",
		ExpiresAt:      "2026-08-06 23:30",
		PayURL:         "https://pay.example.com/wr2",
	}}
	svc := newPurchaseService(contract.PurchasePorts{
		Catalog:  &stubCatalog{},
		Orders:   &stubOrders{},
		Payments: &stubPayments{channels: []contract.ShopPaymentChannel{{ID: 1, Name: "USDT", ChannelType: "epusdt"}}},
		Recharge: rechargeStub,
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u", Locale: "zh-CN"}},
		Settings: &stubSettings{cur: "CNY"},
	}, bot, func() string { return "zh-CN" })

	// 进入充值并输入金额（单 epusdt 渠道直接创建充值）
	if err := svc.enterRecharge(context.Background(), "tok", 100, &webhookdomain.User{ID: 200, UserName: "alice"}); err != nil {
		t.Fatalf("enterRecharge err: %v", err)
	}
	view := svc.snapshot(100)
	if err := svc.handleRechargeAmount(context.Background(), "tok", view, "100", &webhookdomain.User{ID: 200, UserName: "alice"}); err != nil {
		t.Fatalf("handleRechargeAmount err: %v", err)
	}
	// 充值信息现在作为图片消息的 caption 发送（二维码为主图，一条消息）。
	if len(bot.photos) != 1 {
		t.Fatalf("expected 1 photo message with recharge caption, got: %d", len(bot.photos))
	}
	last := bot.photos[0]
	for _, want := range []string{"TLq32V4saMHPS71juNAZ6KBmhTTSLCRkp6", "6.98", "USDT", "TRC20", "WR2"} {
		if !containsStr(last, want) {
			t.Fatalf("expected %q in recharge caption, got: %v", want, last)
		}
	}
	if !containsStr(last, "```") {
		t.Fatalf("expected address in code block, got: %v", last)
	}
	if containsStr(last, "100.00 CNY") {
		t.Fatalf("store currency line should be removed, got: %v", last)
	}
	markup := bot.markups[len(bot.markups)-1]
	if !keyboardContains(markup, "返回主页") {
		t.Fatalf("expected exit home button, got: %+v", markup)
	}

	// 付款页展示后会话已清空：再次输入数字应未被消费（回退主菜单），且不重复创建充值订单。
	handled, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		Message: &webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}, Text: "200"},
	})
	if err != nil {
		t.Fatalf("second input err: %v", err)
	}
	if handled {
		t.Fatalf("expected second input NOT handled after payment page")
	}
	if rechargeStub.calls != 1 {
		t.Fatalf("expected exactly 1 recharge order, got %d", rechargeStub.calls)
	}
}

func TestPurchaseServiceBinStock(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newPurchaseService(contract.PurchasePorts{
		Catalog: &stubCatalog{
			products: []contract.ShopProduct{
				{ID: 1, Title: "卡A", PriceAmount: "10.00", Currency: "CNY", PickEnabled: true},
				{ID: 2, Title: "卡B", PriceAmount: "20.00", Currency: "CNY", PickEnabled: false},
			},
			binCountBy: map[string]int64{"123456": 5},
		},
		Orders:   &stubOrders{},
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u"}},
	}, bot, func() string { return "zh-CN" })

	if err := svc.enterBinStock(context.Background(), "tok", 100); err != nil {
		t.Fatalf("enterBinStock err: %v", err)
	}
	view := svc.snapshot(100)
	if err := svc.handleBinStockInput(context.Background(), "tok", view, "123456"); err != nil {
		t.Fatalf("handleBinStockInput err: %v", err)
	}
	last := bot.sent[len(bot.sent)-1]
	if !containsStr(last, "5") || !containsStr(last, "卡A") {
		t.Fatalf("expected bin stock summary, got: %v", last)
	}
}

func TestPurchaseServicePickOptionLabelsLocalized(t *testing.T) {
	svc := newPurchaseService(contract.PurchasePorts{}, &fakeBotAPI{}, func() string { return "zh-CN" })
	stock := &contract.ShopPickStock{
		Brands: []contract.ShopPickBrand{
			{Key: "random", Name: "随机"},
			{Key: "visa", Name: "Visa"},
			{Key: "mastercard", Name: "Mastercard"},
		},
		CardTypes: []contract.ShopPickCardType{
			{Key: "random", Name: "随机"},
			{Key: "D", Name: "D卡（含预付）"},
			{Key: "PD", Name: "纯D（不含预付）"},
			{Key: "C", Name: "纯C"},
		},
	}
	zh := &purchaseView{locale: "zh-CN", pickStock: stock}
	en := &purchaseView{locale: "en-US", pickStock: stock}

	zhBrands := svc.brandKeyboard(zh)
	if !keyboardContains(zhBrands, "随机") {
		t.Fatalf("zh brand keyboard should show 随机, got: %+v", zhBrands)
	}
	enBrands := svc.brandKeyboard(en)
	if keyboardContains(enBrands, "随机") {
		t.Fatalf("en brand keyboard should not show 随机, got: %+v", enBrands)
	}
	for _, want := range []string{"Random", "Visa", "Mastercard"} {
		if !keyboardContains(enBrands, want) {
			t.Fatalf("en brand keyboard should show %q, got: %+v", want, enBrands)
		}
	}

	enTypes := svc.cardTypeKeyboard(en)
	if keyboardContains(enTypes, "D卡（含预付）") {
		t.Fatalf("en card type keyboard should not show Chinese labels, got: %+v", enTypes)
	}
	for _, want := range []string{"Random", "D (incl. prepaid)", "Pure D (excl. prepaid)", "Pure C"} {
		if !keyboardContains(enTypes, want) {
			t.Fatalf("en card type keyboard should show %q, got: %+v", want, enTypes)
		}
	}
}

func TestPurchaseServiceToggleLanguageSendsNewLocaleMenu(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newPurchaseService(contract.PurchasePorts{
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u", Locale: "zh-CN"}},
	}, bot, func() string { return "zh-CN" })
	var gotLocale, gotChatID string
	svc.setMainMenuRenderer(func(_ context.Context, _, chatID, locale string) error {
		gotChatID = chatID
		gotLocale = locale
		return nil
	})
	if err := svc.toggleLanguage(context.Background(), "tok", 100, webhookdomain.User{ID: 200, UserName: "alice"}); err != nil {
		t.Fatalf("toggleLanguage err: %v", err)
	}
	if gotLocale != "en-US" {
		t.Fatalf("expected main menu in en-US, got %q", gotLocale)
	}
	if gotChatID != "100" {
		t.Fatalf("expected chatID 100, got %q", gotChatID)
	}

	// 反向切换回中文
	svc2 := newPurchaseService(contract.PurchasePorts{
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u", Locale: "en-US"}},
	}, &fakeBotAPI{}, func() string { return "en-US" })
	gotLocale = ""
	svc2.setMainMenuRenderer(func(_ context.Context, _, _, locale string) error {
		gotLocale = locale
		return nil
	})
	if err := svc2.toggleLanguage(context.Background(), "tok", 100, webhookdomain.User{ID: 200, UserName: "alice"}); err != nil {
		t.Fatalf("toggleLanguage en->zh err: %v", err)
	}
	if gotLocale != "zh-CN" {
		t.Fatalf("expected main menu in zh-CN, got %q", gotLocale)
	}
}

func TestPurchaseServiceToggleLanguageReRendersShopStep(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newPurchaseService(contract.PurchasePorts{
		Catalog: &stubCatalog{cats: []contract.ShopCategory{{ID: 1, Name: "Cards"}}},
		Orders:  &stubOrders{},
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u", Locale: "zh-CN"}},
	}, bot, func() string { return "zh-CN" })
	// 进入 /shop（会话处于浏览分类步骤）
	if _, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		Message: &webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}, From: &webhookdomain.User{ID: 200, UserName: "alice"}, Text: "/shop"},
	}); err != nil {
		t.Fatalf("/shop err: %v", err)
	}
	// 切换到英文：应以英文重渲当前分类页（不再发送独立确认行）
	if err := svc.toggleLanguage(context.Background(), "tok", 100, webhookdomain.User{ID: 200, UserName: "alice"}); err != nil {
		t.Fatalf("toggleLanguage err: %v", err)
	}
	last := bot.sent[len(bot.sent)-1]
	if !containsStr(last, "Select a category") {
		t.Fatalf("expected re-rendered categories in English, got: %v", last)
	}
}

// sendEscapeCommand 发送一个退出命令（/start / /menu / /help / /cancel）并返回是否被 bot 消费。
func sendEscapeCommand(svc *purchaseService, chatID int64, cmd string) (bool, error) {
	handled, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		Message: &webhookdomain.Message{
			Chat: webhookdomain.Chat{ID: chatID, Type: "private"},
			From: &webhookdomain.User{ID: 200, UserName: "alice"},
			Text: cmd,
		},
	})
	return handled, err
}

// newBinstockService 构造一个进入「卡头库存」步骤的 service。
func newBinstockService(t *testing.T, bot *fakeBotAPI) *purchaseService {
	t.Helper()
	svc := newPurchaseService(contract.PurchasePorts{
		Catalog: &stubCatalog{products: []contract.ShopProduct{
			{ID: 1, Title: "卡A", PriceAmount: "10.00", Currency: "CNY", PickEnabled: true},
		}, binCountBy: map[string]int64{"123456": 5}},
		Orders:   &stubOrders{},
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u"}},
	}, bot, func() string { return "zh-CN" })
	if err := svc.enterBinStock(context.Background(), "tok", 100); err != nil {
		t.Fatalf("enterBinStock err: %v", err)
	}
	if svc.snapshot(100) == nil {
		t.Fatalf("expected a bin_stock session")
	}
	return svc
}

// TestPurchaseServiceBinStockEscapeByStart 验证在「卡头库存」输入步骤发 /start 能退出并清空会话。
func TestPurchaseServiceBinStockEscapeByStart(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newBinstockService(t, bot)
	handled, err := sendEscapeCommand(svc, 100, "/start")
	if err != nil {
		t.Fatalf("/start err: %v", err)
	}
	// /start 应交还主 Service（handled=false），且会话被清空。
	if handled {
		t.Fatalf("expected /start to fall through to main Service (handled=false), got handled=true")
	}
	if svc.snapshot(100) != nil {
		t.Fatalf("expected session cleared after /start, but session still exists")
	}
}

// TestPurchaseServiceBinStockEscapeByCancel 验证 /cancel 直接取消并清空会话。
func TestPurchaseServiceBinStockEscapeByCancel(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newBinstockService(t, bot)
	handled, err := sendEscapeCommand(svc, 100, "/cancel")
	if err != nil {
		t.Fatalf("/cancel err: %v", err)
	}
	if !handled {
		t.Fatalf("expected /cancel handled by purchase service, got handled=false")
	}
	if svc.snapshot(100) != nil {
		t.Fatalf("expected session cleared after /cancel")
	}
	last := bot.sent[len(bot.sent)-1]
	if !containsStr(last, "已取消购买") {
		t.Fatalf("expected cancel confirmation, got: %v", last)
	}
}

// TestPurchaseServiceBinStockEscapeByMenuAndHelp 验证 /menu 与 /help 同样能退出并清空会话。
func TestPurchaseServiceBinStockEscapeByMenuAndHelp(t *testing.T) {
	for _, cmd := range []string{"/menu", "/help"} {
		bot := &fakeBotAPI{}
		svc := newBinstockService(t, bot)
		handled, err := sendEscapeCommand(svc, 100, cmd)
		if err != nil {
			t.Fatalf("%s err: %v", cmd, err)
		}
		if handled {
			t.Fatalf("%s should fall through to main Service (handled=false)", cmd)
		}
		if svc.snapshot(100) != nil {
			t.Fatalf("expected session cleared after %s", cmd)
		}
	}
}

// newRechargeService 构造一个进入「充值金额」输入步骤的 service。
func newRechargeService(t *testing.T, bot *fakeBotAPI) *purchaseService {
	t.Helper()
	svc := newPurchaseService(contract.PurchasePorts{
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u"}},
		Settings: &stubSettings{cur: "USD", name: "shop"},
		Recharge: &stubRecharge{recharge: &contract.ShopRecharge{}},
	}, bot, func() string { return "zh-CN" })
	if err := svc.enterRecharge(context.Background(), "tok", 100, &webhookdomain.User{ID: 200, UserName: "alice"}); err != nil {
		t.Fatalf("enterRecharge err: %v", err)
	}
	if svc.snapshot(100) == nil {
		t.Fatalf("expected a recharge_amount session")
	}
	return svc
}

// TestPurchaseServiceRechargeAmountEscapeByStart 验证在「充值金额」步骤发 /start 能退出。
func TestPurchaseServiceRechargeAmountEscapeByStart(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newRechargeService(t, bot)
	handled, err := sendEscapeCommand(svc, 100, "/start")
	if err != nil {
		t.Fatalf("/start err: %v", err)
	}
	if handled {
		t.Fatalf("expected /start to fall through to main Service, got handled=true")
	}
	if svc.snapshot(100) != nil {
		t.Fatalf("expected session cleared after /start in recharge step")
	}
}

// TestPurchaseServiceRechargeAmountEscapeByCancel 验证 /cancel 在充值步骤也能取消。
func TestPurchaseServiceRechargeAmountEscapeByCancel(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newRechargeService(t, bot)
	handled, err := sendEscapeCommand(svc, 100, "/cancel")
	if err != nil {
		t.Fatalf("/cancel err: %v", err)
	}
	if !handled {
		t.Fatalf("expected /cancel handled")
	}
	if svc.snapshot(100) != nil {
		t.Fatalf("expected session cleared after /cancel in recharge step")
	}
}

// newConfigureService 构造一个进入「商品配置（挑卡）」步骤的 service。
func newConfigureService(t *testing.T, bot *fakeBotAPI) *purchaseService {
	t.Helper()
	product := &contract.ShopProduct{
		ID: 10, Slug: "dx", Title: "迪士尼卡", Currency: "CNY",
		PriceAmount: "50.00", FulfillmentType: "auto",
		PickEnabled: true, PickPrices: map[string]string{"bin": "5.00"},
	}
	svc := newPurchaseService(contract.PurchasePorts{
		Catalog: &stubCatalog{
			cats:     []contract.ShopCategory{{ID: 1, Name: "卡密"}},
			products: []contract.ShopProduct{*product}, bySlug: map[string]*contract.ShopProduct{"dx": product},
		},
		Orders:   &stubOrders{},
		Payments: &stubPayments{},
		Wallet:   &stubWallet{},
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u"}},
		Settings: &stubSettings{cur: "CNY", name: "shop"},
	}, bot, func() string { return "zh-CN" })
	// 进入 /shop → 选分类 → 选商品 → 进入配置面板
	if _, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		Message: &webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}, From: &webhookdomain.User{ID: 200, UserName: "alice"}, Text: "/shop"},
	}); err != nil {
		t.Fatalf("/shop err: %v", err)
	}
	if _, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{
			ID: "c1", From: webhookdomain.User{ID: 200, UserName: "alice"},
			Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}}, Data: "shop:cat:1",
		},
	}); err != nil {
		t.Fatalf("select cat err: %v", err)
	}
	if _, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{
			ID: "c2", From: webhookdomain.User{ID: 200, UserName: "alice"},
			Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}}, Data: "shop:prod:dx",
		},
	}); err != nil {
		t.Fatalf("select prod err: %v", err)
	}
	if svc.snapshot(100) == nil || svc.snapshot(100).step != purchaseStepConfigure {
		t.Fatalf("expected configure step, got step=%v", func() interface{} {
			if v := svc.snapshot(100); v != nil {
				return v.step
			}
			return "<nil>"
		}())
	}
	return svc
}

// TestPurchaseServiceConfigureEscapeByStart 验证在「配置挑卡」步骤发 /start 能退出。
func TestPurchaseServiceConfigureEscapeByStart(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newConfigureService(t, bot)
	handled, err := sendEscapeCommand(svc, 100, "/start")
	if err != nil {
		t.Fatalf("/start err: %v", err)
	}
	if handled {
		t.Fatalf("expected /start to fall through to main Service from configure step")
	}
	if svc.snapshot(100) != nil {
		t.Fatalf("expected session cleared after /start from configure step")
	}
}

// TestPurchaseServiceBinStockInvalidTextReprompts 验证无效文本（非命令）仍被按 BIN 处理（提示重输），不会悄悄退出。
func TestPurchaseServiceBinStockInvalidTextReprompts(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newBinstockService(t, bot)
	handled, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		Message: &webhookdomain.Message{
			Chat: webhookdomain.Chat{ID: 100, Type: "private"},
			From: &webhookdomain.User{ID: 200, UserName: "alice"},
			Text: "hello",
		},
	})
	if err != nil {
		t.Fatalf("handle err: %v", err)
	}
	if !handled {
		t.Fatalf("expected invalid text handled by bin stock step (reprompt), got handled=false")
	}
	// 无效输入不应清空会话（用户仍在等 BIN）
	if svc.snapshot(100) == nil {
		t.Fatalf("session should remain after invalid non-command text")
	}
	last := bot.sent[len(bot.sent)-1]
	if !containsStr(last, "请输入 6 位卡头") {
		t.Fatalf("expected re-prompt for BIN, got: %v", last)
	}
}

// TestPurchaseServiceBinStockPromptHasCancelKeyboard 验证卡头库存入口提示带「取消」按钮，提供可见退出路径。
func TestPurchaseServiceBinStockPromptHasCancelKeyboard(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newPurchaseService(contract.PurchasePorts{
		Catalog: &stubCatalog{}, Orders: &stubOrders{},
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u"}},
	}, bot, func() string { return "zh-CN" })
	if err := svc.enterBinStock(context.Background(), "tok", 100); err != nil {
		t.Fatalf("enterBinStock err: %v", err)
	}
	if len(bot.markups) == 0 {
		t.Fatalf("expected a cancel keyboard on bin stock prompt")
	}
	if !keyboardContains(bot.markups[len(bot.markups)-1], "取消") {
		t.Fatalf("expected cancel button on bin stock prompt, got: %+v", bot.markups)
	}
}

// TestPurchaseServiceRechargePromptHasCancelKeyboard 验证充值金额入口提示带「取消」按钮。
func TestPurchaseServiceRechargePromptHasCancelKeyboard(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newPurchaseService(contract.PurchasePorts{
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u"}},
		Settings: &stubSettings{cur: "USD", name: "shop"},
		Recharge: &stubRecharge{recharge: &contract.ShopRecharge{}},
	}, bot, func() string { return "zh-CN" })
	if err := svc.enterRecharge(context.Background(), "tok", 100, &webhookdomain.User{ID: 200, UserName: "alice"}); err != nil {
		t.Fatalf("enterRecharge err: %v", err)
	}
	if len(bot.markups) == 0 {
		t.Fatalf("expected a cancel keyboard on recharge prompt")
	}
	if !keyboardContains(bot.markups[len(bot.markups)-1], "取消") {
		t.Fatalf("expected cancel button on recharge prompt, got: %+v", bot.markups)
	}
}

// --- 新购买流程（8 按钮 + 首位数字 + reply 键盘测活）测试 ---

// newBuyFlowService 构造启用挑卡+测活的商品，用于新购买流程测试。
func newBuyFlowService(t *testing.T, bot *fakeBotAPI, withCardCheck bool) *purchaseService {
	t.Helper()
	product := &contract.ShopProduct{
		ID: 10, Slug: "dx", Title: "挑卡商品", Currency: "CNY",
		PriceAmount: "0.10", FulfillmentType: "auto",
		PickEnabled: true, PickPrices: map[string]string{
			"bin": "0.10", "head4": "0.05", "C": "0.02", "D": "0.03",
		},
		CardCheckEnabled: withCardCheck,
		CardCheckFee:     "0.90",
		StockAvailable:  100,
	}
	return newPurchaseService(contract.PurchasePorts{
		Catalog: &stubCatalog{
			cats:     []contract.ShopCategory{{ID: 1, Name: "卡密"}},
			products: []contract.ShopProduct{*product},
			bySlug:   map[string]*contract.ShopProduct{"dx": product},
			pickStock: &contract.ShopPickStock{
				Countries: []contract.ShopPickCountry{{Code: "US", Name: "美国", Stock: 50}},
			},
			heads: []contract.ShopBinHead{{Head: "4", Stock: 30}},
		},
		Orders: &stubOrders{
			preview: &contract.PurchasePreview{
				Currency: "CNY", TotalAmount: "0.10",
				Items: []contract.PurchasePreviewItem{{ProductID: 10, Quantity: 1, UnitPrice: "0.10", TotalPrice: "0.10", Title: "挑卡商品", PickCountry: "US"}},
			},
			created: &contract.PurchaseCreated{OrderID: 1, OrderNo: "O10", Currency: "CNY", TotalAmount: "0.10"},
		},
		Payments: &stubPayments{result: &contract.PurchasePaymentResult{OrderPaid: true}},
		Wallet:   &stubWallet{},
		Identity: &stubIdentity{user: &contract.PurchaseUser{ID: 7, DisplayName: "u"}},
		Settings: &stubSettings{cur: "CNY", name: "商店"},
	}, bot, func() string { return "zh-CN" })
}

func TestPurchaseServiceRenderDetailShowsTwoPriceLinesAndBuyButtons(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newBuyFlowService(t, bot, true)
	handle := func(u webhookdomain.Update) {
		if _, err := svc.handle(context.Background(), "tok", u); err != nil {
			t.Fatalf("handle err: %v", err)
		}
	}
	handle(webhookdomain.Update{Message: &webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}, From: &webhookdomain.User{ID: 200}, Text: "/shop"}})
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "1", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbCatPrefix + "1"}})
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "2", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbProdPrefix + "dx"}})

	// 商品初始页文本：随机购买两档价格 + 挑头购买两档价格，不显示基础价行。
	last := bot.sent[len(bot.sent)-1]
	if !containsStr(last, "随机购买") || !containsStr(last, "不测活价") || !containsStr(last, "测活价") {
		t.Fatalf("expected two-tier price lines, got: %s", last)
	}
	if containsStr(last, "价格 0.10") { // 不应再显示「价格 <基础价>」行
		t.Fatalf("detail must not show base price line, got: %s", last)
	}
	// 键盘含 8 个购买类型按钮。
	mk := bot.markups[len(bot.markups)-1]
	for _, want := range []string{"随机购买", "挑头购买", "3头", "4头", "5头", "6头", "CREDIT", "DEBIT"} {
		if !keyboardContains(mk, want) {
			t.Fatalf("expected buy-type button %q in keyboard, got: %+v", want, mk)
		}
	}
	// 挑头购买按钮不应显示库存（库存留待输入 BIN 后回显）。
	binBtn := findKeyboardButton(mk, "挑头购买")
	if binBtn == "" {
		t.Fatalf("expected 挑头购买 button in keyboard, got: %+v", mk)
	}
	if strings.Contains(binBtn, "[") {
		t.Fatalf("挑头购买 button must not show stock, got: %s", binBtn)
	}
	// 随机购买按钮库存括号内只显示数字 [100]，价格后缀用 U。
	randomBtn := findKeyboardButton(mk, "随机购买")
	if !strings.Contains(randomBtn, "[100]") {
		t.Fatalf("随机购买 button should show stock as [100], got: %s", randomBtn)
	}
	if strings.Contains(randomBtn, "可发") || strings.Contains(randomBtn, "库存") {
		t.Fatalf("随机购买 button stock must be digits only, got: %s", randomBtn)
	}
	if !strings.Contains(randomBtn, " U") {
		t.Fatalf("随机购买 button price should use U suffix, got: %s", randomBtn)
	}
	// 大按钮（随机/挑头）各占一行；小按钮（3/4/5/6头、CREDIT/DEBIT）一行两个。
	rows := mk.InlineKeyboard
	randomRowIdx := -1
	for i, row := range rows {
		if len(row) == 1 && containsStr(row[0].Text, "随机购买") {
			randomRowIdx = i
			break
		}
	}
	if randomRowIdx < 0 {
		t.Fatalf("expected 随机购买 on its own row, got: %+v", rows)
	}
	if randomRowIdx+1 >= len(rows) || len(rows[randomRowIdx+1]) != 1 || !containsStr(rows[randomRowIdx+1][0].Text, "挑头购买") {
		t.Fatalf("expected 挑头购买 on its own row right after 随机购买, got: %+v", rows)
	}
	// 后续小按钮行每行 2 个：3头+4头、5头+6头、CREDIT+DEBIT。
	for i := randomRowIdx + 2; i < len(rows); i++ {
		row := rows[i]
		// 排除尾部「返回/取消」导航行（2 个但文本含 🔙/❌）。
		if containsStr(row[0].Text, "🔙") || containsStr(row[0].Text, "❌") {
			continue
		}
		if len(row) != 2 {
			t.Fatalf("expected small buy-type rows of 2 buttons, row %d got %d: %+v", i, len(row), row)
		}
	}
}

func TestPurchaseServiceBuyTypeRandomToCountryThenConfirm(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newBuyFlowService(t, bot, false) // 非测活商品：选国家后直接确认
	handle := func(u webhookdomain.Update) {
		if _, err := svc.handle(context.Background(), "tok", u); err != nil {
			t.Fatalf("handle err: %v", err)
		}
	}
	handle(webhookdomain.Update{Message: &webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}, From: &webhookdomain.User{ID: 200}, Text: "/shop"}})
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "1", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbCatPrefix + "1"}})
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "2", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbProdPrefix + "dx"}})
	// 点随机购买
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "3", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbBuyPrefix + "random"}})
	// 选国家 US
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "4", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbCountryPrefix + "US"}})
	// 非测活商品：选国家后直接进确认页
	last := bot.sent[len(bot.sent)-1]
	if !containsStr(last, "订单确认") {
		t.Fatalf("expected confirm page after country for non-cardcheck product, got: %s", last)
	}
}

func TestPurchaseServiceBuyTypeRandomToCountryThenCheckChoiceReplyKeyboard(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newBuyFlowService(t, bot, true) // 测活商品：选国家后进测活选择
	handle := func(u webhookdomain.Update) {
		if _, err := svc.handle(context.Background(), "tok", u); err != nil {
			t.Fatalf("handle err: %v", err)
		}
	}
	handle(webhookdomain.Update{Message: &webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}, From: &webhookdomain.User{ID: 200}, Text: "/shop"}})
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "1", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbCatPrefix + "1"}})
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "2", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbProdPrefix + "dx"}})
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "3", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbBuyPrefix + "random"}})
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "4", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbCountryPrefix + "US"}})
	// 测活商品：应进入测活选择，文本含「请选择是否测活」
	last := bot.sent[len(bot.sent)-1]
	if !containsStr(last, "请选择是否测活") {
		t.Fatalf("expected check-choice prompt, got: %s", last)
	}
	// 应回 reply 键盘（毛料/包活），非 inline。
	if len(bot.replyMarkups) == 0 {
		t.Fatalf("expected a reply keyboard markup for check choice")
	}
	rk, ok := bot.replyMarkups[len(bot.replyMarkups)-1].(replyKeyboard)
	if !ok {
		t.Fatalf("expected replyKeyboard markup, got: %T", bot.replyMarkups[len(bot.replyMarkups)-1])
	}
	var labels []string
	for _, row := range rk.Keyboard {
		for _, b := range row {
			labels = append(labels, b.Text)
		}
	}
	if !containsStr(strings.Join(labels, ","), "毛料") || !containsStr(strings.Join(labels, ","), "包活") {
		t.Fatalf("expected 毛料/包活 reply buttons, got: %v", labels)
	}
	// 用户发送「包活」
	handle(webhookdomain.Update{Message: &webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}, Text: "包活"}})
	// 应回 replyKeyboardRemove + 确认页
	last = bot.sent[len(bot.sent)-1]
	if !containsStr(last, "订单确认") {
		t.Fatalf("expected confirm page after check choice, got: %s", last)
	}
}

func TestPurchaseServiceBuyTypeBinEntersBinInput(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newBuyFlowService(t, bot, false)
	handle := func(u webhookdomain.Update) {
		if _, err := svc.handle(context.Background(), "tok", u); err != nil {
			t.Fatalf("handle err: %v", err)
		}
	}
	handle(webhookdomain.Update{Message: &webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100, Type: "private"}, From: &webhookdomain.User{ID: 200}, Text: "/shop"}})
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "1", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbCatPrefix + "1"}})
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "2", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbProdPrefix + "dx"}})
	// 点挑头购买 → 进 BIN 输入步骤
	handle(webhookdomain.Update{CallbackQuery: &webhookdomain.CallbackQuery{ID: "3", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbBuyPrefix + "bin"}})
	last := bot.sent[len(bot.sent)-1]
	if !containsStr(last, "请直接输入 6 位 BIN") {
		t.Fatalf("expected BIN input prompt after 挑头购买, got: %s", last)
	}
}

func TestPurchaseServiceCountryKeyboardHasEmojiAndPagination(t *testing.T) {
	bot := &fakeBotAPI{}
	svc := newBuyFlowService(t, bot, false)
	// 造 8 个国家以触发翻页。
	svc.mu.Lock()
	// 直接造一个会话验证 countryKeyboard 分页。
	svc.sessions[100] = &purchaseSession{chatID: 100, locale: "zh-CN", selected: &contract.ShopProduct{ID: 10, Currency: "CNY"}, pickStock: &contract.ShopPickStock{
		Countries: []contract.ShopPickCountry{
			{Code: "US", Name: "美国", Stock: 10},
			{Code: "DE", Name: "德国", Stock: 9},
			{Code: "FR", Name: "法国", Stock: 8},
			{Code: "GB", Name: "英国", Stock: 7},
			{Code: "CA", Name: "加拿大", Stock: 6},
			{Code: "AU", Name: "澳大利亚", Stock: 5},
			{Code: "JP", Name: "日本", Stock: 4},
			{Code: "KR", Name: "韩国", Stock: 3},
		},
	}, lastUpdatedAt: time.Now()}
	svc.mu.Unlock()
	view := svc.snapshot(100)
	mk := svc.countryKeyboard(view, 1)
	// 第一页 6 个国家按钮，含 emoji。
	joined := ""
	for _, row := range mk.InlineKeyboard {
		for _, b := range row {
			joined += b.Text + " | "
		}
	}
	if !containsStr(joined, "🇺🇸") {
		t.Fatalf("expected US emoji flag, got: %s", joined)
	}
	// 第一页有「下一页」按钮（因共 8 个）。
	if !keyboardContains(mk, "下一页 ▶") {
		t.Fatalf("expected next-page button on page 1, got: %s", joined)
	}
	mk2 := svc.countryKeyboard(view, 2)
	if !keyboardContains(mk2, "上一页") {
		t.Fatalf("expected prev-page button on page 2")
	}
}
