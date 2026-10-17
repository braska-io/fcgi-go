# Every reference responder implements the same fixtures

A responder is the FastCGI process that runs scripts, such as php-fpm. The suite runs each case and scenario against four reference responders written in different languages. Each one implements the same fixtures, defined here, so a test never depends on one language's runtime. A fixture is a named test script with fixed behaviour, such as `dump`.

A responder picks the fixture from the path of `SCRIPT_NAME`, without the leading slash and the extension. `/dump.php` and `/dump` both select `dump`; `/app/session/start.php` selects `app/session/start`.

## Four reference responders cover four runtimes and four protocol stacks

| Name | Language | FastCGI stack | Process model | Special role |
| --- | --- | --- | --- | --- |
| `php` | PHP 8.3 | php-fpm | pre-forked children | the most common production responder; scripts in `fixtures/php/` |
| `c` | C | libfcgi (`fcgiapp.h`) | one process per connection slot | the reference C stack |
| `cpp` | C++ | own code; no maintained C++ library exists | threads | a second protocol stack beside libfcgi |
| `rust` | Rust | own code, no FastCGI crate | async tasks | also emits malformed protocol on request |

Each responder lives in `fixtures/<name>/` with a `Dockerfile` and a `responder.yaml`. The container listens for FastCGI on TCP port 9000 and keeps state under `$FIXTURE_STATE` (`/state`).

`responder.yaml` tells the suite how to reach the responder and which fixtures it implements. The file of the `c` responder:

```yaml
name: c
address: tcp:127.0.0.1:9002   # as the suite reaches it
document_root: /srv           # DOCUMENT_ROOT the gateway sends
fixtures: [dump, respond, crash, slow, big, stderr, exitcode, txn, fatal]
deviations: []                # documented differences, as {fixture, note} entries
```

The `php` and `cpp` files list deviations. For example, PHP folds `.` and space in parameter names to `_`. The `php` responder also lists `app/*`: the scripts under `fixtures/php/app/` that the `app` tier cases call by path. A trailing `*` in `fixtures` matches a prefix.

Docker Compose publishes each responder on its own host port:

| Responder | Host port |
| --- | --- |
| `php` | 9001 |
| `c` | 9002 |
| `cpp` | 9003 |
| `rust` | 9004 |

A case or scenario runs against every responder that lists its fixture. A responder that lacks the fixture shows the run as pending, never as passed.

## Ten fixtures cover request data, responses, failures, and shared state

All fixtures set the response header `X-Fixture: <name>`, so the suite knows the responder ran. A fixture reads its options from `QUERY_STRING`, form-encoded.

| Fixture | Query | Behaviour |
| --- | --- | --- |
| `dump` | none | 200, JSON `{"env": {name: value}, "stdin_length": n, "stdin_sha256": hex}`. `env` holds every FastCGI parameter as received, raw names where the runtime allows it. |
| `respond` | `status`, `raw_status`, `header` (repeatable, `Name: value`), `body`, `body_size`, `stderr`, `sleep_ms`, `flush` | builds the CGI response from the query, in that order: sleep, headers, body, stderr, flush |
| `crash` | `after_bytes` (default 0), `signal` (`SEGV`, `ABRT`, `KILL`; default `SEGV`), `flush` | writes the header block and `after_bytes` body bytes, flushes when asked, then dies by the signal. PHP uses `posix_kill(getmypid(), SIGSEGV)` |
| `slow` | `drip_ms`, `chunks`, `chunk_size` | writes `chunks` chunks of `chunk_size` bytes, flushing and sleeping `drip_ms` before each |
| `big` | `bytes` | writes `bytes` pattern bytes (0x00..0xff repeating) |
| `stderr` | `bytes`, `interleave` | writes `bytes` to stderr; with `interleave=1` alternates 1 KiB stderr and 1 KiB stdout |
| `fatal` | none | writes the body `before`, then ends with an unhandled error of the runtime |
| `exitcode` | `code` | writes a complete 200 response, then ends with application status `code` in `FCGI_END_REQUEST` |
| `txn` | see below | a SQLite transaction against shared state |
| `malformed` | `kind` | `rust` only: answers with broken FastCGI framing, see below |

### `txn` runs a real transaction against shared state

State is one SQLite file per responder, `$FIXTURE_STATE/state.db`, table `kv (k TEXT PRIMARY KEY, n INTEGER NOT NULL)`. PHP uses PDO; the others use the SQLite C API or its binding.

| `op` | Behaviour |
| --- | --- |
| `reset` | deletes all rows, 200 |
| `count&k=<key>` | 200, body is the row count for `k` as decimal |
| `insert&k=<key>&sleep_ms=<n>` | `BEGIN IMMEDIATE`, insert `k`, sleep `n` ms. Then write the body `inserted`. Flush. Commit only after the flush succeeded; roll back when any write or flush fails |
| `insert_commit_crash&k=<key>` | `BEGIN`, insert, `COMMIT`, then `crash` with `SEGV` before writing anything |
| `insert_crash&k=<key>` | `BEGIN`, insert, then `crash` with `SEGV` before commit |

The rule "commit only after a successful flush" lets the suite see a lost client in the data. If the gateway closes the connection on a timeout, the responder's next write fails and the row is gone.

### `malformed` covers what no well-behaved responder sends

The `rust` responder breaks the protocol on purpose:

| `kind` | What the responder sends |
| --- | --- |
| `bad_version` | a record with version 2 |
| `truncated_record` | a header that announces 100 content bytes, then 10 bytes and close |
| `missing_end_request` | stdout and an empty stdout record, then close without `FCGI_END_REQUEST` |
| `wrong_request_id` | stdout records for request id `requestId + 1` |
| `stdout_after_end` | `FCGI_END_REQUEST`, then more stdout on the same connection |
| `no_header_terminator` | stdout without the blank line that ends the CGI header block |
| `oversized_header_block` | 1 MiB of header lines before the blank line |
| `stall` | the header block, then nothing until the client closes |
