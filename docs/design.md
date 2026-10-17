# fcgi-go serves HTTP requests from a FastCGI responder through one handler per script

Status: Draft, 2026-10-17. The API exists; the behaviour below does not. Every handler answers `501 Not Implemented` today.

fcgi-go is a Go library that puts a FastCGI responder such as php-fpm behind `net/http`. A responder is the FastCGI process that runs the scripts. It replaces the FastCGI part of a web server like nginx; routing stays with the caller. This page is for the engineer who implements the library and the engineer who mounts it. It fixes the public API, the behaviour behind it, and how the [compliance suite](../compliance/README.md) proves them.

## The caller routes, the library runs one script per handler

```go
gw := fcgi.New("unix:/run/php/fpm.sock",
	fcgi.WithMaxConns(30),
	fcgi.WithTimeout(120*time.Second),
	fcgi.WithDocumentRoot("/var/www/html"),
	fcgi.WithProtectedHeaders("X-Auth-*"),
	fcgi.WithParam("APP_ENV", "production"),
)

mux.Handle("/api/", gw.Script("api.php",
	fcgi.WithParams(func(r *http.Request) map[string]string {
		return map[string]string{"HTTP_X_AUTH_SUBJECT": subjectFrom(r)}
	}),
))
mux.Handle("/export/", gw.Script("export.php",
	fcgi.WithTimeout(600*time.Second),
	fcgi.WithParam("APP_ENV", "export"),
))
```

The library implements CGI/1.1 and FastCGI 1.0. Path rewrites, script allowlists, static files, and authentication are the caller's `http.ServeMux` code.

## The public API is one package

```go
package fcgi

func New(address string, opts ...GatewayOption) *Gateway
func (g *Gateway) Script(name string, opts ...ScriptOption) http.HandlerFunc
func (g *Gateway) Ping(ctx context.Context) error
func (g *Gateway) Shutdown(ctx context.Context) error
```

`address` is `unix:<path>` or `tcp:<host:port>`.

Options come in two kinds. An `Option` works on `New` and on `Script`. On `Script` it overrides the gateway's value for that script only. A `GatewayOption` works on `New` only, because it describes the shared connection to the responder.

| Option | Kind | Default | Meaning |
| --- | --- | --- | --- |
| `WithMaxConns(n)` | gateway | 16 | concurrent calls to the responder; set it to the responder's worker count |
| `WithSpool(dir, inMemory)` | gateway | `os.TempDir()`, 1 MiB | the spool: request and response bodies stay in memory up to `inMemory` bytes, then move to a file in `dir` |
| `WithDocumentRoot(dir)` | gateway | `""` | `DOCUMENT_ROOT`, and the base of `SCRIPT_FILENAME` |
| `WithProtectedHeaders(names...)` | gateway | none | client headers that never reach the responder; a trailing `*` matches a prefix |
| `WithLogger(l)` | gateway | `slog.Default()` | where stderr output and errors go |
| `WithTimeout(d)` | both | 60 s | deadline of one call, from the wait for a responder slot to the last byte written to the spool. Reading the request body does not count: a slow upload holds no responder worker |
| `WithMaxBody(n)` | both | 10 MiB | larger request bodies answer 413 |
| `WithParam(name, value)` | both | none | a fixed parameter |
| `WithParams(f)` | both | none | parameters computed per request |
| `WithPathInfo(f)` | script | derived | `PATH_INFO` per request; by default the path after `/<script>`, as RFC 3875 section 3.3 splits the script URI |
| `WithQuery(f)` | script | the raw query | `QUERY_STRING` per request |
| `WithDropResponseHeaders(names...)` | both | none | responder headers removed before the client sees them |

## Parameters are written in a fixed order, and later ones win

1. The CGI/1.1 meta-variables that RFC 3875 section 4 derives from the request, such as `REQUEST_METHOD` and `QUERY_STRING`.
2. One `HTTP_<NAME>` variable per client header, after protected headers are dropped (see [Client headers cannot forge caller parameters](#client-headers-cannot-forge-caller-parameters)).
3. Gateway `WithParam` values.
4. Script `WithParam` values.
5. The result of `WithParams` for this request: the script's function if set, else the gateway's.

A name set at a later stage replaces the earlier value. An empty string from stages 3 to 5 removes the parameter.

## The gateway deviates from RFC 3875 where applications depend on nginx

Each deviation follows what nginx sends, because PHP applications rely on it. The compliance cases for the RFC rule stay in the catalog and are waived with the reason below.

| RFC 3875 rule | fcgi-go | Why |
| --- | --- | --- |
| Section 4.1.18: a server SHOULD NOT pass `Authorization` | passes it as `HTTP_AUTHORIZATION` | applications need it for Basic and Bearer authentication |
| Section 4.1.18: a server SHOULD remove `Content-Type` and `Content-Length` from `HTTP_*` | passes them as `HTTP_CONTENT_TYPE` and `HTTP_CONTENT_LENGTH` too | applications detect JSON bodies on `HTTP_CONTENT_TYPE` |
| Section 3.3: `SCRIPT_NAME` may be empty when `PATH_INFO` covers the path | `SCRIPT_NAME` is always `/<script>` | front controllers compare it with their own name |

## The gateway sets these variables beyond RFC 3875

| Variable | Value |
| --- | --- |
| `REQUEST_URI` | the raw request-target as the client sent it, with the query; not decoded, not normalised |
| `HTTPS` | `on` when the client connection uses TLS; absent otherwise |
| `DOCUMENT_ROOT` | the value of `WithDocumentRoot` |
| `SCRIPT_FILENAME` | `DOCUMENT_ROOT` plus `/<script>` |

## Client headers cannot forge caller parameters

A caller passes identity or environment through parameters such as `HTTP_X_AUTH_SUBJECT`. A client must not be able to produce them.

1. Each client header name is normalised: upper case, and every character outside `A-Z0-9` becomes `_`. A header whose normalised name matches a protected name is dropped.
2. A client header whose raw name contains `_` or `.` is dropped. Some runtimes fold `.` and space to `_` when they read parameters, so `X.Auth.Subject` could arrive as `HTTP_X_AUTH_SUBJECT`.
3. Caller parameters are written after all client-derived ones (order above).

## Both directions are buffered, so slow clients never hold a responder worker

- The request body is read completely before the responder sees the request. Up to the in-memory limit it stays in memory; above it, it spills to the spool directory. `CONTENT_LENGTH` is always exact, also for chunked HTTP/1.1 bodies and HTTP/2 bodies without `content-length`.
- The responder's output is read completely into the spool before the client receives a byte. The worker is free as soon as the script ends.

## Each call gets its own connection, capped by a semaphore

php-fpm does not multiplex requests on one connection (`FCGI_MPXS_CONNS` is 0). A kept-alive connection holds its worker even while idle. Version 0.1 therefore dials the address per call and does not set `FCGI_KEEP_CONN`. `WithMaxConns` caps concurrent calls; a call that gets no slot before its deadline answers 503.

## A cancelled or failed call closes its connection

php-fpm ignores `FCGI_ABORT_REQUEST`. Closing the connection is the only cancel, so the library closes it on deadline, client cancel, or any protocol error. A closed connection is never reused.

| Situation | Status |
| --- | --- |
| responder address unreachable | 502 |
| no free slot before the deadline | 503 |
| deadline reached during the call | 504 |
| responder closed without `FCGI_END_REQUEST`, or sent broken framing | 502 |
| responder output without a complete CGI header block | 502 |
| responder `Content-Length` differs from the body it sent | 502 |
| gateway shutting down | 503 |

A failed call never sends part of the responder's output to the client. The library never retries a call.

## The responder's output becomes the HTTP response under RFC 3875 section 6

- `Status:` sets the status code and reason. Without it the status is 200.
- A `Location` with an absolute URI and no `Status:` gives 302.
- `FCGI_STDERR` content goes to the logger at warn level, capped at 8 KiB per call. It never reaches the client.
- A non-zero application status in `FCGI_END_REQUEST` is logged; the response stands.
- A response without `Content-Length` gets one from the spooled body. For `HEAD` the body is empty by definition, so the gateway adds none.
- `Range` request headers reach the responder unchanged. The gateway never builds a 206 itself; RFC 9110 section 14.2 allows a server to ignore `Range`.

## Readiness never runs a script

`Ping` dials the address and closes the connection. A FastCGI request would occupy a worker, and a saturated pool would then look unready. `Shutdown` answers 503 to new calls and waits for running ones until its context ends.

## Correctness is defined by the compliance suite

The suite in `compliance/` holds cases taken from the specifications and the guarantees on this page. Four reference responders in PHP, C, C++, and Rust implement the same fixtures: small test scripts with fixed behaviour. Every case runs over HTTP/1.1 and HTTP/2 against each responder. HTTP/3 cases exist in the catalog and run once the library adds HTTP/3. A release requires every HTTP/1.1 and HTTP/2 case to pass. HTTP/3 cases stay pending until HTTP/3 lands.

## Open questions

1. Keep-alive to the responder: off in 0.1. It comes back only with a measured gain and an idle timeout below the responder's worker lifetime.
2. Streaming responses that bypass the spool: not in 0.1.
3. HTTP/3: catalog first, transport later.
