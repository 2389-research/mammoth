// ABOUTME: Server configuration loaded from MAMMOTH_* environment variables.
// ABOUTME: Enforces security constraint: remote access requires auth token.
package server

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// ConfigError represents configuration validation errors.
var (
	ErrRemoteWithoutToken = errors.New(
		"MAMMOTH_ALLOW_REMOTE is true but MAMMOTH_AUTH_TOKEN is not set; refusing to start without authentication",
	)
	ErrNonLoopbackBind = errors.New(
		"MAMMOTH_BIND is a non-loopback address but MAMMOTH_ALLOW_REMOTE is not true; set MAMMOTH_ALLOW_REMOTE=true and MAMMOTH_AUTH_TOKEN to allow remote access",
	)
)

// MammothConfig holds server configuration loaded from environment variables.
type MammothConfig struct {
	Home            string // Data directory (MAMMOTH_HOME, default: ~/.mammoth-specd)
	Bind            string // Socket address (MAMMOTH_BIND, default: 127.0.0.1:7770)
	AllowRemote     bool   // Allow non-loopback connections (MAMMOTH_ALLOW_REMOTE, default: false)
	AuthToken       string // Bearer token for API auth (MAMMOTH_AUTH_TOKEN, optional)
	DefaultProvider string // LLM provider (MAMMOTH_DEFAULT_PROVIDER, default: anthropic)
	DefaultModel    string // LLM model name (MAMMOTH_DEFAULT_MODEL, optional)
	PublicBaseURL   string // Public URL for the server (MAMMOTH_PUBLIC_BASE_URL)
}

// ConfigFromEnv loads configuration from MAMMOTH_* environment variables with sensible defaults.
func ConfigFromEnv() (*MammothConfig, error) {
	home := envOrDefault("MAMMOTH_HOME", "")
	if home == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			homeDir = "/tmp"
		}
		home = filepath.Join(homeDir, ".mammoth-specd")
	}

	bind := envOrDefault("MAMMOTH_BIND", "127.0.0.1:7770")

	allowRemote := false
	if v := os.Getenv("MAMMOTH_ALLOW_REMOTE"); v == "true" || v == "1" || v == "yes" {
		allowRemote = true
	}

	authToken := nonEmptyEnv("MAMMOTH_AUTH_TOKEN")
	defaultProvider := envOrDefault("MAMMOTH_DEFAULT_PROVIDER", "anthropic")
	defaultModel := nonEmptyEnv("MAMMOTH_DEFAULT_MODEL")

	publicBaseURL := envOrDefault("MAMMOTH_PUBLIC_BASE_URL", fmt.Sprintf("http://%s", bind))

	// Security: remote access requires auth token
	if allowRemote && authToken == "" {
		return nil, ErrRemoteWithoutToken
	}

	// Security: refuse non-loopback binds unless explicitly opting into remote access.
	// Checks both IP literals and hostnames; only 127.0.0.0/8, ::1, and "localhost"
	// are considered safe.
	if !allowRemote {
		if host, _, err := net.SplitHostPort(bind); err == nil && host != "" {
			ip := net.ParseIP(host)
			switch {
			case ip != nil && ip.IsLoopback():
				// Safe: 127.x.x.x or ::1
			case ip != nil:
				// Non-loopback IP literal (e.g. 0.0.0.0, 192.168.x.x)
				return nil, fmt.Errorf("%w: MAMMOTH_BIND=%s", ErrNonLoopbackBind, bind)
			case host == "localhost":
				// Safe: conventional loopback hostname
			default:
				// Non-localhost hostname (e.g. myhost, example.com)
				return nil, fmt.Errorf("%w: MAMMOTH_BIND=%s", ErrNonLoopbackBind, bind)
			}
		}
	}

	return &MammothConfig{
		Home:            home,
		Bind:            bind,
		AllowRemote:     allowRemote,
		AuthToken:       authToken,
		DefaultProvider: defaultProvider,
		DefaultModel:    defaultModel,
		PublicBaseURL:   publicBaseURL,
	}, nil
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func nonEmptyEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		return ""
	}
	return v
}
