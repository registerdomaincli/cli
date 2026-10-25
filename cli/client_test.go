package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/registerdomaincli/cli/apitypes"
)

// fakeServer builds an httptest server routing /v1/* with the given handler,
// plus a request recorder for header assertions.
func fakeServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	return s
}

func TestSignupVerify(t *testing.T) {
	var gotSignup, gotVerify apitypes.VerifyRequest
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/signup" && r.Method == http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&gotSignup); err != nil {
				t.Errorf("decode signup body: %v", err)
			}
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(apitypes.SignupResponse{Status: "sent"})
		case r.URL.Path == "/v1/auth/verify" && r.Method == http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&gotVerify); err != nil {
				t.Errorf("decode verify body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(apitypes.VerifyResponse{Token: "dcl_testtoken"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	c := NewClient(s.URL, "", "en")
	if _, err := c.Signup("me@example.com"); err != nil {
		t.Fatalf("Signup: %v", err)
	}
	if gotSignup.Email != "me@example.com" {
		t.Errorf("signup email = %q", gotSignup.Email)
	}
	token, err := c.Verify("me@example.com", "123456")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if token != "dcl_testtoken" {
		t.Errorf("token = %q", token)
	}
	if gotVerify.Code != "123456" || gotVerify.Email != "me@example.com" {
		t.Errorf("verify body = %+v", gotVerify)
	}
}

func TestClientHeaders(t *testing.T) {
	var auth, lang, accept string
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		lang = r.Header.Get("Accept-Language")
		accept = r.Header.Get("Accept")
		_ = json.NewEncoder(w).Encode(apitypes.Me{Email: "me@example.com"})
	})
	c := NewClient(s.URL, "dcl_secret", "es")
	if _, err := c.Me(); err != nil {
		t.Fatalf("Me: %v", err)
	}
	if auth != "Bearer dcl_secret" {
		t.Errorf("Authorization = %q", auth)
	}
	if lang != "es" {
		t.Errorf("Accept-Language = %q, want detected locale", lang)
	}
	if accept != "application/json" {
		t.Errorf("Accept = %q", accept)
	}
}

func TestAPIErrorFraming(t *testing.T) {
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(apitypes.APIError{
			Error: "domain not managed by us",
			Code:  "domain_not_managed",
		})
	})
	c := NewClient(s.URL, "dcl_x", "en")
	_, err := c.Domain("example.com")
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Message != "domain not managed by us" {
		t.Errorf("message = %q, want verbatim upstream message", apiErr.Message)
	}
	if apiErr.Code != "domain_not_managed" {
		t.Errorf("code = %q", apiErr.Code)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d", apiErr.StatusCode)
	}
}

func TestRateLimitedRetryAfter(t *testing.T) {
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c := NewClient(s.URL, "dcl_x", "en")
	_, err := c.Search("acme", "")
	rl, ok := err.(*RateLimitedError)
	if !ok {
		t.Fatalf("expected *RateLimitedError, got %T: %v", err, err)
	}
	if rl.Seconds != 7 {
		t.Errorf("Seconds = %d, want 7", rl.Seconds)
	}
	// The command-level framing prints the localized message with the
	// Retry-After seconds.
	framed := fail(err).Error()
	if !strings.Contains(framed, "7") {
		t.Errorf("fail(RateLimitedError) = %q, want retry message with 7", framed)
	}
}

func TestMalformedErrorBodyFallsBackToStatus(t *testing.T) {
	s := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>bad gateway</html>"))
	})
	c := NewClient(s.URL, "dcl_x", "en")
	_, err := c.Me()
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Message == "" {
		t.Error("expected fallback status message")
	}
}
