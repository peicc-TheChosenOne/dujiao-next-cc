package config

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/shared/passwordpolicy"

	"github.com/spf13/viper"
)

// Config 应用配置结构
type Config struct {
	App          AppConfig          `mapstructure:"app"`
	Server       ServerConfig       `mapstructure:"server"`
	Log          LogConfig          `mapstructure:"log"`
	Database     DatabaseConfig     `mapstructure:"database"`
	JWT          JWTConfig          `mapstructure:"jwt"`
	UserJWT      JWTConfig          `mapstructure:"user_jwt"`
	Bootstrap    BootstrapConfig    `mapstructure:"bootstrap"`
	TelegramAuth TelegramAuthConfig `mapstructure:"telegram_auth"`
	GoogleAuth   GoogleAuthConfig   `mapstructure:"google_auth"`
	Redis        RedisConfig        `mapstructure:"redis"`
	Queue        QueueConfig        `mapstructure:"queue"`
	Upload       UploadConfig       `mapstructure:"upload"`
	CORS         CORSConfig         `mapstructure:"cors"`
	Security     SecurityConfig     `mapstructure:"security"`
	Email        EmailConfig        `mapstructure:"email"`
	Order        OrderConfig        `mapstructure:"order"`
	Captcha      CaptchaConfig      `mapstructure:"captcha"`
	Web          WebConfig          `mapstructure:"web"`
	Reseller     ResellerConfig     `mapstructure:"reseller"`
}

// AppConfig 应用级配置
type AppConfig struct {
	SecretKey  string `mapstructure:"secret_key"`  // 通用加密密钥（AES-256，用于加密存储敏感信息）
	TOTPIssuer string `mapstructure:"totp_issuer"` // 2FA 验证器中显示的发行方名称（避免使用 & 等特殊字符，Google Authenticator 解析容错差）
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Host           string   `mapstructure:"host"`
	Port           string   `mapstructure:"port"`
	Mode           string   `mapstructure:"mode"` // debug / release
	TrustedProxies []string `mapstructure:"trusted_proxies"`
}

// LogConfig 日志配置
type LogConfig struct {
	Dir        string `mapstructure:"dir"`
	Filename   string `mapstructure:"filename"`
	MaxSizeMB  int    `mapstructure:"max_size_mb"`
	MaxBackups int    `mapstructure:"max_backups"`
	MaxAgeDays int    `mapstructure:"max_age_days"`
	Compress   bool   `mapstructure:"compress"`
}

// ToLoggerOptions 转换为 logger 配置
func (c LogConfig) ToLoggerOptions() logger.Options {
	return logger.Options{
		Dir:        c.Dir,
		Filename:   c.Filename,
		MaxSizeMB:  c.MaxSizeMB,
		MaxBackups: c.MaxBackups,
		MaxAgeDays: c.MaxAgeDays,
		Compress:   c.Compress,
	}
}

// DatabasePoolConfig 数据库连接池配置
type DatabasePoolConfig struct {
	MaxOpenConns           int `mapstructure:"max_open_conns"`
	MaxIdleConns           int `mapstructure:"max_idle_conns"`
	ConnMaxLifetimeSeconds int `mapstructure:"conn_max_lifetime_seconds"`
	ConnMaxIdleTimeSeconds int `mapstructure:"conn_max_idle_time_seconds"`
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Driver string             `mapstructure:"driver"` // 数据库驱动（sqlite/postgres）
	DSN    string             `mapstructure:"dsn"`    // 数据库连接串
	Pool   DatabasePoolConfig `mapstructure:"pool"`
}

// JWTConfig JWT 配置
type JWTConfig struct {
	SecretKey             string `mapstructure:"secret"`
	ExpireHours           int    `mapstructure:"expire_hours"`
	RememberMeExpireHours int    `mapstructure:"remember_me_expire_hours"`
}

// BootstrapConfig 启动初始化配置
type BootstrapConfig struct {
	DefaultAdminUsername string `mapstructure:"default_admin_username"`
	DefaultAdminPassword string `mapstructure:"default_admin_password"`
}

// TelegramAuthConfig Telegram 登录配置
type TelegramAuthConfig struct {
	Enabled            bool   `mapstructure:"enabled"`
	BotUsername        string `mapstructure:"bot_username"`
	BotToken           string `mapstructure:"bot_token"`
	ClientSecret       string `mapstructure:"client_secret"`
	OIDCRedirectURI    string `mapstructure:"oidc_redirect_uri"`
	MiniAppURL         string `mapstructure:"mini_app_url"`
	LoginExpireSeconds int    `mapstructure:"login_expire_seconds"`
	ReplayTTLSeconds   int    `mapstructure:"replay_ttl_seconds"`
}

// GoogleAuthConfig Google Identity Services 登录配置。
type GoogleAuthConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	ClientID string `mapstructure:"client_id"`
}

// RedisConfig Redis 配置
type RedisConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
	Prefix   string `mapstructure:"prefix"`
}

// QueueConfig 异步队列配置
type QueueConfig struct {
	Enabled              bool           `mapstructure:"enabled"`
	Host                 string         `mapstructure:"host"`
	Port                 int            `mapstructure:"port"`
	Password             string         `mapstructure:"password"`
	DB                   int            `mapstructure:"db"`
	Concurrency          int            `mapstructure:"concurrency"`
	Queues               map[string]int `mapstructure:"queues"`
	UpstreamSyncInterval string         `mapstructure:"upstream_sync_interval"` // 上游库存同步间隔，如 "5m"、"10m"，默认 "5m"
}

// OrderConfig 订单配置
type OrderConfig struct {
	PaymentExpireMinutes int `mapstructure:"payment_expire_minutes"`
	MaxRefundDays        int `mapstructure:"max_refund_days"`
}

// EmailConfig 邮件服务配置
type EmailConfig struct {
	Enabled    bool             `mapstructure:"enabled"`
	Host       string           `mapstructure:"host"`
	Port       int              `mapstructure:"port"`
	Username   string           `mapstructure:"username"`
	Password   string           `mapstructure:"password"`
	From       string           `mapstructure:"from"`
	FromName   string           `mapstructure:"from_name"`
	UseTLS     bool             `mapstructure:"use_tls"`
	UseSSL     bool             `mapstructure:"use_ssl"`
	VerifyCode VerifyCodeConfig `mapstructure:"verify_code"`
}

// VerifyCodeConfig 邮箱验证码配置
type VerifyCodeConfig struct {
	ExpireMinutes       int `mapstructure:"expire_minutes"`
	SendIntervalSeconds int `mapstructure:"send_interval_seconds"`
	MaxAttempts         int `mapstructure:"max_attempts"`
	Length              int `mapstructure:"length"`
}

// CaptchaConfig 验证码配置
type CaptchaConfig struct {
	Provider  string                 `mapstructure:"provider"`
	Scenes    CaptchaSceneConfig     `mapstructure:"scenes"`
	Image     CaptchaImageConfig     `mapstructure:"image"`
	Turnstile CaptchaTurnstileConfig `mapstructure:"turnstile"`
}

// CaptchaSceneConfig 验证码场景开关
type CaptchaSceneConfig struct {
	Login            bool `mapstructure:"login"`
	RegisterSendCode bool `mapstructure:"register_send_code"`
	ResetSendCode    bool `mapstructure:"reset_send_code"`
	GuestCreateOrder bool `mapstructure:"guest_create_order"`
	GiftCardRedeem   bool `mapstructure:"gift_card_redeem"`
}

// CaptchaImageConfig 图片验证码配置
type CaptchaImageConfig struct {
	Length        int `mapstructure:"length"`
	Width         int `mapstructure:"width"`
	Height        int `mapstructure:"height"`
	NoiseCount    int `mapstructure:"noise_count"`
	ShowLine      int `mapstructure:"show_line"`
	ExpireSeconds int `mapstructure:"expire_seconds"`
	MaxStore      int `mapstructure:"max_store"`
}

// CaptchaTurnstileConfig Cloudflare Turnstile 配置
type CaptchaTurnstileConfig struct {
	SiteKey   string `mapstructure:"site_key"`
	SecretKey string `mapstructure:"secret_key"`
	VerifyURL string `mapstructure:"verify_url"`
	TimeoutMS int    `mapstructure:"timeout_ms"`
}

// UploadConfig 文件上传配置
type UploadConfig struct {
	MaxSize           int64    `mapstructure:"max_size"`
	AllowedTypes      []string `mapstructure:"allowed_types"`
	AllowedExtensions []string `mapstructure:"allowed_extensions"`
	MaxWidth          int      `mapstructure:"max_width"`
	MaxHeight         int      `mapstructure:"max_height"`
}

// CORSConfig 跨域配置
type CORSConfig struct {
	AllowedOrigins   []string `mapstructure:"allowed_origins"`
	AllowedMethods   []string `mapstructure:"allowed_methods"`
	AllowedHeaders   []string `mapstructure:"allowed_headers"`
	AllowCredentials bool     `mapstructure:"allow_credentials"`
	MaxAge           int      `mapstructure:"max_age"`
}

var (
	defaultCORSAllowedOrigins = []string{"*"}
	defaultCORSAllowedMethods = []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions, http.MethodPatch}
	defaultCORSAllowedHeaders = []string{
		"Content-Type",
		"Content-Length",
		"Accept-Encoding",
		"Authorization",
		"Cache-Control",
		"X-Requested-With",
		"X-CSRF-Token",
	}
)

// DefaultCORSAllowedOrigins 返回默认允许跨域来源（副本，避免调用方修改全局默认值）
func DefaultCORSAllowedOrigins() []string {
	return append([]string(nil), defaultCORSAllowedOrigins...)
}

// DefaultCORSAllowedMethods 返回默认允许跨域方法（副本，避免调用方修改全局默认值）
func DefaultCORSAllowedMethods() []string {
	return append([]string(nil), defaultCORSAllowedMethods...)
}

// DefaultCORSAllowedHeaders 返回默认允许跨域请求头（副本，避免调用方修改全局默认值）
func DefaultCORSAllowedHeaders() []string {
	return append([]string(nil), defaultCORSAllowedHeaders...)
}

// SecurityConfig 安全配置
type SecurityConfig struct {
	LoginRateLimit LoginRateLimitConfig `mapstructure:"login_rate_limit"`
	PasswordPolicy PasswordPolicyConfig `mapstructure:"password_policy"`
}

// LoginRateLimitConfig 登录限流配置
type LoginRateLimitConfig struct {
	WindowSeconds int `mapstructure:"window_seconds"`
	MaxAttempts   int `mapstructure:"max_attempts"`
	BlockSeconds  int `mapstructure:"block_seconds"`
}

// PasswordPolicyConfig 密码策略配置
type PasswordPolicyConfig struct {
	MinLength      int  `mapstructure:"min_length"`
	RequireUpper   bool `mapstructure:"require_upper"`
	RequireLower   bool `mapstructure:"require_lower"`
	RequireNumber  bool `mapstructure:"require_number"`
	RequireSpecial bool `mapstructure:"require_special"`
}

// ValidationPolicy converts configuration into the shared identity policy.
func (p PasswordPolicyConfig) ValidationPolicy() passwordpolicy.Policy {
	return passwordpolicy.Policy{
		MinLength:      p.MinLength,
		RequireUpper:   p.RequireUpper,
		RequireLower:   p.RequireLower,
		RequireNumber:  p.RequireNumber,
		RequireSpecial: p.RequireSpecial,
	}
}

// WebConfig 仅在 fullstack 二进制模式下生效。
// 默认构建模式（无 -tags fullstack）下这些字段不被任何代码读取。
type WebConfig struct {
	// AdminPath 后台访问路径前缀，例如 "/admin" 或 "/dj-mgmt-7x9k2"。
	// 校验规则见 internal/web.ValidateAdminPath。
	AdminPath string `mapstructure:"admin_path"`
}

// ResellerConfig 分销商模式配置。
type ResellerConfig struct {
	Enabled              bool     `mapstructure:"enabled"`
	MainHosts            []string `mapstructure:"main_hosts"`
	TrustedForwardedHost bool     `mapstructure:"trusted_forwarded_host"`
	SubdomainBase        string   `mapstructure:"subdomain_base"`
	SelfApplyEnabled     bool     `mapstructure:"self_apply_enabled"`
	// SettlementConfirmDays 分销利润入账后转为可提现的确认天数（0 表示即时到账）。
	SettlementConfirmDays int                         `mapstructure:"settlement_confirm_days"`
	DomainBinding         ResellerDomainBindingConfig `mapstructure:"domain_binding"`
}

type ResellerDomainBindingConfig struct {
	Enabled                     bool   `mapstructure:"enabled"`
	Provider                    string `mapstructure:"provider"`
	CNAMETarget                 string `mapstructure:"cname_target"`
	SyncBatchSize               int    `mapstructure:"sync_batch_size"`
	MaxCustomDomainsPerReseller int    `mapstructure:"max_custom_domains_per_reseller"`
	MaxHostnames                int    `mapstructure:"max_hostnames"`
	PendingExpiryHours          int    `mapstructure:"pending_expiry_hours"`
	Cloudflare                  struct {
		ZoneID       string `mapstructure:"zone_id"`
		APIToken     string `mapstructure:"api_token"`
		CustomOrigin string `mapstructure:"custom_origin"`
	} `mapstructure:"cloudflare"`
}

// Load 按默认值、普通配置、私有配置、环境变量的顺序加载配置。
func Load() *Config {
	cfg, err := loadConfig(os.Getenv("DUJIAO_CONFIG_FILE"), os.Getenv("DUJIAO_PRIVATE_CONFIG_FILE"))
	if err != nil {
		logger.Errorw("config_load_failed", "error", err)
		panic(err)
	}
	return cfg
}

func loadConfig(configFile, privateFile string) (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	if configFile != "" {
		v.SetConfigFile(configFile)
	}
	v.SetConfigType("yaml")
	v.AddConfigPath(".")     // 从当前目录查找
	v.AddConfigPath("./")    // 备用路径
	v.AddConfigPath("../")   // 如果从 cmd/server 运行
	v.AddConfigPath("./etc") // etc 文件夹

	// 设置默认值（可选）
	v.SetDefault("app.secret_key", "change-me-32-byte-secret-key!!")
	v.SetDefault("app.totp_issuer", "AI大玩家")
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", "8080")
	v.SetDefault("server.mode", "debug")
	v.SetDefault("server.trusted_proxies", []string{"127.0.0.1/32", "::1/128"})
	v.SetDefault("log.dir", "")
	v.SetDefault("log.filename", "app.log")
	v.SetDefault("log.max_size_mb", 100)
	v.SetDefault("log.max_backups", 7)
	v.SetDefault("log.max_age_days", 30)
	v.SetDefault("log.compress", true)
	v.SetDefault("database.driver", "sqlite")
	v.SetDefault("database.dsn", "./db/dujiao.db")
	v.SetDefault("database.pool.max_open_conns", 1)
	v.SetDefault("database.pool.max_idle_conns", 1)
	v.SetDefault("database.pool.conn_max_lifetime_seconds", 0)
	v.SetDefault("database.pool.conn_max_idle_time_seconds", 0)
	v.SetDefault("jwt.secret", "change-me-in-production")
	v.SetDefault("jwt.expire_hours", 24)
	v.SetDefault("user_jwt.secret", "user-change-me-in-production")
	v.SetDefault("user_jwt.expire_hours", 24)
	v.SetDefault("user_jwt.remember_me_expire_hours", 168)
	v.SetDefault("bootstrap.default_admin_username", "")
	v.SetDefault("bootstrap.default_admin_password", "")
	v.SetDefault("telegram_auth.enabled", false)
	v.SetDefault("telegram_auth.bot_username", "")
	v.SetDefault("telegram_auth.bot_token", "")
	v.SetDefault("telegram_auth.client_secret", "")
	v.SetDefault("telegram_auth.login_expire_seconds", 300)
	v.SetDefault("telegram_auth.replay_ttl_seconds", 300)
	v.SetDefault("google_auth.enabled", false)
	v.SetDefault("google_auth.client_id", "")
	v.SetDefault("redis.enabled", true)
	v.SetDefault("redis.host", "127.0.0.1")
	v.SetDefault("redis.port", 6379)
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.prefix", constants.RedisPrefixDefault)
	v.SetDefault("queue.enabled", true)
	v.SetDefault("queue.host", "127.0.0.1")
	v.SetDefault("queue.port", 6379)
	v.SetDefault("queue.password", "")
	v.SetDefault("queue.db", 1)
	v.SetDefault("queue.concurrency", 10)
	v.SetDefault("queue.queues", map[string]int{
		"default":  10,
		"critical": 5,
	})
	v.SetDefault("upload.max_size", 10485760)
	v.SetDefault("upload.allowed_types", []string{
		"image/jpeg",
		"image/png",
		"image/gif",
		"image/webp",
	})
	v.SetDefault("upload.allowed_extensions", []string{
		".jpg",
		".jpeg",
		".png",
		".gif",
		".webp",
	})
	v.SetDefault("upload.max_width", 4096)
	v.SetDefault("upload.max_height", 4096)
	v.SetDefault("cors.allowed_origins", DefaultCORSAllowedOrigins())
	v.SetDefault("cors.allowed_methods", DefaultCORSAllowedMethods())
	v.SetDefault("cors.allowed_headers", DefaultCORSAllowedHeaders())
	v.SetDefault("cors.allow_credentials", true)
	v.SetDefault("cors.max_age", 600)
	v.SetDefault("security.login_rate_limit.window_seconds", 300)
	v.SetDefault("security.login_rate_limit.max_attempts", 5)
	v.SetDefault("security.login_rate_limit.block_seconds", 900)
	v.SetDefault("security.password_policy.min_length", 8)
	v.SetDefault("security.password_policy.require_upper", true)
	v.SetDefault("security.password_policy.require_lower", true)
	v.SetDefault("security.password_policy.require_number", true)
	v.SetDefault("security.password_policy.require_special", false)
	v.SetDefault("email.enabled", false)
	v.SetDefault("email.host", "")
	v.SetDefault("email.port", 587)
	v.SetDefault("email.username", "")
	v.SetDefault("email.password", "")
	v.SetDefault("email.from", "")
	v.SetDefault("email.from_name", "")
	v.SetDefault("email.use_tls", true)
	v.SetDefault("email.use_ssl", false)
	v.SetDefault("email.verify_code.expire_minutes", 10)
	v.SetDefault("email.verify_code.send_interval_seconds", 60)
	v.SetDefault("email.verify_code.max_attempts", 5)
	v.SetDefault("email.verify_code.length", 6)
	v.SetDefault("order.payment_expire_minutes", 15)
	v.SetDefault("order.max_refund_days", 30)
	v.SetDefault("captcha.provider", "none")
	v.SetDefault("captcha.scenes.login", false)
	v.SetDefault("captcha.scenes.register_send_code", false)
	v.SetDefault("captcha.scenes.reset_send_code", false)
	v.SetDefault("captcha.scenes.guest_create_order", false)
	v.SetDefault("captcha.scenes.gift_card_redeem", false)
	v.SetDefault("captcha.image.length", 5)
	v.SetDefault("captcha.image.width", 240)
	v.SetDefault("captcha.image.height", 80)
	v.SetDefault("captcha.image.noise_count", 2)
	v.SetDefault("captcha.image.show_line", 2)
	v.SetDefault("captcha.image.expire_seconds", 300)
	v.SetDefault("captcha.image.max_store", 10240)
	v.SetDefault("captcha.turnstile.site_key", "")
	v.SetDefault("captcha.turnstile.secret_key", "")
	v.SetDefault("captcha.turnstile.verify_url", "https://challenges.cloudflare.com/turnstile/v0/siteverify")
	v.SetDefault("captcha.turnstile.timeout_ms", 2000)
	v.SetDefault("web.admin_path", "/admin")
	v.SetDefault("reseller.enabled", false)
	v.SetDefault("reseller.main_hosts", []string{"localhost", "127.0.0.1", "::1"})
	v.SetDefault("reseller.trusted_forwarded_host", false)
	v.SetDefault("reseller.subdomain_base", "")
	v.SetDefault("reseller.self_apply_enabled", true)
	v.SetDefault("reseller.settlement_confirm_days", 7)
	v.SetDefault("reseller.domain_binding.enabled", false)
	v.SetDefault("reseller.domain_binding.provider", "cloudflare")
	v.SetDefault("reseller.domain_binding.cname_target", "")
	v.SetDefault("reseller.domain_binding.sync_batch_size", 50)
	v.SetDefault("reseller.domain_binding.max_custom_domains_per_reseller", 5)
	v.SetDefault("reseller.domain_binding.max_hostnames", 100)
	v.SetDefault("reseller.domain_binding.pending_expiry_hours", 168)
	v.SetDefault("reseller.domain_binding.cloudflare.zone_id", "")
	v.SetDefault("reseller.domain_binding.cloudflare.api_token", "")
	v.SetDefault("reseller.domain_binding.cloudflare.custom_origin", "")

	// 环境变量支持
	v.AutomaticEnv()                                   // 自动读取环境变量
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_")) // 将 . 替换为 _ (例如 server.port -> SERVER_PORT)

	var missing viper.ConfigFileNotFoundError
	if err := v.ReadInConfig(); err != nil {
		if configFile != "" || !errors.As(err, &missing) {
			return nil, fmt.Errorf("cannot read configuration %q; check the file, YAML syntax and permissions", v.ConfigFileUsed())
		}
		logger.Warnw("config_file_not_found", "fallback", "env_or_defaults")
	} else {
		logger.Infow("config_file_loaded", "file", v.ConfigFileUsed())
	}

	explicitPrivate := privateFile != ""
	if privateFile == "" {
		privateFile = filepath.Join(filepath.Dir(v.ConfigFileUsed()), "config.private.yml")
	}
	file, err := os.Open(privateFile)
	if err != nil {
		if explicitPrivate || !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("cannot read private configuration %q; check the file and permissions", privateFile)
		}
	} else {
		defer file.Close()
		if err := v.MergeConfig(file); err != nil {
			// YAML 解析错误可能包含原始值，私有配置的错误只报告文件位置。
			return nil, fmt.Errorf("cannot parse private configuration %q; check the YAML syntax", privateFile)
		}
		logger.Infow("private_config_file_loaded", "file", privateFile)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration field types; check %q and %q", v.ConfigFileUsed(), privateFile)
	}
	if binding := cfg.Reseller.DomainBinding; binding.Enabled {
		if binding.MaxCustomDomainsPerReseller < 1 || binding.MaxHostnames < 2 || binding.PendingExpiryHours < 1 || binding.PendingExpiryHours > 8760 {
			return nil, fmt.Errorf("reseller.domain_binding requires positive domain limits and pending_expiry_hours between 1 and 8760")
		}
		validZone := len(binding.Cloudflare.ZoneID) == 32
		for _, ch := range binding.Cloudflare.ZoneID {
			if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
				validZone = false
			}
		}
		if !cfg.Reseller.Enabled || !cfg.Queue.Enabled || binding.Provider != "cloudflare" || !validBindingHost(binding.CNAMETarget) || binding.Cloudflare.CustomOrigin != "" && !validBindingHost(binding.Cloudflare.CustomOrigin) || !validZone || strings.TrimSpace(binding.Cloudflare.APIToken) == "" || binding.SyncBatchSize < 1 || binding.SyncBatchSize > 500 {
			return nil, fmt.Errorf("reseller.domain_binding requires enabled reseller and queue, cloudflare provider, cname_target, zone_id, private api_token and sync_batch_size between 1 and 500")
		}
	}
	return &cfg, nil
}

func validBindingHost(host string) bool {
	if len(host) > 253 || !strings.Contains(host, ".") || net.ParseIP(host) != nil {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
	}
	return true
}
