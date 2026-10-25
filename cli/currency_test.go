package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/registerdomaincli/cli/apitypes"
)

// TestSearchCurrencyFlow covers the multi-currency contract end to end: the
// requested currency travels to the server, and the response's own currency —
// not the CLI's guess — drives the rendered symbols and headers.
func TestSearchCurrencyFlow(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")

	var gotReq atomic.Value
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/domains/search" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var req apitypes.SearchRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotReq.Store(req.Currency) // may be ""
		_ = json.NewEncoder(w).Encode(apitypes.SearchResponse{
			Results: []apitypes.SearchResult{
				{
					Name:          "acme.com",
					Purchasable:   true,
					PurchasePrice: new(12.99),
					RenewalPrice:  new(19.99),
					Currency:      "usd",
				},
			},
		})
	})

	out := runRoot(t, []string{"--server", s.URL, "--token", "dcl_x", "--currency", "usd", "search", "acme"}, "")

	if got := gotReq.Load(); got != "usd" {
		t.Errorf("server saw currency %v, want usd", got)
	}
	// The response currency (usd) drives the symbols — $, not €.
	if !strings.Contains(out, "$12.99") || !strings.Contains(out, "$19.99") {
		t.Errorf("output missing $-priced rows:\n%s", out)
	}
	if !strings.Contains(out, "(USD)") {
		t.Errorf("header should name the currency:\n%s", out)
	}
}

// TestSearchCurrencyDefaultServer: without --currency the request omits the
// field and the server's currency is still displayed.
func TestSearchCurrencyDefaultServer(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")

	var gotReq atomic.Value
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req apitypes.SearchRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotReq.Store(req.Currency)
		_ = json.NewEncoder(w).Encode(apitypes.SearchResponse{
			Results: []apitypes.SearchResult{
				{Name: "acme.com", Purchasable: true, PurchasePrice: new(12.99), RenewalPrice: new(19.99), Currency: "eur"},
			},
		})
	})

	out := runRoot(t, []string{"--server", s.URL, "--token", "dcl_x", "search", "acme"}, "")

	if got := gotReq.Load(); got != "" {
		t.Errorf("server saw currency %q, want empty", got)
	}
	if !strings.Contains(out, "€12.99") {
		t.Errorf("output missing €-priced rows:\n%s", out)
	}
	if !strings.Contains(out, "(EUR)") {
		t.Errorf("header should name the currency:\n%s", out)
	}
}

// TestResolveCurrencyPrecedence mirrors the config precedence rule.
func TestResolveCurrencyPrecedence(t *testing.T) {
	t.Setenv("DOMAIN_CLI_CURRENCY", "")
	cases := []struct {
		name string
		flag string
		env  string
		cfg  string
		want string
	}{
		{"flag wins", "usd", "eur", "gbp", "usd"},
		{"env next", "", "eur", "gbp", "eur"},
		{"config last", "", "", "gbp", "gbp"},
		{"empty default", "", "", "", ""},
		{"lowercased", "USD", "", "", "usd"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("DOMAIN_CLI_CURRENCY", c.env)
			if got := ResolveCurrency(c.flag, Config{Currency: c.cfg}); got != c.want {
				t.Errorf("ResolveCurrency(%q, cfg %q) = %q, want %q", c.flag, c.cfg, got, c.want)
			}
		})
	}
}
