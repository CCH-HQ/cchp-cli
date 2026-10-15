// Package auth stores the management token, binds it to one server and
// resolves the credential for each run.
package auth

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ErrNotSignedIn means no credential is available for the server.
var ErrNotSignedIn = errors.New("not signed in")

// NormalizeServer checks a server address and returns its canonical form:
// scheme, host, optional port and optional path prefix, without a trailing slash.
// Plain http is allowed only for this machine, so tokens never cross a network
// unencrypted.
func NormalizeServer(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("enter the server address")
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("that doesn't look like a web address: %w", err)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopback(u.Hostname()) {
			return "", errors.New("use https:// for servers on other machines; plain http is only allowed for this computer")
		}
	default:
		return "", errors.New("the address must start with https://")
	}
	if u.Hostname() == "" {
		return "", errors.New("the address needs a host name")
	}
	if u.User != nil {
		return "", errors.New("leave the user name and password out of the address")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("leave ? and # parts out of the address")
	}
	u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/api")
	u.RawPath = ""
	u.Host = strings.ToLower(u.Host)
	return u.String(), nil
}

// Origin returns scheme://host[:port] of a server address from NormalizeServer.
func Origin(server string) string {
	u, err := url.Parse(server)
	if err != nil {
		panic(fmt.Sprintf("auth: server address %q was not normalized: %v", server, err))
	}
	return u.Scheme + "://" + u.Host
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// DisplayName is how cchp names a signed-in identity to people.
func DisplayName(id Identity) string {
	switch {
	case id.Email != "":
		return id.Email
	case id.Name != "":
		return id.Name
	case id.UserID != "":
		return "user " + id.UserID
	}
	return "an unknown user"
}

// TokenPrefix returns the public part of a management token, cch_auth_live_<prefix>.
func TokenPrefix(token string) string {
	parts := strings.SplitN(token, "_", 5)
	if len(parts) == 5 && parts[0] == "cch" {
		return strings.Join(parts[:4], "_")
	}
	return ""
}

// CheckTokenShape rejects text that cannot be a management token.
func CheckTokenShape(token string) error {
	t := strings.TrimSpace(token)
	switch {
	case t == "":
		return errors.New("paste your management token")
	case strings.HasPrefix(t, "cch_llm_"):
		return errors.New("this is an LLM token for calling models; cchp needs a management token (cch_auth_live_…)")
	case !strings.HasPrefix(t, "cch_auth_"):
		return errors.New("management tokens start with cch_auth_live_")
	case TokenPrefix(t) == "":
		return errors.New("this token looks incomplete; copy it again")
	}
	return nil
}
