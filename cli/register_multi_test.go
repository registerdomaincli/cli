package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/registerdomaincli/cli/apitypes"
)

// TestRegisterMultiDomainsOneOrder drives `register d1 d2` against a fake
// server: one batch quote, one order carrying both domains as items, one
// checkout URL.
func TestRegisterMultiDomainsOneOrder(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("SSH_CONNECTION", "1")

	var gotQuotes apitypes.QuoteRequest
	var gotOrder apitypes.CreateOrderRequest
	quoteCalls := 0
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/quotes":
			quoteCalls++
			_ = json.NewDecoder(r.Body).Decode(&gotQuotes)
			_ = json.NewEncoder(w).Encode(apitypes.QuoteResponse{
				Quotes: []apitypes.Quote{
					{Domain: "acme.com", Years: 1, Kind: "registration", NetCents: 1364, TotalCents: 1600, Currency: "usd"},
					{Domain: "acme.net", Years: 1, Kind: "registration", NetCents: 1525, TotalCents: 1800, Currency: "usd"},
				},
				NetCents: 2889, TaxCents: 611, TotalCents: 3500, Currency: "usd",
			})
		case r.URL.Path == "/v1/orders":
			_ = json.NewDecoder(r.Body).Decode(&gotOrder)
			ord := apitypes.Order{
				ID: "ord1", Kind: "registration", Domain: "acme.com",
				NetCents: 2889, TaxCents: 611, TotalCents: 3500, Currency: "usd",
				Status:      "pending_payment",
				CheckoutURL: "https://checkout.example/pay",
				Items: []apitypes.OrderItem{
					{Domain: "acme.com", Years: 1, Status: "pending"},
					{Domain: "acme.net", Years: 1, Status: "pending"},
				},
			}
			_ = json.NewEncoder(w).Encode(ord)
		case r.URL.Path == "/v1/contact-sets" || r.URL.Path == "/v1/me":
			_ = json.NewEncoder(w).Encode(apitypes.ContactSetsResponse{})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	out := runRoot(t, []string{
		"--server", s.URL, "--token", "dcl_x", "--yes", "--no-wait", "--json",
		"register", "acme.com", "acme.net",
		"--first-name", "T", "--last-name", "U", "--email", "t@u.io",
		"--phone", "+34000000000", "--address1", "X", "--city", "Y",
		"--zip", "Z", "--country", "ES",
	}, "")

	if quoteCalls != 1 {
		t.Errorf("batch quote called %d times, want 1", quoteCalls)
	}
	if len(gotQuotes.Domains) != 2 || gotQuotes.Domains[0].Domain != "acme.com" {
		t.Errorf("batch quote domains mismatch: %+v", gotQuotes.Domains)
	}
	if len(gotOrder.Items) != 2 || gotOrder.Items[1].Domain != "acme.net" {
		t.Errorf("order must carry 2 items: %+v", gotOrder.Items)
	}
	if gotOrder.Domain != "" {
		t.Errorf("multi order must not set the legacy domain field: %q", gotOrder.Domain)
	}
	var ord apitypes.Order
	if err := json.Unmarshal([]byte(lastJSONBlock(out)), &ord); err != nil {
		t.Fatalf("json output unparsable: %v\n%s", err, out)
	}
	if len(ord.Items) != 2 || ord.CheckoutURL == "" {
		t.Errorf("order output missing items/checkout: %+v", ord)
	}
}

// TestRegisterSingleDomainKeepsLegacyShape asserts one-domain register
// still sends domain/years, no items.
func TestRegisterSingleDomainKeepsLegacyShape(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("SSH_CONNECTION", "1")

	var gotOrder apitypes.CreateOrderRequest
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/quote":
			_ = json.NewEncoder(w).Encode(apitypes.Quote{
				Domain: "acme.com", Years: 1, Kind: "registration",
				NetCents: 1364, TaxCents: 289, TotalCents: 1653, Currency: "usd",
			})
		case r.URL.Path == "/v1/orders":
			_ = json.NewDecoder(r.Body).Decode(&gotOrder)
			_ = json.NewEncoder(w).Encode(apitypes.Order{
				ID: "ord1", Kind: "registration", Domain: "acme.com",
				Status: "pending_payment", CheckoutURL: "https://checkout.example/pay",
			})
		case r.URL.Path == "/v1/contact-sets" || r.URL.Path == "/v1/me":
			_ = json.NewEncoder(w).Encode(apitypes.ContactSetsResponse{})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	runRoot(t, []string{
		"--server", s.URL, "--token", "dcl_x", "--yes", "--no-wait", "--json",
		"register", "acme.com",
		"--first-name", "T", "--last-name", "U", "--email", "t@u.io",
		"--phone", "+34000000000", "--address1", "X", "--city", "Y",
		"--zip", "Z", "--country", "ES",
	}, "")

	if gotOrder.Domain != "acme.com" || len(gotOrder.Items) != 0 {
		t.Errorf("single register must keep the legacy shape: domain=%q items=%v",
			gotOrder.Domain, gotOrder.Items)
	}
}

// TestOrdersTableShowsMultiDomains renders the orders table for a
// multi-domain order and asserts every domain appears in the domain column.
func TestOrdersTableShowsMultiDomains(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/orders" {
			_ = json.NewEncoder(w).Encode(apitypes.OrdersResponse{Orders: []apitypes.Order{{
				ID: "ord9", Kind: "registration", Status: "pending_payment",
				Items: []apitypes.OrderItem{
					{Domain: "a.com"}, {Domain: "b.com"},
				},
			}}})
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	})

	out := runRoot(t, []string{"--server", s.URL, "--token", "dcl_x", "orders"}, "")
	if !strings.Contains(out, "a.com") || !strings.Contains(out, "b.com") {
		t.Errorf("orders table missing multi domains:\n%s", out)
	}
}

// lastJSONBlock returns the last {...} JSON object printed by emitJSON
// (encoding/json MarshalIndent output starts at column 0 on its own line).
func lastJSONBlock(s string) string {
	start := strings.LastIndex(s, "\n{")
	if start < 0 {
		return s
	}
	return s[start+1:]
}
