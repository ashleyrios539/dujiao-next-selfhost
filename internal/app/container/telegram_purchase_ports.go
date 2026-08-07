package container

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	categoryapp "github.com/dujiao-next/internal/modules/catalog/category/application"
	productapp "github.com/dujiao-next/internal/modules/catalog/product/application"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	"github.com/dujiao-next/internal/modules/identity/userauth/application"
	orderapp "github.com/dujiao-next/internal/modules/order/application"
	ordercontract "github.com/dujiao-next/internal/modules/order/contract"
	paymentapp "github.com/dujiao-next/internal/modules/payment/application"
	paymentcontract "github.com/dujiao-next/internal/modules/payment/contract"
	reseller "github.com/dujiao-next/internal/modules/reseller/contract"
	settingsapp "github.com/dujiao-next/internal/modules/settings/application"
	"github.com/dujiao-next/internal/modules/telegram/webhook/contract"
	walletapp "github.com/dujiao-next/internal/modules/wallet/application"
	walletcontract "github.com/dujiao-next/internal/modules/wallet/contract"
	"github.com/dujiao-next/internal/shared/countries"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

// telegramPurchasePorts 把业务模块具体服务适配为 bot 内购买所需的窄端口。
// 模式与 internal/app/container/reseller_order_gateway.go 一致。
type telegramPurchasePorts struct {
	products *productapp.Service
	cats     *categoryapp.Service
	orders   *orderapp.OrderService
	payments *paymentapp.PaymentService
	wallet   *walletapp.Service
	auth     *application.Service
	settings *settingsapp.Service
	locale   string
}

// localeValue 从多语言 json 取指定 locale 文案，缺失回退。
func (p *telegramPurchasePorts) localeValue(m map[string]interface{}) string {
	if len(m) == 0 {
		return ""
	}
	if v, ok := m[p.locale]; ok {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	for _, lang := range []string{"zh-CN", "zh-TW", "en-US"} {
		if v, ok := m[lang]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

// --- PurchaseCatalogReader ---

func (p *telegramPurchasePorts) ListActiveCategories(_ context.Context) ([]contract.ShopCategory, error) {
	if p.cats == nil {
		return nil, nil
	}
	categories, err := p.cats.ListActive()
	if err != nil {
		return nil, err
	}
	out := make([]contract.ShopCategory, 0, len(categories))
	for _, cat := range categories {
		name := p.localeValue(cat.NameJSON)
		if name == "" {
			name = cat.Slug
		}
		out = append(out, contract.ShopCategory{
			ID:   cat.ID,
			Slug: cat.Slug,
			Name: name,
			Icon: cat.Icon,
		})
	}
	return out, nil
}

// ListProducts 返回 bot 商品列表（bot_visible=true，可含仅 bot 商品），并填充可发库存。
func (p *telegramPurchasePorts) ListProducts(ctx context.Context, categoryID string, page, pageSize int) ([]contract.ShopProduct, int64, error) {
	if p.products == nil {
		return nil, 0, nil
	}
	products, total, err := p.products.ListPublicForBot(categoryID, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	_ = p.products.ApplyAutoStockCounts(products)
	out := make([]contract.ShopProduct, 0, len(products))
	for i := range products {
		out = append(out, p.toShopProduct(&products[i]))
	}
	return out, total, nil
}

func (p *telegramPurchasePorts) GetProductBySlug(_ context.Context, slug string) (*contract.ShopProduct, error) {
	if p.products == nil {
		return nil, fmt.Errorf("product service unavailable")
	}
	product, err := p.products.GetPublicBySlugForBot(slug)
	if err != nil {
		return nil, err
	}
	item := p.toShopProduct(product)
	return &item, nil
}

func (p *telegramPurchasePorts) CountPickAttrs(_ context.Context, productID uint) ([]contract.PickAttrCount, error) {
	if p.products == nil {
		return nil, nil
	}
	attrs, err := p.products.CountPickAttrs(productID)
	if err != nil {
		return nil, err
	}
	out := make([]contract.PickAttrCount, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, contract.PickAttrCount{
			ProductID: a.ProductID,
			SKUID:     a.SKUID,
			Country:   a.Country,
			Brand:     a.Brand,
			CardType:  a.CardType,
			Total:     a.Total,
		})
	}
	return out, nil
}

func (p *telegramPurchasePorts) CountAvailableByBinPrefix(_ context.Context, productID uint, bin string) (int64, error) {
	if p.products == nil {
		return 0, nil
	}
	return p.products.CountAvailableByBinPrefix(productID, bin)
}

// CountByBinHead 按卡号首位聚合商品可用库存（bot 首位挑卡 3头/4头/5头/6头 展示）。
func (p *telegramPurchasePorts) CountByBinHead(_ context.Context, productID uint) ([]contract.ShopBinHead, error) {
	if p.products == nil {
		return nil, nil
	}
	heads, err := p.products.CountByBinHead(productID)
	if err != nil {
		return nil, err
	}
	out := make([]contract.ShopBinHead, 0, len(heads))
	for _, h := range heads {
		out = append(out, contract.ShopBinHead{Head: h.Head, Stock: h.Total})
	}
	return out, nil
}

// CountAvailableByBinHead 统计商品下卡号以指定首位开头的可用卡密数量（首位挑卡库存）。
func (p *telegramPurchasePorts) CountAvailableByBinHead(_ context.Context, productID uint, head string) (int64, error) {
	if p.products == nil {
		return 0, nil
	}
	return p.products.CountAvailableByBinPrefix(productID, head)
}

// GetPickStock 聚合挑卡可选维度：国家按可用库存降序，品牌/卡类型为固定选项。
func (p *telegramPurchasePorts) GetPickStock(ctx context.Context, productID uint) (*contract.ShopPickStock, error) {
	attrs, err := p.CountPickAttrs(ctx, productID)
	if err != nil {
		return nil, err
	}
	stock := &contract.ShopPickStock{
		Brands: []contract.ShopPickBrand{
			{Key: "random", Name: "随机"},
			{Key: "visa", Name: "Visa"},
			{Key: "mastercard", Name: "Mastercard"},
			{Key: "discover", Name: "Discover"},
			{Key: "amex", Name: "AMEX"},
			{Key: "jcb", Name: "JCB"},
		},
		CardTypes: []contract.ShopPickCardType{
			{Key: "random", Name: "随机"},
			{Key: "D", Name: "D卡（含预付）"},
			{Key: "PD", Name: "纯D（不含预付）"},
			{Key: "C", Name: "纯C"},
		},
	}

	// 按国家聚合库存（全部 SKU 求和），降序排序。
	countryStock := map[string]int64{}
	for _, a := range attrs {
		if a.Country == "" {
			continue
		}
		countryStock[a.Country] += a.Total
	}
	for code, total := range countryStock {
		stock.Countries = append(stock.Countries, contract.ShopPickCountry{
			Code:  code,
			Name:  countries.ChineseName(code),
			Stock: total,
		})
	}
	// 降序排列
	sort.Slice(stock.Countries, func(i, j int) bool {
		return stock.Countries[i].Stock > stock.Countries[j].Stock
	})
	return stock, nil
}

func (p *telegramPurchasePorts) toShopProduct(product *productdomain.Product) contract.ShopProduct {
	skus := make([]contract.ShopSKU, 0, len(product.SKUs))
	for i := range product.SKUs {
		skus = append(skus, contract.ShopSKU{
			ID:          product.SKUs[i].ID,
			Code:        product.SKUs[i].SKUCode,
			PriceAmount: product.SKUs[i].PriceAmount.String(),
			IsActive:    product.SKUs[i].IsActive,
		})
	}
	pickPrices := map[string]string{}
	if len(product.PickPrices) > 0 {
		for k, v := range product.PickPrices {
			pickPrices[k] = fmt.Sprint(v)
		}
	}
	// 可发库存：auto 用自动库存可用量；manual 用剩余库存（-1=无限）。
	var stockAvailable int64
	switch product.FulfillmentType {
	case "auto":
		stockAvailable = product.AutoStockAvailable
	case "manual":
		stockAvailable = int64(product.ManualStockTotal)
	}
	return contract.ShopProduct{
		ID:               product.ID,
		Slug:             product.Slug,
		Title:            p.localeValue(product.TitleJSON),
		Description:      p.localeValue(product.DescriptionJSON),
		Currency:         p.currency(),
		PriceAmount:      product.PriceAmount.String(),
		FulfillmentType:  product.FulfillmentType,
		CardCheckEnabled: product.CardCheckEnabled,
		CardCheckFee:     product.CardCheckFee.String(),
		PickEnabled:      product.PickEnabled,
		PickPrices:       pickPrices,
		StockAvailable:   stockAvailable,
		SKUs:             skus,
	}
}

func (p *telegramPurchasePorts) currency() string {
	if p.settings != nil {
		if cur, err := p.settings.GetSiteCurrency(""); err == nil && cur != "" {
			return cur
		}
	}
	return ""
}

// translateOrderError 把订单/支付模块的内部错误归类为 bot 契约层的分类哨兵，
// 使 bot 端可以据此展示本地化文案而不泄露内部细节；无法归类时原样返回。
func translateOrderError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, orderapp.ErrCardSecretInsufficient):
		return contract.ErrOrderStockInsufficient
	case errors.Is(err, walletcontract.ErrInsufficientBalance):
		return contract.ErrOrderInsufficient
	}
	return err
}

// --- PurchaseOrderGateway ---

func (p *telegramPurchasePorts) Preview(ctx context.Context, input contract.PurchasePreviewInput) (*contract.PurchasePreview, error) {
	if p.orders == nil {
		return nil, fmt.Errorf("order service unavailable")
	}
	preview, err := p.orders.PreviewOrder(orderapp.CreateOrderInput{
		UserID: input.UserID,
		Items:  mapPurchaseItems(input.Items),
		// Bot 渠道无真实客户端 IP，跳过 IP 维度风控。
		SkipIPRiskControl: true,
	})
	if err != nil {
		return nil, translateOrderError(err)
	}
	out := &contract.PurchasePreview{
		Currency:       preview.Currency,
		OriginalAmount: preview.OriginalAmount.String(),
		DiscountAmount: preview.DiscountAmount.String(),
		TotalAmount:    preview.TotalAmount.String(),
	}
	for _, item := range preview.Items {
		out.Items = append(out.Items, contract.PurchasePreviewItem{
			ProductID:        item.ProductID,
			SKUID:            item.SKUID,
			Title:            p.localeValue(item.TitleJSON),
			Quantity:         item.Quantity,
			UnitPrice:        item.UnitPrice.String(),
			TotalPrice:       item.TotalPrice.String(),
			CardCheckEnabled: item.CardCheckEnabled,
			PickCountry:      item.PickCountry,
			PickBrands:       []string(item.PickBrands),
			PickCardTypes:    []string(item.PickCardTypes),
			PickBin:          item.PickBin,
		})
	}
	return out, nil
}

func (p *telegramPurchasePorts) Create(ctx context.Context, input contract.PurchaseCreateInput) (*contract.PurchaseCreated, error) {
	if p.orders == nil {
		return nil, fmt.Errorf("order service unavailable")
	}
	order, err := p.orders.CreateOrder(orderapp.CreateOrderInput{
		UserID: input.UserID,
		Items:  mapPurchaseItems(input.Items),
		// Bot 渠道无真实客户端 IP，跳过 IP 维度风控。
		SkipIPRiskControl: true,
	})
	if err != nil {
		return nil, translateOrderError(err)
	}
	return &contract.PurchaseCreated{
		OrderID:     order.ID,
		OrderNo:     order.OrderNo,
		Currency:    order.Currency,
		TotalAmount: order.TotalAmount.String(),
	}, nil
}

func mapPurchaseItems(items []contract.PurchaseItem) []orderapp.CreateOrderItem {
	out := make([]orderapp.CreateOrderItem, 0, len(items))
	for _, item := range items {
		out = append(out, orderapp.CreateOrderItem{
			ProductID:        item.ProductID,
			SKUID:            item.SKUID,
			Quantity:         item.Quantity,
			FulfillmentType:  item.FulfillmentType,
			CardCheckEnabled: item.CardCheckEnabled,
			PickCountry:      item.PickCountry,
			PickBrands:       item.PickBrands,
			PickCardTypes:    item.PickCardTypes,
			PickBin:          item.PickBin,
		})
	}
	return out
}

// --- PurchasePaymentGateway ---

// --- PurchasePaymentGateway ---

// ListPaymentChannels 返回活跃的在线支付渠道（bot 在线支付选择用）。
func (p *telegramPurchasePorts) ListPaymentChannels(ctx context.Context) ([]contract.ShopPaymentChannel, error) {
	if p.payments == nil {
		return nil, nil
	}
	channels, _, err := p.payments.ListChannels(paymentcontract.ChannelListFilter{
		Page:       1,
		PageSize:   100,
		ActiveOnly: true,
	})
	if err != nil {
		return nil, err
	}
	out := make([]contract.ShopPaymentChannel, 0, len(channels))
	for _, ch := range channels {
		out = append(out, contract.ShopPaymentChannel{
			ID:          ch.ID,
			Name:        ch.Name,
			ChannelType: ch.ChannelType,
		})
	}
	return out, nil
}

func (p *telegramPurchasePorts) CreatePayment(ctx context.Context, input contract.PurchasePaymentInput) (*contract.PurchasePaymentResult, error) {
	if p.payments == nil {
		return nil, fmt.Errorf("payment service unavailable")
	}
	result, err := p.payments.CreatePayment(paymentapp.CreatePaymentInput{
		OrderID:    input.OrderID,
		ChannelID:  input.ChannelID,
		UseBalance: input.UseBalance,
		Context:    ctx,
	})
	if err != nil {
		return nil, translateOrderError(err)
	}
	out := &contract.PurchasePaymentResult{
		OrderPaid:        result.OrderPaid,
		WalletPaidAmount: result.WalletPaidAmount.String(),
		OnlinePayAmount:  result.OnlinePayAmount.String(),
	}
	if result.Payment != nil {
		out.PayURL = result.Payment.PayURL
		out.QRCode = result.Payment.QRCode
		out.ProviderType = result.Payment.ProviderType
		out.ChannelType = result.Payment.ChannelType
		out.InteractionMode = result.Payment.InteractionMode
		fillEpusdtPaymentInfo(result.Payment.ProviderPayload, out)
	}
	return out, nil
}

// fillEpusdtPaymentInfo 从支付记录 ProviderPayload 提取 epusdt 付款关键字段，
// 供 bot 在聊天内直接展示应付 USDT 金额与收款地址。非 epusdt 渠道无这些字段，静默跳过。
func fillEpusdtPaymentInfo(payload map[string]interface{}, out *contract.PurchasePaymentResult) {
	if payload == nil || out == nil {
		return
	}
	if v := payloadString(payload, "receive_address"); v != "" {
		out.ReceiveAddress = v
	}
	if v := payloadString(payload, "actual_amount"); v != "" {
		out.PayAmount = v
	}
	out.Token = payloadString(payload, "token")
	out.Network = payloadString(payload, "network")
	if exp, ok := payloadInt64(payload, "expiration_time"); ok && exp > 0 {
		out.ExpiresAt = time.Unix(exp, 0).Format("2006-01-02 15:04")
	}
}

// payloadString 从任意 payload map 读取字符串字段，兼容 float64/int 的 fmt.Sprint。
func payloadString(payload map[string]interface{}, key string) string {
	if payload == nil {
		return ""
	}
	v, ok := payload[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

// payloadInt64 从任意 payload map 读取 int64 字段（兼容 float64 / string）。
func payloadInt64(payload map[string]interface{}, key string) (int64, bool) {
	if payload == nil {
		return 0, false
	}
	v, ok := payload[key]
	if !ok || v == nil {
		return 0, false
	}
	switch val := v.(type) {
	case int64:
		return val, true
	case int:
		return int64(val), true
	case float64:
		return int64(val), true
	case string:
		if i, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64); err == nil {
			return i, true
		}
	}
	return 0, false
}

// --- PurchaseWalletReader ---

func (p *telegramPurchasePorts) GetBalance(_ context.Context, userID uint) (string, error) {
	if p.wallet == nil {
		return "0", nil
	}
	account, err := p.wallet.GetAccount(userID)
	if err != nil {
		return "0", err
	}
	return account.Balance.String(), nil
}

// --- PurchaseIdentityResolver ---

func (p *telegramPurchasePorts) ResolveOrProvision(_ context.Context, channelUserID, username, firstName, lastName string) (*contract.PurchaseUser, error) {
	if p.auth == nil {
		return nil, fmt.Errorf("identity service unavailable")
	}
	user, _, _, err := p.auth.ProvisionTelegramChannelIdentity(application.TelegramChannelIdentityInput{
		ChannelUserID: channelUserID,
		Username:      username,
		FirstName:     firstName,
		LastName:      lastName,
	})
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("identity resolution failed")
	}
	return &contract.PurchaseUser{ID: user.ID, DisplayName: user.DisplayName, Locale: user.Locale}, nil
}

// SetLocale 持久化商城账号语言偏好（bot 内切换语言用）。
func (p *telegramPurchasePorts) SetLocale(_ context.Context, userID uint, locale string) error {
	if p.auth == nil {
		return fmt.Errorf("identity service unavailable")
	}
	_, err := p.auth.UpdateProfile(userID, nil, &locale)
	return err
}

// --- PurchaseOrderReader ---

// ListOrders 返回用户订单列表（bot「我的订单」）。
func (p *telegramPurchasePorts) ListOrders(ctx context.Context, userID uint, page, pageSize int) ([]contract.ShopOrder, int64, error) {
	if p.orders == nil {
		return nil, 0, nil
	}
	orders, total, err := p.orders.ListOrdersByUserForTenant(reseller.MainTenantContext(""), ordercontract.ListFilter{
		Page:     page,
		PageSize: pageSize,
		UserID:   userID,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]contract.ShopOrder, 0, len(orders))
	for i := range orders {
		title := ""
		if len(orders[i].Items) > 0 {
			title = p.localeValue(orders[i].Items[0].TitleJSON)
		}
		out = append(out, contract.ShopOrder{
			OrderNo:     orders[i].OrderNo,
			Status:      orders[i].Status,
			Currency:    orders[i].Currency,
			TotalAmount: orders[i].TotalAmount.String(),
			CreatedAt:   orders[i].CreatedAt.Format("2006-01-02 15:04"),
			Title:       title,
		})
	}
	return out, total, nil
}

// GetOrderByOrderNo 返回订单详情（含已发货卡密 payload）。
func (p *telegramPurchasePorts) GetOrderByOrderNo(ctx context.Context, userID uint, orderNo string) (*contract.ShopOrderDetail, error) {
	if p.orders == nil {
		return nil, fmt.Errorf("order service unavailable")
	}
	order, err := p.orders.GetOrderByUserOrderNoForTenant(reseller.MainTenantContext(""), orderNo, userID)
	if err != nil {
		return nil, err
	}
	detail := &contract.ShopOrderDetail{
		OrderNo:     order.OrderNo,
		Status:      order.Status,
		Currency:    order.Currency,
		TotalAmount: order.TotalAmount.String(),
		CreatedAt:   order.CreatedAt.Format("2006-01-02 15:04"),
	}
	for i := range order.Items {
		detail.Items = append(detail.Items, contract.ShopOrderItem{
			Title:     p.localeValue(order.Items[i].TitleJSON),
			Quantity:  order.Items[i].Quantity,
			UnitPrice: order.Items[i].UnitPrice.String(),
		})
	}
	if order.Fulfillment != nil {
		detail.Fulfillment = &contract.ShopFulfillment{
			Type:    order.Fulfillment.Type,
			Status:  order.Fulfillment.Status,
			Payload: order.Fulfillment.Payload,
		}
	}
	return detail, nil
}

// --- PurchaseRechargeGateway ---

// CreateRecharge 创建钱包充值订单并返回支付链接（复用网页端充值流程）。
func (p *telegramPurchasePorts) CreateRecharge(ctx context.Context, input contract.PurchaseRechargeInput) (*contract.ShopRecharge, error) {
	if p.payments == nil {
		return nil, fmt.Errorf("payment service unavailable")
	}
	amount, err := decimal.NewFromString(strings.TrimSpace(input.Amount))
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("invalid recharge amount")
	}
	result, err := p.payments.CreateWalletRechargePayment(paymentapp.CreateWalletRechargePaymentInput{
		UserID:    input.UserID,
		ChannelID: input.ChannelID,
		Amount:    money.FromDecimal(amount.Round(2)),
		Currency:  input.Currency,
		Context:   ctx,
	})
	if err != nil {
		return nil, translateOrderError(err)
	}
	recharge := result.Recharge
	if recharge == nil {
		return nil, fmt.Errorf("recharge order creation failed")
	}
	out := &contract.ShopRecharge{
		RechargeNo:    recharge.RechargeNo,
		Amount:        recharge.Amount.String(),
		PayableAmount: recharge.PayableAmount.String(),
		Currency:      recharge.Currency,
		Status:        recharge.Status,
	}
	if result.Payment != nil {
		out.PayURL = result.Payment.PayURL
		out.QRCode = result.Payment.QRCode
		out.ProviderType = result.Payment.ProviderType
		out.ChannelType = result.Payment.ChannelType
		out.InteractionMode = result.Payment.InteractionMode
		fillEpusdtRechargeInfo(result.Payment.ProviderPayload, out)
	}
	return out, nil
}

// fillEpusdtRechargeInfo 从充值支付记录 ProviderPayload 提取 epusdt 收款字段。
func fillEpusdtRechargeInfo(payload map[string]interface{}, out *contract.ShopRecharge) {
	if payload == nil || out == nil {
		return
	}
	if v := payloadString(payload, "receive_address"); v != "" {
		out.ReceiveAddress = v
	}
	if v := payloadString(payload, "actual_amount"); v != "" {
		out.PayAmount = v
	}
	out.Token = payloadString(payload, "token")
	out.Network = payloadString(payload, "network")
	if exp, ok := payloadInt64(payload, "expiration_time"); ok && exp > 0 {
		out.ExpiresAt = time.Unix(exp, 0).Format("2006-01-02 15:04")
	}
}

// GetRechargeStatus 查询充值订单状态（bot 内展示到账结果）。
func (p *telegramPurchasePorts) GetRechargeStatus(_ context.Context, userID uint, rechargeNo string) (*contract.ShopRecharge, error) {
	if p.wallet == nil {
		return nil, fmt.Errorf("wallet service unavailable")
	}
	recharge, err := p.wallet.GetRechargeOrderByRechargeNo(userID, rechargeNo)
	if err != nil {
		return nil, err
	}
	if recharge == nil {
		return nil, fmt.Errorf("recharge order not found")
	}
	return &contract.ShopRecharge{
		RechargeNo:    recharge.RechargeNo,
		Amount:        recharge.Amount.String(),
		PayableAmount: recharge.PayableAmount.String(),
		Currency:      recharge.Currency,
		Status:        recharge.Status,
	}, nil
}

// --- PurchaseSettingReader ---

func (p *telegramPurchasePorts) GetCurrency(_ context.Context) (string, error) {
	if p.settings == nil {
		return "", nil
	}
	return p.settings.GetSiteCurrency("")
}

func (p *telegramPurchasePorts) GetSiteName(_ context.Context) (string, error) {
	if p.settings == nil {
		return "", nil
	}
	brand, err := p.settings.GetSiteBrand()
	if err != nil {
		return "", err
	}
	return brand.SiteName, nil
}

// 断言实现契约。
var _ contract.PurchaseCatalogReader = (*telegramPurchasePorts)(nil)
var _ contract.PurchaseOrderGateway = (*telegramPurchasePorts)(nil)
var _ contract.PurchasePaymentGateway = (*telegramPurchasePorts)(nil)
var _ contract.PurchaseWalletReader = (*telegramPurchasePorts)(nil)
var _ contract.PurchaseIdentityResolver = (*telegramPurchasePorts)(nil)
var _ contract.PurchaseSettingReader = (*telegramPurchasePorts)(nil)
