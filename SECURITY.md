# Security policy

fcgi-go sits between untrusted HTTP clients and a FastCGI responder, such as php-fpm.
The responder receives each request as a body and a set of parameters, such as `REQUEST_METHOD`.
A flaw in fcgi-go can let a client change the parameters and body that reach the responder.

## Supported versions

fcgi-go has no release yet.
Only the `main` branch receives security fixes.
Once releases exist, this policy lists the supported release lines.

## Report a vulnerability privately

Do not open a public issue, pull request, or discussion for a security problem.

Report it through GitHub private vulnerability reporting:

1. Open [github.com/braska-io/fcgi-go](https://github.com/braska-io/fcgi-go).
2. Select the **Security** tab.
3. Select **Report a vulnerability**.
4. Fill in the form and submit it.

The form is also at [github.com/braska-io/fcgi-go/security/advisories/new](https://github.com/braska-io/fcgi-go/security/advisories/new).

Only the maintainers can read the report.

A good report contains:

- the commit you tested
- the HTTP request that triggers the problem: raw bytes or a script that sends them
- the responder and its version, such as php-fpm 8.3
- what the responder received or the client got, and what you expected
- the specification section that the behaviour violates, if one applies

A failing compliance case in the report shortens the path to a fix.
[compliance/README.md](compliance/README.md) describes the case format.

## What is in scope

- Header forgery into FastCGI parameters.
  An example is a client header that arrives as a parameter the caller sets, such as `HTTP_X_AUTH_SUBJECT`.
- Request smuggling.
  An example is a request whose body length the library and the responder read differently.
- Response splitting.
  An example is responder output that injects headers or a second response.
- Resource exhaustion.
  Examples include unbounded memory or disk use, leaked connections, and a slow client that holds a responder worker.
- Output from a failed call that reaches the client.

## What is out of scope

- Vulnerabilities in the responder itself, such as php-fpm, or in a script it runs.
- Vulnerabilities in the Go standard library's HTTP server.
  Report those to the [Go project](https://go.dev/doc/security/policy).
- The reference responders under `compliance/fixtures/`.
  They are test code and do not ship with the library.
- Problems that need a malicious caller, meaning the Go code that uses fcgi-go.
  The library trusts that code.

## What happens after you report

Volunteers maintain fcgi-go.
The maintainers do not promise fixed response times.

1. A maintainer acknowledges the report.
2. The maintainers reproduce the problem and agree on its impact with you.
3. The maintainers prepare a fix and a compliance case that covers it, in a private fork when needed.
4. The maintainers publish a GitHub security advisory together with the fix.

The advisory credits you unless you ask otherwise.
Please keep the problem private until the advisory appears.
