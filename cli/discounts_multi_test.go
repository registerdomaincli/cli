package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/registerdomaincli/cli/apitypes"
)

// discountMultiServer stubs the batch-quote + order endpoints for a
// multi-domain order with a discount code. Per-domain quote rows are
// UNDISCOUNTED (1364 + 1525 = 2889 gross); the order-level response carries
// the 10% discount (288 off) with VAT on the discounted base (total 3149).
func discountMultiServer(t *testing.T, gotQuotes *apitypes.QuoteRequest, gotOrder *apitypes.CreateOrderRequest) string {
	t.Helper()
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/quotes":
			_ = json.NewDecoder(r.Body).Decode(gotQuotes)
			_ = json.NewEncoder(w).Encode(apitypes.QuoteResponse{
				Quotes: []apitypes.Quote{
					{Domain: "acme.com", Years: 1, Kind: "registration", NetCents: 1364, TotalCents: 1600, Currency: "usd"},
					{Domain: "acme.net", Years: 1, Kind: "registration", NetCents: 1525, TotalCents: 1800, Currency: "usd"},
				},
				NetCents: 2889 - 288, TaxCents: 548, TotalCents: 3149, Currency: "usd",
				Discount: &apitypes.DiscountApp{
					Code: "BUNDLE10", Percent: "10.00", AmountCents: 288, Currency: "usd",
				},
			})
		case r.URL.Path == "/v1/orders":
			_ = json.NewDecoder(r.Body).Decode(gotOrder)
			_ = json.NewEncoder(w).Encode(apitypes.Order{
				ID: "ord-m1", Kind: "registration",
				NetCents: 2889 - 288, TaxCents: 548, TotalCents: 3149, Currency: "usd",
				Status:      "pending_payment",
				CheckoutURL: "https://checkout.example/pay",
				Discount:    &apitypes.DiscountApp{Code: "BUNDLE10", AmountCents: 288, Currency: "usd"},
				Items: []apitypes.OrderItem{
					{Domain: "acme.com", Years: 1, Status: "pending"},
					{Domain: "acme.net", Years: 1, Status: "pending"},
				},
			})
		case r.URL.Path == "/v1/contact-sets" || r.URL.Path == "/v1/me":
			_ = json.NewEncoder(w).Encode(apitypes.ContactSetsResponse{})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	return s.URL
}

const discountMultiArgs = "--server %s --token dcl_x --yes --no-wait register acme.com acme.net --discount BUNDLE10 --first-name T --last-name U --email t@u.io --phone +34000000000 --address1 X --city Y --zip Z --country ES"

// TestRegisterMultiDiscountQuoteAndOrderJSON pins the machine contract: with
// --json the code reaches BOTH the batch quote (discountCode) and the order
// (DiscountCode), and the echoed order carries the discounted totals while
// the per-domain items keep their undiscounted nets.
func TestRegisterMultiDiscountQuoteAndOrderJSON(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("SSH_CONNECTION", "1")
	var gotQuotes apitypes.QuoteRequest
	var gotOrder apitypes.CreateOrderRequest
	s := discountMultiServer(t, &gotQuotes, &gotOrder)

	out := runRoot(t, []string{
		"--server", s, "--token", "dcl_x", "--yes", "--no-wait", "--json",
		"register", "acme.com", "acme.net",
		"--discount", "BUNDLE10",
		"--first-name", "T", "--last-name", "U", "--email", "t@u.io",
		"--phone", "+34000000000", "--address1", "X", "--city", "Y",
		"--zip", "Z", "--country", "ES",
	}, "")

	if gotQuotes.DiscountCode != "BUNDLE10" {
		t.Errorf("batch quote DiscountCode = %q, want BUNDLE10", gotQuotes.DiscountCode)
	}
	if len(gotQuotes.Domains) != 2 {
		t.Errorf("batch quote must carry both domains: %+v", gotQuotes.Domains)
	}
	if gotOrder.DiscountCode != "BUNDLE10" {
		t.Errorf("order DiscountCode = %q, want BUNDLE10", gotOrder.DiscountCode)
	}
	var ord apitypes.Order
	if err := json.Unmarshal([]byte(lastJSONBlock(out)), &ord); err != nil {
		t.Fatalf("json output unparsable: %v\n%s", err, out)
	}
	if ord.Discount == nil || ord.Discount.Code != "BUNDLE10" || ord.Discount.AmountCents != 288 {
		t.Errorf("order discount echo wrong: %+v", ord.Discount)
	}
	if ord.NetCents != 2601 || ord.TotalCents != 3149 {
		t.Errorf("discounted totals = net %d total %d, want 2601/3149", ord.NetCents, ord.TotalCents)
	}
}

// TestRegisterMultiDiscountRender pins the human confirm rendering: ONE
// order-level discount row with the server-taken amount, per-domain rows
// undiscounted, and the discounted total on the confirmation line. Uses
// --dry-run (no order created) — the confirmation text is identical to the
// pre-checkout screen an order run shows.
func TestRegisterMultiDiscountRender(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	var ordersCalled bool
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/quotes":
			_ = json.NewEncoder(w).Encode(apitypes.QuoteResponse{
				Quotes: []apitypes.Quote{
					{Domain: "acme.com", Years: 1, Kind: "registration", NetCents: 1364, TotalCents: 1600, Currency: "usd"},
					{Domain: "acme.net", Years: 1, Kind: "registration", NetCents: 1525, TotalCents: 1800, Currency: "usd"},
				},
				NetCents: 2889 - 288, TaxCents: 548, TotalCents: 3149, Currency: "usd",
				Discount: &apitypes.DiscountApp{
					Code: "BUNDLE10", Percent: "10.00", AmountCents: 288, Currency: "usd",
				},
			})
		case r.URL.Path == "/v1/orders":
			ordersCalled = true
			_ = json.NewEncoder(w).Encode(apitypes.Order{ID: "o1"})
		case r.URL.Path == "/v1/contact-sets" || r.URL.Path == "/v1/me":
			_ = json.NewEncoder(w).Encode(apitypes.ContactSetsResponse{})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	out := runRoot(t, []string{
		"--server", s.URL, "--token", "dcl_x",
		"register", "acme.com", "acme.net", "--dry-run", "--discount", "BUNDLE10",
	}, "")

	if n := strings.Count(out, "Discount BUNDLE10"); n != 1 {
		t.Errorf("discount row count = %d, want 1:\n%s", n, out)
	}
	if !strings.Contains(out, "$2.88") {
		t.Errorf("discount amount (288 cents) missing:\n%s", out)
	}
	if !strings.Contains(out, "$13.64") || !strings.Contains(out, "$15.25") {
		t.Errorf("per-domain undiscounted amounts missing:\n%s", out)
	}
	if !strings.Contains(out, "$31.49") {
		t.Errorf("discounted total missing:\n%s", out)
	}
	if ordersCalled {
		t.Error("--dry-run must not create an order")
	}
}
