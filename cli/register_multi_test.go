package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
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

// TestRegisterMultiUsesQuotedYears asserts the order carries the SERVER's
func TestRegisterMultiUsesQuotedYears(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("SSH_CONNECTION", "1")

	var gotOrder apitypes.CreateOrderRequest
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/quotes":
			_ = json.NewEncoder(w).Encode(apitypes.QuoteResponse{
				Quotes: []apitypes.Quote{
					{Domain: "q1.com", Years: 2, NetCents: 2728, TotalCents: 3200, Currency: "usd"},
					{Domain: "q2.com", Years: 1, NetCents: 1364, TotalCents: 1600, Currency: "usd"},
				},
				NetCents: 4092, TaxCents: 708, TotalCents: 4800, Currency: "usd",
			})
		case "/v1/orders":
			_ = json.NewDecoder(r.Body).Decode(&gotOrder)
			_ = json.NewEncoder(w).Encode(apitypes.Order{
				ID: "o", Status: "pending_payment", CheckoutURL: "https://x",
			})
		case "/v1/contact-sets", "/v1/me":
			_ = json.NewEncoder(w).Encode(apitypes.ContactSetsResponse{})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	runRoot(t, []string{
		"--server", s.URL, "--token", "dcl_x", "--yes", "--no-wait", "--json",
		"register", "q1.com", "q2.com", "--years", "5",
		"--first-name", "T", "--last-name", "U", "--email", "t@u.io",
		"--phone", "+34000000000", "--address1", "X", "--city", "Y",
		"--zip", "Z", "--country", "ES",
	}, "")

	if gotOrder.Items[0].Years != 2 || gotOrder.Items[1].Years != 1 {
		t.Errorf("order must carry quoted years (2,1), got (%d,%d)",
			gotOrder.Items[0].Years, gotOrder.Items[1].Years)
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

// TestRegisterMultiDryRunShowsTotals exercises the human (non-JSON) confirm
// output: per-domain rows with quoted terms, the aggregate net/total, and
// that --dry-run never creates an order.
func TestRegisterMultiDryRunShowsTotals(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("SSH_CONNECTION", "1")
	var ordersCalled atomic.Bool
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/quotes":
			_ = json.NewEncoder(w).Encode(apitypes.QuoteResponse{
				Quotes: []apitypes.Quote{
					{Domain: "d1.com", Years: 2, NetCents: 2728, TotalCents: 3200, Currency: "EUR"},
					{Domain: "d2.com", Years: 1, NetCents: 1181, TotalCents: 1400, Currency: "EUR"},
				},
				NetCents: 3909, TaxCents: 821, TotalCents: 4730, Currency: "EUR",
			})
		case "/v1/orders":
			ordersCalled.Store(true)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(apitypes.Order{ID: "o1"})
		case "/v1/contact-sets", "/v1/me":
			_ = json.NewEncoder(w).Encode(apitypes.ContactSetsResponse{})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	out := runRoot(t, []string{
		"--server", s.URL, "--token", "dcl_x",
		"register", "d1.com", "d2.com", "--dry-run",
	}, "")
	if !strings.Contains(out, "d1.com, 2 yr — €27.28") {
		t.Errorf("first quoted row missing or wrong:\n%s", out)
	}
	if !strings.Contains(out, "d2.com, 1 yr — €11.81") {
		t.Errorf("second quoted row missing or wrong:\n%s", out)
	}
	if !strings.Contains(out, "2 domains — €39.09 + VAT = €47.30") {
		t.Errorf("aggregate confirmation line missing or wrong:\n%s", out)
	}
	if !strings.Contains(out, "Dry run: no order was created.") {
		t.Errorf("dry-run marker missing:\n%s", out)
	}
	if ordersCalled.Load() {
		t.Error("--dry-run must not create an order")
	}
}

// TestRegisterMultiRejectsIncompleteQuote covers the AvN gate: a truncated
// batch quote (fewer rows than requested) aborts before --yes and creates
// no order.
func TestRegisterMultiRejectsIncompleteQuote(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("SSH_CONNECTION", "1")
	var ordersCalled atomic.Bool
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/quotes":
			// Two domains requested, server answers with ONE row.
			_ = json.NewEncoder(w).Encode(apitypes.QuoteResponse{
				Quotes: []apitypes.Quote{
					{Domain: "q1.com", Years: 1, NetCents: 1364, TotalCents: 1600, Currency: "usd"},
				},
				NetCents: 1364, TaxCents: 300, TotalCents: 1664, Currency: "usd",
			})
		case "/v1/orders":
			ordersCalled.Store(true)
			_ = json.NewEncoder(w).Encode(apitypes.Order{ID: "o"})
		case "/v1/contact-sets", "/v1/me":
			_ = json.NewEncoder(w).Encode(apitypes.ContactSetsResponse{})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	out := runRootErr(t, []string{
		"--server", s.URL, "--token", "dcl_x", "--yes", "--no-wait", "--json",
		"register", "q1.com", "q2.com",
		"--first-name", "T", "--last-name", "U", "--email", "t@u.io",
		"--phone", "+34000000000", "--address1", "X", "--city", "Y",
		"--zip", "Z", "--country", "ES",
	})
	if !strings.Contains(out, "could not be quoted") && !strings.Contains(out, "err.quoteIncomplete") {
		t.Errorf("incomplete-quote error missing:\n%s", out)
	}
	if ordersCalled.Load() {
		t.Error("incomplete quote must never create an order")
	}
}

// TestRegisterMultiRejectsMismatchedQuoteRow covers the other half of the
// gate: a same-length quote whose row is for a DIFFERENT domain aborts.
func TestRegisterMultiRejectsMismatchedQuoteRow(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("SSH_CONNECTION", "1")
	var ordersCalled atomic.Bool
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/quotes":
			_ = json.NewEncoder(w).Encode(apitypes.QuoteResponse{
				Quotes: []apitypes.Quote{
					{Domain: "wrong1.com", Years: 1, NetCents: 900, TotalCents: 1100, Currency: "usd"},
					{Domain: "wrong2.com", Years: 1, NetCents: 900, TotalCents: 1100, Currency: "usd"},
				},
				NetCents: 1800, TaxCents: 378, TotalCents: 2178, Currency: "usd",
			})
		case "/v1/orders":
			ordersCalled.Store(true)
			_ = json.NewEncoder(w).Encode(apitypes.Order{ID: "o"})
		case "/v1/contact-sets", "/v1/me":
			_ = json.NewEncoder(w).Encode(apitypes.ContactSetsResponse{})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	out := runRootErr(t, []string{
		"--server", s.URL, "--token", "dcl_x", "--yes", "--no-wait", "--json",
		"register", "m1.com", "m2.com",
		"--first-name", "T", "--last-name", "U", "--email", "t@u.io",
		"--phone", "+34000000000", "--address1", "X", "--city", "Y",
		"--zip", "Z", "--country", "ES",
	})
	if !strings.Contains(out, "could not be quoted") && !strings.Contains(out, "err.quoteIncomplete") {
		t.Errorf("mismatched-quote error missing:\n%s", out)
	}
	if ordersCalled.Load() {
		t.Error("mismatched quote must never create an order")
	}
}

// TestRegisterMultiDryRunJSONPrintsQuote asserts --dry-run --json emits the
// machine-readable QuoteResponse (agents read the per-domain terms/totals
// without polling a human).
func TestRegisterMultiDryRunJSONPrintsQuote(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("SSH_CONNECTION", "1")
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/quotes":
			_ = json.NewEncoder(w).Encode(apitypes.QuoteResponse{
				Quotes: []apitypes.Quote{
					{Domain: "j1.com", Years: 1, NetCents: 1364, TotalCents: 1600, Currency: "usd"},
					{Domain: "j2.com", Years: 1, NetCents: 1525, TotalCents: 1800, Currency: "usd"},
				},
				NetCents: 2889, TaxCents: 611, TotalCents: 3500, Currency: "usd",
			})
		case "/v1/orders", "/v1/contact-sets", "/v1/me":
			t.Errorf("dry-run must not touch %s", r.URL.Path)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	out := runRoot(t, []string{
		"--server", s.URL, "--token", "dcl_x", "register", "j1.com", "j2.com",
		"--dry-run", "--json",
	}, "")
	if !strings.Contains(out, `"j1.com"`) || !strings.Contains(out, `"j2.com"`) {
		t.Errorf("quote rows missing from JSON dry-run:\n%s", out)
	}
	if !strings.Contains(out, `"netCents": 2889`) || !strings.Contains(out, `"totalCents": 3500`) {
		t.Errorf("order-level totals missing:\n%s", out)
	}
}
