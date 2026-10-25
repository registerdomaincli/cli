package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/registerdomaincli/cli/apitypes"
)

// APIError is a non-2xx response carrying the {"error","code"} envelope. The
// English message is printed verbatim; Code lets callers special-case
// machine codes when they need to.
type APIError struct {
	StatusCode int
	Message    string
	Code       string
}

func (e *APIError) Error() string { return e.Message }

// RateLimitedError surfaces a 429 with its Retry-After value so commands can
// print the localized "rate limited, retry after Ns" message.
type RateLimitedError struct {
	Seconds int
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("rate limited, retry after %ds", e.Seconds)
}

// Client is the thin HTTP client over the /v1 contract. It sends the detected
// locale as Accept-Language on every request so the server can localize
// emails and server-rendered documents for this user.
type Client struct {
	BaseURL string
	Token   string
	Lang    string
	HTTP    *http.Client
}

// NewClient builds a client for the given server base URL. Trailing slashes
// are trimmed so paths always join cleanly.
func NewClient(baseURL, token, lang string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		Lang:    lang,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", c.Lang)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("request to %s failed: %w", c.BaseURL+path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return decodeError(resp)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil && err != io.EOF {
			return fmt.Errorf("invalid response from %s: %w", path, err)
		}
	}
	return nil
}

func decodeError(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusTooManyRequests {
		secs := 0
		if v := resp.Header.Get("Retry-After"); v != "" {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				secs = n
			}
		}
		return &RateLimitedError{Seconds: secs}
	}
	var env apitypes.APIError
	if err := json.Unmarshal(data, &env); err == nil && env.Error != "" {
		return &APIError{StatusCode: resp.StatusCode, Message: env.Error, Code: env.Code}
	}
	return &APIError{
		StatusCode: resp.StatusCode,
		Message:    fmt.Sprintf("server returned %s", resp.Status),
	}
}

// Signup requests the verification email for an account. The server answers
// 202 with {"status":"sent"} whether or not the email is known.
func (c *Client) Signup(email string) (apitypes.SignupResponse, error) {
	var out apitypes.SignupResponse
	err := c.do(http.MethodPost, "/v1/auth/signup", apitypes.SignupRequest{Email: email}, &out)
	return out, err
}

// Verify exchanges the emailed 6-digit code for an API token.
func (c *Client) Verify(email, code string) (string, error) {
	var out apitypes.VerifyResponse
	err := c.do(http.MethodPost, "/v1/auth/verify", apitypes.VerifyRequest{Email: email, Code: code}, &out)
	return out.Token, err
}

// Me returns the account summary.
func (c *Client) Me() (apitypes.Me, error) {
	var out apitypes.Me
	err := c.do(http.MethodGet, "/v1/me", nil, &out)
	return out, err
}

// DeleteMe removes the account and all associated data.
func (c *Client) DeleteMe() error {
	return c.do(http.MethodDelete, "/v1/me", nil, nil)
}

// Logout revokes the token this client was built with. Only that session
// dies: the same account signed in elsewhere holds a different token and
// stays signed in.
func (c *Client) Logout() error {
	return c.do(http.MethodPost, "/v1/auth/logout", nil, nil)
}

// Search runs a keyword or FQDN availability search. currency (ISO 4217,
// lowercase) is forwarded so the server converts its indicative prices;
// empty means the server's default billing currency.
func (c *Client) Search(query, currency string) (apitypes.SearchResponse, error) {
	var out apitypes.SearchResponse
	err := c.do(http.MethodPost, "/v1/domains/search", apitypes.SearchRequest{Query: query, Currency: currency}, &out)
	return out, err
}

// Quote fetches the authoritative pre-order price. years is ignored for
// transfers. currency is the billing currency requested for the quote;
// empty means the server's default.
func (c *Client) Quote(domain string, years int, kind, currency string) (apitypes.Quote, error) {
	var out apitypes.Quote
	if currency != "" {
		currency = "&currency=" + url.QueryEscape(currency)
	}
	err := c.do(http.MethodGet,
		fmt.Sprintf("/v1/quote?domain=%s&years=%d&kind=%s%s", domain, years, kind, currency), nil, &out)
	return out, err
}

// CreateOrder places an order. The server answers 201 with the Order.
func (c *Client) CreateOrder(req apitypes.CreateOrderRequest) (apitypes.Order, error) {
	var out apitypes.Order
	err := c.do(http.MethodPost, "/v1/orders", req, &out)
	return out, err
}

// Orders lists the account's orders.
func (c *Client) Orders() (apitypes.OrdersResponse, error) {
	var out apitypes.OrdersResponse
	err := c.do(http.MethodGet, "/v1/orders", nil, &out)
	return out, err
}

// Order fetches a single order by id.
func (c *Client) Order(id string) (apitypes.Order, error) {
	var out apitypes.Order
	err := c.do(http.MethodGet, "/v1/orders/"+id, nil, &out)
	return out, err
}

// CancelOrder cancels an open order.
func (c *Client) CancelOrder(id string) (apitypes.Order, error) {
	var out apitypes.Order
	err := c.do(http.MethodPost, "/v1/orders/"+id+"/cancel", nil, &out)
	return out, err
}

// Domains lists the domains owned by the account.
func (c *Client) Domains() (apitypes.DomainsResponse, error) {
	var out apitypes.DomainsResponse
	err := c.do(http.MethodGet, "/v1/domains", nil, &out)
	return out, err
}

// Domain fetches one owned domain.
func (c *Client) Domain(name string) (apitypes.Domain, error) {
	var out apitypes.Domain
	err := c.do(http.MethodGet, "/v1/domains/"+name, nil, &out)
	return out, err
}

// AuthCode fetches the transfer (EPP) code of a domain.
func (c *Client) AuthCode(name string) (apitypes.AuthCodeResponse, error) {
	var out apitypes.AuthCodeResponse
	err := c.do(http.MethodGet, "/v1/domains/"+name+"/authcode", nil, &out)
	return out, err
}

// SetLock locks or unlocks a domain against transfers.
func (c *Client) SetLock(name string, locked bool) error {
	return c.do(http.MethodPut, "/v1/domains/"+name+"/lock", apitypes.LockInput{Locked: locked}, nil)
}

// SetAutorenew enables or disables autorenew for a domain.
func (c *Client) SetAutorenew(name string, enabled bool) error {
	return c.do(http.MethodPut, "/v1/domains/"+name+"/autorenew", apitypes.AutorenewInput{Enabled: enabled}, nil)
}

// Records lists the DNS records of a domain.
func (c *Client) Records(domain string) (apitypes.RecordsResponse, error) {
	var out apitypes.RecordsResponse
	err := c.do(http.MethodGet, "/v1/domains/"+domain+"/records", nil, &out)
	return out, err
}

// CreateRecord adds a DNS record; the server answers 201 with the created
// record carrying its numeric id.
func (c *Client) CreateRecord(domain string, in apitypes.RecordInput) (apitypes.Record, error) {
	var out apitypes.Record
	err := c.do(http.MethodPost, "/v1/domains/"+domain+"/records", in, &out)
	return out, err
}

// DeleteRecord removes a DNS record by its numeric id.
func (c *Client) DeleteRecord(domain string, id int64) error {
	return c.do(http.MethodDelete,
		fmt.Sprintf("/v1/domains/%s/records/%d", domain, id), nil, nil)
}

// Nameservers returns the nameservers assigned to a domain.
func (c *Client) Nameservers(domain string) (apitypes.NameserversInput, error) {
	var out apitypes.NameserversInput
	err := c.do(http.MethodGet, "/v1/domains/"+domain+"/nameservers", nil, &out)
	return out, err
}

// SetNameservers replaces the nameservers of a domain.
func (c *Client) SetNameservers(domain string, ns []string) error {
	return c.do(http.MethodPut, "/v1/domains/"+domain+"/nameservers",
		apitypes.NameserversInput{Nameservers: ns}, nil)
}

// ContactSets lists the saved registrant contact sets.
func (c *Client) ContactSets() (apitypes.ContactSetsResponse, error) {
	var out apitypes.ContactSetsResponse
	err := c.do(http.MethodGet, "/v1/contact-sets", nil, &out)
	return out, err
}

// CreateContactSet saves a registrant under a label.
func (c *Client) CreateContactSet(label string, reg apitypes.Registrant) (apitypes.ContactSet, error) {
	var out apitypes.ContactSet
	err := c.do(http.MethodPost, "/v1/contact-sets",
		struct {
			Label      string              `json:"label"`
			Registrant apitypes.Registrant `json:"registrant"`
		}{Label: label, Registrant: reg}, &out)
	return out, err
}

// DeleteContactSet removes a saved contact set by id.
func (c *Client) DeleteContactSet(id string) error {
	return c.do(http.MethodDelete, "/v1/contact-sets/"+id, nil, nil)
}

// SetDefaultContactSet marks a contact set as the account default.
func (c *Client) SetDefaultContactSet(id string) (apitypes.ContactSetsResponse, error) {
	var out apitypes.ContactSetsResponse
	err := c.do(http.MethodPut, "/v1/contact-sets/"+id+"/default", nil, &out)
	return out, err
}

// Billing returns the billing profile printed on invoices.
func (c *Client) Billing() (apitypes.BillingProfile, error) {
	var out apitypes.BillingProfile
	err := c.do(http.MethodGet, "/v1/billing", nil, &out)
	return out, err
}

// UpdateBilling replaces the billing profile.
func (c *Client) UpdateBilling(p apitypes.BillingProfile) (apitypes.BillingProfile, error) {
	var out apitypes.BillingProfile
	err := c.do(http.MethodPut, "/v1/billing", p, &out)
	return out, err
}

// Portal returns the Stripe Billing Portal URL. The server answers 501 when
// the portal is not activated.
func (c *Client) Portal() (apitypes.PortalResponse, error) {
	var out apitypes.PortalResponse
	err := c.do(http.MethodPost, "/v1/billing/portal", nil, &out)
	return out, err
}
