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
	purchaseStepConfigure       = "configure"       // 商品配置面板
	purchaseStepPickMode        = "pick_mode"       // 挑卡模式选择
	purchaseStepPickCountry     = "pick_country"    // 选国家
	purchaseStepPickBrand       = "pick_brand"      // 选品牌（type 模式）
	purchaseStepPickCardType    = "pick_card_type"  // 选卡类型（type 模式）
	purchaseStepPickBin         = "pick_bin"        // 输入 BIN
	purchaseStepConfirm         = "confirm"         // 确认下单
)

// 回调 data 前缀。
const (
	cbShopPrefix     = "shop:"
	cbShopStart      = "shop:start"
	cbCatPrefix      = "shop:cat:"
	cbProdPrefix     = "shop:prod:"
	cbSkuPrefix      = "shop:sku:"
	cbCcPrefix       = "shop:cc:"
	cbQtyPrefix      = "shop:qty:"
	cbPickModePrefix = "shop:pick:"
	cbCountryPrefix  = "shop:country:"
	cbBrandPrefix    = "shop:brand:"
	cbCTypePrefix    = "shop:ctype:"
	cbConfirm        = "shop:confirm"
	cbPayBalance     = "shop:pay:balance"
	cbPayOnline      = "shop:pay:online"
	cbBackCat        = "shop:back:cat"
	cbBackProd       = "shop:back:prod"
	cbBackDetail     = "shop:back:detail"
	cbBackPick       = "shop:back:pick"
	cbBackCountry    = "shop:back:country"
	cbCancel         = "shop:cancel"
	cbHelpBuy        = "shop:help"
)

// purchaseSessionTTL 购买会话空闲过期时间：超过该时长无交互则自动清理，避免会话无限驻留内存。
const purchaseSessionTTL = 30 * time.Minute

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
	// 挑卡
	pickMode     contract.PickMode
	pickCountry  string
	pickBrand    string
	pickCardType string
	pickBin      string
	pickStock    *contract.ShopPickStock
	// 测活
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
	pickMode     contract.PickMode
	pickCountry  string
	pickBrand    string
	pickCardType string
	pickBin      string
	pickStock    *contract.ShopPickStock
	cardCheck     bool
}

func (s *purchaseService) snapshot(chatID int64) *purchaseView {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[chatID]
	if !ok {
		return nil
	}
	// 空闲过期：超过 TTL 无交互则清理会话，视为无会话。
	if time.Since(sess.lastUpdatedAt) > purchaseSessionTTL {
		delete(s.sessions, chatID)
		return nil
	}
	sess.lastUpdatedAt = time.Now()
	return &purchaseView{
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
		pickMode:     sess.pickMode,
		pickCountry:  sess.pickCountry,
		pickBrand:    sess.pickBrand,
		pickCardType: sess.pickCardType,
		pickBin:      sess.pickBin,
		pickStock:    sess.pickStock,
		cardCheck:     sess.cardCheck,
	}
}

// purchaseService 实现 bot 内购买流程（浏览分类→商品→分步配置→确认→下单→支付）。
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
	// 群组内发 /shop：提示私聊使用（购买流程仅私聊可用）。
	if update.Message != nil && update.Message.IsGroupChat() {
		text := strings.TrimSpace(update.Message.Text)
		if text == "/shop" || strings.HasPrefix(text, "/shop ") {
			chatID := update.Message.Chat.ID
			loc := s.locale()
			_ = s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID),
				localizedText(purchaseTexts["purchase.group_not_supported"], loc),
				contract.SendMessageOptions{DisableWebPagePreview: true})
			return true, nil
		}
		return false, nil
	}
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
	// 配置面板中的文本输入：BIN 或国家双字母
	if view.step == purchaseStepConfigure || view.step == purchaseStepPickBin ||
		view.step == purchaseStepPickCountry || view.step == purchaseStepPickBrand {
		return true, s.handleConfigureText(ctx, token, view, text)
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
	case data == cbShopStart:
		return true, s.StartFromMenu(ctx, token, chatID, cb.From)
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
	case data == cbBackPick:
		return true, s.renderPickMode(ctx, token, chatID)
	case data == cbBackCountry:
		return true, s.renderPickCountry(ctx, token, chatID)
	case strings.HasPrefix(data, cbSkuPrefix):
		return true, s.selectSKU(ctx, token, chatID, strings.TrimPrefix(data, cbSkuPrefix))
	case strings.HasPrefix(data, cbQtyPrefix):
		return true, s.setQuantity(ctx, token, chatID, strings.TrimPrefix(data, cbQtyPrefix))
	case strings.HasPrefix(data, cbPickModePrefix):
		return true, s.selectPickMode(ctx, token, chatID, strings.TrimPrefix(data, cbPickModePrefix))
	case strings.HasPrefix(data, cbCountryPrefix):
		return true, s.selectCountry(ctx, token, chatID, strings.TrimPrefix(data, cbCountryPrefix))
	case strings.HasPrefix(data, cbBrandPrefix):
		return true, s.selectBrand(ctx, token, chatID, strings.TrimPrefix(data, cbBrandPrefix))
	case strings.HasPrefix(data, cbCTypePrefix):
		return true, s.selectCardType(ctx, token, chatID, strings.TrimPrefix(data, cbCTypePrefix))
	case strings.HasPrefix(data, cbCcPrefix):
		return true, s.toggleCardCheck(ctx, token, chatID, strings.TrimPrefix(data, cbCcPrefix) == "1")
	case data == cbConfirm:
		return true, s.confirmOrder(ctx, token, chatID)
	case data == cbPayBalance:
		return true, s.payWithBalance(ctx, token, chatID)
	case data == cbPayOnline:
		return true, s.payOnline(ctx, token, chatID)
	}
	return true, nil
}

// enterShop 进入购买流程：解析身份 + 展示分类（文本 /shop 入口）。
func (s *purchaseService) enterShop(ctx context.Context, token string, chatID int64, msg *webhookdomain.Message) error {
	var from *webhookdomain.User
	if msg != nil {
		from = msg.From
	}
	return s.enterShopForUser(ctx, token, chatID, from)
}

// StartFromMenu 从主菜单/欢迎语按钮进入购买流程（无既有会话时也允许）。
func (s *purchaseService) StartFromMenu(ctx context.Context, token string, chatID int64, from webhookdomain.User) error {
	return s.enterShopForUser(ctx, token, chatID, &from)
}

// ShowWallet 查询并展示当前商城账号余额（主菜单「我的钱包」入口）。
func (s *purchaseService) ShowWallet(ctx context.Context, token string, chatID int64, from webhookdomain.User) error {
	if s.ports.Identity == nil || s.ports.Wallet == nil {
		return s.sendError(ctx, token, chatID, nil, "purchase.unavailable")
	}
	user, err := s.ports.Identity.ResolveOrProvision(ctx,
		fmt.Sprintf("%d", from.ID), from.UserName, from.FirstName, from.LastName)
	if err != nil || user == nil {
		return s.sendError(ctx, token, chatID, err, "purchase.identity_failed")
	}
	loc := s.locale()
	currency := ""
	if s.ports.Settings != nil {
		if cur, e := s.ports.Settings.GetCurrency(ctx); e == nil && cur != "" {
			currency = cur
		}
	}
	balance, err := s.ports.Wallet.GetBalance(ctx, user.ID)
	if err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	msg := localizedText(purchaseTexts["purchase.wallet_title"], loc) + "\n\n" +
		localizedText(purchaseTexts["purchase.wallet_balance"], loc) + " " + formatAmount(balance, currency)
	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), msg,
		contract.SendMessageOptions{DisableWebPagePreview: true})
}

// enterShopForUser 进入购买流程：解析/创建商城账号并展示分类。
func (s *purchaseService) enterShopForUser(ctx context.Context, token string, chatID int64, from *webhookdomain.User) error {
	if s.ports.Catalog == nil || s.ports.Orders == nil || s.ports.Identity == nil {
		return s.sendError(ctx, token, chatID, nil, "purchase.unavailable")
	}

	var user *contract.PurchaseUser
	var err error
	if from != nil {
		user, err = s.ports.Identity.ResolveOrProvision(ctx,
			fmt.Sprintf("%d", from.ID), from.UserName, from.FirstName, from.LastName)
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

// selectProduct 进入商品配置面板。
func (s *purchaseService) selectProduct(ctx context.Context, token string, chatID int64, slug string) error {
	product, err := s.ports.Catalog.GetProductBySlug(ctx, slug)
	if err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	// 预取挑卡可选维度（商品支持挑卡时）
	var pickStock *contract.ShopPickStock
	if product.PickEnabled && s.ports.Catalog != nil {
		pickStock, _ = s.ports.Catalog.GetPickStock(ctx, product.ID)
	}
	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.step = purchaseStepConfigure
		sess.selected = product
		sess.selectedSKUID = 0
		// 有 SKU 时默认选中第一个，避免用户漏选而报 "SKU not selected"。
		if len(product.SKUs) > 0 {
			sess.selectedSKUID = product.SKUs[0].ID
		}
		sess.quantity = 1
		sess.pickMode = ""
		sess.pickCountry = ""
		sess.pickBrand = ""
		sess.pickCardType = ""
		sess.pickBin = ""
		sess.pickStock = pickStock
		sess.cardCheck = false
	}
	s.mu.Unlock()
	return s.renderDetail(ctx, token, chatID)
}

// renderDetail 渲染商品配置面板（当前选择摘要 + 两档价格 + 上下文按钮）。
func (s *purchaseService) renderDetail(ctx context.Context, token string, chatID int64) error {
	view := s.snapshot(chatID)
	if view == nil || view.selected == nil {
		return nil
	}
	product := view.selected

	var sb strings.Builder
	sb.WriteString("🛍️ " + product.Title + "\n\n")

	// 价格区
	sb.WriteString(s.t(view, "purchase.price") + " " + formatAmount(product.PriceAmount, product.Currency) + "\n")

	// 当前选择摘要
	sb.WriteString("\n" + s.t(view, "purchase.current") + "\n")
	if len(product.SKUs) > 0 {
		skuCode := ""
		for _, sku := range product.SKUs {
			if sku.ID == view.selectedSKUID {
				skuCode = sku.Code
				break
			}
		}
		sb.WriteString("  " + s.t(view, "purchase.sku") + ": " + orDefault(skuCode, s.t(view, "purchase.not_selected")) + "\n")
	}
	sb.WriteString("  " + s.t(view, "purchase.quantity") + ": " + fmt.Sprintf("%d", view.quantity) + "\n")

	if product.PickEnabled {
		sb.WriteString("  " + s.t(view, "purchase.pick_mode") + ": " + pickModeLabel(view, s) + "\n")
		switch view.pickMode {
		case contract.PickModeBin:
			sb.WriteString("  " + s.t(view, "purchase.pick_bin") + ": " + orDefault(view.pickBin, s.t(view, "purchase.not_selected")))
			if view.pickBin != "" && s.ports.Catalog != nil {
				if count, err := s.ports.Catalog.CountAvailableByBinPrefix(ctx, product.ID, view.pickBin); err == nil {
					sb.WriteString("  " + fmt.Sprintf("(" + s.t(view, "purchase.available") + " %d)", count))
				}
			}
			sb.WriteString("\n")
		case contract.PickModeRandom, contract.PickModeType:
			sb.WriteString("  " + s.t(view, "purchase.country") + ": " + orDefault(view.pickCountry, s.t(view, "purchase.not_selected")))
			if view.pickCountry != "" {
				sb.WriteString("  " + fmt.Sprintf("(" + s.t(view, "purchase.available") + " %d)", s.countryStock(view)))
			}
			sb.WriteString("\n")
		}
		if view.pickMode == contract.PickModeType {
			sb.WriteString("  " + s.t(view, "purchase.brand") + ": " + orDefault(view.pickBrand, s.t(view, "purchase.not_selected")) + "\n")
			sb.WriteString("  " + s.t(view, "purchase.card_type") + ": " + orDefault(view.pickCardType, s.t(view, "purchase.not_selected")) + "\n")
		}
	}

	if product.CardCheckEnabled {
		cc := "❌ " + s.t(view, "purchase.cc_off")
		if view.cardCheck {
			cc = "✅ " + s.t(view, "purchase.cc_on")
		}
		sb.WriteString("  " + s.t(view, "purchase.card_check") + ": " + cc + "\n")
	}

	// 两档价格（含挑卡加价）
	if product.CardCheckEnabled {
		surcharge := s.pickUnitSurcharge(view)
		plain := product.PriceAmount
		if !surcharge.IsZero() {
			plain = addAmounts(plain, surcharge.String())
		}
		checked := addAmounts(plain, product.CardCheckFee)
		sb.WriteString("\n" + s.t(view, "purchase.plain_price") + " " + formatAmount(plain, product.Currency) + "\n")
		sb.WriteString(s.t(view, "purchase.checked_price") + " " + formatAmount(checked, product.Currency) + "\n")
	}

	sb.WriteString("\n" + s.t(view, "purchase.detail_hint"))

	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), sb.String(),
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: s.detailKeyboard(view)})
}

// pickModeLabel 挑卡模式显示名。
func pickModeLabel(view *purchaseView, s *purchaseService) string {
	switch view.pickMode {
	case contract.PickModeBin:
		return s.t(view, "purchase.pick_mode_bin")
	case contract.PickModeRandom:
		return s.t(view, "purchase.pick_mode_random")
	case contract.PickModeType:
		return s.t(view, "purchase.pick_mode_type")
	}
	return s.t(view, "purchase.not_selected")
}

// pickUnitSurcharge 计算挑卡加价。
func (s *purchaseService) pickUnitSurcharge(view *purchaseView) decimal.Decimal {
	if view == nil || view.selected == nil || !view.selected.PickEnabled {
		return decimal.Zero
	}
	prices := view.selected.PickPrices
	switch view.pickMode {
	case contract.PickModeBin:
		if v, ok := prices["bin"]; ok {
			if d, err := decimal.NewFromString(strings.TrimSpace(v)); err == nil {
				return d.Round(2)
			}
		}
	case contract.PickModeRandom, contract.PickModeType:
		maxByGroup := func(key string) decimal.Decimal {
			if v, ok := prices[key]; ok {
				if d, err := decimal.NewFromString(strings.TrimSpace(v)); err == nil {
					return d.Round(2)
				}
			}
			return decimal.Zero
		}
		var brandFee decimal.Decimal
		if view.pickMode == contract.PickModeType {
			brandFee = maxByGroup(view.pickBrand)
		}
		cardFee := maxByGroup(view.pickCardType)
		return brandFee.Add(cardFee).Round(2)
	}
	return decimal.Zero
}

// countryStock 计算所选国家在当前筛选下的可用库存。
func (s *purchaseService) countryStock(view *purchaseView) int64 {
	if view == nil || view.pickStock == nil || view.pickCountry == "" {
		return 0
	}
	for _, c := range view.pickStock.Countries {
		if c.Code == view.pickCountry {
			return c.Stock
		}
	}
	return 0
}

// selectSKU 选择 SKU 并回到配置面板。
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

func (s *purchaseService) toggleCardCheck(ctx context.Context, token string, chatID int64, on bool) error {
	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.cardCheck = on
	}
	s.mu.Unlock()
	return s.renderDetail(ctx, token, chatID)
}

// --- 挑卡分步 ---

// renderPickMode 渲染挑卡模式选择（随机/BIN/种类）。
func (s *purchaseService) renderPickMode(ctx context.Context, token string, chatID int64) error {
	view := s.snapshot(chatID)
	if view == nil || view.selected == nil {
		return nil
	}
	var sb strings.Builder
	sb.WriteString(s.t(view, "purchase.pick_mode_title") + "\n\n")
	sb.WriteString(s.t(view, "purchase.pick_mode_desc"))

	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.step = purchaseStepPickMode
	}
	s.mu.Unlock()

	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), sb.String(),
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: s.pickModeKeyboard(view)})
}

func (s *purchaseService) selectPickMode(ctx context.Context, token string, chatID int64, mode string) error {
	var pm contract.PickMode
	switch mode {
	case "random":
		pm = contract.PickModeRandom
	case "bin":
		pm = contract.PickModeBin
	case "type":
		pm = contract.PickModeType
	default:
		return s.sendError(ctx, token, chatID, nil, "purchase.error")
	}
	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.pickMode = pm
		// 切换模式时清空对应选择
		sess.pickCountry = ""
		sess.pickBrand = ""
		sess.pickCardType = ""
		sess.pickBin = ""
	}
	s.mu.Unlock()

	switch pm {
	case contract.PickModeBin:
		// BIN 模式：进入输入状态（无需国家）
		s.mu.Lock()
		if sess := s.sessions[chatID]; sess != nil {
			sess.step = purchaseStepPickBin
		}
		s.mu.Unlock()
		return s.sendConfigurePrompt(ctx, token, chatID, "purchase.bin_prompt")
	case contract.PickModeRandom, contract.PickModeType:
		// 需要选国家
		return s.renderPickCountry(ctx, token, chatID)
	}
	return s.renderDetail(ctx, token, chatID)
}

// renderPickCountry 渲染国家选择（按库存降序）。
func (s *purchaseService) renderPickCountry(ctx context.Context, token string, chatID int64) error {
	view := s.snapshot(chatID)
	if view == nil || view.selected == nil {
		return nil
	}
	var sb strings.Builder
	sb.WriteString(s.t(view, "purchase.country_title") + "\n\n")
	sb.WriteString(s.t(view, "purchase.country_desc"))

	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.step = purchaseStepPickCountry
	}
	s.mu.Unlock()

	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), sb.String(),
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: s.countryKeyboard(view)})
}

func (s *purchaseService) selectCountry(ctx context.Context, token string, chatID int64, code string) error {
	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.pickCountry = code
		sess.pickBrand = ""
		sess.pickCardType = ""
	}
	s.mu.Unlock()
	// 选了国家后：random 直接回配置面板；type 继续选品牌
	view := s.snapshot(chatID)
	if view != nil && view.pickMode == contract.PickModeType {
		return s.renderPickBrand(ctx, token, chatID)
	}
	return s.renderDetail(ctx, token, chatID)
}

// renderPickBrand 渲染品牌选择（type 模式）。
func (s *purchaseService) renderPickBrand(ctx context.Context, token string, chatID int64) error {
	view := s.snapshot(chatID)
	if view == nil || view.selected == nil {
		return nil
	}
	var sb strings.Builder
	sb.WriteString(s.t(view, "purchase.brand_title") + "\n\n")
	sb.WriteString(s.t(view, "purchase.brand_desc"))

	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.step = purchaseStepPickBrand
	}
	s.mu.Unlock()

	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), sb.String(),
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: s.brandKeyboard(view)})
}

func (s *purchaseService) selectBrand(ctx context.Context, token string, chatID int64, brand string) error {
	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.pickBrand = brand
		sess.pickCardType = ""
	}
	s.mu.Unlock()
	return s.renderPickCardType(ctx, token, chatID)
}

// renderPickCardType 渲染卡类型选择（type 模式）。
func (s *purchaseService) renderPickCardType(ctx context.Context, token string, chatID int64) error {
	view := s.snapshot(chatID)
	if view == nil || view.selected == nil {
		return nil
	}
	var sb strings.Builder
	sb.WriteString(s.t(view, "purchase.card_type_title") + "\n\n")
	sb.WriteString(s.t(view, "purchase.card_type_desc"))

	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.step = purchaseStepPickCardType
	}
	s.mu.Unlock()

	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), sb.String(),
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: s.cardTypeKeyboard(view)})
}

func (s *purchaseService) selectCardType(ctx context.Context, token string, chatID int64, cardType string) error {
	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.pickCardType = cardType
	}
	s.mu.Unlock()
	return s.renderDetail(ctx, token, chatID)
}

// sendConfigurePrompt 发送提示并等待文本输入（BIN）。
func (s *purchaseService) sendConfigurePrompt(ctx context.Context, token string, chatID int64, key string) error {
	view := s.snapshot(chatID)
	loc := "zh-CN"
	if view != nil {
		loc = view.locale
	}
	_ = s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), localizedText(purchaseTexts[key], loc),
		contract.SendMessageOptions{DisableWebPagePreview: true})
	return nil
}

// handleConfigureText 处理配置面板的文本输入：BIN 或国家双字母。
func (s *purchaseService) handleConfigureText(ctx context.Context, token string, view *purchaseView, text string) error {
	if view == nil || view.selected == nil {
		return nil
	}

	// BIN 输入：仅当处于挑头模式（输入 6 位）时处理。
	if isBinInput(text) && (view.pickMode == contract.PickModeBin || view.step == purchaseStepPickBin) {
		count, err := s.ports.Catalog.CountAvailableByBinPrefix(ctx, view.selected.ID, text)
		if err == nil && count == 0 {
			return s.sendError(ctx, token, view.chatID, nil, "purchase.bin_none")
		}
		s.mu.Lock()
		if sess := s.sessions[view.chatID]; sess != nil {
			sess.pickBin = text
			sess.step = purchaseStepConfigure
		}
		s.mu.Unlock()
		msg := fmt.Sprintf(s.t(view, "purchase.bin_set"), text)
		if err == nil {
			msg += "\n" + fmt.Sprintf(s.t(view, "purchase.bin_available"), count)
		}
		_ = s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", view.chatID), msg,
			contract.SendMessageOptions{DisableWebPagePreview: true})
		return s.renderDetail(ctx, token, view.chatID)
	}

	// 国家双字母输入：仅当模式需要国家（random/type）时处理。
	code := strings.ToUpper(text)
	if isCountryCode(code) && (view.pickMode == contract.PickModeRandom || view.pickMode == contract.PickModeType) {
		// 校验该国家在可选项中
		if s.countryAvailable(view, code) {
			s.mu.Lock()
			if sess := s.sessions[view.chatID]; sess != nil {
				sess.pickCountry = code
			}
			s.mu.Unlock()
			if view.pickMode == contract.PickModeType {
				return s.renderPickBrand(ctx, token, view.chatID)
			}
			return s.renderDetail(ctx, token, view.chatID)
		}
		return s.sendError(ctx, token, view.chatID, nil, "purchase.country_invalid")
	}

	return s.sendHelp(ctx, token, view.chatID)
}

// countryAvailable 判断国家是否在可选项内。
func (s *purchaseService) countryAvailable(view *purchaseView, code string) bool {
	if view == nil || view.pickStock == nil {
		return false
	}
	for _, c := range view.pickStock.Countries {
		if c.Code == code {
			return true
		}
	}
	return false
}

// --- 下单与支付 ---

// confirmOrder 预下单并展示金额，等待确认。
func (s *purchaseService) confirmOrder(ctx context.Context, token string, chatID int64) error {
	view := s.snapshot(chatID)
	if view == nil || view.selected == nil || view.userID == 0 {
		return nil
	}
	if err := s.validateOrder(view); err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.incomplete")
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
	// 多 SKU 商品在确认单顶部显示所选规格。
	if len(view.selected.SKUs) > 1 {
		for _, sku := range view.selected.SKUs {
			if sku.ID == view.selectedSKUID {
				sb.WriteString(s.t(view, "purchase.sku") + "：" + sku.Code + "\n\n")
				break
			}
		}
	}
	for _, it := range preview.Items {
		sb.WriteString("• " + it.Title)
		if it.CardCheckEnabled {
			sb.WriteString(" [" + s.t(view, "purchase.card_check") + "]")
		}
		if it.PickBin != "" {
			sb.WriteString(" [" + s.t(view, "purchase.pick_bin") + " " + it.PickBin + "]")
		}
		if it.PickCountry != "" {
			sb.WriteString(" [" + it.PickCountry)
			if len(it.PickBrands) > 0 {
				sb.WriteString(" " + strings.Join(it.PickBrands, "/"))
			}
			if len(it.PickCardTypes) > 0 {
				sb.WriteString(" " + strings.Join(it.PickCardTypes, "/"))
			}
			sb.WriteString("]")
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

// validateOrder 下单前校验选择的完整性。
func (s *purchaseService) validateOrder(view *purchaseView) error {
	if view == nil || view.selected == nil {
		return fmt.Errorf("no product selected")
	}
	if len(view.selected.SKUs) > 0 && view.selectedSKUID == 0 {
		return fmt.Errorf("SKU not selected")
	}
	if view.selected.PickEnabled {
		switch view.pickMode {
		case contract.PickModeBin:
			if !isBinInput(view.pickBin) {
				return fmt.Errorf("BIN not set")
			}
		case contract.PickModeRandom:
			if view.pickCountry == "" {
				return fmt.Errorf("country not selected")
			}
		case contract.PickModeType:
			if view.pickCountry == "" {
				return fmt.Errorf("country not selected")
			}
			if view.pickBrand == "" {
				return fmt.Errorf("brand not selected")
			}
			if view.pickCardType == "" {
				return fmt.Errorf("card type not selected")
			}
		default:
			return fmt.Errorf("pick mode not selected")
		}
	}
	return nil
}

// buildPurchaseItems 从快照生成订单项。
func buildPurchaseItems(view *purchaseView) []contract.PurchaseItem {
	if view == nil || view.selected == nil {
		return nil
	}
	item := contract.PurchaseItem{
		ProductID:        view.selected.ID,
		SKUID:            view.selectedSKUID,
		Quantity:         view.quantity,
		FulfillmentType:  view.selected.FulfillmentType,
		CardCheckEnabled: view.cardCheck,
	}
	// 按挑卡模式裁剪字段，避免残留的 BIN/国家/品牌污染后端下单参数。
	switch view.pickMode {
	case contract.PickModeBin:
		item.PickBin = view.pickBin
	case contract.PickModeRandom:
		item.PickCountry = view.pickCountry
	case contract.PickModeType:
		item.PickCountry = view.pickCountry
		if view.pickBrand != "" && view.pickBrand != "random" {
			item.PickBrands = []string{view.pickBrand}
		}
		if view.pickCardType != "" && view.pickCardType != "random" {
			item.PickCardTypes = []string{view.pickCardType}
		}
	}
	return []contract.PurchaseItem{item}
}

// createOrder 实际下单。
func (s *purchaseService) createOrder(ctx context.Context, token string, chatID int64) (*contract.PurchaseCreated, error) {
	view := s.snapshot(chatID)
	if view == nil || view.selected == nil || view.userID == 0 {
		return nil, fmt.Errorf("no active purchase session")
	}
	if err := s.validateOrder(view); err != nil {
		return nil, err
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
	// 余额支付成功后展示当前账户余额。
	if result.WalletPaidAmount != "" && result.WalletPaidAmount != "0" {
		if view != nil && s.ports.Wallet != nil {
			if balance, err := s.ports.Wallet.GetBalance(ctx, view.userID); err == nil {
				sb.WriteString(localizedText(purchaseTexts["purchase.balance_after"], loc) + " " + formatAmount(balance, created.Currency) + "\n")
			}
		}
	}
	sb.WriteString("\n" + localizedText(purchaseTexts["purchase.pay_help"], loc))

	s.mu.Lock()
	delete(s.sessions, chatID)
	s.mu.Unlock()

	// 在线支付：附带「打开支付链接」按钮。
	var markup inlineKeyboard
	if result.PayURL != "" {
		markup = inlineKeyboard{InlineKeyboard: [][]inlineButton{
			{{Text: localizedText(purchaseTexts["purchase.pay_open_link"], loc), URL: result.PayURL}},
		}}
	}
	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), sb.String(),
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: markup})
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
	// 优先用错误分类映射的友好文案；无分类时回退到调用方指定的通用 key。
	msgKey := key
	if err != nil {
		if classified := contract.ClassifyPurchaseError(err); classified != "" {
			msgKey = classified
		}
	}
	msg := localizedText(purchaseTexts[msgKey], loc)
	if msg == "" {
		msg = localizedText(purchaseTexts["purchase.error"], loc)
	}
	// 不把内部错误原文拼进用户消息，避免泄露内部细节。
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
		label := p.Title + "  " + formatAmount(p.PriceAmount, currency)
		// 可发数徽章：-1 表示无限库存。
		if p.StockAvailable < 0 {
			label += "  (" + s.t(view, "purchase.stock_label") + " " + s.t(view, "purchase.stock_unlimited") + ")"
		} else {
			label += "  (" + s.t(view, "purchase.stock_label") + " " + fmt.Sprintf("%d", p.StockAvailable) + ")"
		}
		rows = append(rows, []inlineButton{{
			Text:         label,
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

// detailKeyboard 配置面板键盘（按上下文动态分组）。
func (s *purchaseService) detailKeyboard(view *purchaseView) inlineKeyboard {
	product := view.selected
	rows := make([][]inlineButton, 0, 8)

	// SKU 选择
	if len(product.SKUs) > 1 {
		row := make([]inlineButton, 0, len(product.SKUs))
		for _, sku := range product.SKUs {
			row = append(row, inlineButton{
				Text:         sku.Code,
				CallbackData: cbSkuPrefix + fmt.Sprintf("%d", sku.ID),
			})
		}
		rows = append(rows, row)
	}

	// 数量
	rows = append(rows, []inlineButton{
		{Text: "−", CallbackData: cbQtyPrefix + fmt.Sprintf("%d", max(1, view.quantity-1))},
		{Text: fmt.Sprintf("%d", view.quantity), CallbackData: cbHelpBuy},
		{Text: "+", CallbackData: cbQtyPrefix + fmt.Sprintf("%d", view.quantity+1)},
	})

	// 挑卡模式
	if product.PickEnabled {
		modeLabel := "🎯 " + s.t(view, "purchase.pick_mode")
		if view.pickMode != "" {
			modeLabel += "：" + pickModeLabel(view, s)
		}
		rows = append(rows, []inlineButton{{
			Text:         modeLabel,
			CallbackData: cbBackPick,
		}})
	}

	// 测活
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

	// 立即购买
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

// pickModeKeyboard 挑卡模式选择键盘。
func (s *purchaseService) pickModeKeyboard(view *purchaseView) inlineKeyboard {
	rows := [][]inlineButton{
		{
			{Text: "🎲 " + s.t(view, "purchase.pick_mode_random"), CallbackData: cbPickModePrefix + "random"},
		},
		{
			{Text: "🔢 " + s.t(view, "purchase.pick_mode_bin"), CallbackData: cbPickModePrefix + "bin"},
		},
		{
			{Text: "🎴 " + s.t(view, "purchase.pick_mode_type"), CallbackData: cbPickModePrefix + "type"},
		},
		{
			{Text: "🔙 " + s.t(view, "purchase.back"), CallbackData: cbBackDetail},
			{Text: "❌ " + s.t(view, "purchase.cancel"), CallbackData: cbCancel},
		},
	}
	return inlineKeyboard{InlineKeyboard: rows}
}

// countryKeyboard 国家选择键盘（按库存降序，每行 2 个）。
func (s *purchaseService) countryKeyboard(view *purchaseView) inlineKeyboard {
	rows := make([][]inlineButton, 0)
	if view.pickStock == nil {
		return inlineKeyboard{InlineKeyboard: rows}
	}
	row := make([]inlineButton, 0, 2)
	for _, c := range view.pickStock.Countries {
		label := c.Code + " " + c.Name
		if c.Stock > 0 {
			label += fmt.Sprintf("(%d)", c.Stock)
		}
		row = append(row, inlineButton{
			Text:         label,
			CallbackData: cbCountryPrefix + c.Code,
		})
		if len(row) == 2 {
			rows = append(rows, row)
			row = make([]inlineButton, 0, 2)
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	rows = append(rows, []inlineButton{
		{Text: "🔙 " + s.t(view, "purchase.back"), CallbackData: cbBackPick},
		{Text: "❌ " + s.t(view, "purchase.cancel"), CallbackData: cbCancel},
	})
	return inlineKeyboard{InlineKeyboard: rows}
}

// brandKeyboard 品牌选择键盘。
func (s *purchaseService) brandKeyboard(view *purchaseView) inlineKeyboard {
	rows := make([][]inlineButton, 0)
	if view.pickStock == nil {
		return inlineKeyboard{InlineKeyboard: rows}
	}
	row := make([]inlineButton, 0, 2)
	for _, b := range view.pickStock.Brands {
		row = append(row, inlineButton{
			Text:         b.Name,
			CallbackData: cbBrandPrefix + b.Key,
		})
		if len(row) == 2 {
			rows = append(rows, row)
			row = make([]inlineButton, 0, 2)
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	rows = append(rows, []inlineButton{
		{Text: "🔙 " + s.t(view, "purchase.back"), CallbackData: cbBackCountry},
		{Text: "❌ " + s.t(view, "purchase.cancel"), CallbackData: cbCancel},
	})
	return inlineKeyboard{InlineKeyboard: rows}
}

// cardTypeKeyboard 卡类型选择键盘。
func (s *purchaseService) cardTypeKeyboard(view *purchaseView) inlineKeyboard {
	rows := make([][]inlineButton, 0)
	if view.pickStock == nil {
		return inlineKeyboard{InlineKeyboard: rows}
	}
	row := make([]inlineButton, 0, 2)
	for _, t := range view.pickStock.CardTypes {
		row = append(row, inlineButton{
			Text:         t.Name,
			CallbackData: cbCTypePrefix + t.Key,
		})
		if len(row) == 2 {
			rows = append(rows, row)
			row = make([]inlineButton, 0, 2)
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	rows = append(rows, []inlineButton{
		{Text: "🔙 " + s.t(view, "purchase.back"), CallbackData: cbBackCountry},
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

func isCountryCode(text string) bool {
	if len(text) != 2 {
		return false
	}
	for _, r := range text {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
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

func addAmounts(a, b string) string {
	da, err1 := decimal.NewFromString(strings.TrimSpace(a))
	db, err2 := decimal.NewFromString(strings.TrimSpace(b))
	if err1 != nil || err2 != nil {
		return a
	}
	return da.Add(db).Round(2).StringFixed(2)
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
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
	"purchase.pick_mode":       {"zh-CN": "挑卡", "zh-TW": "挑卡", "en-US": "Pick"},
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
	"purchase.help":            {"zh-CN": "在商品配置面板依次选择 SKU、数量、挑卡与测活后点击立即购买；挑头可直接输入 6 位 BIN，选国家可直接回复双字母代码。输入 /shop 重新开始。", "zh-TW": "在商品配置面板依次選擇 SKU、數量、挑卡與測活後點擊立即購買；挑頭可直接輸入 6 位 BIN，選國家可直接回覆雙字母代碼。輸入 /shop 重新開始。", "en-US": "In the product panel choose SKU, quantity, pick and card check in order, then Buy Now. Enter a 6-digit BIN for pick-by-BIN, or reply a 2-letter country code to pick a country. Type /shop to restart."},
	"purchase.error":           {"zh-CN": "操作失败。", "zh-TW": "操作失敗。", "en-US": "Operation failed."},
	"purchase.canceled":        {"zh-CN": "已取消购买。", "zh-TW": "已取消購買。", "en-US": "Purchase canceled."},
	"purchase.bin_set":         {"zh-CN": "已设置挑头 BIN：%s", "zh-TW": "已設定挑頭 BIN：%s", "en-US": "Pick BIN set: %s"},
	"purchase.bin_available":   {"zh-CN": "可用库存：%d", "zh-TW": "可用庫存：%d", "en-US": "Available: %d"},
	"purchase.bin_none":        {"zh-CN": "该 BIN 暂无可用库存，请换一个。", "zh-TW": "該 BIN 暫無可用庫存，請換一個。", "en-US": "No stock for this BIN, try another."},
	"purchase.bin_prompt":      {"zh-CN": "请直接输入 6 位 BIN（挑头）。", "zh-TW": "請直接輸入 6 位 BIN（挑頭）。", "en-US": "Please enter the 6-digit BIN."},
	"purchase.detail_hint":     {"zh-CN": "依次选择数量、挑卡与测活，然后立即购买。", "zh-TW": "依次選擇數量、挑卡與測活，然後立即購買。", "en-US": "Choose quantity, pick and card check, then Buy Now."},
	"purchase.current":         {"zh-CN": "当前选择", "zh-TW": "目前選擇", "en-US": "Current"},
	"purchase.sku":             {"zh-CN": "规格", "zh-TW": "規格", "en-US": "SKU"},
	"purchase.quantity":        {"zh-CN": "数量", "zh-TW": "數量", "en-US": "Qty"},
	"purchase.not_selected":    {"zh-CN": "未选择", "zh-TW": "未選擇", "en-US": "not selected"},
	"purchase.country":         {"zh-CN": "国家", "zh-TW": "國家", "en-US": "Country"},
	"purchase.brand":           {"zh-CN": "品牌", "zh-TW": "品牌", "en-US": "Brand"},
	"purchase.card_type":       {"zh-CN": "卡类型", "zh-TW": "卡類型", "en-US": "Card type"},
	"purchase.available":       {"zh-CN": "可发", "zh-TW": "可發", "en-US": "stock"},
	"purchase.plain_price":     {"zh-CN": "不测活价", "zh-TW": "不測活價", "en-US": "Plain"},
	"purchase.checked_price":   {"zh-CN": "测活价", "zh-TW": "測活價", "en-US": "Checked"},
	"purchase.pick_mode_random": {"zh-CN": "随机", "zh-TW": "隨機", "en-US": "Random"},
	"purchase.pick_mode_bin":    {"zh-CN": "挑头(BIN)", "zh-TW": "挑頭(BIN)", "en-US": "By BIN"},
	"purchase.pick_mode_type":   {"zh-CN": "挑卡种类", "zh-TW": "挑卡種類", "en-US": "By type"},
	"purchase.pick_mode_title":  {"zh-CN": "选择挑卡方式", "zh-TW": "選擇挑卡方式", "en-US": "Choose pick mode"},
	"purchase.pick_mode_desc":   {"zh-CN": "随机：只选国家；挑头：输入 BIN；挑卡种类：选国家+品牌+卡类型。", "zh-TW": "隨機：只選國家；挑頭：輸入 BIN；挑卡種類：選國家+品牌+卡類型。", "en-US": "Random: country only. By BIN: enter a BIN. By type: country + brand + card type."},
	"purchase.country_title":    {"zh-CN": "选择国家", "zh-TW": "選擇國家", "en-US": "Select country"},
	"purchase.country_desc":     {"zh-CN": "点击下方国家，或直接回复双字母代码（如 US）。", "zh-TW": "點擊下方國家，或直接回覆雙字母代碼（如 US）。", "en-US": "Tap a country below or reply a 2-letter code (e.g. US)."},
	"purchase.brand_title":      {"zh-CN": "选择品牌", "zh-TW": "選擇品牌", "en-US": "Select brand"},
	"purchase.brand_desc":       {"zh-CN": "选择品牌：", "zh-TW": "選擇品牌：", "en-US": "Select a brand:"},
	"purchase.card_type_title":  {"zh-CN": "选择卡类型", "zh-TW": "選擇卡類型", "en-US": "Select card type"},
	"purchase.card_type_desc":   {"zh-CN": "选择卡类型：", "zh-TW": "選擇卡類型：", "en-US": "Select a card type:"},
	"purchase.incomplete":       {"zh-CN": "请先完成全部必选配置（挑卡模式/国家/品牌/卡类型/BIN）。", "zh-TW": "請先完成全部必選配置（挑卡模式/國家/品牌/卡類型/BIN）。", "en-US": "Please complete all required selections first."},
	"purchase.country_invalid":  {"zh-CN": "该国家代码不在可选范围内，请重试。", "zh-TW": "該國家代碼不在可選範圍內，請重試。", "en-US": "Country code not available, try again."},
	"purchase.group_not_supported": {"zh-CN": "🛒 购买功能请在私聊中使用，群组内暂不支持。", "zh-TW": "🛒 購買功能請在私聊中使用，群組內暫不支援。", "en-US": "🛒 Please buy in a private chat with the bot."},
	"purchase.session_expired":    {"zh-CN": "⏰ 购买会话已过期，请重新发送 /shop 开始选购。", "zh-TW": "⏰ 購買會話已過期，請重新發送 /shop 開始選購。", "en-US": "⏰ Purchase session expired. Send /shop to start over."},
	"purchase.stock_insufficient": {"zh-CN": "😔 所选组合库存不足，请调整数量或挑选条件后重试。", "zh-TW": "😔 所選組合庫存不足，請調整數量或挑選條件後重試。", "en-US": "😔 Not enough stock for the selected options, adjust and retry."},
	"purchase.identity_required":  {"zh-CN": "请先绑定商城账号后再购买。", "zh-TW": "請先綁定商城帳號後再購買。", "en-US": "Please link your shop account first."},
	"purchase.insufficient":       {"zh-CN": "💳 余额不足，请先充值。", "zh-TW": "💳 餘額不足，請先充值。", "en-US": "💳 Insufficient balance, please top up."},
	"purchase.payment_failed":     {"zh-CN": "支付发起失败，请稍后重试或联系客服。", "zh-TW": "支付發起失敗，請稍後重試或聯絡客服。", "en-US": "Failed to start payment, try again later."},
	"purchase.order_failed":       {"zh-CN": "下单失败，请稍后重试。", "zh-TW": "下單失敗，請稍後重試。", "en-US": "Failed to place order, try again later."},
	"purchase.stock_label":        {"zh-CN": "可发", "zh-TW": "可發", "en-US": "stock"},
	"purchase.stock_unlimited":    {"zh-CN": "不限", "zh-TW": "不限", "en-US": "unlimited"},
	"purchase.pay_open_link":      {"zh-CN": "🔗 打开支付链接", "zh-TW": "🔗 開啟支付連結", "en-US": "🔗 Open payment link"},
	"purchase.balance_after":      {"zh-CN": "账户余额", "zh-TW": "帳戶餘額", "en-US": "Balance"},
	"purchase.wallet_title":       {"zh-CN": "💰 我的钱包", "zh-TW": "💰 我的錢包", "en-US": "💰 My Wallet"},
	"purchase.wallet_balance":     {"zh-CN": "当前余额", "zh-TW": "目前餘額", "en-US": "Current balance"},
}
