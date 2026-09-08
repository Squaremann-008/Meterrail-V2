// Package config loads and validates all runtime configuration from the
// environment. Everything the services need is resolved once at boot so that
// no other package has to reach for os.Getenv.
package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Config struct {
	App      App
	HTTP     HTTP
	Database Database
	Redis    Redis
	Auth     Auth
	Storage  Storage
	Agora    Agora
	Envio    Envio
	Worker   Worker
}

type App struct {
	Name        string `env:"APP_NAME" envDefault:"meterrail-api"`
	Environment string `env:"APP_ENV" envDefault:"development"`
	LogLevel    string `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat   string `env:"LOG_FORMAT" envDefault:"text"`
}

func (a App) IsProduction() bool { return a.Environment == "production" }

type HTTP struct {
	Host            string        `env:"HTTP_HOST" envDefault:"0.0.0.0"`
	Port            int           `env:"PORT" envDefault:"8080"`
	ReadTimeout     time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"15s"`
	WriteTimeout    time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"30s"`
	IdleTimeout     time.Duration `env:"HTTP_IDLE_TIMEOUT" envDefault:"60s"`
	ShutdownTimeout time.Duration `env:"HTTP_SHUTDOWN_TIMEOUT" envDefault:"20s"`
	AllowedOrigins  []string      `env:"CORS_ALLOWED_ORIGINS" envSeparator:"," envDefault:"http://localhost:3000"`
	TrustedProxies  bool          `env:"HTTP_TRUST_PROXY_HEADERS" envDefault:"false"`
	RateLimitPerMin int           `env:"HTTP_RATE_LIMIT_PER_MIN" envDefault:"120"`
}

func (h HTTP) Addr() string { return fmt.Sprintf("%s:%d", h.Host, h.Port) }

// Database points at a Postgres instance. Neon and Supabase are both plain
// Postgres over TLS, so the only thing that changes between them is the URL.
type Database struct {
	URL             string        `env:"DATABASE_URL,required"`
	MaxOpenConns    int           `env:"DB_MAX_OPEN_CONNS" envDefault:"25"`
	MaxIdleConns    int           `env:"DB_MAX_IDLE_CONNS" envDefault:"5"`
	ConnMaxLifetime time.Duration `env:"DB_CONN_MAX_LIFETIME" envDefault:"30m"`
	ConnMaxIdleTime time.Duration `env:"DB_CONN_MAX_IDLE_TIME" envDefault:"5m"`
	SlowThreshold   time.Duration `env:"DB_SLOW_QUERY_THRESHOLD" envDefault:"250ms"`
	AutoMigrate     bool          `env:"DB_AUTO_MIGRATE" envDefault:"false"`
}

type Redis struct {
	URL         string `env:"REDIS_URL" envDefault:"redis://localhost:6379/0"`
	CachePrefix string `env:"REDIS_CACHE_PREFIX" envDefault:"meterrail:cache"`
	PoolSize    int    `env:"REDIS_POOL_SIZE" envDefault:"20"`
}

// Auth wires Dynamic (dynamic.xyz) as the identity provider. Dynamic issues a
// JWT after the user proves wallet ownership; we verify it against the JWKS
// published for the environment.
type Auth struct {
	DynamicEnvironmentID string        `env:"DYNAMIC_ENVIRONMENT_ID"`
	DynamicJWKSURL       string        `env:"DYNAMIC_JWKS_URL"`
	DynamicAPIBase       string        `env:"DYNAMIC_API_BASE" envDefault:"https://app.dynamicauth.com"`
	DynamicAPIToken      string        `env:"DYNAMIC_API_TOKEN"`
	Issuer               string        `env:"DYNAMIC_JWT_ISSUER"`
	Audience             string        `env:"DYNAMIC_JWT_AUDIENCE"`
	JWKSRefreshInterval  time.Duration `env:"DYNAMIC_JWKS_REFRESH_INTERVAL" envDefault:"1h"`
	Leeway               time.Duration `env:"DYNAMIC_JWT_LEEWAY" envDefault:"30s"`
	ServiceToken         string        `env:"INTERNAL_SERVICE_TOKEN"`
}

// JWKS returns the effective JWKS endpoint, deriving it from the environment ID
// when an explicit URL was not supplied.
func (a Auth) JWKS() string {
	if a.DynamicJWKSURL != "" {
		return a.DynamicJWKSURL
	}
	if a.DynamicEnvironmentID == "" {
		return ""
	}
	return fmt.Sprintf("%s/api/v0/environments/%s/keys",
		strings.TrimRight(a.DynamicAPIBase, "/"), a.DynamicEnvironmentID)
}

// Storage targets Cloudflare R2, which speaks the S3 API. PublicBaseURL is the
// CDN/custom domain in front of the bucket; presigned URLs are used for writes.
type Storage struct {
	AccountID       string        `env:"R2_ACCOUNT_ID"`
	AccessKeyID     string        `env:"R2_ACCESS_KEY_ID"`
	SecretAccessKey string        `env:"R2_SECRET_ACCESS_KEY"`
	Bucket          string        `env:"R2_BUCKET"`
	Endpoint        string        `env:"R2_ENDPOINT"`
	PublicBaseURL   string        `env:"R2_PUBLIC_BASE_URL"`
	Region          string        `env:"R2_REGION" envDefault:"auto"`
	PresignTTL      time.Duration `env:"R2_PRESIGN_TTL" envDefault:"15m"`
	MaxUploadBytes  int64         `env:"R2_MAX_UPLOAD_BYTES" envDefault:"26214400"`
}

func (s Storage) Configured() bool {
	return s.AccessKeyID != "" && s.SecretAccessKey != "" && s.Bucket != ""
}

// ResolveEndpoint returns the S3-compatible endpoint for the R2 account.
func (s Storage) ResolveEndpoint() string {
	if s.Endpoint != "" {
		return strings.TrimRight(s.Endpoint, "/")
	}
	if s.AccountID == "" {
		return ""
	}
	return fmt.Sprintf("https://%s.r2.cloudflarestorage.com", s.AccountID)
}

type Agora struct {
	AppID          string        `env:"AGORA_APP_ID"`
	AppCertificate string        `env:"AGORA_APP_CERTIFICATE"`
	TokenTTL       time.Duration `env:"AGORA_TOKEN_TTL" envDefault:"1h"`
}

func (a Agora) Configured() bool { return a.AppID != "" && a.AppCertificate != "" }

// Envio is the HyperIndex GraphQL endpoint that serves indexed onchain data.
type Envio struct {
	GraphQLURL   string        `env:"ENVIO_GRAPHQL_URL" envDefault:"http://localhost:8080/v1/graphql"`
	AdminSecret  string        `env:"ENVIO_ADMIN_SECRET"`
	Timeout      time.Duration `env:"ENVIO_TIMEOUT" envDefault:"10s"`
	SyncInterval time.Duration `env:"ENVIO_SYNC_INTERVAL" envDefault:"1m"`
}

type Worker struct {
	Concurrency     int            `env:"WORKER_CONCURRENCY" envDefault:"10"`
	Queues          map[string]int `env:"WORKER_QUEUES" envDefault:"critical:6,default:3,low:1"`
	ShutdownTimeout time.Duration  `env:"WORKER_SHUTDOWN_TIMEOUT" envDefault:"30s"`
	MonitorPort     int            `env:"ASYNQMON_PORT" envDefault:"8081"`
}

// Load reads .env files (best effort, for local development) and then parses
// the process environment. Real environments inject variables directly.
func Load() (*Config, error) {
	// Later files never clobber values already set, which is what we want:
	// process env > .env.local > .env.
	_ = godotenv.Load(".env.local", ".env", "../../.env")

	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parse environment: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	var errs []error
	if c.Database.URL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if c.HTTP.Port <= 0 || c.HTTP.Port > 65535 {
		errs = append(errs, fmt.Errorf("PORT %d is out of range", c.HTTP.Port))
	}
	if c.App.IsProduction() {
		if c.Auth.JWKS() == "" {
			errs = append(errs, errors.New("DYNAMIC_ENVIRONMENT_ID or DYNAMIC_JWKS_URL is required in production"))
		}
		if len(c.HTTP.AllowedOrigins) == 1 && strings.HasPrefix(c.HTTP.AllowedOrigins[0], "http://localhost") {
			errs = append(errs, errors.New("CORS_ALLOWED_ORIGINS still points at localhost in production"))
		}
	}
	return errors.Join(errs...)
}
