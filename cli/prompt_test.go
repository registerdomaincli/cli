package cli

import "testing"

// TestValidExternalURL pins the opener's scheme policy. A hostile or
// MITM'd server chooses Order.CheckoutURL, Order.InvoiceURL and Portal.URL,
// so anything but https — plus plain http against a loopback dev server —
// must never reach the OS handler.
func TestValidExternalURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"https", "https://checkout.stripe.com/c/pay/cs_test_123", true},
		{"https with query", "https://registerdomaincli.com/legal/terms?from=cli&page=1", true},
		{"uppercase scheme", "HTTPS://registerdomaincli.com/legal/terms", true},
		{"dev server localhost", "http://localhost:8080/legal/terms", true},
		{"dev server 127.0.0.1", "http://127.0.0.1:8080/x", true},
		{"dev server ipv6 loopback", "http://[::1]:8080/x", true},
		{"remote http", "http://evil.example/checkout", false},
		{"file scheme", "file:///home/user/.config/domain-cli/config.json", false},
		{"smb scheme", "smb://attacker.example/share", false},
		{"custom scheme", "javascript:alert(1)", false},
		{"mailto scheme", "mailto:someone@example.com", false},
		{"userinfo", "https://user:pass@evil.example/checkout", false},
		{"missing host", "https://", false},
		{"empty", "", false},
		{"relative path", "/legal/terms", false},
		{"opaque", "https:evil.example", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := validExternalURL(tc.url); got != tc.want {
				t.Errorf("validExternalURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}
