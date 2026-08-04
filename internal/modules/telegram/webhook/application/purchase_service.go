package application

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
	"github.com/dujiao-next/internal/modules/telegram/webhook/contract"
	webhookdomain "github.com/dujiao-next/internal/modules/telegram/webhook/domain"
	"github.com/shopspring/decimal"
)

// 购买会话步骤。
const (
	purchaseStepBrowseCategory = "browse_category" // 浏览分类
	purchaseStepBrowseProduct  = "browse_product"  // 浏览商品列表
	purchaseStepProductDetail  = "product_detail"  // 商品详情/数量/挑头/测活
	purchaseStepConfirm        = "confirm"         // 确认下单
)

// 回调 data 前缀。
const (
	cbShopPrefix = "shop:"
	cbCatPrefix  = "shop:cat:"
	cbProdPrefix = "shop:prod:"
	cbSkuPrefix  = "shop:sku:"
	cbCcPrefix   = "shop:cc:"
	cbQtyPrefix  = "shop:qty:"
	cbConfirm    = "shop:confirm"
	cbPayBalance = "shop:pay:balance"
	cbPayOnline  = "shop:pay:online"
	cbBackCat    = "shop:back:cat"
	cbBackProd   = "shop:back:prod"
	cbBackDetail = "shop:back:detail"
	cbCancel     = "shop:cancel"
	cbHelpBuy    = "shop:help"
)

// purchaseSession 单个 chat 的购买会话状态（仅在 mu 持有下读写）。
type purchaseSession struct {
	chatID        int64
	userID        uint
	locale        string
	currency      string
	step          string
	categoryID    string
	page          int
	products      []contract.ShopProduct
	selected      *contract.ShopProduct
	selectedSKUID uint
	quantity      int
	pickBin       string
	cardCheck     bool
	lastUpdatedAt time.Time
}

// purchaseView 会话的不可变快照，供渲染/键盘/下单使用（避免锁外读写会话）。
type purchaseView struct {
	chatID        int64
	userID        uint
	locale        string
	currency      string
	step          string
	categoryID    string
	page          int
	products      []contract.ShopProduct
	selected      *contract.ShopProduct
	selectedSKUID uint
	quantity      int
	pickBin       string
	cardCheck     bool
}

func (s *purchaseService) snapshot(chatID int64) *purchaseView {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[chatID]
	if !ok {
		return nil
	}
	view := &purchaseView{
		chatID:        sess.chatID,
		userID:        sess.userID,
		locale:        sess.locale,
		currency:      sess.currency,
		step:          sess.step,
		categoryID:    sess.categoryID,
		page:          sess.page,
		products:      sess.products,
		selected:      sess.selected,
		selectedSKUID: sess.selectedSKUID,
		quantity:      sess.quantity,
		pickBin:       sess.pickBin,
		cardCheck:     sess.cardCheck,
	}
	return view
}

// purchaseService 实现 bot 内购买流程（浏览分类→商品→挑头/测活→确认→下单→支付）。
type purchaseService struct {
	ports  contract.PurchasePorts
	botapi contract.BotAPIClient
	locale func() string // 返回默认 locale

	mu       sync.Mutex
	sessions map[int64]*purchaseSession
}

// newPurchaseService 构造购买服务。
func newPurchaseService(ports contract.PurchasePorts, botapi contract.BotAPIClient, locale func() string) *purchaseService {
	return &purchaseService{
		ports:    ports,
		botapi:   botapi,
		locale:   locale,
		sessions: make(map[int64]*purchaseSession),
	}
}

// handle 处理进入购买的命令或回调；返回 true 表示已消费该交互。
func (s *purchaseService) handle(ctx context.Context, token string, update webhookdomain.Update) (bool, error) {
	if update.Message != nil && update.Message.IsPrivateChat() {
		return s.handleMessage(ctx, token, update.Message)
	}
	if update.CallbackQuery != nil {
		return s.handleCallback(ctx, token, update.CallbackQuery)
	}
	return false, nil
}

func (s *purchaseService) handleMessage(ctx context.Context, token string, msg *webhookdomain.Message) (bool, error) {
	text := strings.TrimSpace(msg.Text)
	chatID := msg.Chat.ID

	// 进入购买：/shop
	if text == "/shop" || strings.HasPrefix(text, "/shop ") {
		return true, s.enterShop(ctx, token, chatID, msg)
	}

	view := s.snapshot(chatID)
	if view == nil {
		return false, nil
	}
	if view.step == purchaseStepProductDetail {
		return true, s.handleDetailText(ctx, token, view, text)
	}
	return false, nil
}

func (s *purchaseService) handleCallback(ctx context.Context, token string, cb *webhookdomain.CallbackQuery) (bool, error) {
	data := strings.TrimSpace(cb.Data)
	if !strings.HasPrefix(data, cbShopPrefix) {
		return false, nil
	}
	chatID := cb.Message.Chat.ID

	// 先应答回调，避免转圈
	_ = s.botapi.AnswerCallbackQuery(ctx, token, cb.ID, contract.AnswerCallbackOptions{})

	switch {
	case data == cbCancel:
		return true, s.cancel(ctx, token, chatID)
	case data == cbHelpBuy:
		return true, s.sendHelp(ctx, token, chatID)
	case strings.HasPrefix(data, cbCatPrefix):
		return true, s.selectCategory(ctx, token, chatID, strings.TrimPrefix(data, cbCatPrefix))
	case strings.HasPrefix(data, cbProdPrefix):
		return true, s.handleProductCallback(ctx, token, chatID, strings.TrimPrefix(data, cbProdPrefix))
	case data == cbBackCat:
		return true, s.renderCategories(ctx, token, chatID)
	case data == cbBackProd:
		return true, s.backToProducts(ctx, token, chatID)
	case data == cbBackDetail:
		return true, s.renderDetail(ctx, token, chatID)
	case strings.HasPrefix(data, cbSkuPrefix):
		return true, s.selectSKU(ctx, token, chatID, strings.TrimPrefix(data, cbSkuPrefix))
	case strings.HasPrefix(data, cbCcPrefix):
		return true, s.toggleCardCheck(ctx, token, chatID, strings.TrimPrefix(data, cbCcPrefix) == "1")
	case strings.HasPrefix(data, cbQtyPrefix):
		return true, s.setQuantity(ctx, token, chatID, strings.TrimPrefix(data, cbQtyPrefix))
	case data == cbConfirm:
		return true, s.confirmOrder(ctx, token, chatID)
	case data == cbPayBalance:
		return true, s.payWithBalance(ctx, token, chatID)
	case data == cbPayOnline:
		return true, s.payOnline(ctx, token, chatID)
	}
	return true, nil
}

// enterShop 进入购买流程：解析身份 + 展示分类。
func (s *purchaseService) enterShop(ctx context.Context, token string, chatID int64, msg *webhookdomain.Message) error {
	if s.ports.Catalog == nil || s.ports.Orders == nil || s.ports.Identity == nil {
		return s.sendError(ctx, token, chatID, nil, "purchase.unavailable")
	}

	var user *contract.PurchaseUser
	var err error
	if msg.From != nil {
		user, err = s.ports.Identity.ResolveOrProvision(ctx,
			fmt.Sprintf("%d", msg.From.ID), msg.From.UserName, msg.From.FirstName, msg.From.LastName)
		if err != nil {
			return s.sendError(ctx, token, chatID, err, "purchase.identity_failed")
		}
	}
	if user == nil {
		return s.sendError(ctx, token, chatID, nil, "purchase.identity_failed")
	}

	locale := s.locale()
	currency := ""
	if s.ports.Settings != nil {
		if cur, e := s.ports.Settings.GetCurrency(ctx); e == nil && cur != "" {
			currency = cur
		}
	}

	s.mu.Lock()
	s.sessions[chatID] = &purchaseSession{
		chatID:        chatID,
		userID:        user.ID,
		locale:        locale,
		currency:      currency,
		step:          purchaseStepBrowseCategory,
		quantity:      1,
		lastUpdatedAt: time.Now(),
	}
	s.mu.Unlock()

	return s.renderCategories(ctx, token, chatID)
}

func (s *purchaseService) renderCategories(ctx context.Context, token string, chatID int64) error {
	categories, err := s.ports.Catalog.ListActiveCategories(ctx)
	if err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	view := s.snapshot(chatID)
	if view == nil {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("🛍️ " + s.siteName() + "\n\n")
	sb.WriteString(s.t(view, "purchase.select_category"))
	if len(categories) == 0 {
		sb.WriteString("\n\n" + s.t(view, "purchase.no_category"))
	}

	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), sb.String(),
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: s.categoryKeyboard(categories, view)})
}

func (s *purchaseService) selectCategory(ctx context.Context, token string, chatID int64, categoryID string) error {
	s.mu.Lock()
	if sess, ok := s.sessions[chatID]; ok {
		sess.categoryID = categoryID
		sess.page = 0
		sess.products = nil
		sess.selected = nil
	}
	s.mu.Unlock()
	return s.renderProducts(ctx, token, chatID, 1)
}

func (s *purchaseService) renderProducts(ctx context.Context, token string, chatID int64, page int) error {
	view := s.snapshot(chatID)
	if view == nil {
		return nil
	}
	categoryID := view.categoryID

	const pageSize = 8
	products, total, err := s.ports.Catalog.ListProducts(ctx, categoryID, page, pageSize)
	if err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}

	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.step = purchaseStepBrowseProduct
		sess.page = page
		sess.products = products
	}
	s.mu.Unlock()

	var sb strings.Builder
	sb.WriteString(s.t(view, "purchase.products_title"))
	sb.WriteString(fmt.Sprintf("（%d）\n\n", total))

	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), sb.String(),
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: s.productKeyboard(view, products, page, int(total), pageSize)})
}

func (s *purchaseService) backToProducts(ctx context.Context, token string, chatID int64) error {
	view := s.snapshot(chatID)
	page := 1
	if view != nil && view.page > 1 {
		page = view.page
	}
	return s.renderProducts(ctx, token, chatID, page)
}

// handleProductCallback 处理商品项回调（含分页）。
func (s *purchaseService) handleProductCallback(ctx context.Context, token string, chatID int64, payload string) error {
	if strings.HasPrefix(payload, "page:") {
		page, err := strconv.Atoi(strings.TrimPrefix(payload, "page:"))
		if err != nil || page < 1 {
			return nil
		}
		return s.renderProducts(ctx, token, chatID, page)
	}
	return s.selectProduct(ctx, token, chatID, payload)
}

func (s *purchaseService) selectProduct(ctx context.Context, token string, chatID int64, slug string) error {
	product, err := s.ports.Catalog.GetProductBySlug(ctx, slug)
	if err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.step = purchaseStepProductDetail
		sess.selected = product
		sess.selectedSKUID = 0
		sess.quantity = 1
		sess.pickBin = ""
		sess.cardCheck = false
	}
	s.mu.Unlock()
	return s.renderDetail(ctx, token, chatID)
}

func (s *purchaseService) renderDetail(ctx context.Context, token string, chatID int64) error {
	view := s.snapshot(chatID)
	if view == nil || view.selected == nil {
		return nil
	}
	product := view.selected

	var sb strings.Builder
	sb.WriteString("🛍️ " + product.Title + "\n\n")
	sb.WriteString(s.t(view, "purchase.price") + " " + formatAmount(product.PriceAmount, product.Currency) + "\n")
	if product.CardCheckEnabled {
		sb.WriteString(s.t(view, "purchase.card_check") + " +" + formatAmount(product.CardCheckFee, product.Currency) + "\n")
	}
	if product.PickEnabled {
		sb.WriteString(s.t(view, "purchase.pick_enabled") + "\n")
		if view.pickBin != "" {
			sb.WriteString(s.t(view, "purchase.pick_bin") + " " + view.pickBin + "\n")
		}
	}
	sb.WriteString("\n" + s.t(view, "purchase.detail_hint"))

	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), sb.String(),
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: s.detailKeyboard(view)})
}

func (s *purchaseService) selectSKU(ctx context.Context, token string, chatID int64, skuIDStr string) error {
	id, err := strconv.ParseUint(skuIDStr, 10, 64)
	if err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.selectedSKUID = uint(id)
	}
	s.mu.Unlock()
	return s.renderDetail(ctx, token, chatID)
}

func (s *purchaseService) toggleCardCheck(ctx context.Context, token string, chatID int64, on bool) error {
	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.cardCheck = on
	}
	s.mu.Unlock()
	return s.renderDetail(ctx, token, chatID)
}

func (s *purchaseService) setQuantity(ctx context.Context, token string, chatID int64, qtyStr string) error {
	qty, err := strconv.Atoi(qtyStr)
	if err != nil || qty < 1 {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.quantity = qty
	}
	s.mu.Unlock()
	return s.renderDetail(ctx, token, chatID)
}

// handleDetailText 处理商品详情阶段的文本输入（BIN 挑头）。
func (s *purchaseService) handleDetailText(ctx context.Context, token string, view *purchaseView, text string) error {
	if view.selected != nil && view.selected.PickEnabled && isBinInput(text) {
		s.mu.Lock()
		if sess := s.sessions[view.chatID]; sess != nil {
			sess.pickBin = text
		}
		s.mu.Unlock()
		if s.ports.Catalog != nil && view.selected != nil {
			count, err := s.ports.Catalog.CountAvailableByBinPrefix(ctx, view.selected.ID, text)
			if err == nil {
				msg := fmt.Sprintf(s.t(view, "purchase.bin_set"), text)
				if count > 0 {
					msg += "\n" + fmt.Sprintf(s.t(view, "purchase.bin_available"), count)
				} else {
					msg += "\n" + s.t(view, "purchase.bin_none")
				}
				_ = s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", view.chatID), msg,
					contract.SendMessageOptions{DisableWebPagePreview: true})
			}
		}
		return s.renderDetail(ctx, token, view.chatID)
	}
	return s.sendHelp(ctx, token, view.chatID)
}

// confirmOrder 预下单并展示金额，等待确认。
func (s *purchaseService) confirmOrder(ctx context.Context, token string, chatID int64) error {
	view := s.snapshot(chatID)
	if view == nil || view.selected == nil || view.userID == 0 {
		return nil
	}

	preview, err := s.ports.Orders.Preview(ctx, contract.PurchasePreviewInput{
		UserID: view.userID,
		Items:  buildPurchaseItems(view),
	})
	if err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}

	var sb strings.Builder
	sb.WriteString(s.t(view, "purchase.preview_title") + "\n\n")
	for _, it := range preview.Items {
		sb.WriteString("• " + it.Title)
		if it.CardCheckEnabled {
			sb.WriteString(" [" + s.t(view, "purchase.card_check") + "]")
		}
		if it.PickBin != "" {
			sb.WriteString(" [" + s.t(view, "purchase.pick_bin") + " " + it.PickBin + "]")
		}
		sb.WriteString(fmt.Sprintf(" ×%d\n", it.Quantity))
		sb.WriteString("  " + formatAmount(it.UnitPrice, preview.Currency) + " = " + formatAmount(it.TotalPrice, preview.Currency) + "\n")
	}
	sb.WriteString("\n")
	if preview.DiscountAmount != "" && preview.DiscountAmount != "0" {
		sb.WriteString(s.t(view, "purchase.discount") + " -" + formatAmount(preview.DiscountAmount, preview.Currency) + "\n")
	}
	sb.WriteString(s.t(view, "purchase.total") + " " + formatAmount(preview.TotalAmount, preview.Currency))

	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), sb.String(),
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: s.confirmKeyboard(view)})
}

// buildPurchaseItems 从快照生成订单项。
func buildPurchaseItems(view *purchaseView) []contract.PurchaseItem {
	if view == nil || view.selected == nil {
		return nil
	}
	return []contract.PurchaseItem{{
		ProductID:        view.selected.ID,
		SKUID:            view.selectedSKUID,
		Quantity:         view.quantity,
		FulfillmentType:  view.selected.FulfillmentType,
		CardCheckEnabled: view.cardCheck,
		PickBin:          view.pickBin,
	}}
}

// createOrder 实际下单。
func (s *purchaseService) createOrder(ctx context.Context, token string, chatID int64) (*contract.PurchaseCreated, error) {
	view := s.snapshot(chatID)
	if view == nil || view.selected == nil || view.userID == 0 {
		return nil, fmt.Errorf("no active purchase session")
	}
	return s.ports.Orders.Create(ctx, contract.PurchaseCreateInput{
		UserID: view.userID,
		Items:  buildPurchaseItems(view),
	})
}

func (s *purchaseService) payWithBalance(ctx context.Context, token string, chatID int64) error {
	created, err := s.createOrder(ctx, token, chatID)
	if err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	if s.ports.Payments == nil {
		return s.sendError(ctx, token, chatID, nil, "purchase.error")
	}
	result, err := s.ports.Payments.CreatePayment(ctx, contract.PurchasePaymentInput{
		OrderID:    created.OrderID,
		UseBalance: true,
	})
	if err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	return s.renderPaymentResult(ctx, token, chatID, created, result)
}

func (s *purchaseService) payOnline(ctx context.Context, token string, chatID int64) error {
	created, err := s.createOrder(ctx, token, chatID)
	if err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	if s.ports.Payments == nil {
		return s.sendError(ctx, token, chatID, nil, "purchase.error")
	}
	result, err := s.ports.Payments.CreatePayment(ctx, contract.PurchasePaymentInput{
		OrderID: created.OrderID,
	})
	if err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	return s.renderPaymentResult(ctx, token, chatID, created, result)
}

func (s *purchaseService) renderPaymentResult(ctx context.Context, token string, chatID int64, created *contract.PurchaseCreated, result *contract.PurchasePaymentResult) error {
	view := s.snapshot(chatID)
	loc := "zh-CN"
	if view != nil {
		loc = view.locale
	}

	var sb strings.Builder
	sb.WriteString(localizedText(purchaseTexts["purchase.paid_title"], loc) + "\n\n")
	sb.WriteString(localizedText(purchaseTexts["purchase.order_no"], loc) + " " + created.OrderNo + "\n")
	if result.OrderPaid {
		sb.WriteString("✅ " + localizedText(purchaseTexts["purchase.order_paid"], loc) + "\n")
	}
	if result.WalletPaidAmount != "" && result.WalletPaidAmount != "0" {
		sb.WriteString(localizedText(purchaseTexts["purchase.wallet_paid"], loc) + " " + formatAmount(result.WalletPaidAmount, created.Currency) + "\n")
	}
	if result.OnlinePayAmount != "" && result.OnlinePayAmount != "0" {
		sb.WriteString(localizedText(purchaseTexts["purchase.online_pay"], loc) + " " + formatAmount(result.OnlinePayAmount, created.Currency) + "\n")
	}
	if result.PayURL != "" {
		sb.WriteString("\n🔗 " + result.PayURL + "\n")
	}
	sb.WriteString("\n" + localizedText(purchaseTexts["purchase.pay_help"], loc))

	s.mu.Lock()
	delete(s.sessions, chatID)
	s.mu.Unlock()

	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), sb.String(),
		contract.SendMessageOptions{DisableWebPagePreview: true})
}

func (s *purchaseService) cancel(ctx context.Context, token string, chatID int64) error {
	view := s.snapshot(chatID)
	loc := "zh-CN"
	if view != nil {
		loc = view.locale
	}
	s.mu.Lock()
	delete(s.sessions, chatID)
	s.mu.Unlock()
	_ = s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), localizedText(purchaseTexts["purchase.canceled"], loc),
		contract.SendMessageOptions{DisableWebPagePreview: true})
	return nil
}

func (s *purchaseService) sendHelp(ctx context.Context, token string, chatID int64) error {
	view := s.snapshot(chatID)
	loc := "zh-CN"
	if view != nil {
		loc = view.locale
	}
	_ = s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), localizedText(purchaseTexts["purchase.help"], loc),
		contract.SendMessageOptions{DisableWebPagePreview: true})
	return nil
}

func (s *purchaseService) sendError(ctx context.Context, token string, chatID int64, err error, key string) error {
	view := s.snapshot(chatID)
	loc := "zh-CN"
	if view != nil {
		loc = view.locale
	}
	msg := localizedText(purchaseTexts[key], loc)
	if err != nil {
		msg += "\n\n(" + err.Error() + ")"
	}
	_ = s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), msg,
		contract.SendMessageOptions{DisableWebPagePreview: true})
	return nil
}

// --- 内联键盘构建 ---

func (s *purchaseService) categoryKeyboard(categories []contract.ShopCategory, view *purchaseView) inlineKeyboard {
	rows := make([][]inlineButton, 0, len(categories)+1)
	for _, cat := range categories {
		rows = append(rows, []inlineButton{{
			Text:         cat.Name,
			CallbackData: cbCatPrefix + fmt.Sprintf("%d", cat.ID),
		}})
	}
	rows = append(rows, []inlineButton{{
		Text:         "❌ " + s.t(view, "purchase.cancel"),
		CallbackData: cbCancel,
	}})
	return inlineKeyboard{InlineKeyboard: rows}
}

func (s *purchaseService) productKeyboard(view *purchaseView, products []contract.ShopProduct, page, total, pageSize int) inlineKeyboard {
	currency := ""
	if view != nil {
		currency = view.currency
	}
	rows := make([][]inlineButton, 0, len(products)+2)
	for _, p := range products {
		rows = append(rows, []inlineButton{{
			Text:         p.Title + "  " + formatAmount(p.PriceAmount, currency),
			CallbackData: cbProdPrefix + p.Slug,
		}})
	}
	if pageSize > 0 && total > 0 {
		totalPages := (total + pageSize - 1) / pageSize
		if totalPages > 1 {
			nav := []inlineButton{}
			if page > 1 {
				nav = append(nav, inlineButton{Text: "◀", CallbackData: cbProdPrefix + "page:" + fmt.Sprintf("%d", page-1)})
			}
			nav = append(nav, inlineButton{Text: fmt.Sprintf("%d/%d", page, totalPages), CallbackData: cbHelpBuy})
			if page < totalPages {
				nav = append(nav, inlineButton{Text: "▶", CallbackData: cbProdPrefix + "page:" + fmt.Sprintf("%d", page+1)})
			}
			rows = append(rows, nav)
		}
	}
	rows = append(rows, []inlineButton{{
		Text:         "🔙 " + s.t(view, "purchase.back"),
		CallbackData: cbBackCat,
	}})
	return inlineKeyboard{InlineKeyboard: rows}
}

func (s *purchaseService) detailKeyboard(view *purchaseView) inlineKeyboard {
	product := view.selected
	rows := make([][]inlineButton, 0, 6)

	if len(product.SKUs) > 0 {
		row := make([]inlineButton, 0, len(product.SKUs))
		for _, sku := range product.SKUs {
			row = append(row, inlineButton{
				Text:         sku.Code,
				CallbackData: cbSkuPrefix + fmt.Sprintf("%d", sku.ID),
			})
		}
		rows = append(rows, row)
	}

	rows = append(rows, []inlineButton{
		{Text: "−", CallbackData: cbQtyPrefix + fmt.Sprintf("%d", max(1, view.quantity-1))},
		{Text: fmt.Sprintf("%d", view.quantity), CallbackData: cbHelpBuy},
		{Text: "+", CallbackData: cbQtyPrefix + fmt.Sprintf("%d", view.quantity+1)},
	})

	if product.CardCheckEnabled {
		ccLabel := "❌ " + s.t(view, "purchase.cc_off")
		if view.cardCheck {
			ccLabel = "✅ " + s.t(view, "purchase.cc_on")
		}
		rows = append(rows, []inlineButton{{
			Text:         ccLabel,
			CallbackData: cbCcPrefix + boolStr(!view.cardCheck),
		}})
	}

	rows = append(rows, []inlineButton{{
		Text:         "🛒 " + s.t(view, "purchase.buy_now"),
		CallbackData: cbConfirm,
	}})
	rows = append(rows, []inlineButton{
		{Text: "🔙 " + s.t(view, "purchase.back"), CallbackData: cbBackProd},
		{Text: "❌ " + s.t(view, "purchase.cancel"), CallbackData: cbCancel},
	})
	return inlineKeyboard{InlineKeyboard: rows}
}

func (s *purchaseService) confirmKeyboard(view *purchaseView) inlineKeyboard {
	rows := [][]inlineButton{
		{
			{Text: "💰 " + s.t(view, "purchase.pay_balance"), CallbackData: cbPayBalance},
			{Text: "💳 " + s.t(view, "purchase.pay_online"), CallbackData: cbPayOnline},
		},
		{
			{Text: "🔙 " + s.t(view, "purchase.back"), CallbackData: cbBackDetail},
			{Text: "❌ " + s.t(view, "purchase.cancel"), CallbackData: cbCancel},
		},
	}
	return inlineKeyboard{InlineKeyboard: rows}
}

// --- 辅助 ---

func (s *purchaseService) siteName() string {
	if s.ports.Settings != nil {
		if name, err := s.ports.Settings.GetSiteName(context.Background()); err == nil && name != "" {
			return name
		}
	}
	return "Shop"
}

// t 按会话 locale 取文案。
func (s *purchaseService) t(view *purchaseView, key string) string {
	loc := "zh-CN"
	if view != nil && view.locale != "" {
		loc = view.locale
	}
	return localizedText(purchaseTexts[key], loc)
}

func isBinInput(text string) bool {
	if len(text) != 6 {
		return false
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func formatAmount(amount, currency string) string {
	dec, err := decimal.NewFromString(strings.TrimSpace(amount))
	if err != nil {
		return amount + " " + strings.TrimSpace(currency)
	}
	return dec.Round(2).StringFixed(2) + " " + strings.TrimSpace(currency)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// purchaseTexts 购买流程 i18n 文案。
var purchaseTexts = map[string]settingsmessaging.LocalizedText{
	"purchase.unavailable":     {"zh-CN": "商城购买暂不可用，请稍后再试。", "zh-TW": "商城購買暫不可用，請稍後再試。", "en-US": "Purchase is temporarily unavailable."},
	"purchase.identity_failed": {"zh-CN": "无法登录商城账号，请稍后再试。", "zh-TW": "無法登入商城帳號，請稍後再試。", "en-US": "Failed to sign in to your account."},
	"purchase.select_category": {"zh-CN": "请选择分类：", "zh-TW": "請選擇分類：", "en-US": "Select a category:"},
	"purchase.no_category":     {"zh-CN": "暂无上架分类。", "zh-TW": "暫無上架分類。", "en-US": "No categories available."},
	"purchase.products_title":  {"zh-CN": "商品列表", "zh-TW": "商品列表", "en-US": "Products"},
	"purchase.price":           {"zh-CN": "价格", "zh-TW": "價格", "en-US": "Price"},
	"purchase.card_check":      {"zh-CN": "测活", "zh-TW": "測活", "en-US": "Card check"},
	"purchase.pick_enabled":    {"zh-CN": "支持挑卡", "zh-TW": "支持挑卡", "en-US": "Pick enabled"},
	"purchase.pick_bin":        {"zh-CN": "挑头", "zh-TW": "挑頭", "en-US": "BIN"},
	"purchase.buy_now":         {"zh-CN": "立即购买", "zh-TW": "立即購買", "en-US": "Buy Now"},
	"purchase.back":            {"zh-CN": "返回", "zh-TW": "返回", "en-US": "Back"},
	"purchase.cancel":          {"zh-CN": "取消", "zh-TW": "取消", "en-US": "Cancel"},
	"purchase.cc_on":           {"zh-CN": "开启测活", "zh-TW": "開啟測活", "en-US": "Enable card check"},
	"purchase.cc_off":          {"zh-CN": "关闭测活", "zh-TW": "關閉測活", "en-US": "Disable card check"},
	"purchase.preview_title":   {"zh-CN": "订单确认", "zh-TW": "訂單確認", "en-US": "Confirm Order"},
	"purchase.discount":        {"zh-CN": "优惠", "zh-TW": "優惠", "en-US": "Discount"},
	"purchase.total":           {"zh-CN": "合计", "zh-TW": "合計", "en-US": "Total"},
	"purchase.pay_balance":     {"zh-CN": "余额支付", "zh-TW": "餘額支付", "en-US": "Pay with balance"},
	"purchase.pay_online":      {"zh-CN": "在线支付", "zh-TW": "線上支付", "en-US": "Pay online"},
	"purchase.paid_title":      {"zh-CN": "订单已提交", "zh-TW": "訂單已提交", "en-US": "Order Submitted"},
	"purchase.order_no":        {"zh-CN": "订单号", "zh-TW": "訂單號", "en-US": "Order No."},
	"purchase.order_paid":      {"zh-CN": "支付成功", "zh-TW": "支付成功", "en-US": "Payment successful"},
	"purchase.wallet_paid":     {"zh-CN": "余额支付", "zh-TW": "餘額支付", "en-US": "Balance paid"},
	"purchase.online_pay":      {"zh-CN": "在线支付", "zh-TW": "線上支付", "en-US": "Online paid"},
	"purchase.pay_help":        {"zh-CN": "可在网页商城个人中心查看订单与发货详情。", "zh-TW": "可在網頁商城個人中心查看訂單與發貨詳情。", "en-US": "View order and delivery in the web shop account."},
	"purchase.help":            {"zh-CN": "输入 6 位 BIN（挑头）后点击立即购买，或输入 /shop 重新开始。", "zh-TW": "輸入 6 位 BIN（挑頭）後點擊立即購買，或輸入 /shop 重新開始。", "en-US": "Enter a 6-digit BIN to pick by BIN, then tap Buy Now. Type /shop to restart."},
	"purchase.error":           {"zh-CN": "操作失败。", "zh-TW": "操作失敗。", "en-US": "Operation failed."},
	"purchase.canceled":        {"zh-CN": "已取消购买。", "zh-TW": "已取消購買。", "en-US": "Purchase canceled."},
	"purchase.bin_set":         {"zh-CN": "已设置挑头 BIN：%s", "zh-TW": "已設定挑頭 BIN：%s", "en-US": "Pick BIN set: %s"},
	"purchase.bin_available":   {"zh-CN": "可用库存：%d", "zh-TW": "可用庫存：%d", "en-US": "Available: %d"},
	"purchase.bin_none":        {"zh-CN": "该 BIN 暂无可用库存。", "zh-TW": "該 BIN 暫無可用庫存。", "en-US": "No stock for this BIN."},
	"purchase.detail_hint":     {"zh-CN": "选择数量与测活后点击立即购买；支持挑头时可直接输入 6 位 BIN。", "zh-TW": "選擇數量與測活後點擊立即購買；支援挑頭時可直接輸入 6 位 BIN。", "en-US": "Choose quantity and card check, then Buy Now. If pick is enabled, enter a 6-digit BIN."},
}
