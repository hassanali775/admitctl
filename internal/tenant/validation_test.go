package tenant

import (
	"errors"
	"strings"
	"testing"
)

func validConfig() Config {
	return Config{
		ID:                "acme-corp",
		DisplayName:       "Acme Corporation",
		Auth:              AuthAPIKey,
		DataSchemaVersion: "v1",
		RateLimit:         RateLimit{RequestsPerSecond: 100, Burst: 200},
		FeatureFlags:      map[string]bool{"beta_dashboard": true},
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("expected valid config to pass, got: %v", err)
	}
}

func TestValidate_FieldErrors(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(c Config) Config
		wantField string
	}{
		{
			name:      "empty id",
			mutate:    func(c Config) Config { c.ID = ""; return c },
			wantField: "id",
		},
		{
			name:      "id with uppercase",
			mutate:    func(c Config) Config { c.ID = "AcmeCorp"; return c },
			wantField: "id",
		},
		{
			name:      "id too short",
			mutate:    func(c Config) Config { c.ID = "ab"; return c },
			wantField: "id",
		},
		{
			name:      "id starting with hyphen",
			mutate:    func(c Config) Config { c.ID = "-acme"; return c },
			wantField: "id",
		},
		{
			name:      "empty display name",
			mutate:    func(c Config) Config { c.DisplayName = "   "; return c },
			wantField: "display_name",
		},
		{
			name:      "display name too long",
			mutate:    func(c Config) Config { c.DisplayName = strings.Repeat("a", 129); return c },
			wantField: "display_name",
		},
		{
			name:      "invalid auth method",
			mutate:    func(c Config) Config { c.Auth = AuthMethod("basic"); return c },
			wantField: "auth",
		},
		{
			name:      "empty schema version",
			mutate:    func(c Config) Config { c.DataSchemaVersion = ""; return c },
			wantField: "data_schema_version",
		},
		{
			name:      "malformed schema version",
			mutate:    func(c Config) Config { c.DataSchemaVersion = "version-1"; return c },
			wantField: "data_schema_version",
		},
		{
			name:      "zero requests per second",
			mutate:    func(c Config) Config { c.RateLimit.RequestsPerSecond = 0; return c },
			wantField: "rate_limit.requests_per_second",
		},
		{
			name:      "negative requests per second",
			mutate:    func(c Config) Config { c.RateLimit.RequestsPerSecond = -5; return c },
			wantField: "rate_limit.requests_per_second",
		},
		{
			name: "burst below requests per second",
			mutate: func(c Config) Config {
				c.RateLimit.RequestsPerSecond = 100
				c.RateLimit.Burst = 50
				return c
			},
			wantField: "rate_limit.burst",
		},
		{
			name:      "empty feature flag name",
			mutate:    func(c Config) Config { c.FeatureFlags = map[string]bool{"": true}; return c },
			wantField: "feature_flags",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.mutate(validConfig())
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected validation error for field %q, got nil", tt.wantField)
			}
			var verrs ValidationErrors
			if !errors.As(err, &verrs) {
				t.Fatalf("expected error to be ValidationErrors, got %T", err)
			}
			found := false
			for _, fe := range verrs {
				if fe.Field == tt.wantField {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected an error on field %q, got errors: %v", tt.wantField, verrs)
			}
		})
	}
}

func TestValidate_MultipleErrorsAggregated(t *testing.T) {
	cfg := Config{} // everything unset
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for empty config, got nil")
	}
	var verrs ValidationErrors
	if !errors.As(err, &verrs) {
		t.Fatalf("expected error to be ValidationErrors, got %T", err)
	}
	// Empty config should fail id, display_name, auth, data_schema_version,
	// and rate_limit.requests_per_second at minimum.
	if len(verrs) < 5 {
		t.Fatalf("expected at least 5 aggregated errors for empty config, got %d: %v", len(verrs), verrs)
	}
}
