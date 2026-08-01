package settingsapp

import (
	"sync"
	"time"

	"github.com/dujiao-next/internal/constants"
	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"
)

var callbackRoutesCache struct {
	mu      sync.RWMutex
	routes  *settingsintegration.CallbackRoutesSetting
	loaded  bool
	expires time.Time
}

const callbackRoutesCacheTTL = 5 * time.Minute

// InvalidateCallbackRoutesCache clears the callback route cache after an
// administrator changes its backing setting.
func (s *Service) InvalidateCallbackRoutesCache() {
	callbackRoutesCache.mu.Lock()
	callbackRoutesCache.loaded = false
	callbackRoutesCache.routes = nil
	callbackRoutesCache.expires = time.Time{}
	callbackRoutesCache.mu.Unlock()
}

// GetCallbackRoutesCached returns custom callback routes from the bounded
// settings cache, loading them from the store on a miss. The load happens
// WITHOUT holding the global write lock: a blocking DB read while holding it
// would wedge every request that needs the cache (they take the read lock first).
func (s *Service) GetCallbackRoutesCached() *settingsintegration.CallbackRoutesSetting {
	callbackRoutesCache.mu.RLock()
	routes := callbackRoutesCache.routes
	loaded := callbackRoutesCache.loaded
	expires := callbackRoutesCache.expires
	callbackRoutesCache.mu.RUnlock()
	if loaded && time.Now().Before(expires) {
		return routes
	}

	routes = s.GetCallbackRoutes()

	callbackRoutesCache.mu.Lock()
	defer callbackRoutesCache.mu.Unlock()
	if callbackRoutesCache.loaded && time.Now().Before(callbackRoutesCache.expires) {
		return callbackRoutesCache.routes
	}
	callbackRoutesCache.routes = routes
	callbackRoutesCache.loaded = true
	callbackRoutesCache.expires = time.Now().Add(callbackRoutesCacheTTL)
	return routes
}

// GetCallbackRoutes returns configured custom callback routes, or nil when no
// custom route is active.
func (s *Service) GetCallbackRoutes() *settingsintegration.CallbackRoutesSetting {
	if s == nil {
		return nil
	}
	value, err := s.GetByKey(constants.SettingKeyCallbackRoutesConfig)
	if err != nil || value == nil {
		return nil
	}
	setting := settingsintegration.DecodeCallbackRoutesSetting(value)
	if !setting.HasCustomRoutes() {
		return nil
	}
	return &setting
}
