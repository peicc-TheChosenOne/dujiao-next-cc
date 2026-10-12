package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPrivateConfigMergesBesideSelectedBaseAndEnvWins(t *testing.T) {
	dir := t.TempDir()
	base := writeConfig(t, dir, "development.yml", `server:
  mode: debug
database:
  driver: postgres
  pool:
    max_open_conns: 17
jwt:
  secret: base-admin-secret
  expire_hours: 48
redis:
  enabled: true
  host: base-host
`)
	writeConfig(t, dir, "config.private.yml", `app:
  secret_key: private-encryption-key
jwt:
  secret: private-admin-secret
user_jwt:
  secret: private-user-secret
database:
  dsn: host=localhost password=private-database-password dbname=test
redis:
  host: private-host
  password: private-redis-password
  enabled: false
queue:
  password: private-queue-password
`)
	t.Setenv("JWT_SECRET", "environment-admin-secret")
	t.Setenv("TELEGRAM_AUTH_CLIENT_SECRET", "environment-client-secret")
	cfg, err := loadConfig(base, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.JWT.SecretKey != "environment-admin-secret" || cfg.TelegramAuth.ClientSecret != "environment-client-secret" {
		t.Fatal("environment variables did not override private configuration")
	}
	if cfg.App.SecretKey != "private-encryption-key" || cfg.UserJWT.SecretKey != "private-user-secret" || cfg.Redis.Password != "private-redis-password" || cfg.Queue.Password != "private-queue-password" || !strings.Contains(cfg.Database.DSN, "private-database-password") {
		t.Fatal("private values were not merged")
	}
	if cfg.Server.Mode != "debug" || cfg.Database.Driver != "postgres" || cfg.Database.Pool.MaxOpenConns != 17 || cfg.JWT.ExpireHours != 48 || cfg.Redis.Enabled || cfg.Redis.Host != "private-host" {
		t.Fatal("merging lost ordinary configuration or a false override")
	}
}

func TestLegacyConfigAndSeparateLoadsDoNotSharePrivateValues(t *testing.T) {
	first := t.TempDir()
	base := writeConfig(t, first, "config.yml", "jwt:\n  secret: legacy-secret\n")
	writeConfig(t, first, "config.private.yml", "jwt:\n  secret: first-private-secret\n")
	if _, err := loadConfig(base, ""); err != nil {
		t.Fatal(err)
	}
	second := writeConfig(t, t.TempDir(), "config.yml", "jwt:\n  secret: second-legacy-secret\n")
	cfg, err := loadConfig(second, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.JWT.SecretKey != "second-legacy-secret" {
		t.Fatal("loading an old single-file config retained another environment's private value")
	}
}

func TestPrivateConfigCanBeSelectedOutsideBaseDirectory(t *testing.T) {
	base := writeConfig(t, t.TempDir(), "config.yml", "server:\n  mode: release\n")
	private := writeConfig(t, t.TempDir(), "runtime.yml", "jwt:\n  secret: selected-private-secret\n")
	cfg, err := loadConfig(base, private)
	if err != nil || cfg.JWT.SecretKey != "selected-private-secret" || cfg.Server.Mode != "release" {
		t.Fatal("explicit private file was not loaded")
	}
}

func TestMissingOrInvalidSelectedConfigFailsWithoutLeakingValues(t *testing.T) {
	for _, test := range []struct{ name, base, private string }{
		{"missing base", "missing", ""},
		{"malformed base", "jwt: [do-not-leak-this-secret", ""},
		{"missing private", "server:\n  mode: debug\n", "missing"},
		{"malformed private", "server:\n  mode: debug\n", "jwt: [do-not-leak-this-secret"},
		{"invalid private field type", "server:\n  mode: debug\n", "redis:\n  port: do-not-leak-this-secret\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			base := filepath.Join(dir, "config.yml")
			if test.base != "missing" {
				writeConfig(t, dir, "config.yml", test.base)
			}
			private := ""
			if test.private != "" {
				private = filepath.Join(dir, "config.private.yml")
				if test.private != "missing" {
					writeConfig(t, dir, "config.private.yml", test.private)
				}
			}
			_, err := loadConfig(base, private)
			if err == nil || strings.Contains(err.Error(), "do-not-leak-this-secret") {
				t.Fatal("invalid configuration did not fail safely")
			}
		})
	}
}

func TestAbsentBaseStillSupportsEnvironmentConfiguration(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("SERVER_PORT", "8091")
	t.Setenv("JWT_SECRET", "environment-only-secret")
	cfg, err := loadConfig("", "")
	if err != nil || cfg.Server.Port != "8091" || cfg.JWT.SecretKey != "environment-only-secret" {
		t.Fatal("legacy environment-only startup is not supported")
	}
}
