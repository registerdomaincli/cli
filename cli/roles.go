package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/registerdomaincli/cli/apitypes"
	"github.com/registerdomaincli/cli/cli/i18n"
	"github.com/spf13/cobra"
)

// roleFlags captures the three non-registrant WHOIS roles (admin, tech,
// billing). Two flag shapes feed it: a repeatable --role <name>=<spec> and
// one --role-<name> <spec> each. A spec is either "auto" (the role is the
// registrant — the historical behavior), "registrant" (synonym) or a
// comma-separated list of field=value overrides applied to a copy of the
// inline registrant. Roles left unspecified are nil on the wire and inherit
// the registrant server-side.
//
// Overrides always derive from the INLINE registrant: when the order uses a
// saved contact set there is no local registrant to copy from, so a field
// spec is refused — pass the registrant flags too (or --own-contact).
type roleFlags struct {
	specs   []string // --role repeats
	admin   string   // --role-admin
	tech    string   // --role-tech
	billing string   // --role-billing

	parsed map[string]string // role name → spec, after collect
}

// roleNames is the canonical role order (wire, output, prompts).
var roleNames = [...]string{"admin", "tech", "billing"}

// roleFieldSet lists the override-able registrant fields.
var roleFieldSet = map[string]bool{
	"first-name": true, "last-name": true, "organization": true,
	"email": true, "phone": true, "address1": true, "address2": true,
	"city": true, "state": true, "zip": true, "country": true,
}

// addRoleFlags registers --role and --role-<name> on a command.
func addRoleFlags(cmd *cobra.Command, rf *roleFlags) {
	cmd.Flags().StringArrayVar(&rf.specs, "role", nil,
		"per-role WHOIS contact: --role admin=auto | admin=field=value,... (repeatable; see also --role-admin)")
	for _, name := range roleNames {
		p := map[string]*string{"admin": &rf.admin, "tech": &rf.tech, "billing": &rf.billing}[name]
		cmd.Flags().StringVar(p, "role-"+name, "",
			name+" WHOIS role: auto, registrant, or comma-separated field=value overrides of the inline registrant")
	}
}

// collect reads the flags off the command into parsed. Unknown roles, empty
// specs, unknown fields and duplicate roles fail here — before any order or
// prompt happens.
func (rf *roleFlags) collect(cmd *cobra.Command) error {
	rf.parsed = map[string]string{}
	add := func(name, spec string) error {
		name = strings.ToLower(strings.TrimSpace(name))
		switch name {
		case "admin", "tech", "billing":
		default:
			return errors.New(i18n.T("err.role.unknown", "Role", name))
		}
		if _, dup := rf.parsed[name]; dup {
			return errors.New(i18n.T("err.role.dup", "Role", name))
		}
		if _, _, err := parseRoleSpec(name, spec); err != nil {
			return err
		}
		rf.parsed[name] = strings.TrimSpace(spec)
		return nil
	}
	for _, s := range rf.specs {
		name, spec, _ := strings.Cut(s, "=")
		if err := add(name, spec); err != nil {
			return err
		}
	}
	for _, name := range roleNames {
		v := map[string]string{"admin": rf.admin, "tech": rf.tech, "billing": rf.billing}[name]
		if strings.TrimSpace(v) == "" {
			continue
		}
		if err := add(name, v); err != nil {
			return err
		}
	}
	return nil
}

// parseRoleSpec validates one spec and returns its kind: "auto",
// "registrant" or "fields" (with the parsed overrides).
func parseRoleSpec(name, spec string) (string, map[string]string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", nil, errors.New(i18n.T("err.role.spec", "Role", name, "Spec", spec))
	}
	if spec == "auto" || spec == "registrant" {
		return spec, nil, nil
	}
	fields := map[string]string{}
	for _, kv := range strings.Split(spec, ",") {
		if !strings.Contains(kv, "=") {
			return "", nil, errors.New(i18n.T("err.role.spec", "Role", name, "Spec", spec))
		}
		k, v, _ := strings.Cut(kv, "=")
		k = strings.ToLower(strings.TrimSpace(k))
		if !roleFieldSet[k] {
			return "", nil, errors.New(i18n.T("err.role.spec", "Role", name, "Spec", spec))
		}
		fields[k] = strings.TrimSpace(v)
	}
	if len(fields) == 0 {
		return "", nil, errors.New(i18n.T("err.role.spec", "Role", name, "Spec", spec))
	}
	return "fields", fields, nil
}

// apply builds the wire RoleContacts. base is the inline registrant (nil
// when the order uses a saved contact set). Roles parsed as auto/registrant
// stay nil — same as omitted — so the server's registrant-clone keeps
// applying and legacy payloads stay byte-identical.
func (rf *roleFlags) apply(base *apitypes.Registrant) (*apitypes.RoleContacts, error) {
	if len(rf.parsed) == 0 {
		return nil, nil
	}
	rc := &apitypes.RoleContacts{}
	for _, name := range roleNames {
		spec, ok := rf.parsed[name]
		if !ok {
			continue
		}
		kind, fields, err := parseRoleSpec(name, spec)
		if err != nil {
			return nil, err
		}
		if kind != "fields" {
			continue
		}
		if base == nil {
			return nil, errors.New(i18n.T("err.role.noBase", "Role", name))
		}
		c := *base
		for k, v := range fields {
			if err := applyRoleField(&c, k, v); err != nil {
				return nil, err
			}
		}
		switch name {
		case "admin":
			rc.Admin = &c
		case "tech":
			rc.Tech = &c
		case "billing":
			rc.Billing = &c
		}
	}
	if rc.Admin == nil && rc.Tech == nil && rc.Billing == nil {
		return nil, nil
	}
	return rc, nil
}

// applyRoleField writes one override into a copy of the registrant. Field
// names are the flag spellings (kebab-case), country is normalized like the
// registrant flags do.
func applyRoleField(c *apitypes.Registrant, field, value string) error {
	switch field {
	case "first-name":
		c.FirstName = value
	case "last-name":
		c.LastName = value
	case "organization":
		c.Organization = value
	case "email":
		c.Email = value
	case "phone":
		c.Phone = value
	case "address1":
		c.Address1 = value
	case "address2":
		c.Address2 = value
	case "city":
		c.City = value
	case "state":
		c.State = value
	case "zip":
		c.Zip = value
	case "country":
		c.Country = strings.ToUpper(value)
	default:
		return fmt.Errorf("unknown role field %q", field)
	}
	return nil
}

// resolveRoles finishes contact resolution for order commands and contacts
// add. Precedence: explicit --role* flags win (REPLACE semantics — nothing
// merges with a saved set's roles); with no flags but a just-prompted
// registrant on a TTY the roles are offered interactively; otherwise the
// contact set's own roles (possibly nil) are inherited unchanged.
func resolveRoles(cmd *cobra.Command, app *App, rf *roleFlags, reg *apitypes.Registrant, prompted bool, setRoles *apitypes.RoleContacts) (*apitypes.RoleContacts, error) {
	if err := rf.collect(cmd); err != nil {
		return nil, err
	}
	if len(rf.parsed) > 0 {
		return rf.apply(reg)
	}
	if prompted && reg != nil && app.p.stdinIsTTY() {
		return promptRoles(app.p, *reg), nil
	}
	return setRoles, nil
}

// promptRoles offers per-role contacts after the registrant prompt. An
// empty/EOF answer anywhere keeps the registrant for that role. Reaching
// the end with every role declined returns nil so the wire payload stays
// byte-identical to the legacy all-registrant shape.
func promptRoles(p *prompter, reg apitypes.Registrant) *apitypes.RoleContacts {
	if !p.confirm(i18n.T("roles.askAny")) {
		return nil
	}
	rc := &apitypes.RoleContacts{}
	for _, name := range roleNames {
		if !p.confirm(i18n.T("roles.askRole", "Role", name)) {
			continue
		}
		c := promptRoleContact(p, name, reg)
		switch name {
		case "admin":
			rc.Admin = &c
		case "tech":
			rc.Tech = &c
		case "billing":
			rc.Billing = &c
		}
	}
	return rc
}

// promptRoleContact asks every registrant field for one role, prefilled with
// the registrant's values: enter keeps the default, optional fields accept
// an empty answer.
func promptRoleContact(p *prompter, role string, reg apitypes.Registrant) apitypes.Registrant {
	keep := func(label, current string) string {
		q := i18n.T(label, "Role", role)
		if current != "" {
			q += " [" + current + "]"
		}
		fmt.Fprint(p.out, q)
		v := p.line()
		if v == "" {
			return current
		}
		return v
	}
	c := reg
	c.FirstName = keep("role.prompt.firstName", reg.FirstName)
	c.LastName = keep("role.prompt.lastName", reg.LastName)
	c.Organization = keep("role.prompt.organization", reg.Organization)
	c.Email = keep("role.prompt.email", reg.Email)
	c.Phone = keep("role.prompt.phone", reg.Phone)
	c.Address1 = keep("role.prompt.address1", reg.Address1)
	c.Address2 = keep("role.prompt.address2", reg.Address2)
	c.City = keep("role.prompt.city", reg.City)
	c.State = keep("role.prompt.state", reg.State)
	c.Zip = keep("role.prompt.zip", reg.Zip)
	c.Country = strings.ToUpper(keep("role.prompt.country", reg.Country))
	return c
}
