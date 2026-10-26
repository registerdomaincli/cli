package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/registerdomaincli/cli/apitypes"
)

func searchRow(name string) apitypes.SearchResult {
	price := 12.99
	return apitypes.SearchResult{
		Name:          name,
		Purchasable:   true,
		PurchasePrice: &price,
		RenewalPrice:  &price,
		Currency:      "usd",
	}
}

// TestSearchTLDFilterAndLimit: --tlds and --limit travel to the server
// verbatim; without them the fields stay zero (legacy behavior).
func TestSearchTLDFilterAndLimit(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")

	var gotTLDs atomic.Value
	var gotLimit atomic.Int64
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/domains/search" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var req apitypes.SearchRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotTLDs.Store(strings.Join(req.TLDs, ","))
		gotLimit.Store(int64(req.Limit))
		_ = json.NewEncoder(w).Encode(apitypes.SearchResponse{
			Results: []apitypes.SearchResult{searchRow("acme.com")},
		})
	})
	runRoot(t, []string{"--server", s.URL, "--token", "dcl_x",
		"search", "acme", "--tlds", ".com, net ,ORG", "--limit", "25"}, "")
	if got := gotTLDs.Load().(string); got != ".com, net ,ORG" {
		t.Errorf("server saw tlds %q, want raw passthrough (the server is the single normalization point)", got)
	}
	if gotLimit.Load() != 25 {
		t.Errorf("server saw limit %d, want 25", gotLimit.Load())
	}
}

// TestSearchUnfilteredOmitsFields: no flags → empty tlds and limit, exactly
// the pre-feature wire shape.
func TestSearchUnfilteredOmitsFields(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")

	var gotTLDs atomic.Value
	var gotLimit atomic.Int64
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req apitypes.SearchRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotTLDs.Store(strings.Join(req.TLDs, ","))
		gotLimit.Store(int64(req.Limit))
		_ = json.NewEncoder(w).Encode(apitypes.SearchResponse{
			Results: []apitypes.SearchResult{searchRow("acme.com")},
		})
	})

	runRoot(t, []string{"--server", s.URL, "--token", "dcl_x", "search", "acme"}, "")
	if got := gotTLDs.Load().(string); got != "" {
		t.Errorf("server saw tlds %q, want empty", got)
	}
	if gotLimit.Load() != 0 {
		t.Errorf("server saw limit %d, want 0", gotLimit.Load())
	}
}

// TestTLDsCommandRendersCatalog: the command prints the group menu and the
// wrapped TLD list; --json emits the raw catalog; --group filters server-side.
func TestTLDsCommandRendersCatalog(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")

	catalog := apitypes.TldCatalog{
		Groups: []apitypes.TldGroup{
			{Slug: "local", Label: "Local & regional", Description: "identity TLDs"},
			{Slug: "business", Label: "Business", Description: "companies"},
		},
		TLDs: []apitypes.TldEntry{
			{TLD: "cat", Groups: []string{"local"}},
			{TLD: "com", Groups: []string{"business"}},
		},
	}
	var gotGroup atomic.Value
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tlds" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		gotGroup.Store(r.URL.Query().Get("group"))
		_ = json.NewEncoder(w).Encode(catalog)
	})

	out := runRoot(t, []string{"--server", s.URL, "--token", "dcl_x", "tlds"}, "")
	if !strings.Contains(out, "local") || !strings.Contains(out, "Local & regional") {
		t.Errorf("group menu missing:\n%s", out)
	}
	if !strings.Contains(out, "com") || !strings.Contains(out, "cat") {
		t.Errorf("TLD list missing:\n%s", out)
	}

	outJSON := runRoot(t, []string{"--server", s.URL, "--token", "dcl_x", "--json", "tlds"}, "")
	var decoded apitypes.TldCatalog
	if err := json.Unmarshal([]byte(strings.TrimSpace(outJSON)), &decoded); err != nil {
		t.Fatalf("--json output not a TldCatalog: %v\n%s", err, outJSON)
	}
	if len(decoded.TLDs) != 2 {
		t.Errorf("--json rows = %d, want 2", len(decoded.TLDs))
	}

	runRoot(t, []string{"--server", s.URL, "--token", "dcl_x", "tlds", "--group", "local"}, "")
	if got := gotGroup.Load(); got != "local" {
		t.Errorf("server saw group %v, want local", got)
	}
}

// TestTLDsUnknownGroupSurfacesUpstreamMessage: the server's 400 invalid_group
// message is passed through verbatim, like every other API error.
func TestTLDsUnknownGroupSurfacesUpstreamMessage(t *testing.T) {
	setConfigDir(t)
	setLocale(t, "en")

	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(apitypes.APIError{Error: "unknown group: bogus", Code: "invalid_group"})
	})

	errOut := runRootErr(t, []string{"--server", s.URL, "--token", "dcl_x", "tlds", "--group", "bogus"})
	if !strings.Contains(errOut, "unknown group: bogus") {
		t.Errorf("upstream message not surfaced verbatim: %s", errOut)
	}
}
