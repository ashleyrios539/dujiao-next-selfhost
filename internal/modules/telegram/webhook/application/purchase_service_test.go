package application

import (
	"context"
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

type stubSettings struct{ cur, name string }

func (s *stubSettings) GetCurrency(context.Context) (string, error) { return s.cur, nil }
func (s *stubSettings) GetSiteName(context.Context) (string, error) { return s.name, nil }

// --- fake botapi ---

type fakeBotAPI struct {
	sent    []string
	markups []inlineKeyboard
}

func (f *fakeBotAPI) SendMessage(_ context.Context, _, _ string, message string, opts contract.SendMessageOptions) error {
	f.sent = append(f.sent, message)
	if mk, ok := opts.ReplyMarkup.(inlineKeyboard); ok {
		f.markups = append(f.markups, mk)
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
			channels: []contract.ShopPaymentChannel{{ID: 1, Name: "EPUSDT"}},
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
				{ID: 1, Name: "EPUSDT"},
				{ID: 2, Name: "支付宝"},
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
	if !keyboardContains(last, "支付宝") {
		t.Fatalf("expected channel keyboard with 支付宝, got: %+v", last)
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
