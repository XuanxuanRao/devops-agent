package config

import "testing"

func TestSelectedAuthTokenPrefersDeviceToken(t *testing.T) {
	cfg := Config{
		Auth: AuthConfig{
			Token:       "auth-token",
			DeviceToken: "device-token",
		},
	}

	if got := cfg.SelectedAuthToken(); got != "device-token" {
		t.Fatalf("SelectedAuthToken() = %q, want %q", got, "device-token")
	}
}

func TestUpdateDeviceTokenIgnoresBlankAndNilReceiver(t *testing.T) {
	cfg := &Config{
		Auth: AuthConfig{
			DeviceToken: "existing-token",
		},
	}

	cfg.UpdateDeviceToken("   ")
	if got := cfg.Auth.DeviceToken; got != "existing-token" {
		t.Fatalf("DeviceToken after blank update = %q, want %q", got, "existing-token")
	}

	var nilCfg *Config
	nilCfg.UpdateDeviceToken("new-token")
}
