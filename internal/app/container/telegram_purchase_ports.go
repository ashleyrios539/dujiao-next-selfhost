package container

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	categoryapp "github.com/dujiao-next/internal/modules/catalog/category/application"
	productapp "github.com/dujiao-next/internal/modules/catalog/product/application"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	"github.com/dujiao-next/internal/modules/identity/userauth/application"
	orderapp "github.com/dujiao-next/internal/modules/order/application"
	paymentapp "github.com/dujiao-next/internal/modules/payment/application"
	settingsapp "github.com/dujiao-next/internal/modules/settings/application"
	"github.com/dujiao-next/internal/modules/telegram/webhook/contract"
	walletapp "github.com/dujiao-next/internal/modules/wallet/application"
	walletcontract "github.com/dujiao-next/internal/modules/wallet/contract"
	"github.com/dujiao-next/internal/shared/countries"
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
	}
	return out, nil
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
	return &contract.PurchaseUser{ID: user.ID, DisplayName: user.DisplayName}, nil
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
