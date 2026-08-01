package integrationtest

import (
	"fmt"
	"sync/atomic"

	"github.com/dujiao-next/internal/config"
	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	resellermodule "github.com/dujiao-next/internal/modules/reseller/application"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	resellergormstore "github.com/dujiao-next/internal/modules/reseller/infrastructure/gormstore"
)

// testDBSeq 提供进程中唯一的共享内存库名称后缀。
// 共享内存库（mode=memory&cache=shared）按整条 DSN 命名复用，若并发用例拿到相同的
// time.Now().UnixNano()（Windows 低精度时钟）会碰撞到同一条内存库，导致种子数据串库、偶发 UNIQUE 冲突。
var testDBSeq int64

// uniqueInMemoryDSN 为测试构造进程内唯一的共享内存 DSN。
func uniqueInMemoryDSN(prefix string) string {
	return fmt.Sprintf("file:%s_%d?mode=memory&cache=shared", prefix, atomic.AddInt64(&testDBSeq, 1))
}

// These aliases keep the integration scenarios concise while ensuring every
// service under test is constructed from the bounded reseller module.
type (
	ResellerManagementService           = resellermodule.ManagementService
	ResellerApplyInput                  = resellermodule.ResellerApplyInput
	ResellerApproveInput                = resellermodule.ResellerApproveInput
	ResellerProfileUpdateInput          = resellermodule.ResellerProfileUpdateInput
	ResellerSystemDomainInput           = resellermodule.ResellerSystemDomainInput
	ResellerProductSettingService       = resellermodule.ProductSettingService
	ResellerProductSettingInput         = resellermodule.ProductSettingInput
	ResellerProductSettingSaveInput     = resellermodule.ProductSettingSaveInput
	ResellerProductSettingUserListInput = resellermodule.ProductSettingUserListInput
	ResellerProductSettingPreviewItem   = resellermodule.ProductSettingPreviewItem
	ResellerSiteConfigInput             = resellermodule.ResellerSiteConfigInput
	ResellerAnnouncementInput           = resellermodule.ResellerAnnouncementInput
	ResellerSupportInput                = resellermodule.ResellerSupportInput
	ResellerSEOInput                    = resellermodule.ResellerSEOInput
	LocalizedTextInput                  = resellermodule.LocalizedTextInput
	ResellerSiteConfigFieldError        = resellermodule.ResellerSiteConfigFieldError
	ResellerOrderListInput              = resellercontract.OrderListInput
)

const (
	ResellerProfitStatusCredited    = resellercontract.ProfitStatusCredited
	ResellerProfitStatusPending     = resellercontract.ProfitStatusPending
	ResellerProfitStatusUnavailable = resellercontract.ProfitStatusUnavailable
)

var (
	ErrResellerApplyDisabled        = resellercontract.ErrApplyDisabled
	ErrResellerProfileInactive      = resellercontract.ErrProfileInactive
	ErrResellerProfileStatusInvalid = resellercontract.ErrProfileStatusInvalid
	ErrResellerSiteConfigInvalid    = resellercontract.ErrSiteConfigInvalid
	ErrResellerPriceBelowBase       = resellercontract.ErrPriceBelowBase
	ErrResellerMarkupExceeded       = resellercontract.ErrMarkupExceeded
	ResellerTenantContext           = resellercontract.ResellerTenantContext
)

func NewResellerManagementService(store *resellergormstore.Store, cfg config.ResellerConfig) *resellermodule.ManagementService {
	return resellermodule.NewManagementService(store, cfg)
}

func NewResellerProductSettingService(
	settingStore *resellergormstore.Store,
	_ *resellergormstore.Store,
	productRepo productcontract.Repository,
) *resellermodule.ProductSettingService {
	return resellermodule.NewProductSettingService(settingStore, productRepo)
}

func NewResellerSiteConfigService(store *resellergormstore.Store) *resellermodule.SiteConfigService {
	return resellermodule.NewSiteConfigService(store)
}

func NewResellerOrderService(store *resellergormstore.Store) *resellermodule.OrderQueryService {
	return resellermodule.NewOrderQueryService(store)
}
