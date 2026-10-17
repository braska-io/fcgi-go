# The compliance suite checks a CGI gateway against the CGI and FastCGI specifications

The suite sends HTTP requests through the handler under test to a real FastCGI responder. The handler under test is an `http.HandlerFunc` from fcgi-go's `Gateway.Script`. A responder is the FastCGI process that runs scripts, such as php-fpm. The suite then asserts what the responder received and what the client got.

A case is one request together with its expected result. A scenario chains several requests. Four reference responders in PHP, C, C++, and Rust implement the same fixtures: small test scripts with fixed behaviour. No case therefore depends on one runtime. The cases come from the specifications, not from any implementation. A failing case means the gateway is wrong, not the case. Changing a case is a separate decision with its own commit and a cited source.

## Each case cites a source and a tier

The tier names the contract a case checks.

| Tier | Source | Question the tier answers |
| --- | --- | --- |
| `spec` | RFC 3875 (CGI/1.1), FastCGI 1.0, RFC 9110, RFC 9112, RFC 9113, RFC 9114 | Does the responder receive what the specifications require for this HTTP request? |
| `gateway` | this library's design (`docs/design.md`) | Does the gateway keep its own contract: header policy, spooling, timeouts, error statuses? |
| `app` | the documentation of the application runtime, such as php.net | Does a real application behave through the gateway as its runtime documents? |
| `server` | RFC 9110, RFC 9112, RFC 9113 | Does the HTTP server in front of the gateway reject malformed requests? These cases do not distinguish gateways. |

`level` is `MUST`, `SHOULD`, or `MAY` from the source text. `OBSERVED` marks de-facto behaviour that no normative text requires. A `SHOULD` or `MAY` case still fails when broken; the level shows how much weight the source gives it.

## Every case runs over each transport

The suite serves the handler under test over TLS and sends each case as:

- `h1`: HTTP/1.1 as raw bytes. Request-target, header order, header case, and chunked framing arrive exactly as written.
- `h2`: HTTP/2, written frame by frame, so pseudo-headers, split `cookie` fields, and a missing `content-length` arrive exactly as written.
- `h3`: HTTP/3. The schema accepts it; the suite marks these runs pending until the library adds HTTP/3.

A case runs on every transport in `transports`, default `[h1, h2, h3]`. The expectation must hold unchanged on each, except fields that legitimately differ. Those cases restrict `transports` or use `env_match`.

## The case schema

Catalog files live in `catalog/*.yaml`. Each holds one list under the top-level key `cases`.

```yaml
cases:
  - id: rfc3875-4.1.12-request-method       # unique, lowercase, kebab-case
    tier: spec                              # spec | gateway | app | server
    source: "RFC 3875 section 4.1.12"       # document and section
    level: MUST                             # MUST | SHOULD | MAY | OBSERVED
    summary: REQUEST_METHOD carries the method name in the case the client sent.
    transports: [h1, h2, h3]                # optional, default all three
    waived: ""                              # optional: why the project deliberately deviates
    request:
      method: POST
      target: "/dump.php?a=1&b=%20"         # request-target, sent as is on h1 and as :path on h2/h3
      authority: example.test               # optional: Host on h1, :authority on h2/h3; default example.test
      headers:                              # ordered; duplicates allowed; names sent in the case written
        - [Content-Type, text/plain]
      body: "hello"                         # literal body, or:
      body_base64: "AAH/"                   # binary body, or:
      body_size: 1048576                    # generated body of N bytes (pattern 0x00..0xff repeating)
      framing: length                       # length | chunked | none
    route:                                  # what the caller's router hands to the gateway
      script: dump.php                      # default: the path up to ".php", else the first path segment
      path_info: /extra                     # optional PATH_INFO
      query_string: "a=1"                   # optional override of QUERY_STRING
      params: { HTTP_X_AUTH_SUBJECT: "7" }  # params the caller sets; written last
      timeout_ms: 500                       # optional per-call deadline
      max_response_bytes: 65536             # optional response limit
    expect:
      reaches_responder: true               # default true; false = the gateway or server answers itself
      status: 200                           # exact status, or:
      status_in: [400, 431]
      env: { REQUEST_METHOD: POST }         # exact meta-variable values
      env_match: { SERVER_PROTOCOL: "^HTTP/1\\.1$" }  # Go regular expressions
      env_absent: [HTTP_PROXY]              # meta-variables that must not exist
      stdin_length: 5                       # bytes the responder read from stdin
      stdin_sha256: "..."                   # hex
      headers: { Content-Type: text/plain } # response headers, exact first value, canonical name
      headers_all: { Set-Cookie: [a=1, b=2] } # every value of a field, in order
      headers_absent: [X-Powered-By]
      body: "exact body"
      body_contains: "substring"
      body_length: 5242880                  # response body bytes
      body_sha256: "..."                    # hex digest of the response body
```

Rules for `framing`:

- `length` sends `Content-Length` with the body length. Default when a body is present.
- `chunked` uses chunked transfer coding on `h1`. On `h2` and `h3` it means no `content-length`, data in several frames.
- `none` sends no length information. On `h1` this is only valid without a body.

`waived` marks a case the project deliberately does not follow. A waived case stays in the catalog unchanged and shows as skipped with its reason, never as passed. A waiver names the decision in `docs/design.md` or the runtime behaviour it rests on.

A case with `expect.reaches_responder: false` asserts only `status`, `status_in`, `headers`, `headers_absent`, and `body*`.

## Cases and scenarios select a fixture through the script name

[RESPONDERS.md](RESPONDERS.md) defines the fixtures and the four reference responders: PHP, C, C++, and Rust. A case names its fixture through `route.script`, or through `request.target` when `route.script` is empty. It runs once per responder that implements the fixture and once per transport.

## Scenarios chain several requests

A scenario tests behaviour that one request cannot show. Examples: a transaction after a timeout, a crash, a saturated responder. Scenario files live in `catalog/scenarios/*.yaml` under the top-level key `scenarios`.

```yaml
scenarios:
  - id: gw-timeout-rolls-back-transaction
    tier: gateway
    source: "docs/design.md, timeouts and cancellation"
    level: MUST
    summary: A call that hits its deadline closes the connection, so the responder rolls back.
    steps:
      - request: { method: POST, target: "/txn?op=reset" }
        route: { script: txn }
        expect: { status: 200 }
      - request: { method: POST, target: "/txn?op=insert&k=a&sleep_ms=1500" }
        route: { script: txn, timeout_ms: 300 }
        expect: { status: 504, reaches_responder: false, duration_max_ms: 1000 }
      - wait_ms: 1500
      - request: { method: GET, target: "/txn?op=count&k=a" }
        route: { script: txn }
        expect: { status: 200, body: "0" }
      - parallel:                       # steps that start together
          - request: { method: GET, target: "/slow?drip_ms=200&chunks=5&chunk_size=1" }
            route: { script: slow }
            expect: { status: 200 }
          - request: { method: GET, target: "/dump" }
            route: { script: dump }
            expect: { status: 200 }
```

A step holds `request`, `route`, and `expect` as in a case, or `wait_ms`, or `parallel`. With `cookie_jar: true` on the scenario, cookies set by one step go out with every later step, as a browser sends them. `expect` adds `duration_min_ms` and `duration_max_ms`. A request step inside `parallel` may set `delay_ms` to start later than its siblings. `duration_*` counts from the request, not from the delay. Steps run in order on one transport; a scenario runs on each transport and responder, like a case.

## The suite configures the handler the same way for every case

| Option | Value |
| --- | --- |
| protected header names | `X-Auth-Subject`, `X-Auth-*` |
| response headers dropped | `X-Powered-By` |
| in-memory spool | 1 MiB, then disk |
| maximum request body | 10 MiB |
| default call timeout | 5 s |
| concurrent calls to the responder | 4 |
| server name and port | `example.test`, `443` |
| responders | from `responder.yaml` of each responder under `fixtures/` |

## Run the suite

```sh
task compliance
```

The task builds and starts the four reference responders with Docker Compose, then runs the Ginkgo suite against each. A responder that does not start fails the suite; it never skips.

Stop the responders afterwards:

```sh
task responders:down
```
