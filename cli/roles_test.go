package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/registerdomaincli/cli/apitypes"
)

// newRoleTestServer answers /v1/quote and /v1/orders like a real backend and
// captures the CreateOrderRequest body for assertions.
func newRoleTestServer(t *testing.T, captured *apitypes.CreateOrderRequest) *httptest.Server {
	t.Helper()
	return fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/quote":
			_ = json.NewEncoder(w).Encode(apitypes.Quote{
				Domain: "example.com", Years: 1, Kind: "registration",
				NetCents: 1150, Currency: "EUR",
			})
		case r.URL.Path == "/v1/orders":
			_ = json.NewDecoder(r.Body).Decode(captured)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(apitypes.Order{
				ID: "ord-roles", Kind: "registration", Domain: "example.com",
				Status: "pending_payment", CheckoutURL: "",
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

// roleRegistrantFlags is the inline registrant every test reuses.
func roleRegistrantFlags() []string {
	return []string{
		"--first-name", "A", "--last-name", "B",
		"--email", "a@example.com", "--phone", "+34.600000000",
		"--address1", "Street 1", "--city", "BCN", "--zip", "08001",
		"--country", "es",
	}
}

// TestRegisterRoleFlagsWire checks that --role-admin builds a full admin
// contact derived from the inline registrant and that unspecified roles stay
// nil on the wire — the server keeps them on the registrant.
func TestRegisterRoleFlagsWire(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("SSH_CONNECTION", "1")
	var got apitypes.CreateOrderRequest
	s := newRoleTestServer(t, &got)
	args := []string{
		"--server", s.URL, "--token", "dcl_x", "--yes",
		"register", "example.com",
	}
	args = append(args, roleRegistrantFlags()...)
	args = append(args, "--no-wait", "--role-admin", "email=tech@example.com,first-name=Tech")
	out := runRoot(t, args, "")
	if got.RoleContacts == nil || got.RoleContacts.Admin == nil {
		t.Fatalf("roleContacts.admin missing: %+v", got.RoleContacts)
	}
	if got.RoleContacts.Admin.Email != "tech@example.com" || got.RoleContacts.Admin.FirstName != "Tech" {
		t.Fatalf("admin contact = %+v", got.RoleContacts.Admin)
	}
	// Derived-from-registrant invariant: untouched fields ride along.
	if got.RoleContacts.Admin.LastName != "B" || got.RoleContacts.Admin.City != "BCN" ||
		got.RoleContacts.Admin.Country != "ES" {
		t.Fatalf("admin contact did not inherit registrant fields: %+v", got.RoleContacts.Admin)
	}
	if got.RoleContacts.Tech != nil || got.RoleContacts.Billing != nil {
		t.Fatalf("unspecified roles must stay nil: %+v", got.RoleContacts)
	}
	if !strings.Contains(out, "ord-roles") {
		t.Errorf("order id missing from output:\n%s", out)
	}
}

// TestRegisterRolesRegistrantSpecPinsLegacyWire pins that "registrant"/"auto"
// specs and NO role flags at all produce a wire payload identical to the
// pre-roles shape: roleContacts absent entirely.
func TestRegisterRolesRegistrantSpecPinsLegacyWire(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("SSH_CONNECTION", "1")
	for _, extra := range [][]string{
		nil,
		{"--role-admin", "registrant"},
		{"--role", "tech=auto"},
	} {
		var got apitypes.CreateOrderRequest
		s := newRoleTestServer(t, &got)
		args := []string{
			"--server", s.URL, "--token", "dcl_x", "--yes",
			"register", "example.com",
		}
		args = append(args, roleRegistrantFlags()...)
		args = append(args, "--no-wait")
		args = append(args, extra...)
		runRoot(t, args, "")
		if got.RoleContacts != nil {
			t.Fatalf("extra=%v: roleContacts must be absent on the wire, got %+v", extra, got.RoleContacts)
		}
	}
}

// TestRegisterRoleSpecValidation pins the parametric failure modes: unknown
// role, unknown field, empty spec, duplicate role.
func TestRegisterRoleSpecValidation(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	var sink apitypes.CreateOrderRequest
	srv := newRoleTestServer(t, &sink)
	base := []string{"--server", srv.URL, "--token", "dcl_x", "--yes", "register", "example.com"}
	base = append(base, roleRegistrantFlags()...)
	base = append(base, "--no-wait")
	for _, tc := range []struct{ flag, spec, want string }{
		{"--role", "owner=auto", "unknown WHOIS role"},
		{"--role-admin", "email=x@y.z,not-a-field=1", "invalid admin contact spec"},
		{"--role", "admin=", "invalid admin contact spec"},
		{"--role-admin", "", "invalid admin contact spec"},
		{"--role-tech", "   ", "invalid tech contact spec"},
	} {
		args := append(append([]string{}, base...), tc.flag, tc.spec)
		out := runRootErr(t, args)
		if !strings.Contains(out, tc.want) {
			t.Errorf("%s %q: want %q in:\n%s", tc.flag, tc.spec, tc.want, out)
		}
	}
	args := append(append([]string{}, base...), "--role", "admin=auto", "--role-admin", "auto")
	out := runRootErr(t, args)
	if !strings.Contains(out, `role "admin" given twice`) {
		t.Errorf("duplicate role not rejected:\n%s", out)
	}
}

// TestRegisterRoleSpecNeedsInlineRegistrant pins that a field spec without
// an inline registrant (contact-set path) fails instead of silently building
// an empty contact.
func TestRegisterRoleSpecNeedsInlineRegistrant(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	var got apitypes.CreateOrderRequest
	s := newRoleTestServer(t, &got)
	s.Config.Handler = withContactSets(s.Config.Handler)
	args := []string{
		"--server", s.URL, "--token", "dcl_x", "--yes",
		"register", "example.com",
		"--contact", "set-with-roles",
		"--no-wait", "--role-billing", "email=billing@example.com",
	}
	out := runRootErr(t, args)
	if !strings.Contains(out, "needs the inline registrant") {
		t.Fatalf("expected no-base error, got:\n%s", out)
	}
}

// withContactSets decorates a handler so GET /v1/contact-sets answers with
// one non-default set labeled "set-with-roles" carrying a standing tech role.
func withContactSets(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/contact-sets" {
			_ = json.NewEncoder(w).Encode(apitypes.ContactSetsResponse{ContactSets: []apitypes.ContactSet{{
				ID:         "cs-roles",
				Label:      "set-with-roles",
				Registrant: apitypes.Registrant{FirstName: "S", LastName: "T", Email: "s@example.com"},
				Roles: &apitypes.RoleContacts{
					Tech: &apitypes.Registrant{FirstName: "Tech", LastName: "Set", Email: "tech@example.com"},
				},
			}}})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// TestContactsShowRoles renders the role summary of an expanded set and
// checks legacy sets (roles absent) print nothing extra.
func TestContactsShowRoles(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/contact-sets" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(apitypes.ContactSetsResponse{ContactSets: []apitypes.ContactSet{
			{
				ID: "cs1", Label: "expanded", IsDefault: true,
				Registrant: apitypes.Registrant{FirstName: "A", LastName: "B", Email: "a@example.com"},
				Roles: &apitypes.RoleContacts{
					Admin: &apitypes.Registrant{FirstName: "Ad", LastName: "Min", Email: "admin@example.com"},
				},
			},
			{
				ID: "cs2", Label: "legacy",
				Registrant: apitypes.Registrant{FirstName: "C", LastName: "D", Email: "c@example.com"},
			},
		}})
	})
	out := runRoot(t, []string{"--server", s.URL, "--token", "dcl_x", "contacts", "show", "expanded"}, "")
	if !strings.Contains(out, "role admin:  Ad Min <admin@example.com>") {
		t.Errorf("expanded admin role missing:\n%s", out)
	}
	if !strings.Contains(out, "role tech:  same as registrant") || !strings.Contains(out, "role billing:  same as registrant") {
		t.Errorf("inherit line missing:\n%s", out)
	}
	out = runRoot(t, []string{"--server", s.URL, "--token", "dcl_x", "contacts", "show", "legacy"}, "")
	if strings.Contains(out, "role admin") {
		t.Errorf("legacy set must not print roles:\n%s", out)
	}
}

// TestRegisterInheritsSetRolesOnWire pins saved-role inheritance: an order
// via --contact <label> with NO role flags carries the set's standing tech
// role onto the wire untouched (REPLACE semantics with nothing to replace).
func TestRegisterInheritsSetRolesOnWire(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("SSH_CONNECTION", "1")
	var got apitypes.CreateOrderRequest
	s := newRoleTestServer(t, &got)
	s.Config.Handler = withContactSets(s.Config.Handler)
	args := []string{
		"--server", s.URL, "--token", "dcl_x", "--yes",
		"register", "example.com",
		"--contact", "set-with-roles",
		"--no-wait",
	}
	runRoot(t, args, "")
	if got.RoleContacts == nil || got.RoleContacts.Tech == nil || got.RoleContacts.Tech.Email != "tech@example.com" {
		t.Fatalf("order roleContacts = %+v, want the set's tech role on the wire", got.RoleContacts)
	}
	if got.RoleContacts.Admin != nil || got.RoleContacts.Billing != nil {
		t.Fatalf("other roles must stay nil (inherit registrant): %+v", got.RoleContacts)
	}
}

// TestRegisterAutoDiscardsSetRoles pins flag precedence: with a saved set
// carrying roles, an explicit --role-admin registrant spec REPLACES the
// whole bundle — the set's tech must NOT ride along, and the auto/registrant
// spec produces no admin contact (roleContacts absent = legacy shape).
func TestRegisterAutoDiscardsSetRoles(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	t.Setenv("SSH_CONNECTION", "1")
	var got apitypes.CreateOrderRequest
	s := newRoleTestServer(t, &got)
	s.Config.Handler = withContactSets(s.Config.Handler)
	args := []string{
		"--server", s.URL, "--token", "dcl_x", "--yes",
		"register", "example.com",
		"--contact", "set-with-roles",
		"--no-wait", "--role-admin", "registrant",
	}
	runRoot(t, args, "")
	if got.RoleContacts != nil {
		t.Fatalf("explicit registrant spec must discard the saved bundle, got %+v", got.RoleContacts)
	}
}
