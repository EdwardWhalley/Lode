// The form of a Kind reached by a URL-style DSN: composing one from fields and parsing one
// back.

package adapter

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/EdwardWhalley/lode/internal/sourcetext"
)

// ServerForm is the form of a Kind whose Source is a server reached by a URL-style DSN,
// writing schemes[0].
func ServerForm(defaultPort int, schemes []string) Form {
	return Form{
		Fields: []Field{
			{Key: "host", Label: "Host", Type: Text, Required: true,
				Help: "Server name or address; an IPv6 address goes in [brackets]"},
			{Key: "port", Label: "Port", Type: Number, Min: minPort, Max: maxPort, Default: strconv.Itoa(defaultPort),
				Help: "Leave as it is unless the server listens elsewhere"},
			{Key: "database", Label: "Database", Type: Text,
				Help: "The database to open"},
			{Key: "user", Label: "User", Type: Text,
				Help: "The account to sign in as"},
			{Key: "password_var", Label: "Password variable", Type: EnvVar,
				Help: "Name of the environment variable holding the password, not the password"},
			{Key: "options", Label: "Options", Type: Text,
				Help: "Extra settings after the ?, such as tls=skip-verify"},
		},
		Compose: func(v Values, keep string) (string, error) { return composeServer(schemes, v, keep) },
		Parse: func(location string) (Values, bool, error) {
			values, literal, err := parseServer(schemes, location)
			if err != nil {
				_, rest, ok := sourcetext.SplitScheme(location)
				return nil, literal || ok && strings.Contains(rest, "@"), err
			}
			return values, literal, nil
		},
	}
}

const minPort, maxPort = 1, 65535

// errUnplaceable is all parseServer says about a location it cannot split, which may hold a
// password.
var errUnplaceable = errors.New("this location does not fit the form's fields")

// dsnParts is a DSN cut at its separators, every part as written (still escaped).
type dsnParts struct {
	scheme      string
	user        string
	password    string
	hasPassword bool
	hostPort    string
	database    string
	options     string
}

// literalPassword says the DSN holds a password of its own, not a ${NAME}.
func (p dsnParts) literalPassword() bool {
	if !p.hasPassword {
		return false
	}
	_, isVariable := sourcetext.SoleReference(p.password)
	return !isVariable
}

// splitDSN cuts a DSN by hand, because net/url rejects the braces of ${NAME} in a user.
func splitDSN(schemes []string, location string) (dsnParts, bool) {
	var p dsnParts
	scheme, rest, ok := sourcetext.SplitScheme(location)
	if !ok || !slices.ContainsFunc(schemes, func(s string) bool { return strings.EqualFold(s, scheme) }) {
		return p, false
	}
	p.scheme = scheme
	rest, p.options, _ = strings.Cut(rest, "?")
	authority, database, _ := strings.Cut(rest, "/")
	p.database = database
	p.hostPort = authority
	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		p.hostPort = authority[at+1:]
		p.user, p.password, p.hasPassword = strings.Cut(authority[:at], ":")
	}
	return p, true
}

// parseServer places a DSN in the form's fields, and reports a literal password rather than
// returning it.
func parseServer(schemes []string, location string) (Values, bool, error) {
	p, ok := splitDSN(schemes, location)
	if !ok {
		return nil, false, errUnplaceable
	}
	if optionsHoldPassword(p.options) {
		return nil, true, errUnplaceable
	}
	user, err := url.PathUnescape(p.user)
	if err != nil {
		return nil, false, errUnplaceable
	}
	database, err := url.PathUnescape(p.database)
	if err != nil {
		return nil, false, errUnplaceable
	}

	host, port := p.hostPort, ""
	if strings.HasPrefix(p.hostPort, "[") {
		end := strings.IndexByte(p.hostPort, ']')
		if end < 0 {
			return nil, false, errUnplaceable
		}
		host = p.hostPort[:end+1]
		if tail := p.hostPort[end+1:]; tail != "" {
			if !strings.HasPrefix(tail, ":") {
				return nil, false, errUnplaceable
			}
			port = tail[1:]
		}
	} else if colon := strings.LastIndexByte(p.hostPort, ':'); colon >= 0 {
		host, port = p.hostPort[:colon], p.hostPort[colon+1:]
	}
	if host == "" {
		return nil, false, errUnplaceable
	}

	values := Values{"host": host, "port": port, "database": database, "user": user, "options": p.options}
	if name, isVariable := sourcetext.SoleReference(p.password); isVariable {
		values["password_var"] = name
	}
	for k, v := range values {
		if v == "" {
			delete(values, k)
		}
	}

	if again, err := composeServer(schemes, values, location); err != nil || again != location {
		return nil, false, errUnplaceable
	}
	return values, p.literalPassword() && p.password != "", nil
}

// passwordOptionKeys are option names that carry a password in a DSN's query.
var passwordOptionKeys = map[string]bool{"password": true, "passwd": true, "pwd": true, "sslpassword": true}

// optionsHoldPassword reports a password among the options, the part after ?.
func optionsHoldPassword(options string) bool {
	for _, pair := range strings.FieldsFunc(options, func(r rune) bool { return r == '&' || r == ';' }) {
		name, _, _ := strings.Cut(pair, "=")
		if unescaped, err := url.QueryUnescape(name); err == nil {
			name = unescaped
		}
		if passwordOptionKeys[strings.ToLower(strings.TrimSpace(name))] {
			return true
		}
	}
	return false
}

// composeServer makes a DSN from the form's fields, never repeating a field value in an
// error.
func composeServer(schemes []string, v Values, keep string) (string, error) {
	host := v["host"]
	switch {
	case strings.TrimSpace(host) == "":
		return "", errors.New("host is required")
	case strings.ContainsAny(host, "/?#@") || strings.IndexFunc(host, unicode.IsSpace) >= 0 || strings.IndexFunc(host, unicode.IsControl) >= 0:
		return "", errors.New("host must be a name or address, with no spaces, control characters, slashes, @ or ?")
	case strings.HasPrefix(host, "[") != strings.HasSuffix(host, "]"),
		strings.HasPrefix(host, "[") && (strings.Count(host, "[") != 1 || strings.Count(host, "]") != 1),
		!strings.HasPrefix(host, "[") && strings.Contains(host, ":"):
		return "", errors.New("an IPv6 host goes in [brackets], and a port in its own field")
	}
	if p := v["port"]; p != "" {
		if n, err := sourcetext.Number(p); err != nil || n < minPort || n > maxPort {
			return "", fmt.Errorf("port must be a number from %d to %d", minPort, maxPort)
		}
	}
	if strings.IndexFunc(v["options"], unicode.IsControl) >= 0 {
		return "", errors.New("options must not contain line breaks or other control characters")
	}
	if optionsHoldPassword(v["options"]) {
		return "", errors.New("options must not contain a password: use the password variable field")
	}
	pw := v["password_var"]
	if pw != "" && !sourcetext.ValidVarName(pw) {
		return "", errors.New("password variable must be the name of an environment variable (letters, digits and _, not starting with a digit), not the password itself")
	}

	scheme, password := schemes[0], ""
	if pw != "" {
		password = ":" + sourcetext.Reference(pw)
	}
	if kept, ok := splitDSN(schemes, keep); ok {
		scheme = kept.scheme
		if pw == "" && kept.literalPassword() {
			password = ":" + kept.password
		}
	}

	var b strings.Builder
	b.WriteString(scheme + "://")
	if user := v["user"]; user != "" || password != "" {
		b.WriteString(sourcetext.MapAround(user, escapeUser) + password + "@")
	}
	b.WriteString(host)
	if p := v["port"]; p != "" {
		b.WriteString(":" + p)
	}
	if db := v["database"]; db != "" {
		b.WriteString("/" + sourcetext.MapAround(db, url.PathEscape))
	}
	if o := v["options"]; o != "" {
		b.WriteString("?" + o)
	}
	return b.String(), nil
}

func escapeUser(s string) string {
	return strings.TrimSuffix(url.UserPassword(s, "").String(), ":")
}
