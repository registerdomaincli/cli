package cli

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/registerdomaincli/cli/apitypes"
	"github.com/registerdomaincli/cli/cli/i18n"
)

// prompter reads answers from one input stream. It wraps the stream in a
// single bufio.Reader for the whole run — one reader per prompt would drop
// buffered input — and stops re-asking once EOF is reached instead of
// spinning forever on a closed pipe.
type prompter struct {
	r   *bufio.Reader
	src io.Reader
	out io.Writer
	eof bool
}

func newPrompter(in io.Reader, out io.Writer) *prompter {
	return &prompter{r: bufio.NewReader(in), src: in, out: out}
}

func (p *prompter) line() string {
	if p.eof {
		return ""
	}
	line, err := p.r.ReadString('\n')
	if err != nil && line == "" {
		p.eof = true
		return ""
	}
	return strings.TrimSpace(line)
}

// ask prints a label and reads one line.
func (p *prompter) ask(label string) string {
	fmt.Fprint(p.out, label)
	return p.line()
}

// required keeps asking until a non-empty answer is given; at EOF it gives
// up with an empty answer rather than looping forever.
func (p *prompter) required(label string) string {
	for {
		v := p.ask(label)
		if v != "" {
			return v
		}
		if p.eof {
			return ""
		}
	}
}

// confirm answers true only on y/yes (case-insensitive); empty input is the
// safe "no".
func (p *prompter) confirm(question string) bool {
	ans := strings.ToLower(p.ask(question))
	return ans == "y" || ans == "yes"
}

// registrant asks for every registrant field. Optional fields accept an empty
// answer; required fields are re-asked. The email default (the account email)
// is accepted by pressing enter.
func (p *prompter) registrant(defaultEmail string) apitypes.Registrant {
	var r apitypes.Registrant
	r.FirstName = p.required(i18n.T("prompt.firstName"))
	r.LastName = p.required(i18n.T("prompt.lastName"))
	r.Organization = p.ask(i18n.T("prompt.organization"))
	label := i18n.T("prompt.email", "Email", defaultEmail)
	for {
		v := p.ask(label)
		if v != "" {
			r.Email = v
			break
		}
		if defaultEmail != "" || p.eof {
			r.Email = defaultEmail
			break
		}
	}
	r.Phone = p.required(i18n.T("prompt.phone"))
	r.Address1 = p.required(i18n.T("prompt.address1"))
	r.Address2 = p.ask(i18n.T("prompt.address2"))
	r.City = p.required(i18n.T("prompt.city"))
	r.State = p.ask(i18n.T("prompt.state"))
	r.Zip = p.required(i18n.T("prompt.zip"))
	r.Country = strings.ToUpper(p.required(i18n.T("prompt.country")))
	return r
}

// validExternalURL reports whether rawURL is safe to hand to the OS opener.
// Only https is accepted; plain http is allowed for a loopback host so the
// local dev server (DOMAIN_CLI_ENV=dev) still opens. Everything else is
// rejected — file://, smb://, any registered custom scheme, URLs carrying
// userinfo, and unparseable input. Callers print the URL when this fails.
func validExternalURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.User != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return u.Host != ""
	case "http":
		switch u.Hostname() {
		case "localhost", "127.0.0.1", "::1":
			return true
		}
	}
	return false
}

// openBrowser opens rawURL in the system browser (xdg-open / open /
// rundll32). Over SSH, when the URL is not a safe external URL, or when the
// opener fails, it does nothing and the caller prints the URL instead.
//
// Windows deliberately avoids `cmd /c start`: cmd.exe re-parses the command
// line, so an ampersand anywhere in a server-supplied URL — legal in a Stripe
// checkout URL — would split it into a second command. rundll32 receives the
// URL as a plain argv entry and runs no shell.
func openBrowser(rawURL string) bool {
	if !validExternalURL(rawURL) {
		return false
	}
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {
		return false
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	if err := cmd.Start(); err != nil {
		return false
	}
	go func() { _ = cmd.Wait() }()
	return true
}

// Reader exposes the underlying input stream (for TTY detection).
func (p *prompter) Reader() io.Reader {
	if p.r == nil {
		return nil
	}
	// bufio.Reader does not expose its source; keep a reference instead.
	return p.src
}
