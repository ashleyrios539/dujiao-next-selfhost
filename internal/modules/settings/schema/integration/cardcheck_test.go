package settingsintegration

import (
	"reflect"
	"testing"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

func TestCardCheckCodecPreservesDefaultsAndBounds(t *testing.T) {
	fallback := DefaultCardCheckConfig()
	if fallback.Enabled || fallback.Buffer != 5 || fallback.TimeoutSeconds != 60 || fallback.PollMillis != 2000 {
		t.Fatalf("card check default mismatch: %#v", fallback)
	}

	decoded := DecodeCardCheckConfig(jsonmap.JSON{
		constants.SettingFieldCardCheckEnabled:    true,
		constants.SettingFieldCardCheckKami:       " CheckDx-test-kami ",
		constants.SettingFieldCardCheckInterface:  "post5",
		constants.SettingFieldCardCheckBuffer:     float64(99),
		constants.SettingFieldCardCheckTimeout:    "360",
		constants.SettingFieldCardCheckPollMillis: 100,
	}, fallback)

	if !decoded.Enabled || decoded.Kami != "CheckDx-test-kami" {
		t.Fatalf("card check boolean/string decode mismatch: %#v", decoded)
	}
	if decoded.Interface != "post5" {
		t.Fatalf("card check interface decode mismatch: %#v", decoded)
	}
	if decoded.Buffer != 99 || decoded.TimeoutSeconds != 300 || decoded.PollMillis != 2000 {
		t.Fatalf("card check bounds mismatch: %#v", decoded)
	}

	encoded := EncodeCardCheckConfig(decoded)
	if encoded[constants.SettingFieldCardCheckTimeout] != 300 {
		t.Fatalf("card check encode mismatch: %#v", encoded)
	}

	roundtrip := DecodeCardCheckConfig(encoded, fallback)
	if !reflect.DeepEqual(roundtrip, decoded) {
		t.Fatalf("card check roundtrip mismatch\nwant: %#v\n got: %#v", decoded, roundtrip)
	}
}

func TestCardCheckJSONNormalizerRestoresDefaults(t *testing.T) {
	normalized := NormalizeCardCheckConfigJSON(jsonmap.JSON{
		constants.SettingFieldCardCheckBuffer:     -1,
		constants.SettingFieldCardCheckTimeout:    1,
	})
	if normalized[constants.SettingFieldCardCheckBuffer] != 5 {
		t.Fatalf("card check buffer normalizer mismatch: %#v", normalized)
	}
	if normalized[constants.SettingFieldCardCheckTimeout] != 60 {
		t.Fatalf("card check timeout normalizer mismatch: %#v", normalized)
	}
}
