# Spec issues found while implementing `Caller.Call`

The spec in `docs/design.md` is unchanged. These points are open; each shows what the code does.

1. **`Request.ContentLength` zero value.** The spec says "-1 when unknown" and gives no meaning to 0.
   A `Request{Body: r}` leaves it at 0, and reading 0 as "declared empty" would fail every such call with 400.
   The code follows `net/http`: with a `Body`, 0 and -1 both mean unknown. Only a positive value is checked against the body.
   The spec should say so.
2. **A malformed `Target`.** No row of the failure table covers it. `Call` returns a plain `fcgi: invalid request target` error.
   It is not an `*Error` and the observer is not called, because no call ran. An empty `Target` is invalid too; there is no default.
3. **`OriginClient` error contents.** The spec requires `context.Canceled` in the chain and no `*Error`.
   The code flattens the underlying failure to text, so `errors.As(err, &*Error)` stays false. `Script`'s `CallInfo.Err` for a client cancel
   now has the same shape; before, it also wrapped the inner `*Error`.
4. **Bodyless replies.** `Response.Body` is empty and `ContentLength` 0 for `HEAD` and for statuses without a body (1xx, 204, 304),
   as `Script` writes no body there. The spec does not say.
5. **Wording.** "`WithParams` and `WithQuery` and `WithPathInfo` receive an `*http.Request`": `WithPathInfo` is skipped when `Request.PathInfo` is non-nil.
