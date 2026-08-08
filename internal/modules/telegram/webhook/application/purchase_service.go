package application

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/qr"
	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
	"github.com/dujiao-next/internal/modules/telegram/webhook/contract"
	webhookdomain "github.com/dujiao-next/internal/modules/telegram/webhook/domain"
	"github.com/dujiao-next/internal/shared/countries"
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
	purchaseStepCheckChoice     = "check_choice"    // 测活选择（reply 键盘：毛料/包活）
	purchaseStepQuantity        = "quantity"        // 输入购买数量（文本输入步骤）
	purchaseStepConfirm         = "confirm"         // 确认下单
	purchaseStepPickChannel     = "pick_channel"    // 选择在线支付渠道
	purchaseStepBinStock        = "bin_stock"       // 卡头库存：输入 BIN
	purchaseStepRechargeAmount  = "recharge_amount" // 充值：输入金额
	purchaseStepRechargeChannel = "recharge_channel" // 充值：选择渠道
)

// 购买类型（商品初始页 8 按钮）。
type purchaseKind string

const (
	pickKindRandom purchaseKind = "random" // 随机购买（只选国家）
	pickKindBin    purchaseKind = "bin"    // 挑头购买（输6位BIN，无国家）
	pickKindHead3  purchaseKind = "head3"  // 3头（卡号3开头）
	pickKindHead4  purchaseKind = "head4"  // 4头（卡号4开头）
	pickKindHead5  purchaseKind = "head5"  // 5头（卡号5开头）
	pickKindHead6  purchaseKind = "head6"  // 6头（卡号6开头）
	pickKindCredit purchaseKind = "credit" // CREDIT（信用卡，提交 C）
	pickKindDebit  purchaseKind = "debit"  // DEBIT（借记卡，提交 D，匹配 D+PD）
)

// headKindToDigit 把首位购买类型转为卡号首位数字。
func headKindToDigit(k purchaseKind) string {
	switch k {
	case pickKindHead3:
		return "3"
	case pickKindHead4:
		return "4"
	case pickKindHead5:
		return "5"
	case pickKindHead6:
		return "6"
	}
	return ""
}

// headKindToSurchargeKey 把首位购买类型转为对应加价键（head3/head4/head5/head6）。
func headKindToSurchargeKey(k purchaseKind) string {
	switch k {
	case pickKindHead3:
		return "head3"
	case pickKindHead4:
		return "head4"
	case pickKindHead5:
		return "head5"
	case pickKindHead6:
		return "head6"
	}
	return ""
}

// 回调 data 前缀。
const (
	cbShopPrefix     = "shop:"
	cbShopStart      = "shop:start"
	cbCatPrefix      = "shop:cat:"
	cbProdPrefix     = "shop:prod:"
	cbSkuPrefix      = "shop:sku:"
	cbCcPrefix       = "shop:cc:"
	cbQtyPrefix      = "shop:qty:"
	cbBuyPrefix      = "shop:buy:" // 商品初始页购买类型按钮：random/bin/head3/head4/head5/head6/credit/debit
	cbPickModePrefix = "shop:pick:"
	cbCountryPrefix  = "shop:country:"
	cbCountryPage    = "shop:cpage:" // 选国家翻页：shop:cpage:N
	cbBrandPrefix    = "shop:brand:"
	cbCTypePrefix    = "shop:ctype:"
	cbConfirm        = "shop:confirm"
	cbPayBalance     = "shop:pay:balance"
	cbPayOnline      = "shop:pay:online"
	cbPayChannel     = "shop:pay:ch:"
	cbPayCheck       = "shop:paycheck:"
	cbBinStock       = "shop:binstock"
	cbWallet         = "shop:wallet"
	cbOrders         = "shop:orders"
	cbOrderPrefix    = "shop:order:"
	cbOrderPagePrefix = "shop:orders:page:"
	cbLang           = "shop:lang"
	cbRecharge       = "shop:recharge"
	cbRechargeCh     = "shop:recharge:ch:"
	cbRechargeCheck  = "shop:recharge:check:"
	cbBackCat        = "shop:back:cat"
	cbBackProd       = "shop:back:prod"
	cbBackDetail     = "shop:back:detail"
	cbBackPick       = "shop:back:pick"
	cbBackCountry    = "shop:back:country"
	cbCancel         = "shop:cancel"
	cbHelpBuy        = "shop:help"
	// cbMenu 非 shop: 前缀，由主 Service 处理并回退到主菜单（付款页「返回主页」按钮）。
	cbMenu = "menu"
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
	quantityDone  bool // 购买数量已由用户确认（避免重复询问）
	// 挑卡
	pickMode     contract.PickMode
	pickKind     purchaseKind // 商品初始页选中的购买类型（random/bin/head3/head4/head5/head6/credit/debit）
	pickCountry  string
	pickBrand    string
	pickCardType string
	pickBin      string
	pickStock    *contract.ShopPickStock
	countryPage  int // 选国家翻页
	// 测活
	cardCheck     bool
	// 充值临时金额
	rechargeAmount string
	lastUpdatedAt  time.Time
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
	quantityDone  bool // 购买数量已由用户确认（避免重复询问）
	pickMode     contract.PickMode
	pickKind     purchaseKind
	pickCountry  string
	pickBrand    string
	pickCardType string
	pickBin      string
	pickStock    *contract.ShopPickStock
	countryPage  int
	cardCheck     bool
	// 充值临时金额
	rechargeAmount string
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
		quantityDone:  sess.quantityDone,
		pickMode:     sess.pickMode,
		pickKind:     sess.pickKind,
		pickCountry:  sess.pickCountry,
		pickBrand:    sess.pickBrand,
		pickCardType: sess.pickCardType,
		pickBin:      sess.pickBin,
		pickStock:    sess.pickStock,
		countryPage:  sess.countryPage,
		cardCheck:     sess.cardCheck,
		rechargeAmount: sess.rechargeAmount,
	}
}

// purchaseService 实现 bot 内购买流程（浏览分类→商品→分步配置→确认→下单→支付）。
type purchaseService struct {
	ports  contract.PurchasePorts
	botapi contract.BotAPIClient
	locale func() string // 返回默认 locale
	// sendMainMenu 由主 Service 注入：语言切换后立即以新语言展示主菜单（提示 + 快捷键盘）。
	sendMainMenu func(ctx context.Context, token, chatID, locale string) error

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

// setMainMenuRenderer 注入主菜单渲染回调（由主 Service 在 WithPurchase 时设置）。
func (s *purchaseService) setMainMenuRenderer(fn func(ctx context.Context, token, chatID, locale string) error) {
	s.sendMainMenu = fn
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
	// 充值：/recharge
	if text == "/recharge" || strings.HasPrefix(text, "/recharge ") {
		return true, s.enterRecharge(ctx, token, chatID, msg.From)
	}

	// 退出命令：/cancel 直接取消会话；/start、/menu、/help 清空会话后交还主 Service 处理。
	// 必须在会话文本输入处理之前拦截，否则会卡在「输入 BIN / 充值金额 / 配置挑卡」等步骤里出不来。
	switch {
	case text == "/cancel" || strings.HasPrefix(text, "/cancel "):
		return true, s.cancel(ctx, token, chatID)
	case text == "/start" || strings.HasPrefix(text, "/start "),
		text == "/menu" || strings.HasPrefix(text, "/menu "),
		text == "/help" || strings.HasPrefix(text, "/help "):
		if s.snapshot(chatID) != nil {
			s.mu.Lock()
			delete(s.sessions, chatID)
			s.mu.Unlock()
		}
		return false, nil
	}

	view := s.snapshot(chatID)
	if view == nil {
		return false, nil
	}
	// 卡头库存：输入 6 位 BIN
	if view.step == purchaseStepBinStock {
		return true, s.handleBinStockInput(ctx, token, view, text)
	}
	// 充值：输入金额
	if view.step == purchaseStepRechargeAmount {
		return true, s.handleRechargeAmount(ctx, token, view, text, msg.From)
	}
	// 测活选择（reply 键盘）：识别「毛料/包活」文字
	if view.step == purchaseStepCheckChoice {
		return true, s.handleCheckChoice(ctx, token, view, text)
	}
	// 购买数量：识别纯整数
	if view.step == purchaseStepQuantity {
		return true, s.handleQuantityInput(ctx, token, view, text)
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
		return true, s.renderPickCountry(ctx, token, chatID, s.currentCountryPage(chatID))
	case strings.HasPrefix(data, cbSkuPrefix):
		return true, s.selectSKU(ctx, token, chatID, strings.TrimPrefix(data, cbSkuPrefix))
	case strings.HasPrefix(data, cbQtyPrefix):
		return true, s.setQuantity(ctx, token, chatID, strings.TrimPrefix(data, cbQtyPrefix))
	case strings.HasPrefix(data, cbBuyPrefix):
		return true, s.selectBuyType(ctx, token, chatID, strings.TrimPrefix(data, cbBuyPrefix))
	case strings.HasPrefix(data, cbPickModePrefix):
		return true, s.selectPickMode(ctx, token, chatID, strings.TrimPrefix(data, cbPickModePrefix))
	case strings.HasPrefix(data, cbCountryPage):
		page, err := strconv.Atoi(strings.TrimPrefix(data, cbCountryPage))
		if err != nil || page < 1 {
			page = 1
		}
		return true, s.renderPickCountry(ctx, token, chatID, page)
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
	case strings.HasPrefix(data, cbPayChannel):
		return true, s.selectPayChannel(ctx, token, chatID, strings.TrimPrefix(data, cbPayChannel))
	case strings.HasPrefix(data, cbPayCheck):
		return true, s.checkOrderPaid(ctx, token, chatID, strings.TrimPrefix(data, cbPayCheck), cb.From)
	case data == cbBinStock:
		return true, s.enterBinStock(ctx, token, chatID)
	case data == cbWallet:
		return true, s.ShowWallet(ctx, token, chatID, cb.From)
	case data == cbOrders:
		return true, s.renderOrders(ctx, token, chatID, 1, cb.From)
	case strings.HasPrefix(data, cbOrderPagePrefix):
		page, err := strconv.Atoi(strings.TrimPrefix(data, cbOrderPagePrefix))
		if err != nil || page < 1 {
			return true, nil
		}
		return true, s.renderOrders(ctx, token, chatID, page, cb.From)
	case strings.HasPrefix(data, cbOrderPrefix):
		return true, s.showOrderDetail(ctx, token, chatID, strings.TrimPrefix(data, cbOrderPrefix), cb.From)
	case data == cbLang:
		return true, s.toggleLanguage(ctx, token, chatID, cb.From)
	case data == cbRecharge:
		return true, s.enterRecharge(ctx, token, chatID, &cb.From)
	case strings.HasPrefix(data, cbRechargeCh):
		return true, s.createRechargeWithChannel(ctx, token, chatID, strings.TrimPrefix(data, cbRechargeCh), cb.From)
	case strings.HasPrefix(data, cbRechargeCheck):
		return true, s.checkRecharge(ctx, token, chatID, strings.TrimPrefix(data, cbRechargeCheck), cb.From)
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
	markup := inlineKeyboard{InlineKeyboard: [][]inlineButton{
		{
			{Text: localizedText(purchaseTexts["purchase.wallet_recharge_btn"], loc), CallbackData: cbRecharge},
			{Text: localizedText(purchaseTexts["purchase.orders_title"], loc), CallbackData: cbOrders},
		},
	}}
	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), msg,
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: markup})
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
	if user.Locale != "" {
		locale = resolveLocale(user.Locale)
	}
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
		sess.pickKind = ""
		sess.pickCountry = ""
		sess.pickBrand = ""
		sess.pickCardType = ""
		sess.pickBin = ""
		sess.pickStock = pickStock
		sess.countryPage = 1
		sess.cardCheck = false
	}
	s.mu.Unlock()
	return s.renderDetail(ctx, token, chatID)
}

// renderDetail 渲染商品初始页：文本显示随机/挑头两档价格，按钮为购买类型选择（随机/挑头/3头/4头/5头/6头/CREDIT/DEBIT）。
// 不显示「当前选择」、不显示基础价、不显示测活开关（测活改由后续 reply 键盘选择）。
func (s *purchaseService) renderDetail(ctx context.Context, token string, chatID int64) error {
	view := s.snapshot(chatID)
	if view == nil || view.selected == nil {
		return nil
	}
	product := view.selected

	var sb strings.Builder
	sb.WriteString("🛍️ " + product.Title + "\n\n")

	// 商品简介（与网站商品描述同源，自动同步）
	if desc := strings.TrimSpace(product.Description); desc != "" {
		sb.WriteString("📝 " + s.t(view, "purchase.product_desc") + "\n")
		sb.WriteString(truncateDescription(desc) + "\n\n")
	}

	// 价格区：仅显示随机购买/挑头购买两档（毛料+包活），不显示基础价行。
	surchargeBin := priceValue(product.PickPrices, "bin")
	plainRandom := product.PriceAmount
	plainBin := product.PriceAmount
	if !surchargeBin.IsZero() {
		plainBin = addAmounts(plainBin, surchargeBin.String())
	}
	sb.WriteString(s.t(view, "purchase.kind_random") + "：" + s.t(view, "purchase.plain_price") + " " + formatAmount(plainRandom, product.Currency))
	if product.CardCheckEnabled {
		checkedRandom := addAmounts(plainRandom, product.CardCheckFee)
		sb.WriteString(" " + s.t(view, "purchase.checked_price") + " " + formatAmount(checkedRandom, product.Currency))
	}
	sb.WriteString("\n")
	if product.PickEnabled {
		sb.WriteString(s.t(view, "purchase.kind_bin") + "：" + s.t(view, "purchase.plain_price") + " " + formatAmount(plainBin, product.Currency))
		if product.CardCheckEnabled {
			checkedBin := addAmounts(plainBin, product.CardCheckFee)
			sb.WriteString(" " + s.t(view, "purchase.checked_price") + " " + formatAmount(checkedBin, product.Currency))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n" + s.t(view, "purchase.detail_hint"))

	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), sb.String(),
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: s.detailKeyboard(ctx, view)})
}

// selectBuyType 处理商品初始页 8 个购买类型按钮。
// 挑头 → 输入 6 位 BIN（无国家）；其余 → 选国家（带翻页）。
func (s *purchaseService) selectBuyType(ctx context.Context, token string, chatID int64, kind string) error {
	view := s.snapshot(chatID)
	if view == nil || view.selected == nil {
		return nil
	}
	product := view.selected
	if !product.PickEnabled && kind != "random" {
		// 非挑卡商品只允许随机（立即购买）。
		return s.confirmOrder(ctx, token, chatID)
	}
	k := purchaseKind(kind)
	// 库存校验：库存为 0 的类型不允许进入（除挑头，需输BIN后才知库存）。
	if k != pickKindBin {
		if stock := s.buyTypeStock(view, k); stock <= 0 && product.FulfillmentType == "auto" {
			return s.sendError(ctx, token, chatID, nil, "purchase.stock_insufficient")
		}
	}
	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.pickKind = k
		sess.pickMode = "" // 新流程用 pickKind 取代 pickMode
		sess.pickCountry = ""
		sess.pickBrand = ""
		sess.pickCardType = ""
		sess.pickBin = ""
		sess.cardCheck = false
		sess.countryPage = 1
		sess.quantity = 1
		sess.quantityDone = false // 每次重新选购买类型都重新询问数量
	}
	s.mu.Unlock()
	switch k {
	case pickKindBin:
		// 挑头：进入 BIN 输入步骤（无国家）。
		s.mu.Lock()
		if sess := s.sessions[chatID]; sess != nil {
			sess.step = purchaseStepPickBin
		}
		s.mu.Unlock()
		return s.sendConfigurePrompt(ctx, token, chatID, "purchase.bin_prompt")
	default:
		// 其余类型先选国家。
		return s.renderPickCountry(ctx, token, chatID, 1)
	}
}

// handleCheckChoice 处理测活选择的文本输入（reply 键盘：毛料/包活）。
func (s *purchaseService) handleCheckChoice(ctx context.Context, token string, view *purchaseView, text string) error {
	t := strings.TrimSpace(text)
	switch t {
	case s.t(view, "purchase.check_plain_btn"), "毛料", "Plain", "plain", "PLAIn":
		s.mu.Lock()
		if sess := s.sessions[view.chatID]; sess != nil {
			sess.cardCheck = false
		}
		s.mu.Unlock()
		return s.sendCheckConfirm(ctx, token, view.chatID, false)
	case s.t(view, "purchase.check_checked_btn"), "包活", "Checked", "checked", "CHECKED":
		s.mu.Lock()
		if sess := s.sessions[view.chatID]; sess != nil {
			sess.cardCheck = true
		}
		s.mu.Unlock()
		return s.sendCheckConfirm(ctx, token, view.chatID, true)
	}
	return s.sendError(ctx, token, view.chatID, nil, "purchase.check_invalid")
}

// sendCheckConfirm 测活选择完成后展示用户选择并移除 reply 键盘，进入确认页。
func (s *purchaseService) sendCheckConfirm(ctx context.Context, token string, chatID int64, checked bool) error {
	// 移除 reply 键盘（发一条带 replyKeyboardRemove 的消息），然后进入确认页。
	_ = s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), s.checkChoiceSummary(ctx, chatID, checked),
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: replyKeyboardRemove{RemoveKeyboard: true}})
	return s.confirmOrder(ctx, token, chatID)
}

// checkChoiceSummary 生成测活选择后的摘要文本（展示用户选择 + 付款金额）。
func (s *purchaseService) checkChoiceSummary(ctx context.Context, chatID int64, checked bool) string {
	view := s.snapshot(chatID)
	if view == nil || view.selected == nil {
		return ""
	}
	product := view.selected
	var sb strings.Builder
	sb.WriteString("✅ " + s.t(view, "purchase.current") + "\n")
	sb.WriteString("  " + s.t(view, "purchase.kind_random") + "/" + buyTypeLabel(view) + ": ")
	plain := s.buyTypePlainPrice(view, view.pickKind)
	amt := plain
	if checked {
		amt = addAmounts(plain, product.CardCheckFee)
		sb.WriteString(s.t(view, "purchase.checked_price"))
	} else {
		sb.WriteString(s.t(view, "purchase.plain_price"))
	}
	sb.WriteString(" " + formatAmount(amt, product.Currency))
	if view.pickCountry != "" {
		sb.WriteString(" | " + s.t(view, "purchase.country") + " " + view.pickCountry)
	}
	sb.WriteString("\n")
	return sb.String()
}

// buyTypeLabel 返回当前 pickKind 的可读标签。
func buyTypeLabel(view *purchaseView) string {
	switch view.pickKind {
	case pickKindBin:
		return "挑头"
	case pickKindHead3:
		return "3头"
	case pickKindHead4:
		return "4头"
	case pickKindHead5:
		return "5头"
	case pickKindHead6:
		return "6头"
	case pickKindCredit:
		return "CREDIT"
	case pickKindDebit:
		return "DEBIT"
	}
	return "随机"
}

// priceValue 从 PickPrices 取 key 对应的加价（缺失/解析失败返回 0）。
func priceValue(prices map[string]string, key string) decimal.Decimal {
	if v, ok := prices[key]; ok {
		if d, err := decimal.NewFromString(strings.TrimSpace(v)); err == nil {
			return d.Round(2)
		}
	}
	return decimal.Zero
}

// headStockLabel 取首位挑卡库存（CountByBinHead 结果里该首位的合计；缺失返回 0）。
func (s *purchaseService) headStock(view *purchaseView, head string) int64 {
	if s.ports.Catalog == nil {
		return 0
	}
	heads, err := s.ports.Catalog.CountByBinHead(context.Background(), view.selected.ID)
	if err != nil {
		return 0
	}
	for _, h := range heads {
		if h.Head == head {
			return h.Stock
		}
	}
	return 0
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

// pickBrandName 品牌选项的本地化名称（与网页端一致：random 走 i18n，品牌名固定不翻译）。
func (s *purchaseService) pickBrandName(view *purchaseView, key string) string {
	switch key {
	case "random":
		return s.t(view, "purchase.pick_brand_random")
	case "visa":
		return "Visa"
	case "mastercard":
		return "Mastercard"
	case "discover":
		return "Discover"
	case "amex":
		return "AMEX"
	case "jcb":
		return "JCB"
	}
	return key
}

// pickCardTypeName 卡类型选项的本地化名称（与网页端 i18n 文案一致）。
func (s *purchaseService) pickCardTypeName(view *purchaseView, key string) string {
	switch key {
	case "random":
		return s.t(view, "purchase.pick_type_random")
	case "D":
		return s.t(view, "purchase.pick_type_d")
	case "PD":
		return s.t(view, "purchase.pick_type_pd")
	case "C":
		return s.t(view, "purchase.pick_type_c")
	}
	return key
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
		return s.renderPickCountry(ctx, token, chatID, 1)
	}
	return s.renderDetail(ctx, token, chatID)
}

// renderPickCountry 渲染国家选择（按库存降序，emoji+双字母，一页6个，带翻页）。
func (s *purchaseService) renderPickCountry(ctx context.Context, token string, chatID int64, page int) error {
	view := s.snapshot(chatID)
	if view == nil || view.selected == nil {
		return nil
	}
	if page < 1 {
		page = 1
	}
	var sb strings.Builder
	sb.WriteString(s.t(view, "purchase.country_title") + "\n\n")
	sb.WriteString(s.t(view, "purchase.country_desc"))

	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.step = purchaseStepPickCountry
		sess.countryPage = page
	}
	s.mu.Unlock()

	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), sb.String(),
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: s.countryKeyboard(view, page)})
}

func (s *purchaseService) selectCountry(ctx context.Context, token string, chatID int64, code string) error {
	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.pickCountry = code
		sess.pickBrand = ""
		sess.pickCardType = ""
	}
	s.mu.Unlock()
	// 新流程：选了国家后，若测活商品 → 测活选择（reply 键盘）；否则直接确认。
	view := s.snapshot(chatID)
	if view == nil || view.selected == nil {
		return s.renderDetail(ctx, token, chatID)
	}
	if view.pickKind == "" {
		// 兼容旧 type 模式：选品牌。
		if view.pickMode == contract.PickModeType {
			return s.renderPickBrand(ctx, token, chatID)
		}
		return s.renderDetail(ctx, token, chatID)
	}
	return s.enterCheckOrConfirm(ctx, token, chatID)
}

// currentCountryPage 返回当前选国家翻页（无会话返回 1）。
func (s *purchaseService) currentCountryPage(chatID int64) int {
	view := s.snapshot(chatID)
	if view == nil || view.countryPage < 1 {
		return 1
	}
	return view.countryPage
}

// enterCheckOrConfirm 确认前先询问购买数量（文本输入步骤），再走测活选择或确认页。
// quantityDone 标记本次会话已询问过数量，避免重复询问。
func (s *purchaseService) enterCheckOrConfirm(ctx context.Context, token string, chatID int64) error {
	view := s.snapshot(chatID)
	if view == nil || view.selected == nil {
		return nil
	}
	if !view.quantityDone {
		return s.sendQuantityPrompt(ctx, token, chatID)
	}
	if view.selected.CardCheckEnabled {
		return s.renderCheckChoice(ctx, token, chatID)
	}
	return s.confirmOrder(ctx, token, chatID)
}

// sendQuantityPrompt 发送「请输入购买数量：」并等待用户回复数字。
func (s *purchaseService) sendQuantityPrompt(ctx context.Context, token string, chatID int64) error {
	view := s.snapshot(chatID)
	loc := "zh-CN"
	if view != nil {
		loc = view.locale
	}
	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.step = purchaseStepQuantity
	}
	s.mu.Unlock()
	markup := inlineKeyboard{InlineKeyboard: [][]inlineButton{{
		{Text: "❌ " + localizedText(purchaseTexts["purchase.cancel"], loc), CallbackData: cbCancel},
	}}}
	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), localizedText(purchaseTexts["purchase.quantity_prompt"], loc),
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: markup})
}

// handleQuantityInput 处理购买数量的文本输入：纯整数 ≥1 写入会话并继续测活/确认；否则提示重输。
func (s *purchaseService) handleQuantityInput(ctx context.Context, token string, view *purchaseView, text string) error {
	if view == nil || view.selected == nil {
		return nil
	}
	qty, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || qty < 1 {
		return s.sendError(ctx, token, view.chatID, nil, "purchase.quantity_invalid")
	}
	s.mu.Lock()
	if sess := s.sessions[view.chatID]; sess != nil {
		sess.quantity = qty
		sess.quantityDone = true
	}
	s.mu.Unlock()
	return s.enterCheckOrConfirm(ctx, token, view.chatID)
}

// renderCheckChoice 渲染测活选择：reply 键盘 [毛料] [包活]。
func (s *purchaseService) renderCheckChoice(ctx context.Context, token string, chatID int64) error {
	view := s.snapshot(chatID)
	if view == nil || view.selected == nil {
		return nil
	}
	var sb strings.Builder
	sb.WriteString(s.t(view, "purchase.check_choice_title") + "\n\n")
	sb.WriteString(s.t(view, "purchase.check_choice_desc"))
	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.step = purchaseStepCheckChoice
	}
	s.mu.Unlock()
	markup := replyKeyboard{
		Keyboard: [][]replyKeyboardButton{{
			{Text: s.t(view, "purchase.check_plain_btn")},
			{Text: s.t(view, "purchase.check_checked_btn")},
		}},
		ResizeKeyboard:  true,
		OneTimeKeyboard: true,
	}
	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), sb.String(),
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: markup})
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

	// BIN 输入：挑头购买（6 位）时处理。
	if isBinInput(text) && (view.pickKind == pickKindBin || view.pickMode == contract.PickModeBin || view.step == purchaseStepPickBin) {
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
		// 新流程：挑头输入 BIN 后进入测活/确认；旧流程回配置面板。
		if view.pickKind == pickKindBin {
			return s.enterCheckOrConfirm(ctx, token, view.chatID)
		}
		return s.renderDetail(ctx, token, view.chatID)
	}

	// 国家双字母输入：需要国家时处理（新流程 pickKind 或旧 pickMode）。
	code := strings.ToUpper(text)
	needCountry := view.pickKind != "" && view.pickKind != pickKindBin ||
		view.pickMode == contract.PickModeRandom || view.pickMode == contract.PickModeType
	if isCountryCode(code) && needCountry {
		// 校验该国家在可选项中
		if s.countryAvailable(view, code) {
			s.mu.Lock()
			if sess := s.sessions[view.chatID]; sess != nil {
				sess.pickCountry = code
			}
			s.mu.Unlock()
			// 新流程：选国家后进入测活/确认。
			if view.pickKind != "" && view.pickKind != pickKindBin {
				return s.enterCheckOrConfirm(ctx, token, view.chatID)
			}
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
				names := make([]string, 0, len(it.PickBrands))
				for _, b := range it.PickBrands {
					names = append(names, s.pickBrandName(view, b))
				}
				sb.WriteString(" " + strings.Join(names, "/"))
			}
			if len(it.PickCardTypes) > 0 {
				names := make([]string, 0, len(it.PickCardTypes))
				for _, c := range it.PickCardTypes {
					names = append(names, s.pickCardTypeName(view, c))
				}
				sb.WriteString(" " + strings.Join(names, "/"))
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
	if !view.selected.PickEnabled {
		return nil
	}
	// 新流程按 pickKind 校验；空则回退旧 pickMode（兼容）。
	if view.pickKind != "" {
		switch view.pickKind {
		case pickKindBin:
			if !isBinInput(view.pickBin) {
				return fmt.Errorf("BIN not set")
			}
		case pickKindRandom:
			if view.pickCountry == "" {
				return fmt.Errorf("country not selected")
			}
		case pickKindHead3, pickKindHead4, pickKindHead5, pickKindHead6, pickKindCredit, pickKindDebit:
			if view.pickCountry == "" {
				return fmt.Errorf("country not selected")
			}
		default:
			return fmt.Errorf("pick kind not selected")
		}
		return nil
	}
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
	// 新流程按 pickKind 裁剪字段；空则回退旧 pickMode（兼容）。
	if view.pickKind != "" {
		switch view.pickKind {
		case pickKindBin:
			item.PickBin = view.pickBin
		case pickKindRandom:
			item.PickCountry = view.pickCountry
		case pickKindHead3, pickKindHead4, pickKindHead5, pickKindHead6:
			item.PickBin = headKindToDigit(view.pickKind) // 1 位首位，后端按位数 LIKE 匹配
			item.PickCountry = view.pickCountry
		case pickKindCredit:
			item.PickCountry = view.pickCountry
			item.PickCardTypes = []string{"C"}
		case pickKindDebit:
			item.PickCountry = view.pickCountry
			item.PickCardTypes = []string{"D"} // D 在库存查询匹配 D+PD
		}
		return []contract.PurchaseItem{item}
	}
	// 兼容旧 pickMode 流程。
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

// payOnline 发起在线支付：仅使用 epusdt（USDT/TRC20）渠道，多渠道时让用户选择，单渠道直接支付。
// 创建订单后直接在聊天内展示应付金额与收款地址，无需跳转收银台。
func (s *purchaseService) payOnline(ctx context.Context, token string, chatID int64) error {
	if s.ports.Payments == nil {
		return s.sendError(ctx, token, chatID, nil, "purchase.error")
	}
	channels, err := s.ports.Payments.ListPaymentChannels(ctx)
	if err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	epusdtChannels := filterEpusdtChannels(channels)
	if len(epusdtChannels) == 0 {
		return s.sendError(ctx, token, chatID, nil, "purchase.no_epusdt")
	}
	if len(epusdtChannels) == 1 {
		return s.createOnlinePayment(ctx, token, chatID, epusdtChannels[0].ID)
	}
	// 多个 epusdt 渠道：展示选择键盘（此时尚未创建订单）。
	view := s.snapshot(chatID)
	loc := "zh-CN"
	if view != nil {
		loc = view.locale
	}
	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.step = purchaseStepPickChannel
	}
	s.mu.Unlock()
	msg := localizedText(purchaseTexts["purchase.select_channel_title"], loc)
	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), msg,
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: s.channelKeyboard(epusdtChannels, loc)})
}

// filterEpusdtChannels 仅保留 epusdt（USDT/TRC20）在线支付渠道。
func filterEpusdtChannels(channels []contract.ShopPaymentChannel) []contract.ShopPaymentChannel {
	out := make([]contract.ShopPaymentChannel, 0, len(channels))
	for _, ch := range channels {
		switch strings.ToLower(strings.TrimSpace(ch.ChannelType)) {
		case "epusdt", "usdt", "usdt-trc20", "trx", "tron", "trc20":
			out = append(out, ch)
		}
	}
	return out
}

// selectPayChannel 用户选定在线支付渠道后发起支付。
func (s *purchaseService) selectPayChannel(ctx context.Context, token string, chatID int64, channelIDStr string) error {
	id, err := strconv.ParseUint(channelIDStr, 10, 64)
	if err != nil || id == 0 {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	return s.createOnlinePayment(ctx, token, chatID, uint(id))
}

// createOnlinePayment 创建订单并发起指定渠道的在线支付。
func (s *purchaseService) createOnlinePayment(ctx context.Context, token string, chatID int64, channelID uint) error {
	created, err := s.createOrder(ctx, token, chatID)
	if err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	if s.ports.Payments == nil {
		return s.sendError(ctx, token, chatID, nil, "purchase.error")
	}
	result, err := s.ports.Payments.CreatePayment(ctx, contract.PurchasePaymentInput{
		OrderID:   created.OrderID,
		ChannelID: channelID,
	})
	if err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	return s.renderPaymentResult(ctx, token, chatID, created, result)
}

// channelKeyboard 在线支付渠道选择键盘。
func (s *purchaseService) channelKeyboard(channels []contract.ShopPaymentChannel, loc string) inlineKeyboard {
	rows := make([][]inlineButton, 0, len(channels)+1)
	for _, ch := range channels {
		rows = append(rows, []inlineButton{{
			Text:         ch.Name,
			CallbackData: cbPayChannel + fmt.Sprintf("%d", ch.ID),
		}})
	}
	rows = append(rows, []inlineButton{{Text: "🔙 " + localizedText(purchaseTexts["purchase.back"], loc), CallbackData: cbBackDetail}})
	return inlineKeyboard{InlineKeyboard: rows}
}

func (s *purchaseService) renderPaymentResult(ctx context.Context, token string, chatID int64, created *contract.PurchaseCreated, result *contract.PurchasePaymentResult) error {
	view := s.snapshot(chatID)
	loc := "zh-CN"
	if view != nil {
		loc = view.locale
	}

	var sb strings.Builder
	// epusdt 渠道：直接在聊天内展示应付 USDT 金额与收款地址（只保留 USDT 一行，避免币种混淆）。
	if result.ReceiveAddress != "" {
		sb.WriteString("🪙 " + localizedText(purchaseTexts["purchase.epusdt_title"], loc) + "\n\n")
		sb.WriteString(localizedText(purchaseTexts["purchase.order_no"], loc) + " " + created.OrderNo + "\n")
		if result.PayAmount != "" && result.PayAmount != "0" {
			token := strings.ToUpper(result.Token)
			if token == "" {
				token = "USDT"
			}
			sb.WriteString(localizedText(purchaseTexts["purchase.epusdt_pay_amount"], loc) + " " + formatCryptoAmount(result.PayAmount) + " " + token + "\n")
		}
		sb.WriteString("\n" + localizedText(purchaseTexts["purchase.epusdt_address"], loc) + "\n")
		sb.WriteString("```\n" + result.ReceiveAddress + "\n```\n")
		sb.WriteString(localizedText(purchaseTexts["purchase.epusdt_copy_hint"], loc) + "\n")
		network := strings.ToUpper(result.Network)
		if network == "TRON" {
			network = "TRON（TRC20）"
		}
		sb.WriteString("\n" + localizedText(purchaseTexts["purchase.epusdt_network"], loc) + " " + network + "\n")
		if result.ExpiresAt != "" {
			sb.WriteString(localizedText(purchaseTexts["purchase.epusdt_expires"], loc) + " " + result.ExpiresAt + "\n")
		}
		sb.WriteString("\n" + localizedText(purchaseTexts["purchase.epusdt_hint"], loc))
		sb.WriteString("\n" + localizedText(purchaseTexts["purchase.epusdt_qr_hint"], loc))

		s.mu.Lock()
		delete(s.sessions, chatID)
		s.mu.Unlock()

		rows := [][]inlineButton{}
		if result.PayURL != "" {
			rows = append(rows, []inlineButton{{Text: localizedText(purchaseTexts["purchase.pay_open_link"], loc), URL: result.PayURL}})
		}
		rows = append(rows, []inlineButton{{Text: localizedText(purchaseTexts["purchase.pay_check"], loc), CallbackData: cbPayCheck + created.OrderNo}})
		rows = append(rows, []inlineButton{
			{Text: localizedText(purchaseTexts["purchase.orders_title"], loc), CallbackData: cbOrders},
			{Text: localizedText(purchaseTexts["purchase.exit_home"], loc), CallbackData: cbMenu},
		})
		// 二维码作为主图、说明文字作为 caption、按钮挂在图片消息上 —— 一条消息完成付款信息展示。
		return s.sendEpusdtQR(ctx, token, chatID, loc, result.ReceiveAddress, sb.String(), rows)
	}

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

	// 在线支付：附带「打开支付链接」按钮。PayURL 为空时保持 nil，避免序列化出
	// 空的 inline_keyboard 数组被 Telegram 拒绝（400 Bad Request）。
	var markup interface{}
	if result.PayURL != "" {
		markup = inlineKeyboard{InlineKeyboard: [][]inlineButton{
			{{Text: localizedText(purchaseTexts["purchase.pay_open_link"], loc), URL: result.PayURL}},
		}}
	}
	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), sb.String(),
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: markup})
}

// checkOrderPaid 查询订单支付/发货状态（epusdt 付款后用户点击「刷新支付状态」）。
func (s *purchaseService) checkOrderPaid(ctx context.Context, token string, chatID int64, orderNo string, from webhookdomain.User) error {
	if s.ports.OrderReader == nil {
		return s.sendError(ctx, token, chatID, nil, "purchase.unavailable")
	}
	user, err := s.resolveUser(ctx, from)
	if err != nil || user == nil {
		return s.sendError(ctx, token, chatID, err, "purchase.identity_failed")
	}
	loc := s.locale()
	if user.Locale != "" {
		loc = resolveLocale(user.Locale)
	}
	detail, err := s.ports.OrderReader.GetOrderByOrderNo(ctx, user.ID, orderNo)
	if err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	var sb strings.Builder
	sb.WriteString("🧾 " + localizedText(purchaseTexts["purchase.order_detail_title"], loc) + "\n\n")
	sb.WriteString("🆔 " + detail.OrderNo + "\n")
	sb.WriteString(localizedText(purchaseTexts["purchase.order_status"], loc) + "：" + orderStatusText(detail.Status, loc) + "\n")
	sb.WriteString(localizedText(purchaseTexts["purchase.total"], loc) + " " + formatAmount(detail.TotalAmount, detail.Currency) + "\n")
	paid := detail.Status == "paid" || detail.Status == "delivered" || detail.Status == "completed" || detail.Status == "fulfilling"
	if paid {
		sb.WriteString("\n✅ " + localizedText(purchaseTexts["purchase.pay_paid"], loc))
		if detail.Fulfillment != nil && strings.TrimSpace(detail.Fulfillment.Payload) != "" {
			sb.WriteString("\n\n" + localizedText(purchaseTexts["purchase.order_cards"], loc) + "：\n")
			sb.WriteString(truncatePayload(detail.Fulfillment.Payload))
		}
	} else {
		sb.WriteString("\n" + localizedText(purchaseTexts["purchase.pay_pending"], loc))
	}
	rows := [][]inlineButton{
		{{Text: localizedText(purchaseTexts["purchase.pay_check"], loc), CallbackData: cbPayCheck + detail.OrderNo}},
		{{Text: localizedText(purchaseTexts["purchase.orders_title"], loc), CallbackData: cbOrders}},
	}
	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), sb.String(),
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: inlineKeyboard{InlineKeyboard: rows}})
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

// detailKeyboard 商品初始页键盘：SKU（多 SKU）+ 数量 + 8 个购买类型按钮（带库存+毛料价）+ 返回/取消。
func (s *purchaseService) detailKeyboard(ctx context.Context, view *purchaseView) inlineKeyboard {
	product := view.selected
	rows := make([][]inlineButton, 0, 10)

	// SKU 选择（多 SKU 时显示）
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

	// 购买类型按钮：挑卡商品显示 8 个，非挑卡商品只显示「立即购买」。
	// 数量不在此处交互——改为确认前由 bot 发「请输入购买数量：」询问、用户回复数字。
	if product.PickEnabled {
		// 大按钮（一行一个）：随机购买、挑头购买。
		rows = append(rows, []inlineButton{s.buyTypeRow(view, pickKindRandom)})
		rows = append(rows, []inlineButton{s.buyTypeRow(view, pickKindBin)})
		// 小按钮（一行两个）：3头/4头、5头/6头、CREDIT/DEBIT。
		small := []purchaseKind{pickKindHead3, pickKindHead4, pickKindHead5, pickKindHead6, pickKindCredit, pickKindDebit}
		for i := 0; i+1 < len(small); i += 2 {
			rows = append(rows, []inlineButton{
				s.buyTypeRow(view, small[i]),
				s.buyTypeRow(view, small[i+1]),
			})
		}
	} else {
		rows = append(rows, []inlineButton{{
			Text:         "🛒 " + s.t(view, "purchase.buy_now"),
			CallbackData: cbConfirm,
		}})
	}

	rows = append(rows, []inlineButton{
		{Text: "🔙 " + s.t(view, "purchase.back"), CallbackData: cbBackProd},
		{Text: "❌ " + s.t(view, "purchase.cancel"), CallbackData: cbCancel},
	})
	return inlineKeyboard{InlineKeyboard: rows}
}

// buyTypeKindLabel 返回购买类型按钮的中文标签（不含库存/价格）。
func (s *purchaseService) buyTypeKindLabel(view *purchaseView, k purchaseKind) string {
	switch k {
	case pickKindRandom:
		return s.t(view, "purchase.kind_random")
	case pickKindBin:
		return s.t(view, "purchase.kind_bin")
	case pickKindHead3:
		return "3" + s.t(view, "purchase.kind_head_suffix")
	case pickKindHead4:
		return "4" + s.t(view, "purchase.kind_head_suffix")
	case pickKindHead5:
		return "5" + s.t(view, "purchase.kind_head_suffix")
	case pickKindHead6:
		return "6" + s.t(view, "purchase.kind_head_suffix")
	case pickKindCredit:
		return s.t(view, "purchase.kind_credit")
	case pickKindDebit:
		return s.t(view, "purchase.kind_debit")
	}
	return string(k)
}

// buyTypePlainPrice 返回某购买类型的毛料价（基础价 + 对应加价）。
func (s *purchaseService) buyTypePlainPrice(view *purchaseView, k purchaseKind) string {
	product := view.selected
	base := product.PriceAmount
	var surcharge decimal.Decimal
	switch k {
	case pickKindBin:
		surcharge = priceValue(product.PickPrices, "bin")
	case pickKindHead3, pickKindHead4, pickKindHead5, pickKindHead6:
		surcharge = priceValue(product.PickPrices, headKindToSurchargeKey(k))
	case pickKindCredit:
		surcharge = priceValue(product.PickPrices, "C")
	case pickKindDebit:
		surcharge = priceValue(product.PickPrices, "D")
	}
	plain := base
	if !surcharge.IsZero() {
		plain = addAmounts(plain, surcharge.String())
	}
	return plain
}

// buyTypeStock 返回某购买类型的库存（随机=总库存、挑头=总库存、3-6头=首位库存、CREDIT=C库存、DEBIT=D+PD库存）。
func (s *purchaseService) buyTypeStock(view *purchaseView, k purchaseKind) int64 {
	product := view.selected
	switch k {
	case pickKindRandom, pickKindBin:
		return product.StockAvailable
	case pickKindHead3, pickKindHead4, pickKindHead5, pickKindHead6:
		return s.headStock(view, headKindToDigit(k))
	case pickKindCredit:
		return s.cardTypeStock(view, []string{"C"})
	case pickKindDebit:
		return s.cardTypeStock(view, []string{"D"}) // D 在库存聚合里匹配 D+PD
	}
	return 0
}

// cardTypeStock 从 CountPickAttrs 聚合指定卡类型的库存。
func (s *purchaseService) cardTypeStock(view *purchaseView, cardTypes []string) int64 {
	if s.ports.Catalog == nil || view.pickStock == nil {
		return 0
	}
	attrs, err := s.ports.Catalog.CountPickAttrs(context.Background(), view.selected.ID)
	if err != nil {
		return 0
	}
	// D 是超集：匹配 D 与 PD。
	want := func(ct string) bool {
		for _, t := range cardTypes {
			if ct == t {
				return true
			}
			if t == "D" && ct == "PD" {
				return true
			}
		}
		return false
	}
	var total int64
	for _, a := range attrs {
		if want(a.CardType) {
			total += a.Total
		}
	}
	return total
}

// buyTypeRow 构造单个购买类型按钮行：标签[库存] 毛料价。
// buyTypeRow 构造单个购买类型按钮：标签[库存数字] 毛料价U。
// 挑头购买按钮只显示价格（无库存），库存留待用户输入 BIN 后回显。
// 库存括号内只显示数字；价格后缀统一用「U」（站点 USD 收款）。
func (s *purchaseService) buyTypeRow(view *purchaseView, k purchaseKind) inlineButton {
	label := s.buyTypeKindLabel(view, k)
	price := s.buyTypePlainPrice(view, k)
	if k == pickKindBin {
		// 挑头：只显示价格，库存留待输入 BIN 后回显。
		return inlineButton{
			Text:         fmt.Sprintf("%s %s", label, formatButtonAmount(price, view.currency)),
			CallbackData: cbBuyPrefix + string(k),
		}
	}
	stock := s.buyTypeStock(view, k)
	return inlineButton{
		Text:         fmt.Sprintf("%s [%d] %s", label, stock, formatButtonAmount(price, view.currency)),
		CallbackData: cbBuyPrefix + string(k),
	}
}

// formatButtonAmount 把金额格式化为「数字 U」（站点 USD 收款），购买类型按钮专用，
// 避免影响 formatAmount 的其它调用方（确认页/支付页仍显示完整币种）。
func formatButtonAmount(amount, currency string) string {
	dec, err := decimal.NewFromString(strings.TrimSpace(amount))
	if err != nil {
		return strings.TrimSpace(amount) + " U"
	}
	return dec.Round(2).StringFixed(2) + " U"
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

// countryKeyboard 国家选择键盘（按库存降序，emoji+双字母，一页6个，带翻页）。
func (s *purchaseService) countryKeyboard(view *purchaseView, page int) inlineKeyboard {
	rows := make([][]inlineButton, 0)
	if view.pickStock == nil {
		return inlineKeyboard{InlineKeyboard: rows}
	}
	const perPage = 6
	all := view.pickStock.Countries
	start := (page - 1) * perPage
	if start < 0 || start >= len(all) {
		start = 0
	}
	end := start + perPage
	if end > len(all) {
		end = len(all)
	}
	row := make([]inlineButton, 0, 2)
	for _, c := range all[start:end] {
		label := countries.EmojiFlag(c.Code) + " " + c.Code
		if c.Stock > 0 {
			label += fmt.Sprintf(" (库存%d)", c.Stock)
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
	// 翻页行
	var navRow []inlineButton
	if page > 1 {
		navRow = append(navRow, inlineButton{Text: s.t(view, "purchase.country_page_prev"), CallbackData: cbCountryPage + fmt.Sprintf("%d", page-1)})
	}
	if end < len(all) {
		navRow = append(navRow, inlineButton{Text: s.t(view, "purchase.country_page_next"), CallbackData: cbCountryPage + fmt.Sprintf("%d", page+1)})
	}
	if len(navRow) > 0 {
		rows = append(rows, navRow)
	}
	rows = append(rows, []inlineButton{
		{Text: "🔙 " + s.t(view, "purchase.back"), CallbackData: cbBackDetail},
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
			Text:         s.pickBrandName(view, b.Key),
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
			Text:         s.pickCardTypeName(view, t.Key),
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

// formatCryptoAmount 格式化加密币金额：最多保留 6 位小数并去除尾零（如 USDT）。
func formatCryptoAmount(amount string) string {
	dec, err := decimal.NewFromString(strings.TrimSpace(amount))
	if err != nil || dec.IsZero() {
		return strings.TrimSpace(amount)
	}
	return dec.Truncate(6).String()
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
	"purchase.detail_hint":     {"zh-CN": "选择购买类型开始：随机/挑头输入BIN，其余类型先选国家，再选是否测活。", "zh-TW": "選擇購買類型開始：隨機/挑頭輸入BIN，其餘類型先選國家，再選是否測活。", "en-US": "Pick a purchase type to start: random / by-BIN, others pick a country first, then choose card check."},
	"purchase.kind_random":      {"zh-CN": "随机购买", "zh-TW": "隨機購買", "en-US": "Random"},
	"purchase.kind_bin":          {"zh-CN": "挑头购买", "zh-TW": "挑頭購買", "en-US": "By BIN"},
	"purchase.kind_head_suffix":  {"zh-CN": "头", "zh-TW": "頭", "en-US": "-head"},
	"purchase.kind_credit":       {"zh-CN": "CREDIT 信用卡", "zh-TW": "CREDIT 信用卡", "en-US": "CREDIT"},
	"purchase.kind_debit":        {"zh-CN": "DEBIT 借记卡", "zh-TW": "DEBIT 借記卡", "en-US": "DEBIT"},
	"purchase.check_choice_title": {"zh-CN": "请选择是否测活", "zh-TW": "請選擇是否測活", "en-US": "Choose card check option"},
	"purchase.check_choice_desc":  {"zh-CN": "点击下方按钮（或在输入框发送）：「毛料」不测活，「包活」测活。", "zh-TW": "點擊下方按鈕（或在輸入框發送）：「毛料」不測活，「包活」測活。", "en-US": "Tap a button (or type): \"Plain\" = no check, \"Checked\" = card check."},
	"purchase.check_plain_btn":    {"zh-CN": "毛料", "zh-TW": "毛料", "en-US": "Plain"},
	"purchase.check_checked_btn":  {"zh-CN": "包活", "zh-TW": "包活", "en-US": "Checked"},
	"purchase.check_invalid":      {"zh-CN": "请发送「毛料」或「包活」选择测活，或 /cancel 取消。", "zh-TW": "請發送「毛料」或「包活」選擇測活，或 /cancel 取消。", "en-US": "Send \"Plain\" or \"Checked\" to choose, or /cancel to abort."},
	"purchase.country_page_prev": {"zh-CN": "◀ 上一页", "zh-TW": "◀ 上一頁", "en-US": "◀ Prev"},
	"purchase.country_page_next": {"zh-CN": "下一页 ▶", "zh-TW": "下一頁 ▶", "en-US": "Next ▶"},
	"purchase.current":         {"zh-CN": "当前选择", "zh-TW": "目前選擇", "en-US": "Current"},
	"purchase.sku":             {"zh-CN": "规格", "zh-TW": "規格", "en-US": "SKU"},
	"purchase.quantity":        {"zh-CN": "数量", "zh-TW": "數量", "en-US": "Qty"},
	"purchase.quantity_prompt": {"zh-CN": "请输入购买数量：", "zh-TW": "請輸入購買數量：", "en-US": "Please enter quantity:"},
	"purchase.quantity_invalid": {"zh-CN": "请输入有效的数量（≥1 的整数）。", "zh-TW": "請輸入有效的數量（≥1 的整數）。", "en-US": "Please enter a valid quantity (integer ≥ 1)."},
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
	"purchase.pick_brand_random": {"zh-CN": "随机", "zh-TW": "隨機", "en-US": "Random"},
	"purchase.pick_type_random":  {"zh-CN": "随机", "zh-TW": "隨機", "en-US": "Random"},
	"purchase.pick_type_d":       {"zh-CN": "D卡（含预付）", "zh-TW": "D卡（含預付）", "en-US": "D (incl. prepaid)"},
	"purchase.pick_type_pd":      {"zh-CN": "纯D（不含预付）", "zh-TW": "純D（不含預付）", "en-US": "Pure D (excl. prepaid)"},
	"purchase.pick_type_c":       {"zh-CN": "纯C", "zh-TW": "純C", "en-US": "Pure C"},
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
	"purchase.no_channel":         {"zh-CN": "暂无可用的在线支付渠道，请稍后再试或联系客服。", "zh-TW": "暫無可用的線上支付管道，請稍後再試或聯絡客服。", "en-US": "No online payment channel available right now."},
	"purchase.select_channel_title": {"zh-CN": "请选择支付渠道：", "zh-TW": "請選擇支付管道：", "en-US": "Choose a payment channel:"},
	"purchase.binstock_title":     {"zh-CN": "📦 卡头库存", "zh-TW": "📦 卡頭庫存", "en-US": "📦 BIN Stock"},
	"purchase.binstock_prompt":    {"zh-CN": "请输入 6 位卡头(BIN) 查询各商品的可用库存：", "zh-TW": "請輸入 6 位卡頭(BIN) 查詢各商品的可用庫存：", "en-US": "Enter a 6-digit BIN to check available stock:"},
	"purchase.binstock_bin":       {"zh-CN": "卡头", "zh-TW": "卡頭", "en-US": "BIN"},
	"purchase.binstock_none":      {"zh-CN": "未找到该卡头的可用卡密。", "zh-TW": "未找到該卡頭的可用卡密。", "en-US": "No available cards for this BIN."},
	"purchase.binstock_total":     {"zh-CN": "合计可发", "zh-TW": "合計可發", "en-US": "Total available"},
	"purchase.orders_title":       {"zh-CN": "📦 我的订单", "zh-TW": "📦 我的訂單", "en-US": "📦 My Orders"},
	"purchase.orders_empty":       {"zh-CN": "暂无订单。", "zh-TW": "暫無訂單。", "en-US": "No orders yet."},
	"purchase.order_detail_title": {"zh-CN": "📋 订单详情", "zh-TW": "📋 訂單詳情", "en-US": "📋 Order Detail"},
	"purchase.order_status":       {"zh-CN": "状态", "zh-TW": "狀態", "en-US": "Status"},
	"purchase.order_time":         {"zh-CN": "下单时间", "zh-TW": "下單時間", "en-US": "Placed at"},
	"purchase.order_cards":        {"zh-CN": "卡密", "zh-TW": "卡密", "en-US": "Cards"},
	"purchase.order_no_cards":     {"zh-CN": "（暂无卡密，发货后自动显示，并会推送到本对话）", "zh-TW": "（暫無卡密，發貨後自動顯示，並會推送到本對話）", "en-US": "(No cards yet — they will appear here and be pushed here once fulfilled.)"},
	"purchase.orders_back":        {"zh-CN": "⬅️ 返回订单列表", "zh-TW": "⬅️ 返回訂單列表", "en-US": "⬅️ Back to orders"},
	"purchase.lang_switched":      {"zh-CN": "🌐 语言已切换为中文。", "zh-TW": "🌐 語言已切換為中文。", "en-US": "🌐 Language switched to English."},
	"purchase.recharge_title":     {"zh-CN": "💳 充值", "zh-TW": "💳 儲值", "en-US": "💳 Recharge"},
	"purchase.recharge_amount_prompt": {"zh-CN": "请输入充值 USDT 金额（如 100 或 50.5）：", "zh-TW": "請輸入儲值 USDT 金額（如 100 或 50.5）：", "en-US": "Enter recharge amount in USDT (e.g. 100 or 50.5):"},
	"purchase.recharge_amount_invalid": {"zh-CN": "金额无效，请输入大于 0 的数字。", "zh-TW": "金額無效，請輸入大於 0 的數字。", "en-US": "Invalid amount, enter a number greater than 0."},
	"purchase.recharge_payable":   {"zh-CN": "应付", "zh-TW": "應付", "en-US": "Payable"},
	"purchase.recharge_status":    {"zh-CN": "充值状态", "zh-TW": "儲值狀態", "en-US": "Recharge status"},
	"purchase.recharge_check":     {"zh-CN": "🔄 查看到账状态", "zh-TW": "🔄 查看入帳狀態", "en-US": "🔄 Check status"},
	"purchase.recharge_created":   {"zh-CN": "充值订单已创建，请完成支付（支付后自动到账）：", "zh-TW": "儲值訂單已建立，請完成支付（支付後自動入帳）：", "en-US": "Recharge order created, complete the payment (auto-credited):"},
	"purchase.recharge_pending":   {"zh-CN": "待支付", "zh-TW": "待支付", "en-US": "Pending"},
	"purchase.recharge_paid":      {"zh-CN": "已到账 ✅", "zh-TW": "已入帳 ✅", "en-US": "Credited ✅"},
	"purchase.recharge_failed":    {"zh-CN": "失败/已过期", "zh-TW": "失敗/已過期", "en-US": "Failed/Expired"},
	"purchase.wallet_recharge_btn": {"zh-CN": "💳 充值", "zh-TW": "💳 儲值", "en-US": "💳 Recharge"},
	"purchase.no_epusdt":         {"zh-CN": "未配置可用的 USDT 支付渠道，请稍后再试或联系客服。", "zh-TW": "未配置可用的 USDT 支付管道，請稍後再試或聯絡客服。", "en-US": "No USDT payment channel configured right now."},
	"purchase.epusdt_title":      {"zh-CN": "USDT 在线支付", "zh-TW": "USDT 線上支付", "en-US": "USDT Online Payment"},
	"purchase.epusdt_payable":    {"zh-CN": "应付", "zh-TW": "應付", "en-US": "Payable"},
	"purchase.epusdt_pay_amount": {"zh-CN": "应付 USDT", "zh-TW": "應付 USDT", "en-US": "Payable USDT"},
	"purchase.epusdt_address":    {"zh-CN": "收款地址（TRC20）", "zh-TW": "收款地址（TRC20）", "en-US": "Receive address (TRC20)"},
	"purchase.epusdt_copy_hint":  {"zh-CN": "👆 点击上方地址即可一键复制。", "zh-TW": "👆 點擊上方地址即可一鍵複製。", "en-US": "👆 Tap the address above to copy it."},
	"purchase.epusdt_qr_hint":    {"zh-CN": "📲 USDT（TRC20）收款二维码，扫码转账。", "zh-TW": "📲 USDT（TRC20）收款 QR Code，掃碼轉帳。", "en-US": "📲 USDT (TRC20) receive QR code, scan to pay."},
	"purchase.exit_home":         {"zh-CN": "🏠 返回主页", "zh-TW": "🏠 返回主頁", "en-US": "🏠 Home"},
	"purchase.menu_shop":         {"zh-CN": "🛍️ 开始购物", "zh-TW": "🛍️ 開始購物", "en-US": "🛍️ Shop"},
	"purchase.menu_binstock":     {"zh-CN": "📦 卡头库存", "zh-TW": "📦 卡頭庫存", "en-US": "📦 BIN Stock"},
	"purchase.menu_wallet":       {"zh-CN": "💰 我的钱包", "zh-TW": "💰 我的錢包", "en-US": "💰 Wallet"},
	"purchase.menu_orders":       {"zh-CN": "📋 我的订单", "zh-TW": "📋 我的訂單", "en-US": "📋 Orders"},
	"purchase.menu_help":         {"zh-CN": "❓ 帮助中心", "zh-TW": "❓ 幫助中心", "en-US": "❓ Help"},
	"help.back":                  {"zh-CN": "↩️ 返回帮助中心", "zh-TW": "↩️ 返回幫助中心", "en-US": "↩️ Back to help"},
	"help.contact_support":       {"zh-CN": "💬 联系客服", "zh-TW": "💬 聯繫客服", "en-US": "💬 Contact support"},
	"help.item_disabled":         {"zh-CN": "该条目已停用。", "zh-TW": "該條目已停用。", "en-US": "This item is disabled."},
	"purchase.epusdt_network":    {"zh-CN": "网络", "zh-TW": "網路", "en-US": "Network"},
	"purchase.epusdt_expires":    {"zh-CN": "过期时间", "zh-TW": "過期時間", "en-US": "Expires at"},
	"purchase.epusdt_hint":       {"zh-CN": "请将应付 USDT 转账至上方地址，支付成功后订单自动发货，卡密将以 txt 文件推送到本对话。", "zh-TW": "請將應付 USDT 轉帳至上方地址，支付成功後訂單自動發貨，卡密將以 txt 檔案推送到本對話。", "en-US": "Transfer the payable USDT to the address above. Once paid, your order is fulfilled automatically and cards are delivered here as a txt file."},
	"purchase.recharge_epusdt_hint": {"zh-CN": "请将应付 USDT 转账至上方地址，支付成功后自动到账余额。", "zh-TW": "請將應付 USDT 轉帳至上方地址，支付成功後自動入帳餘額。", "en-US": "Transfer the payable USDT to the address above. Your balance is credited automatically once paid."},
	"purchase.pay_check":         {"zh-CN": "🔄 刷新支付状态", "zh-TW": "🔄 重新整理付款狀態", "en-US": "🔄 Refresh payment status"},
	"purchase.pay_pending":       {"zh-CN": "支付处理中或未到账，请确认已向收款地址转账后点击刷新。", "zh-TW": "付款處理中或未入帳，請確認已向收款地址轉帳後點擊重新整理。", "en-US": "Payment pending. Confirm the transfer to the address and tap refresh."},
	"purchase.pay_paid":          {"zh-CN": "支付成功，订单已处理。", "zh-TW": "付款成功，訂單已處理。", "en-US": "Payment successful, order processed."},
	"purchase.product_desc":      {"zh-CN": "商品简介", "zh-TW": "商品簡介", "en-US": "Description"},
	"order.status.pending_payment":     {"zh-CN": "待支付", "zh-TW": "待支付", "en-US": "Pending payment"},
	"order.status.paid":                {"zh-CN": "已支付", "zh-TW": "已支付", "en-US": "Paid"},
	"order.status.fulfilling":          {"zh-CN": "发货中", "zh-TW": "發貨中", "en-US": "Fulfilling"},
	"order.status.partially_delivered": {"zh-CN": "部分发货", "zh-TW": "部分發貨", "en-US": "Partially delivered"},
	"order.status.partially_refunded":  {"zh-CN": "部分退款", "zh-TW": "部分退款", "en-US": "Partially refunded"},
	"order.status.delivered":           {"zh-CN": "已发货", "zh-TW": "已發貨", "en-US": "Delivered"},
	"order.status.completed":           {"zh-CN": "已完成", "zh-TW": "已完成", "en-US": "Completed"},
	"order.status.canceled":            {"zh-CN": "已取消", "zh-TW": "已取消", "en-US": "Canceled"},
	"order.status.refunded":            {"zh-CN": "已退款", "zh-TW": "已退款", "en-US": "Refunded"},
	"order.status.expired":             {"zh-CN": "已过期", "zh-TW": "已過期", "en-US": "Expired"},
}

// --- 个人功能：卡头库存 / 我的订单 / 语言 / 充值 ---

// resolveUser 通过回调/消息的 Telegram 身份解析（或创建）商城账号。
func (s *purchaseService) resolveUser(ctx context.Context, from webhookdomain.User) (*contract.PurchaseUser, error) {
	if s.ports.Identity == nil {
		return nil, fmt.Errorf("identity unavailable")
	}
	return s.ports.Identity.ResolveOrProvision(ctx,
		fmt.Sprintf("%d", from.ID), from.UserName, from.FirstName, from.LastName)
}

// enterBinStock 进入卡头库存查询：提示输入 BIN。
func (s *purchaseService) enterBinStock(ctx context.Context, token string, chatID int64) error {
	loc := s.locale()
	s.mu.Lock()
	if sess, ok := s.sessions[chatID]; ok {
		loc = sess.locale
		sess.step = purchaseStepBinStock
	} else {
		s.sessions[chatID] = &purchaseSession{chatID: chatID, locale: loc, step: purchaseStepBinStock, lastUpdatedAt: time.Now()}
	}
	s.mu.Unlock()
	msg := localizedText(purchaseTexts["purchase.binstock_title"], loc) + "\n\n" + localizedText(purchaseTexts["purchase.binstock_prompt"], loc)
	markup := inlineKeyboard{InlineKeyboard: [][]inlineButton{{
		{Text: "❌ " + localizedText(purchaseTexts["purchase.cancel"], loc), CallbackData: cbCancel},
	}}}
	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), msg,
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: markup})
}

// handleBinStockInput 处理卡头库存的 BIN 输入：遍历 bot 可见商品查可用库存并汇总。
func (s *purchaseService) handleBinStockInput(ctx context.Context, token string, view *purchaseView, text string) error {
	loc := view.locale
	bin := strings.TrimSpace(text)
	if !isBinInput(bin) {
		return s.sendError(ctx, token, view.chatID, nil, "purchase.binstock_prompt")
	}
	if s.ports.Catalog == nil {
		return s.sendError(ctx, token, view.chatID, nil, "purchase.error")
	}
	var sb strings.Builder
	sb.WriteString(localizedText(purchaseTexts["purchase.binstock_title"], loc) + " · " + bin + "\n\n")
	var total int64
	products, _, err := s.ports.Catalog.ListProducts(ctx, "", 1, 200)
	if err == nil {
		for _, p := range products {
			if !p.PickEnabled {
				continue
			}
			count, err := s.ports.Catalog.CountAvailableByBinPrefix(ctx, p.ID, bin)
			if err != nil || count <= 0 {
				continue
			}
			sb.WriteString("• " + p.Title + "：" + fmt.Sprintf("%d", count) + "\n")
			total += count
		}
	}
	if total == 0 {
		return s.sendError(ctx, token, view.chatID, nil, "purchase.binstock_none")
	}
	sb.WriteString("\n" + localizedText(purchaseTexts["purchase.binstock_total"], loc) + "：" + fmt.Sprintf("%d", total))
	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", view.chatID), sb.String(),
		contract.SendMessageOptions{DisableWebPagePreview: true})
}

// renderOrders 返回用户订单列表（bot「我的订单」）。
func (s *purchaseService) renderOrders(ctx context.Context, token string, chatID int64, page int, from webhookdomain.User) error {
	if s.ports.OrderReader == nil {
		return s.sendError(ctx, token, chatID, nil, "purchase.unavailable")
	}
	user, err := s.resolveUser(ctx, from)
	if err != nil || user == nil {
		return s.sendError(ctx, token, chatID, err, "purchase.identity_failed")
	}
	loc := s.locale()
	if user.Locale != "" {
		loc = resolveLocale(user.Locale)
	}
	const pageSize = 10
	orders, total, err := s.ports.OrderReader.ListOrders(ctx, user.ID, page, pageSize)
	if err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	var sb strings.Builder
	sb.WriteString(localizedText(purchaseTexts["purchase.orders_title"], loc))
	if total > 0 {
		sb.WriteString(fmt.Sprintf("（%d）", total))
	}
	sb.WriteString("\n\n")
	if len(orders) == 0 {
		sb.WriteString(localizedText(purchaseTexts["purchase.orders_empty"], loc))
	}
	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), sb.String(),
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: s.orderListKeyboard(orders, page, int(total), pageSize, loc)})
}

// orderListKeyboard 订单列表键盘（每订单一个按钮；多页时附紧凑单行分页导航）。
func (s *purchaseService) orderListKeyboard(orders []contract.ShopOrder, page, total, pageSize int, loc string) inlineKeyboard {
	var rows [][]inlineButton
	for _, o := range orders {
		label := o.Title + "  " + formatAmount(o.TotalAmount, o.Currency) + " [" + orderStatusText(o.Status, loc) + "]"
		rows = append(rows, []inlineButton{{Text: label, CallbackData: cbOrderPrefix + o.OrderNo}})
	}
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages > 1 {
		prevData, nextData := "noop", "noop"
		if page > 1 {
			prevData = cbOrderPagePrefix + fmt.Sprintf("%d", page-1)
		}
		if page < totalPages {
			nextData = cbOrderPagePrefix + fmt.Sprintf("%d", page+1)
		}
		nav := []inlineButton{
			{Text: "⬅️", CallbackData: prevData},
			{Text: fmt.Sprintf("%d/%d", page, totalPages), CallbackData: "noop"},
			{Text: "➡️", CallbackData: nextData},
		}
		rows = append(rows, nav)
	}
	return inlineKeyboard{InlineKeyboard: rows}
}

// showOrderDetail 展示订单详情（含已发货卡密 payload）。
func (s *purchaseService) showOrderDetail(ctx context.Context, token string, chatID int64, orderNo string, from webhookdomain.User) error {
	if s.ports.OrderReader == nil {
		return s.sendError(ctx, token, chatID, nil, "purchase.unavailable")
	}
	user, err := s.resolveUser(ctx, from)
	if err != nil || user == nil {
		return s.sendError(ctx, token, chatID, err, "purchase.identity_failed")
	}
	loc := s.locale()
	if user.Locale != "" {
		loc = resolveLocale(user.Locale)
	}
	detail, err := s.ports.OrderReader.GetOrderByOrderNo(ctx, user.ID, orderNo)
	if err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	var sb strings.Builder
	sb.WriteString(localizedText(purchaseTexts["purchase.order_detail_title"], loc) + "\n\n")
	sb.WriteString("🆔 " + detail.OrderNo + "\n")
	sb.WriteString(localizedText(purchaseTexts["purchase.order_status"], loc) + "：" + orderStatusText(detail.Status, loc) + "\n")
	sb.WriteString(localizedText(purchaseTexts["purchase.order_time"], loc) + "：" + detail.CreatedAt + "\n")
	sb.WriteString(localizedText(purchaseTexts["purchase.total"], loc) + " " + formatAmount(detail.TotalAmount, detail.Currency) + "\n")
	for _, it := range detail.Items {
		sb.WriteString("• " + it.Title + " ×" + fmt.Sprintf("%d", it.Quantity) + "\n")
	}
	if detail.Fulfillment != nil && strings.TrimSpace(detail.Fulfillment.Payload) != "" {
		sb.WriteString("\n" + localizedText(purchaseTexts["purchase.order_cards"], loc) + "：\n")
		sb.WriteString(truncatePayload(detail.Fulfillment.Payload))
	} else {
		sb.WriteString("\n" + localizedText(purchaseTexts["purchase.order_no_cards"], loc))
	}
	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), sb.String(),
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: inlineKeyboard{InlineKeyboard: [][]inlineButton{
			{{Text: localizedText(purchaseTexts["purchase.orders_back"], loc), CallbackData: cbOrders}},
		}}})
}

// orderStatusText 订单状态本地化。
func orderStatusText(status, loc string) string {
	key := "order.status." + status
	if txt, ok := purchaseTexts[key]; ok {
		if v := localizedText(txt, loc); v != "" {
			return v
		}
	}
	return status
}

// truncatePayload 截断卡密 payload 到安全长度，避免超 Telegram 单条消息限制。
func truncatePayload(payload string) string {
	const maxLen = 3000
	if len(payload) <= maxLen {
		return payload
	}
	return payload[:maxLen] + "\n…（已截断）"
}

// truncateDescription 截断商品简介到安全长度。
func truncateDescription(desc string) string {
	const maxLen = 1000
	desc = strings.TrimSpace(desc)
	if len(desc) <= maxLen {
		return desc
	}
	return desc[:maxLen] + "\n…"
}

// sendEpusdtQR 把付款/充值说明文字作为 caption、收款地址二维码作为主图，合并为一条 sendPhoto 消息
// （二维码生成或图片发送失败时退化为纯文本消息，保证付款信息仍可见；不向上抛错避免 webhook 重试造成重复下单）。
func (s *purchaseService) sendEpusdtQR(ctx context.Context, token string, chatID int64, loc, address, caption string, rows [][]inlineButton) error {
	opts := contract.SendMessageOptions{DisableWebPagePreview: true, ParseMode: "Markdown", ReplyMarkup: inlineKeyboard{InlineKeyboard: rows}}
	chatIDStr := fmt.Sprintf("%d", chatID)
	pngBytes, err := buildQRCodePNG(address, 360)
	if err != nil {
		return s.botapi.SendMessage(ctx, token, chatIDStr, caption, opts)
	}
	if err := s.botapi.SendPhotoBytes(ctx, token, chatIDStr, "usdt_qrcode.png", pngBytes, caption, opts); err != nil {
		// 图片发送失败：退化为纯文本消息。
		return s.botapi.SendMessage(ctx, token, chatIDStr, caption, opts)
	}
	return nil
}

// buildQRCodePNG 用 boombuler/barcode 在进程内生成二维码 PNG 字节，不依赖外部二维码服务。
func buildQRCodePNG(content string, size int) ([]byte, error) {
	code, err := qr.Encode(strings.TrimSpace(content), qr.M, qr.Auto)
	if err != nil {
		return nil, err
	}
	code, err = barcode.Scale(code, size, size)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, code); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// toggleLanguage 在中/英之间切换，并持久化到商城账号。
// 切换后立即让用户看到效果：处于购买流程中以新语言重渲当前步骤，否则发送新语言的主菜单。
func (s *purchaseService) toggleLanguage(ctx context.Context, token string, chatID int64, from webhookdomain.User) error {
	if s.ports.Identity == nil {
		return s.sendError(ctx, token, chatID, nil, "purchase.unavailable")
	}
	user, err := s.resolveUser(ctx, from)
	if err != nil || user == nil {
		return s.sendError(ctx, token, chatID, err, "purchase.identity_failed")
	}
	current := user.Locale
	if current == "" {
		current = s.locale()
	}
	newLocale := "zh-CN"
	if strings.HasPrefix(resolveLocale(current), "en") {
		newLocale = "zh-CN"
	} else {
		newLocale = "en-US"
	}
	if err := s.ports.Identity.SetLocale(ctx, user.ID, newLocale); err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	// 若存在购买会话，同步刷新会话语言。
	s.mu.Lock()
	if sess := s.sessions[chatID]; sess != nil {
		sess.locale = newLocale
	}
	s.mu.Unlock()
	// 处于购买流程中：以新语言重渲当前步骤（也顺带刷新最新数据）。
	if s.snapshot(chatID) != nil {
		if err := s.renderCurrentStep(ctx, token, chatID); err == nil {
			return nil
		}
	}
	// 无会话或重渲染失败：直接发送新语言的主菜单，让界面与按钮立即切换。
	if s.sendMainMenu != nil {
		return s.sendMainMenu(ctx, token, fmt.Sprintf("%d", chatID), newLocale)
	}
	msg := localizedText(purchaseTexts["purchase.lang_switched"], newLocale)
	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), msg,
		contract.SendMessageOptions{DisableWebPagePreview: true})
}

// renderCurrentStep 按会话当前步骤以最新数据重渲（语言切换时复用，保证内容实时与网页一致）。
func (s *purchaseService) renderCurrentStep(ctx context.Context, token string, chatID int64) error {
	view := s.snapshot(chatID)
	if view == nil {
		return nil
	}
	switch view.step {
	case purchaseStepBrowseCategory:
		return s.renderCategories(ctx, token, chatID)
	case purchaseStepBrowseProduct:
		return s.renderProducts(ctx, token, chatID, view.page)
	case purchaseStepConfigure:
		return s.renderDetail(ctx, token, chatID)
	case purchaseStepPickMode:
		return s.renderPickMode(ctx, token, chatID)
	case purchaseStepPickCountry:
		return s.renderPickCountry(ctx, token, chatID, s.currentCountryPage(chatID))
	case purchaseStepCheckChoice:
		return s.renderCheckChoice(ctx, token, chatID)
	case purchaseStepQuantity:
		return s.sendQuantityPrompt(ctx, token, chatID)
	case purchaseStepPickBrand:
		return s.renderPickBrand(ctx, token, chatID)
	case purchaseStepPickCardType:
		return s.renderPickCardType(ctx, token, chatID)
	case purchaseStepPickChannel:
		if s.ports.Payments == nil {
			return nil
		}
		channels, err := s.ports.Payments.ListPaymentChannels(ctx)
		if err != nil {
			return err
		}
		epusdtChannels := filterEpusdtChannels(channels)
		msg := localizedText(purchaseTexts["purchase.select_channel_title"], view.locale)
		return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), msg,
			contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: s.channelKeyboard(epusdtChannels, view.locale)})
	case purchaseStepConfirm:
		return s.confirmOrder(ctx, token, chatID)
	}
	return nil
}

// enterRecharge 进入充值：解析用户 + 提示输入金额。
func (s *purchaseService) enterRecharge(ctx context.Context, token string, chatID int64, from *webhookdomain.User) error {
	if s.ports.Recharge == nil {
		return s.sendError(ctx, token, chatID, nil, "purchase.unavailable")
	}
	if from == nil {
		return s.sendError(ctx, token, chatID, nil, "purchase.identity_failed")
	}
	user, err := s.resolveUser(ctx, *from)
	if err != nil || user == nil {
		return s.sendError(ctx, token, chatID, err, "purchase.identity_failed")
	}
	loc := s.locale()
	if user.Locale != "" {
		loc = resolveLocale(user.Locale)
	}
	s.mu.Lock()
	if sess, ok := s.sessions[chatID]; ok {
		sess.userID = user.ID
		sess.locale = loc
		sess.step = purchaseStepRechargeAmount
	} else {
		s.sessions[chatID] = &purchaseSession{chatID: chatID, userID: user.ID, locale: loc, step: purchaseStepRechargeAmount, lastUpdatedAt: time.Now()}
	}
	s.mu.Unlock()
	msg := localizedText(purchaseTexts["purchase.recharge_title"], loc) + "\n\n" + localizedText(purchaseTexts["purchase.recharge_amount_prompt"], loc)
	markup := inlineKeyboard{InlineKeyboard: [][]inlineButton{{
		{Text: "❌ " + localizedText(purchaseTexts["purchase.cancel"], loc), CallbackData: cbCancel},
	}}}
	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), msg,
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: markup})
}

// handleRechargeAmount 处理充值金额输入：校验后查渠道并发起。
func (s *purchaseService) handleRechargeAmount(ctx context.Context, token string, view *purchaseView, text string, from *webhookdomain.User) error {
	loc := view.locale
	if from == nil {
		return s.sendError(ctx, token, view.chatID, nil, "purchase.identity_failed")
	}
	amount := strings.TrimSpace(text)
	dec, err := decimal.NewFromString(amount)
	if err != nil || dec.LessThanOrEqual(decimal.Zero) {
		return s.sendError(ctx, token, view.chatID, nil, "purchase.recharge_amount_invalid")
	}
	channels, err := s.ports.Payments.ListPaymentChannels(ctx)
	if err != nil {
		return s.sendError(ctx, token, view.chatID, err, "purchase.error")
	}
	epusdtChannels := filterEpusdtChannels(channels)
	if len(epusdtChannels) == 0 {
		return s.sendError(ctx, token, view.chatID, nil, "purchase.no_epusdt")
	}
	// 保存金额到会话。
	s.mu.Lock()
	if sess := s.sessions[view.chatID]; sess != nil {
		sess.rechargeAmount = amount
	}
	s.mu.Unlock()
	if len(epusdtChannels) == 1 {
		return s.createRechargeWithChannel(ctx, token, view.chatID, fmt.Sprintf("%d", epusdtChannels[0].ID), *from)
	}
	// 多渠道：展示渠道选择。
	s.mu.Lock()
	if sess := s.sessions[view.chatID]; sess != nil {
		sess.step = purchaseStepRechargeChannel
	}
	s.mu.Unlock()
	msg := localizedText(purchaseTexts["purchase.select_channel_title"], loc)
	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", view.chatID), msg,
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: s.rechargeChannelKeyboard(epusdtChannels, loc)})
}

// rechargeChannelKeyboard 充值渠道选择键盘。
func (s *purchaseService) rechargeChannelKeyboard(channels []contract.ShopPaymentChannel, loc string) inlineKeyboard {
	var rows [][]inlineButton
	for _, ch := range channels {
		rows = append(rows, []inlineButton{{Text: ch.Name, CallbackData: cbRechargeCh + fmt.Sprintf("%d", ch.ID)}})
	}
	rows = append(rows, []inlineButton{{Text: "🔙 " + localizedText(purchaseTexts["purchase.back"], loc), CallbackData: cbCancel}})
	return inlineKeyboard{InlineKeyboard: rows}
}

// createRechargeWithChannel 选定渠道后创建充值订单并展示支付链接。
func (s *purchaseService) createRechargeWithChannel(ctx context.Context, token string, chatID int64, channelIDStr string, from webhookdomain.User) error {
	if s.ports.Recharge == nil {
		return s.sendError(ctx, token, chatID, nil, "purchase.unavailable")
	}
	channelID, err := strconv.ParseUint(channelIDStr, 10, 64)
	if err != nil || channelID == 0 {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	user, err := s.resolveUser(ctx, from)
	if err != nil || user == nil {
		return s.sendError(ctx, token, chatID, err, "purchase.identity_failed")
	}
	loc := s.locale()
	if user.Locale != "" {
		loc = resolveLocale(user.Locale)
	}
	// 从会话取金额（多渠道流程时已存）。
	amount := ""
	if view := s.snapshot(chatID); view != nil {
		amount = view.rechargeAmount
	}
	if amount == "" {
		return s.sendError(ctx, token, chatID, nil, "purchase.recharge_amount_invalid")
	}
	currency := ""
	if s.ports.Settings != nil {
		if cur, e := s.ports.Settings.GetCurrency(ctx); e == nil {
			currency = cur
		}
	}
	recharge, err := s.ports.Recharge.CreateRecharge(ctx, contract.PurchaseRechargeInput{
		UserID:    user.ID,
		ChannelID: uint(channelID),
		Amount:    amount,
		Currency:  currency,
	})
	if err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	// epusdt 渠道：直接在聊天内展示充值 USDT 金额与收款地址（只保留 USDT 一行，避免币种混淆）。
	if recharge.ReceiveAddress != "" {
		var sb strings.Builder
		sb.WriteString("🪙 " + localizedText(purchaseTexts["purchase.recharge_title"], loc) + "\n\n")
		sb.WriteString("🆔 " + recharge.RechargeNo + "\n")
		if recharge.PayAmount != "" && recharge.PayAmount != "0" {
			token := strings.ToUpper(recharge.Token)
			if token == "" {
				token = "USDT"
			}
			sb.WriteString(localizedText(purchaseTexts["purchase.epusdt_pay_amount"], loc) + " " + formatCryptoAmount(recharge.PayAmount) + " " + token + "\n")
		}
		sb.WriteString("\n" + localizedText(purchaseTexts["purchase.epusdt_address"], loc) + "\n")
		sb.WriteString("```\n" + recharge.ReceiveAddress + "\n```\n")
		sb.WriteString(localizedText(purchaseTexts["purchase.epusdt_copy_hint"], loc) + "\n")
		network := strings.ToUpper(recharge.Network)
		if network == "TRON" {
			network = "TRON（TRC20）"
		}
		sb.WriteString("\n" + localizedText(purchaseTexts["purchase.epusdt_network"], loc) + " " + network + "\n")
		if recharge.ExpiresAt != "" {
			sb.WriteString(localizedText(purchaseTexts["purchase.epusdt_expires"], loc) + " " + recharge.ExpiresAt + "\n")
		}
		sb.WriteString("\n" + localizedText(purchaseTexts["purchase.recharge_epusdt_hint"], loc))
		sb.WriteString("\n" + localizedText(purchaseTexts["purchase.epusdt_qr_hint"], loc))

		// 展示付款页后立即清空会话：避免用户再次输入数字时重复创建充值订单，
		// 之后任意文本输入都会回退到主菜单（/start 页面）。
		s.mu.Lock()
		delete(s.sessions, chatID)
		s.mu.Unlock()

		rows := [][]inlineButton{}
		if recharge.PayURL != "" {
			rows = append(rows, []inlineButton{{Text: localizedText(purchaseTexts["purchase.pay_open_link"], loc), URL: recharge.PayURL}})
		}
		rows = append(rows, []inlineButton{{Text: localizedText(purchaseTexts["purchase.recharge_check"], loc), CallbackData: cbRechargeCheck + recharge.RechargeNo}})
		rows = append(rows, []inlineButton{{Text: localizedText(purchaseTexts["purchase.exit_home"], loc), CallbackData: cbMenu}})
		// 二维码作为主图、说明文字作为 caption、按钮挂在图片消息上 —— 一条消息完成充值信息展示。
		return s.sendEpusdtQR(ctx, token, chatID, loc, recharge.ReceiveAddress, sb.String(), rows)
	}

	var sb strings.Builder
	sb.WriteString(localizedText(purchaseTexts["purchase.recharge_created"], loc) + "\n\n")
	sb.WriteString("🆔 " + recharge.RechargeNo + "\n")
	sb.WriteString(localizedText(purchaseTexts["purchase.recharge_payable"], loc) + " " + formatAmount(recharge.PayableAmount, recharge.Currency) + "\n")
	if recharge.PayURL != "" {
		sb.WriteString("\n🔗 " + recharge.PayURL + "\n")
	}
	var markup interface{}
	if recharge.PayURL != "" {
		markup = inlineKeyboard{InlineKeyboard: [][]inlineButton{
			{
				{Text: localizedText(purchaseTexts["purchase.pay_open_link"], loc), URL: recharge.PayURL},
				{Text: localizedText(purchaseTexts["purchase.recharge_check"], loc), CallbackData: cbRechargeCheck + recharge.RechargeNo},
			},
		}}
	}
	// 非 epusdt 渠道同样在展示充值页后清空会话，防止重复充值。
	s.mu.Lock()
	delete(s.sessions, chatID)
	s.mu.Unlock()
	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), sb.String(),
		contract.SendMessageOptions{DisableWebPagePreview: true, ReplyMarkup: markup})
}

// checkRecharge 查询充值到账状态。
func (s *purchaseService) checkRecharge(ctx context.Context, token string, chatID int64, rechargeNo string, from webhookdomain.User) error {
	if s.ports.Recharge == nil {
		return s.sendError(ctx, token, chatID, nil, "purchase.unavailable")
	}
	user, err := s.resolveUser(ctx, from)
	if err != nil || user == nil {
		return s.sendError(ctx, token, chatID, err, "purchase.identity_failed")
	}
	loc := s.locale()
	if user.Locale != "" {
		loc = resolveLocale(user.Locale)
	}
	recharge, err := s.ports.Recharge.GetRechargeStatus(ctx, user.ID, rechargeNo)
	if err != nil {
		return s.sendError(ctx, token, chatID, err, "purchase.error")
	}
	statusText := localizedText(purchaseTexts["purchase.recharge_pending"], loc)
	switch recharge.Status {
	case "success":
		statusText = localizedText(purchaseTexts["purchase.recharge_paid"], loc)
	case "failed", "expired":
		statusText = localizedText(purchaseTexts["purchase.recharge_failed"], loc)
	}
	msg := localizedText(purchaseTexts["purchase.recharge_title"], loc) + "\n\n" +
		"🆔 " + recharge.RechargeNo + "\n" +
		localizedText(purchaseTexts["purchase.recharge_status"], loc) + "：" + statusText + "\n" +
		localizedText(purchaseTexts["purchase.recharge_payable"], loc) + " " + formatAmount(recharge.PayableAmount, recharge.Currency)
	return s.botapi.SendMessage(ctx, token, fmt.Sprintf("%d", chatID), msg,
		contract.SendMessageOptions{DisableWebPagePreview: true})
}