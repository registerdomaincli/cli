package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/registerdomaincli/cli/apitypes"
)

// TestRegisterDiscountQuoteRowAndOrderCode runs a fully parametric register
// with --discount: the quote carries an accepted DiscountApp, so the human
// confirm output shows the discount row, and the created order echoes the
// code in CreateOrderRequest.DiscountCode for the server to revalidate.
func TestRegisterDiscountQuoteRowAndOrderCode(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("SSH_CONNECTION", "1")
	var gotOrder apitypes.CreateOrderRequest
	var gotQuoteDiscount atomic.Bool
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/quote":
			if r.URL.Query().Get("discount") != "LAUNCH10" {
				t.Errorf("quote discount param = %q", r.URL.Query().Get("discount"))
			}
			gotQuoteDiscount.Store(true)
			_ = json.NewEncoder(w).Encode(apitypes.Quote{
				Domain: "example.com", Years: 1, Kind: "registration",
				NetCents: 1000, TaxCents: 210, TotalCents: 1210, Currency: "EUR",
				Discount: &apitypes.DiscountApp{
					Code: "LAUNCH10", Percent: "10.00", AmountCents: 100, Currency: "EUR",
				},
			})
		case r.URL.Path == "/v1/orders":
			_ = json.NewDecoder(r.Body).Decode(&gotOrder)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(apitypes.Order{
				ID: "ord-d1", Kind: "registration", Domain: "example.com",
				Status: "pending_payment", CheckoutURL: "http://checkout.example/d1",
				Discount: &apitypes.DiscountApp{Code: "LAUNCH10", AmountCents: 100, Currency: "EUR"},
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	out := runRoot(t, []string{
		"--server", s.URL, "--token", "dcl_x", "--yes",
		"register", "example.com", "--discount", "LAUNCH10",
		"--first-name", "A", "--last-name", "B",
		"--email", "agent@example.com", "--phone", "+34.600000000",
		"--address1", "Street 1", "--city", "BCN", "--zip", "08001",
		"--country", "es", "--no-wait",
	}, "")
	if !gotQuoteDiscount.Load() {
		t.Fatal("quote endpoint never called")
	}
	if !strings.Contains(out, "Discount LAUNCH10: −€1.00") {
		t.Errorf("discount row missing:\n%s", out)
	}
	if gotOrder.DiscountCode != "LAUNCH10" {
		t.Errorf("order DiscountCode = %q, want LAUNCH10", gotOrder.DiscountCode)
	}
	if !strings.Contains(out, "ord-d1") {
		t.Errorf("order id missing from output:\n%s", out)
	}
}

// TestRegisterDiscountErrorSurfacesVerbatim pins the error channel: a 422
// discount_* APIError reaches the user as the server's own message, with no
// localization wrapper and no key-miss fallback text.
func TestRegisterDiscountErrorSurfacesVerbatim(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/quote":
			if r.URL.Query().Get("discount") != "GONE" {
				t.Errorf("quote discount param = %q", r.URL.Query().Get("discount"))
			}
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(apitypes.APIError{
				Error: "Unknown or expired discount code.",
				Code:  "discount_invalid",
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	out := runRootErr(t, []string{
		"--server", s.URL, "--token", "dcl_x",
		"register", "example.com", "--discount", "GONE", "--dry-run",
	})
	if !strings.Contains(out, "Unknown or expired discount code.") {
		t.Errorf("verbatim server message missing:\n%s", out)
	}
	if strings.Contains(out, "quote.discount") {
		t.Errorf("i18n key leaked into output (key-miss fallback):\n%s", out)
	}
}
