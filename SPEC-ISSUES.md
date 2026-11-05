# Spec issues found while adding the `call` transport

## SERVER_PORT of a `Call` is undefined

`docs/design.md` lists no source for `SERVER_PORT` on a `Call`. `Script` reads
it from the listener (`http.LocalAddrContextKey`). A `Request` has no listener
and no port field, so `Call` sends no `SERVER_PORT`.

Two catalog cases expect it and fail only on `call`:
`http-server-port` and `rfc3875-req-server-port-default`.

The suite does not restrict them, because their expectation holds for any
request, not only for one that came over a socket. The spec decides: either a
`Request` gets a port (or derives it from `Host`, 443 under `TLS`), or these
two cases leave `call` with a stated reason.
