package fcgi

import (
	"log/slog"
	"net/http"
	"os"
	"time"
)

// GatewayOption configures New.
type GatewayOption interface{ applyGateway(*config) }

// ScriptOption configures Gateway.Script.
type ScriptOption interface{ applyScript(*config) }

// Option works on New and on Script. On Script it overrides the gateway's
// value for that script only.
type Option interface {
	GatewayOption
	ScriptOption
}

type config struct {
	script       string
	maxConns     int
	spoolDir     string
	spoolMemory  int64
	documentRoot string
	protected    []string
	logger       *slog.Logger
	timeout      time.Duration
	maxBody      int64
	maxResponse  int64
	params       map[string]string
	paramsFunc   func(*http.Request) map[string]string
	pathInfo     func(*http.Request) string
	query        func(*http.Request) string
	dropHeaders  []string
	observer     func(*http.Request, CallInfo)
}

func defaultConfig() config {
	return config{
		maxConns:    16,
		spoolDir:    os.TempDir(),
		spoolMemory: 1 << 20,
		logger:      slog.Default(),
		timeout:     60 * time.Second,
		maxBody:     10 << 20,
		maxResponse: 1 << 30,
	}
}

// forScript copies the gateway config so script options never leak back.
func (c *config) forScript(name string) config {
	s := *c
	s.script = name
	s.params = make(map[string]string, len(c.params))
	for k, v := range c.params {
		s.params[k] = v
	}
	return s
}

type gatewayOption func(*config)

func (f gatewayOption) applyGateway(c *config) { f(c) }

type scriptOption func(*config)

func (f scriptOption) applyScript(c *config) { f(c) }

type bothOption func(*config)

func (f bothOption) applyGateway(c *config) { f(c) }
func (f bothOption) applyScript(c *config)  { f(c) }

// WithMaxConns caps concurrent calls to the responder. Set it to 2-4 times
// the responder's worker count: a cap equal to the worker count leaves each
// worker idle while the next call is dialled.
func WithMaxConns(n int) GatewayOption {
	return gatewayOption(func(c *config) { c.maxConns = n })
}

// WithSpool sets where request and response bodies spill to disk once they
// exceed inMemory bytes.
func WithSpool(dir string, inMemory int64) GatewayOption {
	return gatewayOption(func(c *config) { c.spoolDir, c.spoolMemory = dir, inMemory })
}

// WithDocumentRoot sets DOCUMENT_ROOT and the base of SCRIPT_FILENAME.
func WithDocumentRoot(dir string) GatewayOption {
	return gatewayOption(func(c *config) { c.documentRoot = dir })
}

// WithProtectedHeaders names client headers that never reach the responder.
// Names are matched after normalisation; a trailing "*" matches a prefix.
func WithProtectedHeaders(names ...string) GatewayOption {
	return gatewayOption(func(c *config) { c.protected = append(c.protected, names...) })
}

// WithLogger sets where responder stderr output and call errors go.
func WithLogger(l *slog.Logger) GatewayOption {
	return gatewayOption(func(c *config) { c.logger = l })
}

// WithTimeout sets the deadline of one call.
func WithTimeout(d time.Duration) Option {
	return bothOption(func(c *config) { c.timeout = d })
}

// WithMaxBody limits the request body; larger bodies answer 413.
func WithMaxBody(n int64) Option {
	return bothOption(func(c *config) { c.maxBody = n })
}

// WithMaxResponse limits what the responder may write to stdout, header
// block included; larger output answers 502. It bounds the spool.
func WithMaxResponse(n int64) Option {
	return bothOption(func(c *config) { c.maxResponse = n })
}

// WithParam sets a fixed parameter. A script's value replaces the
// gateway's; an empty value removes the parameter.
func WithParam(name, value string) Option {
	return bothOption(func(c *config) {
		if c.params == nil {
			c.params = map[string]string{}
		}
		c.params[name] = value
	})
}

// WithParams computes parameters per request. They are written last and
// replace every other value of the same name.
func WithParams(f func(*http.Request) map[string]string) Option {
	return bothOption(func(c *config) { c.paramsFunc = f })
}

// WithDropResponseHeaders removes responder headers before the client sees
// them.
func WithDropResponseHeaders(names ...string) Option {
	return bothOption(func(c *config) { c.dropHeaders = append(c.dropHeaders, names...) })
}

// WithPathInfo sets PATH_INFO per request.
func WithPathInfo(f func(*http.Request) string) ScriptOption {
	return scriptOption(func(c *config) { c.pathInfo = f })
}

// WithQuery sets QUERY_STRING per request instead of the raw query.
func WithQuery(f func(*http.Request) string) ScriptOption {
	return scriptOption(func(c *config) { c.query = f })
}
