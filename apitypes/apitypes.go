// Package apitypes defines the wire contract shared by the RegisterDomainCLI
// client and server. Both modules import this package so the contract cannot
// drift. JSON encoding is camelCase everywhere, without exception.
//
// This package lives at the module root (NOT under internal/) because Go
// forbids importing another module's internal packages.
package apitypes

import "time"

// SignupRequest starts account creation. The server answers 202 regardless of
// whether the email is already known (no account enumeration).
type SignupRequest struct {
	Email string `json:"email"`
}

// VerifyRequest completes signup with the 6-digit code from the email.
type VerifyRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

// VerifyResponse carries the minted API token (prefix "dcl_").
type VerifyResponse struct {
	Token string `json:"token"`
}

// Me is the account summary returned by GET /v1/me.
type Me struct {
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"createdAt"`
}

// SearchRequest is the body of POST /v1/domains/search. A query containing a
// dot is treated as an FQDN availability check, anything else as a keyword
// search.
type SearchRequest struct {
	Query    string `json:"query"`
	Currency string `json:"currency,omitempty"` // ISO 4217, lowercase; empty = server default
}

// SearchResult is one availability row.
//
// PurchasePrice and RenewalPrice are INDICATIVE 1-year discovery prices for
// display only — they are never used for billing. The exact charge always
// comes from GET /v1/quote, which derives from the authoritative
// domains/{d}:getPricing endpoint.
type SearchResult struct {
	Name          string   `json:"name"`
	Purchasable   bool     `json:"purchasable"`
	PurchasePrice *float64 `json:"purchasePrice"`
	RenewalPrice  *float64 `json:"renewalPrice"`
	Currency      string   `json:"currency"` // currency of the prices in this row
	Premium       bool     `json:"premium"`
	PurchaseType  string   `json:"purchaseType"`
	Reason        string   `json:"reason"`
}

// SearchResponse wraps the result list.
type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

// Quote is the real pre-order price returned by GET /v1/quote. NetCents is
// the tax-exclusive charge (upstream price for the requested term plus
// markup); VAT is added by Stripe at checkout.
type Quote struct {
	Domain        string  `json:"domain"`
	Years         int     `json:"years"`
	Kind          string  `json:"kind"` // registration | renewal | transfer
	Premium       bool    `json:"premium"`
	UpstreamPrice float64 `json:"upstreamPrice"`
	NetCents      int64   `json:"netCents"`
	Currency      string  `json:"currency"`
}

// Registrant is the WHOIS registrant contact. Organization, Address2 and
// State are optional; everything else is required. Registrations are made in
// the client's name: this contact is the customer of record for the
// registration, so the customer is the Registered Name Holder.
type Registrant struct {
	FirstName    string `json:"firstName"`
	LastName     string `json:"lastName"`
	Organization string `json:"organization,omitempty"`
	Email        string `json:"email"`
	Phone        string `json:"phone"`
	Address1     string `json:"address1"`
	Address2     string `json:"address2,omitempty"`
	City         string `json:"city"`
	State        string `json:"state,omitempty"`
	Zip          string `json:"zip"`
	Country      string `json:"country"` // ISO 3166-1 alpha-2
}

// CreateOrderRequest is the body of POST /v1/orders. Kind defaults to
// "registration". AuthCode is required for transfers; a Registrant (inline,
// via ContactSetID, or the user's default set) is required for registration
// and transfer. Years is 1-10 for registration/renewal and ignored for
// transfers (a gTLD transfer adds exactly one year).
type CreateOrderRequest struct {
	Kind         string      `json:"kind,omitempty"`
	Domain       string      `json:"domain"`
	Years        int         `json:"years,omitempty"`
	AuthCode     string      `json:"authCode,omitempty"`
	Currency     string      `json:"currency,omitempty"` // ISO 4217, lowercase; empty = server default
	Registrant   *Registrant `json:"registrant,omitempty"`
	ContactSetID string      `json:"contactSetId,omitempty"`
}

// Order is the order resource returned by the orders endpoints.
// InvoiceURL is a pre-signed absolute URL (24 h validity) so it can be opened
// in a browser that carries no Authorization header.
type Order struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	Domain        string    `json:"domain"`
	Years         int       `json:"years"`
	NetCents      int64     `json:"netCents"`
	TaxCents      int64     `json:"taxCents"`
	TotalCents    int64     `json:"totalCents"`
	Currency      string    `json:"currency"`
	Status        string    `json:"status"` // pending_payment | paid | processing | completed | failed | refunded | expired | cancelled
	CheckoutURL   string    `json:"checkoutUrl"`
	InvoiceNumber string    `json:"invoiceNumber,omitempty"`
	InvoiceURL    string    `json:"invoiceUrl,omitempty"`
	FailureReason string    `json:"failureReason,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

// SignupResponse acknowledges that the verification email was sent. The
// server answers with this on every signup (no account enumeration).
type SignupResponse struct {
	Status string `json:"status"` // "sent"
}

// OrdersResponse is the GET /v1/orders envelope.
type OrdersResponse struct {
	Orders []Order `json:"orders"`
}

// DomainsResponse is the GET /v1/domains envelope.
type DomainsResponse struct {
	Domains []Domain `json:"domains"`
}

// RecordsResponse wraps DNS record lists.
type RecordsResponse struct {
	Records []Record `json:"records"`
}

// ContactSetsResponse wraps contact-set lists.
type ContactSetsResponse struct {
	ContactSets []ContactSet `json:"contactSets"`
}

// BillingProfile is the buyer data printed on invoices (GET/PUT /v1/billing).
type BillingProfile struct {
	LegalName string `json:"legalName"`
	Company   string `json:"company,omitempty"`
	TaxID     string `json:"taxId,omitempty"`
	Email     string `json:"email"`
	Address1  string `json:"address1"`
	Address2  string `json:"address2,omitempty"`
	City      string `json:"city"`
	State     string `json:"state,omitempty"`
	Zip       string `json:"zip"`
	Country   string `json:"country"`
}

// ContactSet is a saved registrant contact (contact-sets endpoints).
type ContactSet struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	Registrant Registrant `json:"registrant"`
	IsDefault  bool       `json:"isDefault"`
}

// PortalResponse carries the Stripe Billing Portal URL.
type PortalResponse struct {
	URL string `json:"url"`
}

// Domain is a domain owned by the authenticated user.
type Domain struct {
	Name        string     `json:"name"`
	ExpireDate  *time.Time `json:"expireDate,omitempty"`
	Autorenew   bool       `json:"autorenew"`
	Privacy     bool       `json:"privacy"`
	Locked      bool       `json:"locked"`
	Nameservers []string   `json:"nameservers,omitempty"`
}

// AuthCodeResponse is GET /v1/domains/{name}/authcode — the EPP transfer code.
type AuthCodeResponse struct {
	AuthCode string `json:"authCode"`
}

// Record is one DNS record. ID is the registrar's numeric id (delete and
// update take it). An empty Host is the zone apex.
type Record struct {
	ID     int64  `json:"id"`
	Host   string `json:"host"`
	Type   string `json:"type"`
	Answer string `json:"answer"`
	TTL    int    `json:"ttl"`
}

// RecordInput creates or updates a DNS record. TTL defaults to 3600 when 0.
type RecordInput struct {
	Host   string `json:"host"`
	Type   string `json:"type"`
	Answer string `json:"answer"`
	TTL    int    `json:"ttl,omitempty"`
}

// NameserversInput is the PUT body for /v1/domains/{name}/nameservers.
type NameserversInput struct {
	Nameservers []string `json:"nameservers"`
}

// LockInput is the PUT body for /v1/domains/{name}/lock.
type LockInput struct {
	Locked bool `json:"locked"`
}

// AutorenewInput is the PUT body for /v1/domains/{name}/autorenew. Enabling
// it authorises an annual off-session charge to the saved payment method.
type AutorenewInput struct {
	Enabled bool `json:"enabled"`
}

// PaymentMethod is one saved payment method shown in the panel's payment
// page. Details is a provider-rendered human line ("card •••• 1234,
// exp 12/27").
type PaymentMethod struct {
	ID      string `json:"id"`
	Status  string `json:"status"` // valid | pending
	Method  string `json:"method"` // creditcard | directdebit | paypal | ...
	Details string `json:"details"`
	Created string `json:"createdAt"`
}

// PaymentMethodsResponse is the GET /v1/payment-methods envelope.
type PaymentMethodsResponse struct {
	Methods []PaymentMethod `json:"methods"`
}

// APIError is the single error envelope every non-2xx response uses. Error is
// an English human-readable message; Code is a stable machine code that
// clients localize when they recognise it (e.g. "contacts_required").
type APIError struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}
