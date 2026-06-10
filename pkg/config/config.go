// © 2026 Platform Engineering Labs Inc.
//
// SPDX-License-Identifier: FSL-1.1-ALv2

// Package config defines the PagerDuty plugin Target Config and the credential
// resolution chain. The token is never carried in Target Config (json:"-") so it
// can never be persisted in the formae datastore; it is resolved at client
// construction time from the environment or a local file.
package config

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	pagerduty "github.com/PagerDuty/go-pagerduty"
)

// DefaultTokenFile is the optional fallback file consulted when PAGERDUTY_TOKEN
// is unset. Users may store a single-line token there.
var DefaultTokenFile = defaultTokenFilePath()

// Config is the parsed PagerDuty Target Config.
//
// The Token field carries the resolved API token at runtime but is explicitly
// excluded from JSON (re)serialization (json:"-") so it cannot leak into Target
// Config persistence. See plan: opaque-on-Target-Config is unimplemented today.
type Config struct {
	Type      string `json:"Type"`
	Subdomain string `json:"Subdomain,omitempty"`
	FromEmail string `json:"FromEmail,omitempty"`
	Token     string `json:"-"`
}

// ParseTargetConfig deserializes the Target Config JSON and resolves the
// PagerDuty API token via the credential chain.
func ParseTargetConfig(raw []byte) (*Config, error) {
	cfg := &Config{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, cfg); err != nil {
			return nil, fmt.Errorf("parse target config: %w", err)
		}
	}
	cfg.Token = resolveToken(DefaultTokenFile)
	return cfg, nil
}

// Validate checks that required fields (notably the resolved token) are present.
func (c *Config) Validate() error {
	if c.Token == "" {
		return fmt.Errorf("pagerduty token not found; set PAGERDUTY_TOKEN env var or write a token to %s", DefaultTokenFile)
	}
	return nil
}

// resolveToken tries PAGERDUTY_TOKEN env var first, then falls back to reading
// the given file (typically ~/.config/pagerduty/token). Returns empty string if
// neither source yields a value.
func resolveToken(tokenFile string) string {
	if tok := strings.TrimSpace(os.Getenv("PAGERDUTY_TOKEN")); tok != "" {
		return tok
	}
	if tokenFile == "" {
		return ""
	}
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// NewClient builds a PagerDuty SDK client from a validated Config. The SDK does
// no retry/backoff of its own, so we wrap its HTTP transport to retry 429/5xx
// with backoff. The token is resolved here and never persisted.
func NewClient(cfg *Config) (*pagerduty.Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	// go-pagerduty points every client at a process-global *http.Client, so we
	// must not mutate it. Give this client its own transport (a clone of the
	// default, for sane pooling/timeouts) wrapped exactly once with retry.
	c := pagerduty.NewClient(cfg.Token)
	c.HTTPClient = &http.Client{Transport: newRetryTransport(http.DefaultTransport.(*http.Transport).Clone())}
	return c, nil
}

func defaultTokenFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "pagerduty", "token")
}
