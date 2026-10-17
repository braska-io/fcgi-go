/*
 * C reference FastCGI responder for the compliance suite (RESPONDERS.md).
 *
 * Process model: one process, NUM_WORKERS (4) threads, each looping on
 * FCGX_Accept_r over a shared listening socket. The crash, fatal and
 * *_crash fixtures therefore take the whole process down; run the container
 * with `restart: always` in compose so Docker brings it back.
 */
#define _GNU_SOURCE
#include <fcgiapp.h>
#include <pthread.h>
#include <signal.h>
#include <sqlite3.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <time.h>
#include <poll.h>
#include <sys/socket.h>
#include <sys/wait.h>
#include <unistd.h>

#define NUM_WORKERS 4

extern char **environ;

/* ---------- SHA-256 (FIPS 180-4) ---------- */

typedef struct {
	uint32_t h[8];
	uint8_t buf[64];
	uint64_t len;
	size_t fill;
} sha256_t;

static const uint32_t K256[64] = {
	0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
	0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
	0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
	0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
	0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
	0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
	0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
	0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2};

#define ROR(x, n) (((x) >> (n)) | ((x) << (32 - (n))))

static void sha256_block(sha256_t *s, const uint8_t *p)
{
	uint32_t w[64], a, b, c, d, e, f, g, h;
	for (int i = 0; i < 16; i++)
		w[i] = (uint32_t)p[4 * i] << 24 | (uint32_t)p[4 * i + 1] << 16 | (uint32_t)p[4 * i + 2] << 8 | p[4 * i + 3];
	for (int i = 16; i < 64; i++) {
		uint32_t s0 = ROR(w[i - 15], 7) ^ ROR(w[i - 15], 18) ^ (w[i - 15] >> 3);
		uint32_t s1 = ROR(w[i - 2], 17) ^ ROR(w[i - 2], 19) ^ (w[i - 2] >> 10);
		w[i] = w[i - 16] + s0 + w[i - 7] + s1;
	}
	a = s->h[0]; b = s->h[1]; c = s->h[2]; d = s->h[3];
	e = s->h[4]; f = s->h[5]; g = s->h[6]; h = s->h[7];
	for (int i = 0; i < 64; i++) {
		uint32_t t1 = h + (ROR(e, 6) ^ ROR(e, 11) ^ ROR(e, 25)) + ((e & f) ^ (~e & g)) + K256[i] + w[i];
		uint32_t t2 = (ROR(a, 2) ^ ROR(a, 13) ^ ROR(a, 22)) + ((a & b) ^ (a & c) ^ (b & c));
		h = g; g = f; f = e; e = d + t1; d = c; c = b; b = a; a = t1 + t2;
	}
	s->h[0] += a; s->h[1] += b; s->h[2] += c; s->h[3] += d;
	s->h[4] += e; s->h[5] += f; s->h[6] += g; s->h[7] += h;
}

static void sha256_init(sha256_t *s)
{
	static const uint32_t iv[8] = {0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
				       0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19};
	memcpy(s->h, iv, sizeof iv);
	s->len = 0;
	s->fill = 0;
}

static void sha256_update(sha256_t *s, const uint8_t *p, size_t n)
{
	s->len += n;
	while (n > 0) {
		size_t take = 64 - s->fill;
		if (take > n)
			take = n;
		memcpy(s->buf + s->fill, p, take);
		s->fill += take;
		p += take;
		n -= take;
		if (s->fill == 64) {
			sha256_block(s, s->buf);
			s->fill = 0;
		}
	}
}

static void sha256_hex(sha256_t *s, char out[65])
{
	uint64_t bits = s->len * 8;
	uint8_t pad = 0x80;
	sha256_update(s, &pad, 1);
	pad = 0;
	while (s->fill != 56)
		sha256_update(s, &pad, 1);
	uint8_t lenbe[8];
	for (int i = 0; i < 8; i++)
		lenbe[i] = (uint8_t)(bits >> (56 - 8 * i));
	sha256_update(s, lenbe, 8);
	for (int i = 0; i < 8; i++)
		snprintf(out + 8 * i, 9, "%08x", s->h[i]);
}

/* ---------- helpers ---------- */

typedef struct {
	char *k;
	char *v;
	size_t vlen;
} kv_t;

typedef struct {
	kv_t *items;
	size_t n;
} query_t;

static int hexval(int c)
{
	if (c >= '0' && c <= '9') return c - '0';
	if (c >= 'a' && c <= 'f') return c - 'a' + 10;
	if (c >= 'A' && c <= 'F') return c - 'A' + 10;
	return -1;
}

/* form-decodes s[0..n) into a new NUL-terminated buffer; *outlen excludes the NUL */
static char *form_decode(const char *s, size_t n, size_t *outlen)
{
	char *o = malloc(n + 1);
	size_t j = 0;
	for (size_t i = 0; i < n; i++) {
		if (s[i] == '+') {
			o[j++] = ' ';
		} else if (s[i] == '%' && i + 2 < n && hexval(s[i + 1]) >= 0 && hexval(s[i + 2]) >= 0) {
			o[j++] = (char)(hexval(s[i + 1]) << 4 | hexval(s[i + 2]));
			i += 2;
		} else {
			o[j++] = s[i];
		}
	}
	o[j] = 0;
	if (outlen)
		*outlen = j;
	return o;
}

static query_t query_parse(const char *qs)
{
	query_t q = {NULL, 0};
	if (!qs)
		return q;
	const char *p = qs;
	while (*p) {
		const char *end = strchr(p, '&');
		size_t len = end ? (size_t)(end - p) : strlen(p);
		if (len > 0) {
			const char *eq = memchr(p, '=', len);
			size_t klen = eq ? (size_t)(eq - p) : len;
			q.items = realloc(q.items, (q.n + 1) * sizeof *q.items);
			kv_t *it = &q.items[q.n++];
			it->k = form_decode(p, klen, NULL);
			it->v = eq ? form_decode(eq + 1, len - klen - 1, &it->vlen) : form_decode("", 0, &it->vlen);
		}
		if (!end)
			break;
		p = end + 1;
	}
	return q;
}

static void query_free(query_t *q)
{
	for (size_t i = 0; i < q->n; i++) {
		free(q->items[i].k);
		free(q->items[i].v);
	}
	free(q->items);
}

/* last value wins, like a PHP array assignment */
static const kv_t *q_get(const query_t *q, const char *key)
{
	for (size_t i = q->n; i-- > 0;)
		if (strcmp(q->items[i].k, key) == 0)
			return &q->items[i];
	return NULL;
}

static long long q_int(const query_t *q, const char *key, long long def)
{
	const kv_t *e = q_get(q, key);
	if (!e)
		return def;
	long long v = strtoll(e->v, NULL, 10);
	return v < 0 ? 0 : v;
}

static void sleep_ms(long long ms)
{
	if (ms <= 0)
		return;
	struct timespec ts = {ms / 1000, (ms % 1000) * 1000000L};
	while (nanosleep(&ts, &ts) == -1)
		;
}

/* write helpers: return 0 on success, -1 once the client is gone */
static int put(FCGX_Request *r, const void *b, size_t n)
{
	return FCGX_PutStr(b, (int)n, r->out) == (int)n ? 0 : -1;
}

static int puts_(FCGX_Request *r, const char *s)
{
	return put(r, s, strlen(s));
}

/* peer_closed reports whether the gateway closed the connection. The first
 * write after the peer's FIN still succeeds, so a successful flush alone
 * does not prove the client is there; after the request's empty STDIN the
 * gateway sends nothing, so a readable socket with no data means EOF. */
static int peer_closed(FCGX_Request *r)
{
	struct pollfd p = { .fd = r->ipcFd, .events = POLLIN };
	char c;
	if (poll(&p, 1, 0) <= 0)
		return 0;
	if (p.revents & (POLLHUP | POLLERR))
		return 1;
	return recv(r->ipcFd, &c, 1, MSG_PEEK | MSG_DONTWAIT) == 0;
}

static int flush(FCGX_Request *r)
{
	return FCGX_FFlush(r->out) == 0 ? 0 : -1;
}

static void head(FCGX_Request *r, const char *fixture, const char *extra)
{
	puts_(r, "X-Fixture: ");
	puts_(r, fixture);
	puts_(r, "\r\n");
	if (extra)
		puts_(r, extra);
	puts_(r, "\r\n");
}

static void put_pattern(FCGX_Request *r, long long n, long long *pos)
{
	uint8_t buf[65536];
	while (n > 0) {
		size_t c = n > (long long)sizeof buf ? sizeof buf : (size_t)n;
		for (size_t i = 0; i < c; i++)
			buf[i] = (uint8_t)((*pos + (long long)i) & 0xff);
		if (put(r, buf, c) < 0)
			return;
		*pos += (long long)c;
		n -= (long long)c;
	}
}

static void die_by(const char *sig)
{
	int n = SIGSEGV;
	if (sig && strcmp(sig, "ABRT") == 0)
		n = SIGABRT;
	else if (sig && strcmp(sig, "KILL") == 0)
		n = SIGKILL;
	raise(n);
	sleep(5); /* not reached for these signals; guards against a blocked mask */
}

static void err_write(FCGX_Request *r, const void *b, size_t n)
{
	FCGX_PutStr(b, (int)n, r->err);
	FCGX_FFlush(r->err);
}

/* ---------- dump ---------- */

typedef struct {
	char *p;
	size_t n, cap;
} sb_t;

static void sb_add(sb_t *b, const char *s, size_t n)
{
	if (b->n + n + 1 > b->cap) {
		b->cap = (b->n + n + 1) * 2;
		b->p = realloc(b->p, b->cap);
	}
	memcpy(b->p + b->n, s, n);
	b->n += n;
}

/* emits a JSON string body; invalid UTF-8 sequences become U+FFFD, "/" stays unescaped */
static void json_str(sb_t *b, const unsigned char *s, size_t n)
{
	static const char FFFD[] = "\xef\xbf\xbd";
	sb_add(b, "\"", 1);
	for (size_t i = 0; i < n;) {
		unsigned char c = s[i];
		if (c < 0x80) {
			char t[8];
			switch (c) {
			case '"': sb_add(b, "\\\"", 2); break;
			case '\\': sb_add(b, "\\\\", 2); break;
			case '\b': sb_add(b, "\\b", 2); break;
			case '\f': sb_add(b, "\\f", 2); break;
			case '\n': sb_add(b, "\\n", 2); break;
			case '\r': sb_add(b, "\\r", 2); break;
			case '\t': sb_add(b, "\\t", 2); break;
			default:
				if (c < 0x20) {
					snprintf(t, sizeof t, "\\u%04x", c);
					sb_add(b, t, 6);
				} else {
					sb_add(b, (const char *)&c, 1);
				}
			}
			i++;
			continue;
		}
		size_t need;
		uint32_t cp, min;
		if (c >= 0xc2 && c <= 0xdf) { need = 1; cp = c & 0x1f; min = 0x80; }
		else if (c >= 0xe0 && c <= 0xef) { need = 2; cp = c & 0x0f; min = 0x800; }
		else if (c >= 0xf0 && c <= 0xf4) { need = 3; cp = c & 0x07; min = 0x10000; }
		else { sb_add(b, FFFD, 3); i++; continue; }
		size_t k = 1;
		while (k <= need && i + k < n && (s[i + k] & 0xc0) == 0x80) {
			cp = cp << 6 | (s[i + k] & 0x3f);
			k++;
		}
		if (k <= need || cp < min || cp > 0x10ffff || (cp >= 0xd800 && cp <= 0xdfff)) {
			/* one replacement per maximal bad prefix; resume after the lead byte */
			sb_add(b, FFFD, 3);
			i++;
			continue;
		}
		sb_add(b, (const char *)s + i, need + 1);
		i += need + 1;
	}
	sb_add(b, "\"", 1);
}

static void fx_dump(FCGX_Request *r)
{
	sb_t b = {0};
	sb_add(&b, "{\"env\":{", 8);
	int first = 1;
	for (char **e = r->envp; e && *e; e++) {
		const char *eq = strchr(*e, '=');
		if (!eq)
			continue;
		if (!first)
			sb_add(&b, ",", 1);
		first = 0;
		json_str(&b, (const unsigned char *)*e, (size_t)(eq - *e));
		sb_add(&b, ":", 1);
		json_str(&b, (const unsigned char *)eq + 1, strlen(eq + 1));
	}
	sha256_t sh;
	sha256_init(&sh);
	uint64_t total = 0;
	uint8_t buf[8192];
	int got;
	while ((got = FCGX_GetStr((char *)buf, sizeof buf, r->in)) > 0) {
		sha256_update(&sh, buf, (size_t)got);
		total += (uint64_t)got;
	}
	char hex[65], num[64];
	sha256_hex(&sh, hex);
	int nl = snprintf(num, sizeof num, "},\"stdin_length\":%llu,\"stdin_sha256\":\"", (unsigned long long)total);
	sb_add(&b, num, (size_t)nl);
	sb_add(&b, hex, 64);
	sb_add(&b, "\"}", 2);

	head(r, "dump", "Content-Type: application/json\r\n");
	put(r, b.p, b.n);
	free(b.p);
}

/* ---------- other fixtures ---------- */

static void fx_respond(FCGX_Request *r, const query_t *q)
{
	sleep_ms(q_int(q, "sleep_ms", 0));
	const kv_t *raw = q_get(q, "raw_status"), *st = q_get(q, "status");
	char line[256];
	line[0] = 0;
	if (raw)
		snprintf(line, sizeof line, "Status: %s\r\n", raw->v);
	else if (st)
		snprintf(line, sizeof line, "Status: %lld\r\n", q_int(q, "status", 200));
	puts_(r, "X-Fixture: respond\r\n");
	puts_(r, line);
	int have_ct = 0;
	for (size_t i = 0; i < q->n; i++) {
		if (strcmp(q->items[i].k, "header[]") != 0)
			continue;
		if (strncasecmp(q->items[i].v, "Content-Type:", 13) == 0)
			have_ct = 1;
		puts_(r, q->items[i].v);
		puts_(r, "\r\n");
	}
	if (!have_ct)
		puts_(r, "Content-Type: text/html; charset=UTF-8\r\n");
	puts_(r, "\r\n");
	const kv_t *body = q_get(q, "body");
	if (body)
		put(r, body->v, body->vlen);
	long long pos = 0;
	if (q_get(q, "body_size"))
		put_pattern(r, q_int(q, "body_size", 0), &pos);
	const kv_t *se = q_get(q, "stderr");
	if (se)
		err_write(r, se->v, se->vlen);
	if (q_get(q, "flush") && strcmp(q_get(q, "flush")->v, "1") == 0)
		flush(r);
}

static void fx_crash(FCGX_Request *r, const query_t *q)
{
	head(r, "crash", "Content-Type: application/octet-stream\r\n");
	long long pos = 0;
	put_pattern(r, q_int(q, "after_bytes", 0), &pos);
	const kv_t *f = q_get(q, "flush");
	if (f && strcmp(f->v, "1") == 0)
		flush(r);
	const kv_t *s = q_get(q, "signal");
	die_by(s ? s->v : "SEGV");
}

static void fx_slow(FCGX_Request *r, const query_t *q)
{
	long long chunks = q_int(q, "chunks", 1), size = q_int(q, "chunk_size", 1);
	head(r, "slow", "Content-Type: application/octet-stream\r\n");
	if (flush(r) < 0)
		return;
	char *chunk = malloc((size_t)size + 1);
	memset(chunk, '.', (size_t)size);
	for (long long i = 0; i < chunks; i++) {
		sleep_ms(q_int(q, "drip_ms", 0));
		if (put(r, chunk, (size_t)size) < 0 || flush(r) < 0)
			break;
	}
	free(chunk);
}

static void fx_big(FCGX_Request *r, const query_t *q)
{
	head(r, "big", "Content-Type: application/octet-stream\r\n");
	long long pos = 0;
	put_pattern(r, q_int(q, "bytes", 0), &pos);
}

static void fx_stderr(FCGX_Request *r, const query_t *q)
{
	long long left = q_int(q, "bytes", 0);
	const kv_t *il = q_get(q, "interleave");
	int interleave = il && strcmp(il->v, "1") == 0;
	head(r, "stderr", "Content-Type: text/plain\r\n");
	if (interleave)
		flush(r);
	char e[1024], o[1024];
	memset(e, 'e', sizeof e);
	memset(o, 'o', sizeof o);
	while (left > 0) {
		size_t n = left > 1024 ? 1024 : (size_t)left;
		err_write(r, e, n);
		if (interleave && (put(r, o, sizeof o) < 0 || flush(r) < 0))
			break;
		left -= (long long)n;
	}
}

static void fx_exitcode(FCGX_Request *r, const query_t *q)
{
	head(r, "exitcode", "Content-Type: text/plain\r\n");
	puts_(r, "exitcode");
	flush(r);
	FCGX_SetExitStatus((int)q_int(q, "code", 0), r->out);
	FCGX_Finish_r(r);
}

static void fx_fatal(FCGX_Request *r)
{
	head(r, "fatal", "Content-Type: text/plain\r\n");
	puts_(r, "before");
	flush(r);
	abort();
}

/* ---------- txn ---------- */

static int db_exec(sqlite3 *db, const char *sql)
{
	return sqlite3_exec(db, sql, NULL, NULL, NULL);
}

static int db_insert(sqlite3 *db, const char *k)
{
	sqlite3_stmt *st;
	if (sqlite3_prepare_v2(db, "INSERT INTO kv (k, n) VALUES (?, 1)", -1, &st, NULL) != SQLITE_OK)
		return -1;
	sqlite3_bind_text(st, 1, k, -1, SQLITE_STATIC);
	int rc = sqlite3_step(st);
	sqlite3_finalize(st);
	return rc == SQLITE_DONE ? 0 : -1;
}

static void txn_error(FCGX_Request *r, sqlite3 *db, const char *what)
{
	char line[512];
	snprintf(line, sizeof line, "Status: 500\r\nContent-Type: text/plain\r\n");
	head(r, "txn", line);
	puts_(r, what);
	puts_(r, ": ");
	puts_(r, sqlite3_errmsg(db));
}

static void fx_txn(FCGX_Request *r, const query_t *q)
{
	const char *state = getenv("FIXTURE_STATE");
	char path[1024];
	snprintf(path, sizeof path, "%s/state.db", state ? state : "/state");
	sqlite3 *db;
	if (sqlite3_open(path, &db) != SQLITE_OK) {
		txn_error(r, db, "open");
		sqlite3_close(db);
		return;
	}
	sqlite3_busy_timeout(db, 5000);
	if (db_exec(db, "CREATE TABLE IF NOT EXISTS kv (k TEXT PRIMARY KEY, n INTEGER NOT NULL)") != SQLITE_OK) {
		txn_error(r, db, "create");
		sqlite3_close(db);
		return;
	}
	const kv_t *kk = q_get(q, "k"), *op = q_get(q, "op");
	const char *k = kk ? kk->v : "";
	const char *o = op ? op->v : "";
	const char *plain = "Content-Type: text/plain\r\n";

	if (strcmp(o, "reset") == 0) {
		if (db_exec(db, "DELETE FROM kv") != SQLITE_OK) { txn_error(r, db, "reset"); goto out; }
		head(r, "txn", plain);
		puts_(r, "reset");
	} else if (strcmp(o, "count") == 0) {
		sqlite3_stmt *st;
		long long n = 0;
		if (sqlite3_prepare_v2(db, "SELECT COUNT(*) FROM kv WHERE k = ?", -1, &st, NULL) != SQLITE_OK) {
			txn_error(r, db, "count");
			goto out;
		}
		sqlite3_bind_text(st, 1, k, -1, SQLITE_STATIC);
		if (sqlite3_step(st) == SQLITE_ROW)
			n = sqlite3_column_int64(st, 0);
		sqlite3_finalize(st);
		char num[32];
		snprintf(num, sizeof num, "%lld", n);
		head(r, "txn", plain);
		puts_(r, num);
	} else if (strcmp(o, "insert") == 0) {
		if (db_exec(db, "BEGIN IMMEDIATE") != SQLITE_OK) { txn_error(r, db, "begin"); goto out; }
		if (db_insert(db, k) < 0) {
			txn_error(r, db, "insert");
			db_exec(db, "ROLLBACK");
			goto out;
		}
		sleep_ms(q_int(q, "sleep_ms", 0));
		/* commit only when every write and the flush reached the client */
		int bad = 0;
		head(r, "txn", plain);
		bad |= puts_(r, "inserted");
		bad |= flush(r);
		if (FCGX_HasSeenEOF(r->out) || peer_closed(r))
			bad = -1;
		db_exec(db, bad ? "ROLLBACK" : "COMMIT");
	} else if (strcmp(o, "insert_commit_crash") == 0 || strcmp(o, "insert_crash") == 0) {
		if (db_exec(db, "BEGIN") != SQLITE_OK || db_insert(db, k) < 0) {
			txn_error(r, db, "insert");
			db_exec(db, "ROLLBACK");
			goto out;
		}
		if (strcmp(o, "insert_commit_crash") == 0)
			db_exec(db, "COMMIT");
		die_by("SEGV");
	} else {
		head(r, "txn", "Status: 400\r\nContent-Type: text/plain\r\n");
		puts_(r, "unknown op");
	}
out:
	sqlite3_close(db);
}

/* ---------- dispatch ---------- */

static void handle(FCGX_Request *r)
{
	const char *sn = FCGX_GetParam("SCRIPT_NAME", r->envp);
	char name[64] = "";
	if (sn) {
		const char *base = strrchr(sn, '/');
		base = base ? base + 1 : sn;
		size_t n = strlen(base);
		const char *dot = strrchr(base, '.');
		if (dot && dot != base)
			n = (size_t)(dot - base);
		if (n < sizeof name) {
			memcpy(name, base, n);
			name[n] = 0;
		}
	}
	query_t q = query_parse(FCGX_GetParam("QUERY_STRING", r->envp));

	if (!strcmp(name, "dump")) fx_dump(r);
	else if (!strcmp(name, "respond")) fx_respond(r, &q);
	else if (!strcmp(name, "crash")) fx_crash(r, &q);
	else if (!strcmp(name, "slow")) fx_slow(r, &q);
	else if (!strcmp(name, "big")) fx_big(r, &q);
	else if (!strcmp(name, "stderr")) fx_stderr(r, &q);
	else if (!strcmp(name, "exitcode")) fx_exitcode(r, &q);
	else if (!strcmp(name, "txn")) fx_txn(r, &q);
	else if (!strcmp(name, "fatal")) fx_fatal(r);
	else {
		head(r, "unknown", "Status: 404\r\nContent-Type: text/plain\r\n");
		puts_(r, "unknown fixture");
	}
	query_free(&q);
}

static int listen_sock;

static void *worker(void *arg)
{
	(void)arg;
	FCGX_Request req;
	FCGX_InitRequest(&req, listen_sock, 0);
	while (FCGX_Accept_r(&req) >= 0) {
		handle(&req);
		FCGX_Finish_r(&req);
	}
	return NULL;
}

static pid_t child;

static void forward(int sig)
{
	if (child > 0)
		kill(child, sig);
}

/*
 * As PID 1 the kernel discards SEGV/ABRT/KILL raised from inside the
 * container, so the crash fixtures would not kill anything. Run the server
 * in a child and exit when it dies; the container stops and compose restarts it.
 */
static void supervise_if_pid1(void)
{
	if (getpid() != 1)
		return;
	child = fork();
	if (child < 0) {
		perror("fork");
		exit(1);
	}
	if (child == 0)
		return;
	signal(SIGTERM, forward);
	signal(SIGINT, forward);
	int status;
	while (waitpid(child, &status, 0) < 0)
		;
	_exit(WIFSIGNALED(status) ? 128 + WTERMSIG(status) : WEXITSTATUS(status));
}

int main(void)
{
	supervise_if_pid1();
	signal(SIGPIPE, SIG_IGN); /* a lost client must surface as a failed write, not a dead process */
	if (FCGX_Init() != 0) {
		fprintf(stderr, "fcgx init failed\n");
		return 1;
	}
	listen_sock = FCGX_OpenSocket(":9000", 128);
	if (listen_sock < 0) {
		fprintf(stderr, "listen on :9000 failed\n");
		return 1;
	}
	pthread_t t[NUM_WORKERS];
	for (int i = 0; i < NUM_WORKERS; i++)
		pthread_create(&t[i], NULL, worker, NULL);
	for (int i = 0; i < NUM_WORKERS; i++)
		pthread_join(t[i], NULL);
	return 0;
}
