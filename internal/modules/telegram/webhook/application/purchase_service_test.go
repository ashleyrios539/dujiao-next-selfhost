package application

import (
	"context"
	"testing"

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
	result *contract.PurchasePaymentResult
}

func (p *stubPayments) CreatePayment(context.Context, contract.PurchasePaymentInput) (*contract.PurchasePaymentResult, error) {
	return p.result, nil
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
	// 输入 BIN
	if _, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		Message: &webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}, Text: "412345"},
	}); err != nil {
		t.Fatalf("bin err: %v", err)
	}
	// 确认下单
	if _, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{ID: "c3", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbConfirm},
	}); err != nil {
		t.Fatalf("confirm err: %v", err)
	}
	// 选择余额支付
	if _, err := svc.handle(context.Background(), "tok", webhookdomain.Update{
		CallbackQuery: &webhookdomain.CallbackQuery{ID: "c4", Message: webhookdomain.Message{Chat: webhookdomain.Chat{ID: 100}}, Data: cbPayBalance},
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

func containsStr(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
