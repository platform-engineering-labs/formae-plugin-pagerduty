// © 2026 Platform Engineering Labs
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTargetConfig_MinimalJSON(t *testing.T) {
	cfg, err := ParseTargetConfig([]byte(`{"Type":"PAGERDUTY"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Type != "PAGERDUTY" {
		t.Errorf("want Type=PAGERDUTY, got %q", cfg.Type)
	}
}

func TestParseTargetConfig_AllFields(t *testing.T) {
	cfg, err := ParseTargetConfig([]byte(`{"Type":"PAGERDUTY","Subdomain":"acme","FromEmail":"oncall@acme.com"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Subdomain != "acme" {
		t.Errorf("want Subdomain=acme, got %q", cfg.Subdomain)
	}
	if cfg.FromEmail != "oncall@acme.com" {
		t.Errorf("want FromEmail=oncall@acme.com, got %q", cfg.FromEmail)
	}
}

func TestParseTargetConfig_EmptyJSON(t *testing.T) {
	cfg, err := ParseTargetConfig(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
}

func TestParseTargetConfig_TokenNeverMarshaled(t *testing.T) {
	// Token is json:"-" — even if someone tried to set it via JSON, parser
	// must ignore it. Clear env vars so the env-resolution step doesn't mask
	// the json-ignore property we're verifying.
	t.Setenv("PAGERDUTY_TOKEN", "")
	prevFile := DefaultTokenFile
	DefaultTokenFile = "/nonexistent/path/for/test"
	t.Cleanup(func() { DefaultTokenFile = prevFile })

	cfg, err := ParseTargetConfig([]byte(`{"Type":"PAGERDUTY","Token":"sneaky"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Token != "" {
		t.Errorf("Token must never be parsed from Target Config JSON; got %q", cfg.Token)
	}
}

func TestResolveToken_EnvVarWins(t *testing.T) {
	t.Setenv("PAGERDUTY_TOKEN", "from-env")
	got := resolveToken("")
	if got != "from-env" {
		t.Errorf("want from-env, got %q", got)
	}
}

func TestResolveToken_FileFallback(t *testing.T) {
	t.Setenv("PAGERDUTY_TOKEN", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := resolveToken(path)
	if got != "from-file" {
		t.Errorf("want from-file (trimmed), got %q", got)
	}
}

func TestResolveToken_FileMissingReturnsEmpty(t *testing.T) {
	t.Setenv("PAGERDUTY_TOKEN", "")
	got := resolveToken("/nonexistent/path/to/nowhere")
	if got != "" {
		t.Errorf("want empty, got %q", got)
	}
}

func TestValidate_RequiresToken(t *testing.T) {
	cfg := &Config{Type: "PAGERDUTY"}
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error when token is empty")
	}
}

func TestValidate_OKWhenTokenPresent(t *testing.T) {
	cfg := &Config{Type: "PAGERDUTY", Token: "anything"}
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
