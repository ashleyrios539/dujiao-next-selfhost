package contract

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/dujiao-next/internal/config"
)

type tenantContextKey struct{}

// TenantContext 表示主站或分销站请求上下文。
type TenantContext struct {
	Host              string
	IsMain            bool
	ResellerID        *uint
	ResellerUserID    uint
	PrimaryDomain     string
	Unavailable       bool
	UnavailableReason string
	// WholesalePurchase 标记由主站「代理中心」发起的批发采购上下文。
	// 子站关闭（sub_sites_enabled=false）模式下，分销商以主站身份按渠道价批量下单。
	WholesalePurchase bool
}

// IsReseller 判断当前上下文是否为可用的分销站请求。
func (tenant TenantContext) IsReseller() bool {
	return tenant.ResellerID != nil && !tenant.IsMain && !tenant.Unavailable
}

// IsWholesalePurchase 判断当前上下文是否为主站代理中心批发采购。
func (tenant TenantContext) IsWholesalePurchase() bool {
	return tenant.ResellerID != nil && tenant.WholesalePurchase && !tenant.Unavailable
}

// HasResellerPricing 判断当前上下文是否需要走分销计价（子站零售或主站批发采购）。
func (tenant TenantContext) HasResellerPricing() bool {
	return tenant.IsReseller() || tenant.IsWholesalePurchase()
}

// MainTenantContext 构造主站租户上下文。
func MainTenantContext(host string) TenantContext {
	return TenantContext{Host: NormalizeHost(host), IsMain: true}
}

// ResellerTenantContext 构造可用的分销站租户上下文。
func ResellerTenantContext(host string, resellerID uint, resellerUserID uint, primaryDomain string) TenantContext {
	id := resellerID
	return TenantContext{
		Host:           NormalizeHost(host),
		IsMain:         false,
		ResellerID:     &id,
		ResellerUserID: resellerUserID,
		PrimaryDomain:  NormalizeHost(primaryDomain),
	}
}

// ResellerPurchaseContext 构造主站代理中心批发采购租户上下文（不解析子域名）。
func ResellerPurchaseContext(host string, resellerID uint, resellerUserID uint) TenantContext {
	id := resellerID
	return TenantContext{
		Host:              NormalizeHost(host),
		IsMain:            true,
		ResellerID:        &id,
		ResellerUserID:    resellerUserID,
		WholesalePurchase: true,
	}
}

// UnavailableTenantContext 构造不可用租户上下文（未知域名或未激活等）。
func UnavailableTenantContext(host string, reason string) TenantContext {
	return TenantContext{
		Host:              NormalizeHost(host),
		Unavailable:       true,
		UnavailableReason: strings.TrimSpace(reason),
	}
}

// WithTenantContext 将租户上下文写入 context。
func WithTenantContext(ctx context.Context, tenant TenantContext) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tenant)
}

// TenantFromContext 从 context 读取租户上下文。
func TenantFromContext(ctx context.Context) (TenantContext, bool) {
	if ctx == nil {
		return TenantContext{}, false
	}
	tenant, ok := ctx.Value(tenantContextKey{}).(TenantContext)
	return tenant, ok
}

// NormalizeHost 归一化请求 Host（小写、去端口、去尾点）。
func NormalizeHost(raw string) string {
	host := strings.TrimSpace(strings.ToLower(raw))
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, "[") {
		if parsed, _, err := net.SplitHostPort(host); err == nil {
			host = strings.Trim(parsed, "[]")
		}
	} else if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(host, ".")
	host = strings.Trim(host, "[]")
	return host
}

// ResolveRequestHost 按配置从 HTTP 请求解析 Host。
func ResolveRequestHost(req *http.Request, cfg config.ResellerConfig) string {
	if req == nil {
		return ""
	}
	raw := req.Host
	if cfg.TrustedForwardedHost {
		if forwarded := strings.TrimSpace(req.Header.Get("X-Forwarded-Host")); forwarded != "" {
			raw = strings.Split(forwarded, ",")[0]
		}
	}
	return NormalizeHost(raw)
}
