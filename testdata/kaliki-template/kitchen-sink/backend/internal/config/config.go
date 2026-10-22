// Package config loads runtime configuration from a YAML file with env overrides.
package config

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)
// MailConfig drives the transactional mail package. Provider selects the
// transport at runtime ("smtp" -> Mailpit/SMTP, "ses" -> AWS SES, anything else
// -> the log transport, which prints the message to the server log). Switching
// providers never changes whether credentials leak to clients — they don't.
type MailConfig struct {
	Provider      string `yaml:"provider"`
	SenderAddress string `yaml:"sender_address"`
	SenderName    string `yaml:"sender_name"`
	AppName       string `yaml:"app_name"`
	// BaseURL is the public app URL used to build links (token verification method).
	BaseURL string `yaml:"base_url"`
	SMTP SMTPConfig `yaml:"smtp"`
	SES  SESConfig  `yaml:"ses"`
}

type SMTPConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type SESConfig struct {
	Region          string `yaml:"region"`
	AccessKeyID     string `yaml:"access_key_id"`
	SecretAccessKey string `yaml:"secret_access_key"`
}

type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Database      DatabaseConfig      `yaml:"database"`
	Observability ObservabilityConfig `yaml:"observability"`
	Token         TokenConfig         `yaml:"token"`
	RateLimit     RateLimitConfig     `yaml:"rate_limit"`
	Mail          MailConfig          `yaml:"mail"`
	Redis         RedisConfig         `yaml:"redis"`
	Kafka         KafkaConfig         `yaml:"kafka"`
	Razorpay      RazorpayConfig      `yaml:"razorpay"`
	Firebase      FirebaseConfig      `yaml:"firebase"`
	Storage       StorageConfig       `yaml:"storage"`
}

type RedisConfig struct {
	URL string `yaml:"url"`
}

// RateLimitConfig drives the global per-IP floor (internal/ratelimit). It is
// always generated (rate limiting is default-on infra, like observability and
// Postgres), but the floor itself defaults OFF: an undeclared blanket limit is
// a production surprise, unlike per-operation x-rate-limit rules, which are
// explicit and spec-declared.
type RateLimitConfig struct {
	GlobalEnabled  bool   `yaml:"global_enabled"`
	GlobalRequests int    `yaml:"global_requests"`
	GlobalWindow   string `yaml:"global_window"`
	GlobalBurst    int    `yaml:"global_burst"`
}

type KafkaConfig struct {
	Brokers []string `yaml:"brokers"`
}

type RazorpayConfig struct {
	KeyID     string `yaml:"key_id"`
	KeySecret string `yaml:"key_secret"`
}

type FirebaseConfig struct {
	CredentialsFile string `yaml:"credentials_file"`
}

type StorageConfig struct {
	Region   string `yaml:"region"`
	Bucket   string `yaml:"bucket"`
	Endpoint string `yaml:"endpoint"` // optional, for S3-compatible services
}

// TokenConfig holds JWT signing settings (used by the auth module).
type TokenConfig struct {
	Secret string `yaml:"secret"`
	// ExpiryHours is the access-token TTL. Keep it short; clients refresh.
	ExpiryHours int `yaml:"expiry_hours"`
	// RefreshExpiryHours is the refresh-token TTL (defaults to 720h / 30 days).
	RefreshExpiryHours int `yaml:"refresh_expiry_hours"`
}

type ServerConfig struct {
	Port string `yaml:"port"`
}

type DatabaseConfig struct {
	URL string `yaml:"url"`
}

// ObservabilityConfig is wired in by default. Leaving Endpoint empty selects a
// no-op tracer, so the app runs with zero external dependencies until you point
// it at a real OTel collector.
type ObservabilityConfig struct {
	ServiceName string `yaml:"service_name"`
	Endpoint    string `yaml:"endpoint"`
}

// envOverride copies the named env var into dst when set (a no-op otherwise).
// Centralizing the "if set, override" check keeps Load a flat list of fields
// instead of a branch per field, which is what actually drives its cyclomatic
// complexity.
func envOverride(dst *string, key string) {
	if v := os.Getenv(key); v != "" {
		*dst = v
	}
}

func envOverrideInt(dst *int, key string) {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			*dst = n
		}
	}
}

func envOverrideBool(dst *bool, key string) {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			*dst = b
		}
	}
}

func envOverrideList(dst *[]string, key string) {
	if v := os.Getenv(key); v != "" {
		*dst = strings.Split(v, ",")
	}
}

// Load reads the YAML at path, then applies env overrides for anything that is
// commonly injected by the deployment environment.
func Load(path string) (*Config, error) {
	cfg := &Config{
		Server:        ServerConfig{Port: "8080"},
		Observability: ObservabilityConfig{ServiceName: "backend"},
		Token:         TokenConfig{ExpiryHours: 1, RefreshExpiryHours: 720},
		RateLimit:     RateLimitConfig{GlobalEnabled: false, GlobalRequests: 100, GlobalWindow: "60s", GlobalBurst: 20},
	}

	if b, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(b, cfg); err != nil {
			return nil, err
		}
	}

	envOverride(&cfg.Server.Port, "SERVER_PORT")
	envOverride(&cfg.Database.URL, "DATABASE_URL")
	envOverride(&cfg.Observability.Endpoint, "OTEL_EXPORTER_OTLP_ENDPOINT")
	envOverride(&cfg.Token.Secret, "JWT_SECRET")
	envOverrideBool(&cfg.RateLimit.GlobalEnabled, "RATE_LIMIT_GLOBAL_ENABLED")
	envOverrideInt(&cfg.RateLimit.GlobalRequests, "RATE_LIMIT_GLOBAL_REQUESTS")
	envOverride(&cfg.RateLimit.GlobalWindow, "RATE_LIMIT_GLOBAL_WINDOW")
	envOverrideInt(&cfg.RateLimit.GlobalBurst, "RATE_LIMIT_GLOBAL_BURST")
	envOverride(&cfg.Mail.Provider, "MAIL_PROVIDER")
	envOverride(&cfg.Mail.SenderAddress, "MAIL_SENDER_ADDRESS")
	envOverride(&cfg.Mail.BaseURL, "MAIL_BASE_URL")
	envOverride(&cfg.Mail.SMTP.Host, "SMTP_HOST")
	envOverrideInt(&cfg.Mail.SMTP.Port, "SMTP_PORT")
	envOverride(&cfg.Mail.SMTP.Username, "SMTP_USERNAME")
	envOverride(&cfg.Mail.SMTP.Password, "SMTP_PASSWORD")
	envOverride(&cfg.Mail.SES.Region, "SES_REGION")
	envOverride(&cfg.Mail.SES.AccessKeyID, "SES_ACCESS_KEY_ID")
	envOverride(&cfg.Mail.SES.SecretAccessKey, "SES_SECRET_ACCESS_KEY")
	envOverride(&cfg.Redis.URL, "REDIS_URL")
	envOverrideList(&cfg.Kafka.Brokers, "KAFKA_BROKERS")
	envOverride(&cfg.Razorpay.KeyID, "RAZORPAY_KEY_ID")
	envOverride(&cfg.Razorpay.KeySecret, "RAZORPAY_KEY_SECRET")
	envOverride(&cfg.Firebase.CredentialsFile, "FIREBASE_CREDENTIALS_FILE")
	envOverride(&cfg.Storage.Bucket, "S3_BUCKET")
	envOverride(&cfg.Storage.Region, "S3_REGION")
	envOverride(&cfg.Storage.Endpoint, "S3_ENDPOINT")

	if err := checkProductionSecret(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// checkProductionSecret fails fast if the insecure dev JWT secret survived
// into a real deployment.
func checkProductionSecret(cfg *Config) error {
	if os.Getenv("APP_ENV") != "production" {
		return nil
	}
	if cfg.Token.Secret == "" || cfg.Token.Secret == "dev-insecure-change-me" {
		return errors.New("refusing to start: set a strong JWT_SECRET " +
			"(the dev default is not allowed when APP_ENV=production)")
	}
	return nil
}
