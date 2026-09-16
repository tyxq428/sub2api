package proxyurl

import (
	"strings"
	"testing"
)

// All markers are synthetic; error messages must never contain URL userinfo,
// the raw URL, or query values, including when net/url rejects the input.
func TestParse_ErrorsDoNotExposeCredentials(t *testing.T) {
	tests := []struct{ name, raw string }{
		{"invalid_escape", "http://probe_user:probe_password@proxy.invalid/%zz?token=probe_token"},
		{"invalid_port", "http://probe_user:probe_password@proxy.invalid:invalid?token=probe_token"},
		{"control_character", "http://probe_user:probe_password@proxy.invalid/\n?token=probe_token"},
		{"invalid_ipv6", "http://probe_user:probe_password@[::1?token=probe_token"},
		{"missing_host", "http://probe_user:probe_password@:0/?token=probe_token"},
		{"unsupported_scheme", "ftp://probe_user:probe_password@proxy.invalid/?token=probe_token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized, parsed, err := Parse(tt.raw)
			if err == nil {
				t.Fatal("expected invalid proxy to fail")
			}
			if normalized != "" || parsed != nil {
				t.Fatal("invalid proxy must not return a usable URL")
			}
			for _, marker := range []string{tt.raw, "probe_user", "probe_password", "probe_token"} {
				if strings.Contains(err.Error(), marker) {
					t.Error("error exposes a synthetic credential marker")
				}
			}
		})
	}
}
