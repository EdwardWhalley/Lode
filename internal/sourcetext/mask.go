// Masking a DSN, so that it is safe to show or to keep on disk.

package sourcetext

import (
	"net/url"
	"strings"
)

// UnparseableDSN is what MaskDSN returns for a DSN it shows none of.
const UnparseableDSN = "(unparseable DSN)"

// MaskDSN returns the parts of dsn that are safe to show if each is what its place says it is:
// the scheme, user, masked password, host, port and path. A query becomes "?…" and a fragment
// is dropped. A DSN that is not scheme://..., or for which mayHoldASecret, shows none of itself.
// Masking what MaskDSN returned changes nothing.
func MaskDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return UnparseableDSN
	}
	scheme, _, ok := SplitScheme(dsn)
	if !ok || !strings.EqualFold(scheme, u.Scheme) || mayHoldASecret(u) {
		return UnparseableDSN
	}
	if _, has := u.User.Password(); has {
		u.User = url.UserPassword(u.User.Username(), "xxxxx")
	}
	hasQuery := u.RawQuery != ""
	u.RawQuery, u.ForceQuery, u.Fragment, u.RawFragment = "", false, "", ""
	masked := u.String()
	if u.Host == "" && u.Path == "" && u.User == nil {
		masked = u.Scheme + "://"
	}
	if hasQuery {
		return masked + "?…"
	}
	return masked
}

// mayHoldASecret reports a DSN that does not read as what its places say: an option after a ";",
// an option or escaped query in the path, or an "@" after the host, which is how a password cut
// short by an unescaped "/", "?" or "#" looks. The user, host and path are read with % escapes
// undone.
func mayHoldASecret(u *url.URL) bool {
	return strings.Contains(u.User.Username(), ";") || strings.Contains(u.Host, ";") ||
		strings.ContainsAny(u.Path, ";@?=") ||
		strings.Contains(u.RawQuery, "@") || strings.Contains(u.EscapedFragment(), "@")
}
