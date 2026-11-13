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
//
// TLDs optionally restricts the result to those TLDs (no leading dot,
// "com" not ".com"); empty means no filter, the historical behavior. Limit
// caps the number of rows returned; 0 means the server default (50), which
// is also the ceiling.
type SearchRequest struct {
	Query    string   `json:"query"`
	Currency string   `json:"currency,omitempty"` // ISO 4217, lowercase; empty = server default
	TLDs     []string `json:"tlds,omitempty"`
	Limit    int      `json:"limit,omitempty"`
}

// TldCatalog is the response of GET /v1/tlds. It is self-describing: Groups
// lists the curated groups and every TLD row carries the group slugs it
// belongs to, so one request gives an agent both the menu of groups and the
// concrete TLDs to filter with.
type TldCatalog struct {
	Groups []TldGroup `json:"groups"`
	TLDs   []TldEntry `json:"tlds"`
}

// TldGroup is one curated intent group ("local", "business", …).
type TldGroup struct {
	Slug        string `json:"slug"`        // stable identifier for ?group= filtering
	Label       string `json:"label"`       // short human label (English)
	Description string `json:"description"` // one-line explanation (English)
}

// TldEntry is one sellable TLD with its curated group memberships.
type TldEntry struct {
	TLD    string   `json:"tld"`    // no leading dot: "com", "cat"
	Groups []string `json:"groups"` // group slugs, alphabetical; may be empty
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
// markup, minus any discount). TaxCents is the VAT computed by the server
// from the buyer's billing data on the DISCOUNTED net; TotalCents is the
// exact amount charged at checkout.
type Quote struct {
	Domain        string       `json:"domain"`
	Years         int          `json:"years"`
	Kind          string       `json:"kind"` // registration | renewal | transfer
	Premium       bool         `json:"premium"`
	UpstreamPrice float64      `json:"upstreamPrice"`
	NetCents      int64        `json:"netCents"`
	TaxCents      int64        `json:"taxCents"`
	TotalCents    int64        `json:"totalCents"`
	Currency      string       `json:"currency"`
	Discount      *DiscountApp `json:"discount,omitempty"` // set only when a code was requested and accepted
}

// DiscountApp is the discount a server accepted on a quote or an order: the
// code as normalized server-side (case-insensitive match), what was taken
// off the net, and the currency of that amount. Percent-kind discounts carry
// Percent and a server-computed AmountCents; fixed-kind leave Percent empty.
// The client never calculates discounts itself — it displays AmountCents.
type DiscountApp struct {
	Code        string `json:"code"`
	Percent     string `json:"percent,omitempty"` // kind=percent, e.g. "10.00"
	AmountCents int64  `json:"amountCents"`       // actual cents taken off the net
	Currency    string `json:"currency"`
}

// QuoteRequest is the body of POST /v1/quotes: one quote for a whole list
// of domains. All domains share one kind and one currency; per-domain
// years are allowed (0 inherits the top-level Years).
type QuoteRequest struct {
	Kind         string           `json:"kind,omitempty"` // registration | renewal | transfer; default registration
	Years        int              `json:"years,omitempty"`
	Domains      []QuoteItemInput `json:"domains"`
	Currency     string           `json:"currency,omitempty"`     // ISO 4217, lowercase; empty = server default
	DiscountCode string           `json:"discountCode,omitempty"` // optional; validated server-side
}

// QuoteItemInput is one domain inside a QuoteRequest.
type QuoteItemInput struct {
	Domain string `json:"domain"`
	Years  int    `json:"years,omitempty"`
}

// QuoteResponse is the envelope of POST /v1/quotes: per-domain quotes plus
// the order-level totals the checkout will charge (VAT is order-level).
// Discount, when set, is already reflected in NetCents; the per-domain
// Quotes rows keep their undiscounted NetCents — the discount is an
// order-level adjustment, not per-domain.
type QuoteResponse struct {
	Quotes     []Quote      `json:"quotes"`
	NetCents   int64        `json:"netCents"`
	TaxCents   int64        `json:"taxCents"`
	TotalCents int64        `json:"totalCents"`
	Currency   string       `json:"currency"`
	Discount   *DiscountApp `json:"discount,omitempty"` // order-level; see Quote.Discount
}

// Registrant is the WHOIS contact. Organization, Address2 and State are
// optional; everything else is required. Registrations are made in the
// client's name: the registrant is the customer of record, so the customer
// is the Registered Name Holder.
//
// Name registries also carry admin, tech and billing roles. When the
// server sends only a registrant, the other three default to the REGISTRANT
// (never the reseller account) — this struct is the single shape used for
// every role, so a role contact is the same nine fields.
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
// "registration". A single order may carry ONE domain (Domain/Years, the
// legacy shape) or a LIST of domains (Items) sharing the kind, currency,
// registrant and payment. One checkout charges everything; fulfilment is
// per item. AuthCode is required for transfers; a Registrant (inline, via
// ContactSetID, or the user's default set) is required for registration and
// transfer. Years is 1-10 for registration/renewal and ignored for
// transfers (a gTLD transfer adds exactly one year).
//
// UseSavedMethod is a tri-state (pointer, so false still serializes): nil
// means the default — when the customer has a saved payment method the
// server charges it off-session and the order completes through the
// provider webhook (checkoutUrl comes back empty); false forces the hosted
// checkout even when a method is saved, which is how a customer pays with a
// NEW card. It is never possible to charge without one of the two paths.
type CreateOrderRequest struct {
	Kind           string           `json:"kind,omitempty"`
	Domain         string           `json:"domain"`
	Years          int              `json:"years,omitempty"`
	Items          []OrderItemInput `json:"items,omitempty"` // multi-domain form; wins when set
	AuthCode       string           `json:"authCode,omitempty"`
	Currency       string           `json:"currency,omitempty"`     // ISO 4217, lowercase; empty = server default
	DiscountCode   string           `json:"discountCode,omitempty"` // optional; revalidated server-side at order time
	Registrant     *Registrant      `json:"registrant,omitempty"`
	RoleContacts   *RoleContacts    `json:"roleContacts,omitempty"`
	ContactSetID   string           `json:"contactSetId,omitempty"`
	UseSavedMethod *bool            `json:"useSavedMethod,omitempty"`
}

// RoleContacts carries the non-registrant WHOIS roles. Every pointer is
// tri-state: nil inherits the registrant, non-nil is used verbatim. There
// is no way to delegate a role to the reseller — upstream never gets a
// contact we did not validate.
type RoleContacts struct {
	Admin   *Registrant `json:"admin,omitempty"`
	Tech    *Registrant `json:"tech,omitempty"`
	Billing *Registrant `json:"billing,omitempty"`
}

// OrderItemInput is one domain inside a multi-domain order. Years 0
// inherits the top-level Years; AuthCode is per-domain for transfers.
type OrderItemInput struct {
	Domain   string `json:"domain"`
	Years    int    `json:"years,omitempty"`
	AuthCode string `json:"authCode,omitempty"`
}

// OrderItem is one domain inside a multi-domain order, with its own price
// and fulfilment status. Single-domain orders leave Items empty and keep
// the top-level fields.
type OrderItem struct {
	Domain        string  `json:"domain"`
	Years         int     `json:"years"`
	UpstreamPrice float64 `json:"upstreamPrice"`
	NetCents      int64   `json:"netCents"`
	Premium       bool    `json:"premium"`
	Status        string  `json:"status"` // pending | processing | completed | failed
	FailureReason string  `json:"failureReason,omitempty"`
}

// Order is the order resource returned by the orders endpoints.
// InvoiceURL is a pre-signed absolute URL (24 h validity) so it can be opened
// in a browser that carries no Authorization header.
type Order struct {
	ID            string        `json:"id"`
	Kind          string        `json:"kind"`
	Domain        string        `json:"domain"`
	Items         []OrderItem   `json:"items,omitempty"` // per-domain lines on multi-domain orders
	Years         int           `json:"years"`
	NetCents      int64         `json:"netCents"`
	TaxCents      int64         `json:"taxCents"`
	TotalCents    int64         `json:"totalCents"`
	Currency      string        `json:"currency"`
	Discount      *DiscountApp  `json:"discount,omitempty"` // the accepted discount, already in NetCents
	Status        string        `json:"status"`             // pending_payment | paid | processing | completed | failed | refunded | expired | cancelled
	CheckoutURL   string        `json:"checkoutUrl"`
	InvoiceNumber string        `json:"invoiceNumber,omitempty"`
	InvoiceURL    string        `json:"invoiceUrl,omitempty"`
	RoleContacts  *RoleContacts `json:"roleContacts,omitempty"` // resolved per-role contacts; nil = all roles are the registrant
	FailureReason string        `json:"failureReason,omitempty"`
	CreatedAt     time.Time     `json:"createdAt"`
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

// ContactSet is a saved contact set. Registrant is required; Roles is nil
// for sets saved before per-role contacts existed (all roles = registrant).
type ContactSet struct {
	ID         string        `json:"id"`
	Label      string        `json:"label"`
	Registrant Registrant    `json:"registrant"`
	IsDefault  bool          `json:"isDefault"`
	Roles      *RoleContacts `json:"roles,omitempty"`
}

// Expanded reports whether the set carries per-role contacts.
func (cs *ContactSet) Expanded() bool { return cs.Roles != nil }

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

// DomainContactsResponse is GET /v1/domains/{name}/contacts: the live
// registrar-of-record contacts. Roles identical to the registrant report
// nil — the product-wide "same as registrant" inheritance.
type DomainContactsResponse struct {
	Registrant   Registrant    `json:"registrant"`
	RoleContacts *RoleContacts `json:"roleContacts,omitempty"`
}

// UpdateDomainContactsInput is the PUT /v1/domains/{name}/contacts body.
// Registrant is required; roles follow the order-placing semantics (nil
// role = inherit the registrant; the server always fills all four slots
// upstream). A registrant change may trigger ICANN verification and a
// registrar transfer lock — the server surfaces those as normal errors.
type UpdateDomainContactsInput struct {
	Registrant   Registrant    `json:"registrant"`
	RoleContacts *RoleContacts `json:"roleContacts,omitempty"`
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
