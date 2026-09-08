package config

import "testing"

func TestAuthJWKS(t *testing.T) {
	tests := []struct {
		name string
		auth Auth
		want string
	}{
		{
			name: "derived from environment id",
			auth: Auth{DynamicEnvironmentID: "env-123", DynamicAPIBase: "https://app.dynamicauth.com"},
			want: "https://app.dynamicauth.com/api/v0/environments/env-123/keys",
		},
		{
			name: "trailing slash on the base is not doubled",
			auth: Auth{DynamicEnvironmentID: "env-123", DynamicAPIBase: "https://app.dynamicauth.com/"},
			want: "https://app.dynamicauth.com/api/v0/environments/env-123/keys",
		},
		{
			name: "explicit URL wins over the derived one",
			auth: Auth{DynamicEnvironmentID: "env-123", DynamicJWKSURL: "https://example.test/keys"},
			want: "https://example.test/keys",
		},
		{
			name: "unconfigured yields empty, which disables auth",
			auth: Auth{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.auth.JWKS(); got != tt.want {
				t.Errorf("JWKS() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStorageResolveEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		storage Storage
		want    string
	}{
		{
			name:    "derived from the R2 account id",
			storage: Storage{AccountID: "abc123"},
			want:    "https://abc123.r2.cloudflarestorage.com",
		},
		{
			name:    "explicit endpoint wins and is trimmed",
			storage: Storage{AccountID: "abc123", Endpoint: "https://custom.example.test/"},
			want:    "https://custom.example.test",
		},
		{
			name:    "no account and no endpoint is empty",
			storage: Storage{},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.storage.ResolveEndpoint(); got != tt.want {
				t.Errorf("ResolveEndpoint() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A production config must not boot with a localhost CORS allowlist or without
// an identity provider, since both would be silent security holes.
func TestValidateRejectsUnsafeProductionConfig(t *testing.T) {
	cfg := &Config{
		App:      App{Environment: "production"},
		HTTP:     HTTP{Port: 8080, AllowedOrigins: []string{"http://localhost:3000"}},
		Database: Database{URL: "postgres://localhost/db"},
	}

	err := cfg.validate()
	if err == nil {
		t.Fatal("expected production validation to fail, got nil")
	}
}

func TestValidateAcceptsDevelopmentDefaults(t *testing.T) {
	cfg := &Config{
		App:      App{Environment: "development"},
		HTTP:     HTTP{Port: 8080, AllowedOrigins: []string{"http://localhost:3000"}},
		Database: Database{URL: "postgres://localhost/db"},
	}

	if err := cfg.validate(); err != nil {
		t.Fatalf("expected development config to validate, got %v", err)
	}
}

func TestValidateRequiresDatabaseURL(t *testing.T) {
	cfg := &Config{
		App:  App{Environment: "development"},
		HTTP: HTTP{Port: 8080},
	}

	if err := cfg.validate(); err == nil {
		t.Fatal("expected a missing DATABASE_URL to fail validation")
	}
}
