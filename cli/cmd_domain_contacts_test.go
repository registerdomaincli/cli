package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/registerdomaincli/cli/apitypes"
)

// domainContactsAPI is the fake /v1/domains/{name}/contacts surface.
type domainContactsAPI struct {
	live    apitypes.DomainContactsResponse
	lastPut *apitypes.UpdateDomainContactsInput
}

func (d *domainContactsAPI) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/domains/probe-edit.example/contacts") {
			t.Errorf("unexpected path %s", r.URL.Path)
			writeJSON404(w)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(d.live)
		case http.MethodPut:
			var in apitypes.UpdateDomainContactsInput
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Errorf("decode PUT: %v", err)
			}
			d.lastPut = &in
			_ = json.NewEncoder(w).Encode(apitypes.DomainContactsResponse{
				Registrant: in.Registrant, RoleContacts: in.RoleContacts,
			})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

func writeJSON404(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": "nf"})
}

// TestDomainsContactsShow checks the read-only view: roles identical to the
// registrant print "same as registrant", overrides print the contact.
func TestDomainsContactsShow(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	api := &domainContactsAPI{
		live: apitypes.DomainContactsResponse{
			Registrant:   registrantFixture("ada@example.com"),
			RoleContacts: &apitypes.RoleContacts{Tech: ptrReg(registrantFixture("tech@example.com"))},
		},
	}
	h := api.handler(t)
	s := fakeServer(t, h.ServeHTTP)
	out := runRoot(t, []string{
		"--server", s.URL, "--token", "dcl_x",
		"domains", "contacts", "probe-edit.example",
	}, "")
	if !strings.Contains(out, "registrant:") || !strings.Contains(out, "ada@example.com") {
		t.Errorf("registrant block missing:\n%s", out)
	}
	if !strings.Contains(out, "role tech:") && !strings.Contains(out, "Tech") && !strings.Contains(out, "tech@example.com") {
		t.Errorf("tech role missing:\n%s", out)
	}
	if strings.Contains(out, "all same as registrant") {
		t.Errorf("all-inherit line must not print when a role is set:\n%s", out)
	}
}

// TestDomainsContactsShowAllInherit pins the nil-roles rendering.
func TestDomainsContactsShowAllInherit(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	api := &domainContactsAPI{live: apitypes.DomainContactsResponse{Registrant: registrantFixture("ada@example.com")}}
	h := api.handler(t)
	s := fakeServer(t, h.ServeHTTP)
	out := runRoot(t, []string{
		"--server", s.URL, "--token", "dcl_x",
		"domains", "contacts", "probe-edit.example",
	}, "")
	if !strings.Contains(out, "all same as registrant") {
		t.Errorf("all-inherit line missing:\n%s", out)
	}
}

// TestDomainsContactsUpdateViaSet checks --contact <label>: the PUT carries
// the set's registrant and its roles; --dry-run never calls PUT.
func TestDomainsContactsUpdateViaSet(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	api := &domainContactsAPI{live: apitypes.DomainContactsResponse{Registrant: registrantFixture("ada@example.com")}}
	h := api.handler(t)
	s := fakeServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/contact-sets" {
			_ = json.NewEncoder(w).Encode(apitypes.ContactSetsResponse{ContactSets: []apitypes.ContactSet{{
				ID: "cs-x", Label: "work", IsDefault: true,
				Registrant: registrantFixture("set@example.com"),
				Roles:      &apitypes.RoleContacts{Billing: ptrReg(registrantFixture("bill@example.com"))},
			}}})
			return
		}
		h.ServeHTTP(w, r)
	}))

	// dry-run: shows the plan, no PUT
	out := runRoot(t, []string{
		"--server", s.URL, "--token", "dcl_x", "--yes",
		"domains", "contacts", "probe-edit.example", "--contact", "work", "--dry-run",
	}, "")
	if api.lastPut != nil {
		t.Fatalf("dry-run must not PUT: %+v", api.lastPut)
	}
	if !strings.Contains(out, "Dry run") {
		t.Errorf("dry-run notice missing:\n%s", out)
	}

	// real update
	out = runRoot(t, []string{
		"--server", s.URL, "--token", "dcl_x", "--yes",
		"domains", "contacts", "probe-edit.example", "--contact", "work",
	}, "")
	if api.lastPut == nil {
		t.Fatal("PUT never sent")
	}
	if api.lastPut.Registrant.Email != "set@example.com" {
		t.Fatalf("PUT registrant = %+v, want the set's registrant", api.lastPut.Registrant)
	}
	if api.lastPut.RoleContacts == nil || api.lastPut.RoleContacts.Billing == nil || api.lastPut.RoleContacts.Billing.Email != "bill@example.com" {
		t.Fatalf("PUT roles = %+v, want the set's billing", api.lastPut.RoleContacts)
	}
	if !strings.Contains(out, "bill@example.com") {
		t.Errorf("updated contacts not printed:\n%s", out)
	}
}

// TestDomainsContactsUpdateRoleFlags checks inline registrant + role flags
// and REPLACE semantics: --role-admin auto keeps admin nil while tech gets
// the override.
func TestDomainsContactsUpdateRoleFlags(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	api := &domainContactsAPI{live: apitypes.DomainContactsResponse{Registrant: registrantFixture("ada@example.com")}}
	h := api.handler(t)
	s := fakeServer(t, h.ServeHTTP)
	runRoot(t, []string{
		"--server", s.URL, "--token", "dcl_x", "--yes",
		"domains", "contacts", "probe-edit.example",
		"--first-name", "Ada", "--last-name", "Lovelace",
		"--email", "ada@example.com", "--phone", "+34.600000000",
		"--address1", "Street 1", "--city", "BCN", "--zip", "08001",
		"--country", "es",
		"--role-tech", "email=tech2@example.com,first-name=T2",
		"--role-admin", "auto",
	}, "")
	if api.lastPut == nil {
		t.Fatal("PUT never sent")
	}
	rc := api.lastPut.RoleContacts
	if rc == nil || rc.Tech == nil || rc.Tech.Email != "tech2@example.com" {
		t.Fatalf("PUT roles = %+v, want the tech override", rc)
	}
	if rc.Admin != nil {
		t.Fatalf("admin = %+v, want nil (auto)", rc.Admin)
	}
	if api.lastPut.Registrant.Country != "ES" {
		t.Fatalf("registrant country = %s, want ES", api.lastPut.Registrant.Country)
	}
}

// TestDomainsContactsForeignDomain surfaces the 404 shape as a failure.
func TestDomainsContactsForeignDomain(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")
	s := fakeServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/contacts") {
			writeJSON404(w)
			return
		}
		t.Errorf("unexpected %s", r.URL.Path)
		writeJSON404(w)
	}))
	out := runRootErr(t, []string{
		"--server", s.URL, "--token", "dcl_x",
		"domains", "contacts", "not-owned.example",
	})
	if strings.TrimSpace(out) == "" {
		t.Errorf("expected a failure message, got empty output")
	}
}

func registrantFixture(email string) apitypes.Registrant {
	return apitypes.Registrant{
		FirstName: "Ada", LastName: "Lovelace",
		Email: email, Phone: "+44.1234567890",
		Address1: "1 Main St", City: "London", Zip: "12345", Country: "GB",
	}
}

func ptrReg(r apitypes.Registrant) *apitypes.Registrant { return &r }
