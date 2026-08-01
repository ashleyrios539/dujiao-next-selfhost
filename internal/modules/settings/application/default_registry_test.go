package settingsapp

import (
	"reflect"
	"testing"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

func TestDefaultSettingRegistryCoversLegacyNormalizedKeys(t *testing.T) {
	want := []string{
		constants.SettingKeyAffiliateConfig,
		constants.SettingKeyCallbackRoutesConfig,
		constants.SettingKeyCardBinConfig,
		constants.SettingKeyCardCheckConfig,
		constants.SettingKeyDashboardConfig,
		constants.SettingKeyGoogleAuthConfig,
		constants.SettingKeyHomeAnnouncement,
		constants.SettingKeyNavConfig,
		constants.SettingKeyNotificationCenterConfig,
		constants.SettingKeyOrderConfig,
		constants.SettingKeyOrderRiskControlConfig,
		constants.SettingKeyRegistrationConfig,
		constants.SettingKeySiteConfig,
		constants.SettingKeyTelegramAuthConfig,
		constants.SettingKeyTelegramBotConfig,
		constants.SettingKeyUpstreamSyncConfig,
		constants.SettingKeyWalletConfig,
	}

	if got := defaultSettingRegistry.Keys(); !reflect.DeepEqual(got, want) {
		t.Fatalf("default registry keys mismatch\nwant: %#v\n got: %#v", want, got)
	}
}

func TestSettingServiceUpdateWithEffectsDescribesCacheImpact(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		value      map[string]interface{}
		wantEffect Effect
	}{
		{
			name:       "google auth config invalidates public cache",
			key:        constants.SettingKeyGoogleAuthConfig,
			value:      map[string]interface{}{"enabled": true, "client_id": "client.apps.googleusercontent.com"},
			wantEffect: EffectInvalidatePublicConfigCache,
		},
		{
			name:       "site config invalidates public cache",
			key:        constants.SettingKeySiteConfig,
			value:      map[string]interface{}{},
			wantEffect: EffectInvalidatePublicConfigCache,
		},
		{
			name:       "wallet config is effect only",
			key:        constants.SettingKeyWalletConfig,
			value:      map[string]interface{}{constants.SettingFieldWalletOnlyPayment: true},
			wantEffect: EffectInvalidatePublicConfigCache,
		},
		{
			name:       "callback routes invalidate route cache",
			key:        constants.SettingKeyCallbackRoutesConfig,
			value:      map[string]interface{}{constants.SettingFieldPaymentCallback: "/api/custom/callback"},
			wantEffect: EffectInvalidateCallbackRoutesCache,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newMockSettingRepo()
			service := NewService(repo)
			result, err := service.UpdateWithEffects(test.key, test.value)
			if err != nil {
				t.Fatalf("update setting: %v", err)
			}
			if !result.HasEffect(test.wantEffect) {
				t.Fatalf("expected effect %q, got %#v", test.wantEffect, result.Effects)
			}
		})
	}

	repo := newMockSettingRepo()
	service := NewService(repo)
	result, err := service.UpdateWithEffects("custom_extension_config", map[string]interface{}{"enabled": true})
	if err != nil {
		t.Fatalf("update unknown setting: %v", err)
	}
	if len(result.Effects) != 0 {
		t.Fatalf("unknown setting emitted effects: %#v", result.Effects)
	}
}

func TestSettingServiceUpdateKeepsUnknownKeyPassThroughBehavior(t *testing.T) {
	repo := newMockSettingRepo()
	service := NewService(repo)
	input := map[string]interface{}{
		"nested": map[string]interface{}{"enabled": true},
		"count":  float64(3),
	}

	got, err := service.Update("custom_extension_config", input)
	if err != nil {
		t.Fatalf("update unknown setting: %v", err)
	}
	if !reflect.DeepEqual(got, jsonmap.JSON(input)) {
		t.Fatalf("unknown setting changed during pass-through: %#v", got)
	}
	if saved := repo.store["custom_extension_config"]; !reflect.DeepEqual(saved, jsonmap.JSON(input)) {
		t.Fatalf("unknown setting persisted with a different shape: %#v", saved)
	}
}
