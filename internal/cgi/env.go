// Package cgi maps HTTP requests to CGI/1.1 variables and CGI responses back
// to HTTP, following RFC 3875.
package cgi

import (
	"maps"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/braska-io/fcgi-go/internal/fastcgi"
)

// Config is everything besides the request that shapes the variables.
type Config struct {
	Script       string
	DocumentRoot string
	Protected    []string
	// PathInfo and Query override the derived values when non-nil.
	PathInfo   func(*http.Request) string
	Query      func(*http.Request) string
	Gateway    map[string]string
	ScriptVars map[string]string
	ParamsFunc func(*http.Request) map[string]string
	// BodyLength is the exact length of the spooled body; HasBody tells
	// whether the request carried a message-body at all (RFC 3875 4.1.2).
	BodyLength int64
	HasBody    bool
}

// hopByHop fields describe one connection and never reach the script.
var hopByHop = map[string]bool{
	"Connection": true, "Keep-Alive": true, "Proxy-Connection": true,
	"Te": true, "Transfer-Encoding": true, "Upgrade": true,
	// Credentials for a proxy, not for the application behind us.
	"Proxy-Authorization": true,
}

// env is the parameter list in the order the stages set it. A slice beats a
// map here: a request has a few dozen variables, and a linear scan over them
// costs less than hashing and the map's allocations.
type env struct {
	ps []fastcgi.Param
}

func newEnv() *env { return &env{ps: make([]fastcgi.Param, 0, 32)} }

func (e *env) set(name, value string) {
	for i := range e.ps {
		if e.ps[i].Name == name {
			e.ps[i].Value = value
			return
		}
	}
	e.ps = append(e.ps, fastcgi.Param{Name: name, Value: value})
}

func (e *env) remove(name string) {
	e.ps = slices.DeleteFunc(e.ps, func(p fastcgi.Param) bool { return p.Name == name })
}

// override applies stages 3 to 5: an empty value removes the variable.
func (e *env) override(m map[string]string) {
	if len(m) == 0 {
		return
	}
	for _, name := range sortedKeys(m) {
		if m[name] == "" {
			e.remove(name)
			continue
		}
		e.set(name, m[name])
	}
}

func (e *env) params() []fastcgi.Param { return e.ps }

// Build returns the FastCGI parameters in the order docs/design.md fixes.
func Build(r *http.Request, c Config) []fastcgi.Param {
	e := newEnv()
	setMeta(e, r, c)
	setHeaders(e, r, c.Protected)
	e.override(c.Gateway)
	e.override(c.ScriptVars)
	if c.ParamsFunc != nil {
		e.override(c.ParamsFunc(r))
	}
	return e.params()
}

func setMeta(e *env, r *http.Request, c Config) {
	e.set("GATEWAY_INTERFACE", "CGI/1.1")
	e.set("SERVER_SOFTWARE", "fcgi-go")
	e.set("SERVER_PROTOCOL", r.Proto)
	name, _ := splitHostPort(r.Host)
	e.set("SERVER_NAME", name)
	if addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		if _, port, err := net.SplitHostPort(addr.String()); err == nil {
			e.set("SERVER_PORT", port)
		}
	}
	host, port := splitHostPort(r.RemoteAddr)
	e.set("REMOTE_ADDR", host)
	// RFC 3875 section 4.1.9 allows REMOTE_ADDR as the value; a reverse
	// lookup per request would stall the call.
	e.set("REMOTE_HOST", host)
	if port != "" {
		e.set("REMOTE_PORT", port)
	}
	e.set("REQUEST_METHOD", r.Method)
	e.set("QUERY_STRING", queryString(r, c))
	e.set("REQUEST_URI", r.RequestURI)
	scriptName := "/" + c.Script
	e.set("SCRIPT_NAME", scriptName)
	if pi := pathInfo(r, c, scriptName); pi != "" {
		e.set("PATH_INFO", pi)
		e.set("PATH_TRANSLATED", c.DocumentRoot+pi)
	}
	if c.HasBody {
		e.set("CONTENT_LENGTH", strconv.FormatInt(c.BodyLength, 10))
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		e.set("CONTENT_TYPE", ct)
	}
	if r.TLS != nil {
		e.set("HTTPS", "on")
	}
	e.set("DOCUMENT_ROOT", c.DocumentRoot)
	e.set("SCRIPT_FILENAME", c.DocumentRoot+scriptName)
}

func queryString(r *http.Request, c Config) string {
	if c.Query != nil {
		return c.Query(r)
	}
	return r.URL.RawQuery
}

func pathInfo(r *http.Request, c Config, scriptName string) string {
	if c.PathInfo != nil {
		return c.PathInfo(r)
	}
	rest, ok := strings.CutPrefix(r.URL.Path, scriptName)
	if !ok {
		return ""
	}
	return rest
}

// splitHostPort tolerates a missing port, which net.SplitHostPort rejects.
func splitHostPort(s string) (host, port string) {
	if h, p, err := net.SplitHostPort(s); err == nil {
		return h, p
	}
	return strings.Trim(s, "[]"), ""
}

func setHeaders(e *env, r *http.Request, protected []string) {
	drop := connectionTokens(r.Header)
	if r.Host != "" {
		e.set("HTTP_HOST", r.Host)
	}
	for _, name := range sortedKeys(r.Header) {
		if hopByHop[name] || drop[name] || strings.ContainsAny(name, "_.") {
			continue
		}
		key := httpName(name)
		if isProtected(key[len("HTTP_"):], protected) {
			continue
		}
		e.set(key, joinValues(name, r.Header[name]))
	}
}

// connectionTokens returns the canonical names the Connection header lists.
func connectionTokens(h http.Header) map[string]bool {
	values := h.Values("Connection")
	if len(values) == 0 {
		return nil
	}
	out := map[string]bool{}
	for _, v := range values {
		for tok := range strings.SplitSeq(v, ",") {
			if tok = strings.TrimSpace(tok); tok != "" {
				out[http.CanonicalHeaderKey(tok)] = true
			}
		}
	}
	return out
}

// joinValues joins repeated fields per RFC 9110 section 5.3; Cookie uses
// "; " because RFC 6265 section 5.4 defines it so.
func joinValues(name string, values []string) string {
	sep := ", "
	if name == "Cookie" {
		sep = "; "
	}
	return strings.Join(values, sep)
}

// httpName is "HTTP_" plus the name upper-cased, with everything outside
// A-Z0-9 turned into "_", built in one allocation.
func httpName(name string) string {
	b := make([]byte, len("HTTP_")+len(name))
	copy(b, "HTTP_")
	for i := range len(name) {
		b[len("HTTP_")+i] = normByte(name[i])
	}
	return string(b)
}

func normByte(c byte) byte {
	switch {
	case c >= 'a' && c <= 'z':
		return c - 'a' + 'A'
	case c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return c
	}
	return '_'
}

// isProtected reports whether the normalised name n matches a protected
// pattern. A trailing "*" matches a prefix; patterns are normalised while
// comparing, so the check allocates nothing.
func isProtected(n string, protected []string) bool {
	for _, p := range protected {
		prefix := strings.HasSuffix(p, "*")
		if prefix {
			p = strings.TrimSuffix(p, "*")
		}
		if len(n) < len(p) || (!prefix && len(n) != len(p)) {
			continue
		}
		match := true
		for i := range len(p) {
			if n[i] != normByte(p[i]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
