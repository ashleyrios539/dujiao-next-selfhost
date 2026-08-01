package application

import (
	"context"
	"errors"
	"strconv"
	"strings"

	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"

	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

// ProductSettingService 分销商品改价/上架配置用例。
type ProductSettingService struct {
	store       resellercontract.ProductSettingStore
	productRepo productcontract.Repository
}

func NewProductSettingService(store resellercontract.ProductSettingStore, productRepo productcontract.Repository) *ProductSettingService {
	return &ProductSettingService{store: store, productRepo: productRepo}
}

type ProductSettingInput struct {
	SKUID              uint
	IsListed           bool
	PricingMode        string
	MarkupPercent      decimal.Decimal
	FixedMarkupAmount  decimal.Decimal
	FixedPriceAmount   decimal.Decimal
	ChannelPriceAmount decimal.Decimal
	SortOrder          int
}

type ProductSettingSaveInput struct {
	Settings []ProductSettingInput
}

type ProductSettingUserListInput struct {
	Page       int
	PageSize   int
	Keyword    string
	CategoryID uint
	Configured string
	Listed     string
}

type ProductSettingAdminListInput struct {
	Page        int
	PageSize    int
	ResellerID  uint
	UserID      uint
	ProductID   uint
	Keyword     string
	PricingMode string
	Configured  string
	Listed      string
}

type ProductSettingDetail struct {
	Profile          *resellerdomain.Profile
	Product          productdomain.Product
	Settings         []resellerdomain.ProductSetting
	EffectiveBySKUID map[uint]decimal.Decimal
	RuleBySKUID      map[uint]string
}

type ProductSettingListRow struct {
	Profile          *resellerdomain.Profile
	Product          productdomain.Product
	Settings         []resellerdomain.ProductSetting
	EffectiveBySKUID map[uint]decimal.Decimal
	RuleBySKUID      map[uint]string
}

// ProductSettingPreviewItem 表示某个商品级（SKUID=0）或 SKU 级规则在「拟用配置」下的预览结果。
// 复用与保存/下单完全一致的 ResolveUnitAmount + ValidateUnitAmount，确保预览价与实际成交价零分歧。
type ProductSettingPreviewItem struct {
	SKUID          uint
	IsListed       bool
	BasePrice      decimal.Decimal
	EffectivePrice decimal.Decimal
	Valid          bool
	ErrorCode      string
}

func (s *ProductSettingService) ListUserProductSettings(userID uint, input ProductSettingUserListInput) ([]ProductSettingListRow, int64, error) {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return nil, 0, err
	}
	rows, total, err := s.store.ListProductsWithSettings(resellercontract.ProductSettingListFilter{
		Page:       input.Page,
		PageSize:   input.PageSize,
		ResellerID: profile.ID,
		CategoryID: input.CategoryID,
		Keyword:    input.Keyword,
		Configured: input.Configured,
		Listed:     input.Listed,
		OnlyActive: true,
	})
	if err != nil {
		return nil, 0, err
	}
	return s.decorateRows(profile, rows), total, nil
}

func (s *ProductSettingService) GetUserProductSetting(userID, productID uint) (*ProductSettingDetail, error) {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return nil, err
	}
	return s.getDetail(profile, productID)
}

// PreviewUserProductSettings 在不落库的前提下，按用户拟用的定价规则计算各 SKU 的预计生效价与校验结果。
func (s *ProductSettingService) PreviewUserProductSettings(userID, productID uint, input ProductSettingSaveInput) ([]ProductSettingPreviewItem, error) {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return nil, err
	}
	return s.previewSettings(profile, productID, input)
}

func (s *ProductSettingService) SaveUserProductSettings(userID, productID uint, input ProductSettingSaveInput) (*ProductSettingDetail, error) {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return nil, err
	}
	if err := s.saveSettings(profile, productID, input); err != nil {
		return nil, err
	}
	return s.getDetail(profile, productID)
}

func (s *ProductSettingService) ResetUserProductSetting(userID, productID, skuID uint) error {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return err
	}
	return s.store.DeleteSetting(profile.ID, productID, skuID)
}

func (s *ProductSettingService) ListAdminSettings(input ProductSettingAdminListInput) ([]resellerdomain.ProductSetting, int64, error) {
	return s.store.ListAdminSettings(resellercontract.ProductSettingAdminListFilter(input))
}

func (s *ProductSettingService) SummarizeAdminSettings(resellerID uint) (resellercontract.ProductSettingSummary, error) {
	if s == nil || s.store == nil || resellerID == 0 {
		return resellercontract.ProductSettingSummary{}, nil
	}
	return s.store.SummarizeByResellerID(resellerID)
}

func (s *ProductSettingService) GetAdminProductSetting(resellerID, productID uint) (*ProductSettingDetail, error) {
	profile, err := s.requireActiveProfileByID(resellerID)
	if err != nil {
		return nil, err
	}
	return s.getDetail(profile, productID)
}

// PreviewAdminProductSettings 在不落库的前提下，按管理员拟用的定价规则计算各 SKU 的预计生效价与校验结果。
func (s *ProductSettingService) PreviewAdminProductSettings(resellerID, productID uint, input ProductSettingSaveInput) ([]ProductSettingPreviewItem, error) {
	profile, err := s.requireActiveProfileByID(resellerID)
	if err != nil {
		return nil, err
	}
	return s.previewSettings(profile, productID, input)
}

func (s *ProductSettingService) SaveAdminProductSettings(resellerID, productID uint, input ProductSettingSaveInput) (*ProductSettingDetail, error) {
	profile, err := s.requireActiveProfileByID(resellerID)
	if err != nil {
		return nil, err
	}
	if err := s.saveSettings(profile, productID, input); err != nil {
		return nil, err
	}
	return s.getDetail(profile, productID)
}

func (s *ProductSettingService) ResetAdminProductSetting(resellerID, productID, skuID uint) error {
	profile, err := s.requireActiveProfileByID(resellerID)
	if err != nil {
		return err
	}
	return s.store.DeleteSetting(profile.ID, productID, skuID)
}

func (s *ProductSettingService) requireActiveProfileByUser(userID uint) (*resellerdomain.Profile, error) {
	if s == nil || s.store == nil || s.productRepo == nil || userID == 0 {
		return nil, productcontract.ErrNotFound
	}
	profile, err := s.store.GetProfileByUserID(userID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, resellercontract.ErrNotOpened
	}
	if profile.Status != resellerdomain.ProfileStatusActive {
		return nil, resellercontract.ErrProfileInactive
	}
	return profile, nil
}

func (s *ProductSettingService) requireActiveProfileByID(resellerID uint) (*resellerdomain.Profile, error) {
	if s == nil || s.store == nil || s.productRepo == nil || resellerID == 0 {
		return nil, productcontract.ErrNotFound
	}
	profile, err := s.store.GetProfileByID(resellerID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, productcontract.ErrNotFound
	}
	if profile.Status != resellerdomain.ProfileStatusActive {
		return nil, resellercontract.ErrProfileInactive
	}
	return profile, nil
}

func (s *ProductSettingService) getDetail(profile *resellerdomain.Profile, productID uint) (*ProductSettingDetail, error) {
	if profile == nil || productID == 0 {
		return nil, productcontract.ErrNotFound
	}
	row, err := s.store.GetProductWithSettings(profile.ID, productID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, productcontract.ErrNotFound
	}
	effective, rules, err := computeProductEffectivePrices(profile, row.Product, row.Settings)
	if err != nil {
		return nil, err
	}
	return &ProductSettingDetail{Profile: profile, Product: row.Product, Settings: row.Settings, EffectiveBySKUID: effective, RuleBySKUID: rules}, nil
}

func (s *ProductSettingService) decorateRows(profile *resellerdomain.Profile, rows []resellercontract.ProductSettingProductRow) []ProductSettingListRow {
	out := make([]ProductSettingListRow, 0, len(rows))
	for _, row := range rows {
		effective, rules, _ := computeProductEffectivePrices(profile, row.Product, row.Settings)
		out = append(out, ProductSettingListRow{Profile: profile, Product: row.Product, Settings: row.Settings, EffectiveBySKUID: effective, RuleBySKUID: rules})
	}
	return out
}

func (s *ProductSettingService) saveSettings(profile *resellerdomain.Profile, productID uint, input ProductSettingSaveInput) error {
	if profile == nil || productID == 0 {
		return productcontract.ErrNotFound
	}
	product, err := s.productRepo.GetAdminByID(strconv.FormatUint(uint64(productID), 10))
	if err != nil {
		return err
	}
	if product == nil {
		return productcontract.ErrNotFound
	}
	normalizedSettings := make([]resellerdomain.ProductSetting, 0, len(input.Settings))
	for _, item := range input.Settings {
		normalized, err := normalizeProductSettingInput(profile, product, item)
		if err != nil {
			return err
		}
		normalized.ResellerID = profile.ID
		normalized.ProductID = product.ID
		normalizedSettings = append(normalizedSettings, normalized)
	}
	return s.store.WithinProductSettingTransaction(func(store resellercontract.ProductSettingStore) error {
		for _, setting := range normalizedSettings {
			if _, err := store.UpsertSetting(setting); err != nil {
				return err
			}
		}
		return nil
	})
}

func normalizeProductSettingInput(profile *resellerdomain.Profile, product *productdomain.Product, input ProductSettingInput) (resellerdomain.ProductSetting, error) {
	if product == nil {
		return resellerdomain.ProductSetting{}, productcontract.ErrNotFound
	}
	mode := strings.TrimSpace(input.PricingMode)
	if mode == "" {
		mode = resellerdomain.PricingModeInherit
	}
	switch mode {
	case resellerdomain.PricingModeInherit, resellerdomain.PricingModeMarkupPercent, resellerdomain.PricingModeFixedMarkup, resellerdomain.PricingModeFixedPrice, resellerdomain.PricingModeChannelPrice:
	default:
		return resellerdomain.ProductSetting{}, resellercontract.ErrPricingModeInvalid
	}
	setting := resellerdomain.ProductSetting{
		SKUID:              input.SKUID,
		IsListed:           input.IsListed,
		PricingMode:        mode,
		MarkupPercent:      money.FromDecimal(input.MarkupPercent.Round(2)),
		FixedMarkupAmount:  money.FromDecimal(input.FixedMarkupAmount.Round(2)),
		FixedPriceAmount:   money.FromDecimal(input.FixedPriceAmount.Round(2)),
		ChannelPriceAmount: money.FromDecimal(input.ChannelPriceAmount.Round(2)),
		SortOrder:          input.SortOrder,
	}
	if !setting.IsListed {
		return setting, nil
	}
	if input.SKUID > 0 {
		sku := findProductSKU(product.SKUs, input.SKUID)
		if sku == nil || !sku.IsActive {
			return resellerdomain.ProductSetting{}, productcontract.ErrProductSKUInvalid
		}
		price, rule, err := ResolveUnitAmount(profile, nil, &setting, sku.PriceAmount.Decimal.Round(2))
		if err != nil {
			return resellerdomain.ProductSetting{}, err
		}
		if err := ValidateResolvedUnitAmount(profile, sku, sku.PriceAmount.Decimal.Round(2), price, rule); err != nil {
			return resellerdomain.ProductSetting{}, err
		}
		return setting, nil
	}
	if len(product.SKUs) == 0 {
		basePrice := product.PriceAmount.Decimal.Round(2)
		price, rule, err := ResolveUnitAmount(profile, &setting, nil, basePrice)
		if err != nil {
			return resellerdomain.ProductSetting{}, err
		}
		if err := ValidateResolvedUnitAmount(profile, nil, basePrice, price, rule); err != nil {
			return resellerdomain.ProductSetting{}, err
		}
		costPrice := product.CostPriceAmount.Decimal.Round(2)
		if costPrice.GreaterThan(decimal.Zero) && price.LessThan(costPrice) {
			return resellerdomain.ProductSetting{}, resellercontract.ErrPriceBelowBase
		}
		return setting, nil
	}
	for i := range product.SKUs {
		sku := &product.SKUs[i]
		if !sku.IsActive {
			continue
		}
		price, rule, err := ResolveUnitAmount(profile, &setting, nil, sku.PriceAmount.Decimal.Round(2))
		if err != nil {
			return resellerdomain.ProductSetting{}, err
		}
		if err := ValidateResolvedUnitAmount(profile, sku, sku.PriceAmount.Decimal.Round(2), price, rule); err != nil {
			return resellerdomain.ProductSetting{}, err
		}
	}
	return setting, nil
}

func (s *ProductSettingService) previewSettings(profile *resellerdomain.Profile, productID uint, input ProductSettingSaveInput) ([]ProductSettingPreviewItem, error) {
	if profile == nil || productID == 0 {
		return nil, productcontract.ErrNotFound
	}
	row, err := s.store.GetProductWithSettings(profile.ID, productID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, productcontract.ErrNotFound
	}
	product := row.Product

	var productSetting *resellerdomain.ProductSetting
	skuSettings := map[uint]*resellerdomain.ProductSetting{}
	for _, item := range input.Settings {
		setting, err := buildPreviewSetting(item)
		if err != nil {
			return nil, err
		}
		setting.ResellerID = profile.ID
		setting.ProductID = product.ID
		if item.SKUID == 0 {
			ps := setting
			productSetting = &ps
		} else {
			ss := setting
			skuSettings[item.SKUID] = &ss
		}
	}

	items := make([]ProductSettingPreviewItem, 0, len(product.SKUs)+1)

	productBase := product.PriceAmount.Decimal.Round(2)
	productItem := ProductSettingPreviewItem{
		SKUID:     0,
		IsListed:  productSetting == nil || productSetting.IsListed,
		BasePrice: productBase,
		Valid:     true,
	}
	if productSetting != nil && productSetting.IsListed {
		price, rule, _ := ResolveUnitAmount(profile, productSetting, nil, productBase)
		productItem.EffectivePrice = price.Round(2)
		if len(product.SKUs) == 0 {
			perr := ValidateResolvedUnitAmount(profile, nil, productBase, price, rule)
			if perr == nil {
				cost := product.CostPriceAmount.Decimal.Round(2)
				if cost.GreaterThan(decimal.Zero) && price.LessThan(cost) {
					perr = resellercontract.ErrPriceBelowBase
				}
			}
			productItem.Valid = perr == nil
			productItem.ErrorCode = previewErrorCode(perr)
		} else {
			productItem.Valid, productItem.ErrorCode = previewValidateProductRuleAcrossSKUs(profile, product, productSetting)
		}
	}
	items = append(items, productItem)

	for i := range product.SKUs {
		sku := &product.SKUs[i]
		if !sku.IsActive {
			continue
		}
		skuSetting := skuSettings[sku.ID]
		base := sku.PriceAmount.Decimal.Round(2)
		item := ProductSettingPreviewItem{SKUID: sku.ID, IsListed: true, BasePrice: base, Valid: true}
		if (productSetting != nil && !productSetting.IsListed) || (skuSetting != nil && !skuSetting.IsListed) {
			item.IsListed = false
			items = append(items, item)
			continue
		}
		price, rule, perr := ResolveUnitAmount(profile, productSetting, skuSetting, base)
		if perr == nil {
			perr = ValidateResolvedUnitAmount(profile, sku, base, price, rule)
		}
		item.EffectivePrice = price.Round(2)
		item.Valid = perr == nil
		item.ErrorCode = previewErrorCode(perr)
		items = append(items, item)
	}
	return items, nil
}

func buildPreviewSetting(input ProductSettingInput) (resellerdomain.ProductSetting, error) {
	mode := strings.TrimSpace(input.PricingMode)
	if mode == "" {
		mode = resellerdomain.PricingModeInherit
	}
	switch mode {
	case resellerdomain.PricingModeInherit, resellerdomain.PricingModeMarkupPercent, resellerdomain.PricingModeFixedMarkup, resellerdomain.PricingModeFixedPrice, resellerdomain.PricingModeChannelPrice:
	default:
		return resellerdomain.ProductSetting{}, resellercontract.ErrPricingModeInvalid
	}
	return resellerdomain.ProductSetting{
		SKUID:              input.SKUID,
		IsListed:           input.IsListed,
		PricingMode:        mode,
		MarkupPercent:      money.FromDecimal(input.MarkupPercent.Round(2)),
		FixedMarkupAmount:  money.FromDecimal(input.FixedMarkupAmount.Round(2)),
		FixedPriceAmount:   money.FromDecimal(input.FixedPriceAmount.Round(2)),
		ChannelPriceAmount: money.FromDecimal(input.ChannelPriceAmount.Round(2)),
		SortOrder:          input.SortOrder,
	}, nil
}

func previewValidateProductRuleAcrossSKUs(profile *resellerdomain.Profile, product productdomain.Product, productSetting *resellerdomain.ProductSetting) (bool, string) {
	for i := range product.SKUs {
		sku := &product.SKUs[i]
		if !sku.IsActive {
			continue
		}
		base := sku.PriceAmount.Decimal.Round(2)
		price, rule, err := ResolveUnitAmount(profile, productSetting, nil, base)
		if err == nil {
			err = ValidateResolvedUnitAmount(profile, sku, base, price, rule)
		}
		if err != nil {
			return false, previewErrorCode(err)
		}
	}
	return true, ""
}

func previewErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, resellercontract.ErrMarkupExceeded):
		return "markup_exceeded"
	default:
		return "price_invalid"
	}
}

func computeProductEffectivePrices(profile *resellerdomain.Profile, product productdomain.Product, settings []resellerdomain.ProductSetting) (map[uint]decimal.Decimal, map[uint]string, error) {
	effective := map[uint]decimal.Decimal{}
	rules := map[uint]string{}
	byProduct, bySKU := indexProductSettings(settings)
	productSetting := byProduct[product.ID]
	if productSetting != nil && productSetting.IsListed {
		price, rule, err := ResolveUnitAmount(profile, productSetting, nil, product.PriceAmount.Decimal.Round(2))
		if err != nil {
			return effective, rules, err
		}
		effective[0] = price.Round(2)
		rules[0] = rule.Source
	}
	for i := range product.SKUs {
		sku := &product.SKUs[i]
		if !sku.IsActive {
			continue
		}
		skuSetting := bySKU[productSettingKey{productID: product.ID, skuID: sku.ID}]
		if productSetting != nil && !productSetting.IsListed {
			continue
		}
		if skuSetting != nil && !skuSetting.IsListed {
			continue
		}
		price, rule, err := ResolveUnitAmount(profile, productSetting, skuSetting, sku.PriceAmount.Decimal.Round(2))
		if err != nil {
			return effective, rules, err
		}
		effective[sku.ID] = price.Round(2)
		rules[sku.ID] = rule.Source
	}
	return effective, rules, nil
}

type productSettingKey struct {
	productID uint
	skuID     uint
}

func indexProductSettings(settings []resellerdomain.ProductSetting) (map[uint]*resellerdomain.ProductSetting, map[productSettingKey]*resellerdomain.ProductSetting) {
	byProduct := make(map[uint]*resellerdomain.ProductSetting)
	bySKU := make(map[productSettingKey]*resellerdomain.ProductSetting)
	for i := range settings {
		setting := settings[i]
		row := setting
		if setting.SKUID == 0 {
			byProduct[setting.ProductID] = &row
			continue
		}
		bySKU[productSettingKey{productID: setting.ProductID, skuID: setting.SKUID}] = &row
	}
	return byProduct, bySKU
}

func findProductSKU(items []productdomain.ProductSKU, skuID uint) *productdomain.ProductSKU {
	for i := range items {
		if items[i].ID == skuID {
			return &items[i]
		}
	}
	return nil
}

// PurchaseProfileStore 批发采购查询分销商资料所需的最小持久化接口。
type PurchaseProfileStore interface {
	GetProfileByUserID(userID uint) (*resellerdomain.Profile, error)
}

// PurchaseService 主站代理中心渠道价批发采购用例。
// 子站关闭（sub_sites_enabled=false）模式下，分销商以主站身份按渠道价批量下单。
type PurchaseService struct {
	store   PurchaseProfileStore
	catalog *ProductSettingService
	gateway resellercontract.WholesaleOrderGateway
}

func NewPurchaseService(store PurchaseProfileStore, catalog *ProductSettingService, gateway resellercontract.WholesaleOrderGateway) *PurchaseService {
	return &PurchaseService{store: store, catalog: catalog, gateway: gateway}
}

// ListCatalog 返回当前分销商可批发采购的商品列表（含按分销规则解析的渠道价）。
func (s *PurchaseService) ListCatalog(userID uint, input ProductSettingUserListInput) ([]ProductSettingListRow, int64, error) {
	if s == nil || s.catalog == nil || userID == 0 {
		return nil, 0, productcontract.ErrNotFound
	}
	return s.catalog.ListUserProductSettings(userID, input)
}

// Preview 按渠道价预览批发采购订单金额（不落库）。
func (s *PurchaseService) Preview(userID uint, host, clientIP string, items []resellercontract.WholesaleItemInput) (*resellercontract.WholesalePreview, error) {
	profile, err := s.requireActiveProfile(userID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, resellercontract.ErrNotOpened
	}
	if s.gateway == nil {
		return nil, resellercontract.ErrAccountingUnavailable
	}
	return s.gateway.Preview(context.Background(), resellercontract.WholesaleOrderInput{
		UserID:   userID,
		Tenant:   resellercontract.ResellerPurchaseContext(host, profile.ID, profile.UserID),
		Items:    items,
		ClientIP: clientIP,
	})
}

// Create 按渠道价创建批发采购订单（买家为分销商本人，走现有支付流程）。
func (s *PurchaseService) Create(userID uint, host, clientIP string, items []resellercontract.WholesaleItemInput) (*resellercontract.WholesaleCreated, error) {
	profile, err := s.requireActiveProfile(userID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, resellercontract.ErrNotOpened
	}
	if s.gateway == nil {
		return nil, resellercontract.ErrAccountingUnavailable
	}
	return s.gateway.Create(context.Background(), resellercontract.WholesaleOrderInput{
		UserID:   userID,
		Tenant:   resellercontract.ResellerPurchaseContext(host, profile.ID, profile.UserID),
		Items:    items,
		ClientIP: clientIP,
	})
}

func (s *PurchaseService) requireActiveProfile(userID uint) (*resellerdomain.Profile, error) {
	if s == nil || s.store == nil || userID == 0 {
		return nil, productcontract.ErrNotFound
	}
	profile, err := s.store.GetProfileByUserID(userID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, resellercontract.ErrNotOpened
	}
	if profile.Status != resellerdomain.ProfileStatusActive {
		return nil, resellercontract.ErrProfileInactive
	}
	return profile, nil
}
