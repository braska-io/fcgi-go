# fcgi-go

fcgi-go is a Go library that passes HTTP requests to a FastCGI responder, such as php-fpm, and returns its response.
You mount one `http.HandlerFunc` per script in your own router.

FastCGI is the binary protocol that web servers such as nginx use to talk to php-fpm.
A responder is the FastCGI process that runs your scripts.

> [!WARNING]
> fcgi-go does not work yet.
> The public API and the compliance suite exist.
> The implementation does not: every handler answers `501 Not Implemented`.
> There is no release.
> Do not use it in production.

## Mount one handler per script

The example compiles against the current API.
Every request answers 501 until the implementation lands.

```sh
go get github.com/braska-io/fcgi-go
```

```go
package main

import (
	"log"
	"net/http"
	"time"

	fcgi "github.com/braska-io/fcgi-go"
)

func main() {
	gw := fcgi.New("unix:/run/php/fpm.sock",
		fcgi.WithMaxConns(30),
		fcgi.WithTimeout(120*time.Second),
		fcgi.WithDocumentRoot("/var/www/html"),
		fcgi.WithProtectedHeaders("X-Auth-*"),
		fcgi.WithParam("APP_ENV", "production"),
	)

	mux := http.NewServeMux()
	mux.Handle("/api/", gw.Script("api.php",
		fcgi.WithParams(func(r *http.Request) map[string]string {
			return map[string]string{"HTTP_X_AUTH_SUBJECT": subjectFrom(r)}
		}),
	))
	mux.Handle("/export/", gw.Script("export.php",
		fcgi.WithTimeout(600*time.Second),
		fcgi.WithParam("APP_ENV", "export"),
	))

	log.Fatal(http.ListenAndServe("127.0.0.1:8080", mux))
}

// subjectFrom returns the authenticated user. Your authentication
// middleware decides how; a fixed value keeps the example short.
func subjectFrom(r *http.Request) string { return "anonymous" }
```

The address is `unix:<path>` or `tcp:<host:port>`.
`WithProtectedHeaders("X-Auth-*")` drops client headers that start with `X-Auth-`.
Only your code can then set `HTTP_X_AUTH_SUBJECT`.
[docs/design.md](docs/design.md) lists every option, its default, and the behaviour behind it.

## Why fcgi-go exists

Many Go services in front of a PHP application run nginx for one reason: nginx speaks FastCGI to php-fpm.
Routing then lives in two places: the Go code and the nginx configuration.

fcgi-go replaces the FastCGI part of nginx.
Routing, path rewrites, static files, and authentication stay in your `http.ServeMux`.
The library implements CGI/1.1 and FastCGI 1.0, and nothing else.
CGI/1.1 defines the request variables a script receives, such as `REQUEST_METHOD` and `QUERY_STRING`.

The design fixes these guarantees:

- A client header cannot forge a parameter that the caller sets, such as `HTTP_X_AUTH_SUBJECT`.
- The library buffers request and response bodies, in memory and above a limit on disk.
  This buffer is the spool.
  A slow client never holds a responder worker, such as a php-fpm child process.
- A cancelled or failed call closes its connection.
  The client never receives partial responder output.
- Each failed call maps to one documented status: 502, 503, or 504.
- `Ping` checks readiness without running a script.

## The compliance suite defines correctness

The suite in [compliance/](compliance/README.md) holds cases taken from the specifications:

- RFC 3875 (CGI/1.1)
- FastCGI 1.0
- RFC 9110, RFC 9112, RFC 9113, and RFC 9114 (HTTP semantics, HTTP/1.1, HTTP/2, HTTP/3)
- the library's own contract in [docs/design.md](docs/design.md)

A case is one HTTP request together with the result it must produce.
Each case cites its source document and section.
It also names a tier and a level.
The tier says which contract the case checks, for example the CGI specification or this library's design.
The level is `MUST`, `SHOULD`, `MAY`, or `OBSERVED`.
[compliance/README.md](compliance/README.md) defines both.

The suite sends every case over HTTP/1.1 and HTTP/2 through the handler under test.
HTTP/3 cases exist in the catalog.
They show as pending until the library supports HTTP/3.

The suite runs every case against four reference responders.
Each one implements the same fixtures: small test scripts with fixed behaviour, such as `dump`, which returns every parameter it received.
No case therefore depends on one runtime.

| Responder | FastCGI stack | Process model |
| --- | --- | --- |
| PHP 8.3 | php-fpm | pre-forked children |
| C | libfcgi | one process per connection slot |
| C++ | own protocol code | threads |
| Rust | own protocol code; also sends malformed records on request | async tasks |

[compliance/RESPONDERS.md](compliance/RESPONDERS.md) defines the fixtures.
A failing case means fcgi-go is wrong, not the case.

### Run the suite

You need Go 1.27, Docker with Compose, and [Task](https://taskfile.dev).

The repository holds two Go modules: the library and the compliance suite.
Run the library's unit tests and vet both modules:

```sh
task test
task lint
```

Check that the catalog loads, without running any case:

```sh
task catalog
```

Build and start the four responders, then run every case against each:

```sh
task compliance
```

The responders listen on `127.0.0.1` ports 9001 to 9004.
Stop them afterwards:

```sh
task responders:down
```

The suite uses the [Ginkgo](https://github.com/onsi/ginkgo) test framework.
Each case carries its tier, its level, and its transport (`h1`, `h2`, `h3`) as Ginkgo labels.
Pass Ginkgo flags after `--` to run a subset, for example one case or one transport:

```sh
task compliance -- -ginkgo.focus "gw-protected-header-dropped"
task compliance -- -ginkgo.label-filter "h2 && MUST"
```

Until the implementation exists, `task compliance` fails.
The failing cases are the specification: each one names behaviour that is still to be built.
CI runs the same suite and fails for the same reason.

## The repository holds the library and the compliance suite

```text
.
├── fcgi.go            # public API: New, Script, Ping, Shutdown (stub)
├── options.go         # gateway and script options
├── docs/design.md     # API, behaviour, and error statuses
├── compliance/        # compliance suite, a separate Go module
│   ├── catalog/       # cases and scenarios as YAML
│   ├── fixtures/      # reference responders: php, c, cpp, rust
│   ├── README.md      # case schema and tiers
│   └── RESPONDERS.md  # fixtures every reference responder implements
├── compose.yaml       # starts the reference responders
└── Taskfile.yml       # test, lint, catalog, compliance
```

The library module has no dependencies outside the Go standard library.
The compliance module carries the test dependencies, so they never reach your `go.mod`.

## Roadmap: version 0.1 comes first

1. Implement version 0.1 against the compliance suite.
2. Release 0.1 once every case passes over HTTP/1.1 and HTTP/2 on all four reference responders.
3. Keep-alive to the responder is off in 0.1: each call opens its own connection.
   Keep-alive returns only with a measured gain and an idle timeout below the responder's worker lifetime.
4. HTTP/3: the cases exist; the transport comes after 0.1.

Streaming responses that bypass the spool are not planned for 0.1.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) before you open a pull request.
Report security problems privately, as [SECURITY.md](SECURITY.md) describes.
Everyone in the project follows the [code of conduct](CODE_OF_CONDUCT.md).

## License

fcgi-go is released under the [MIT License](LICENSE).
