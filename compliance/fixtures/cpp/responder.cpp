// C++ reference FastCGI responder for the fcgi-go compliance suite.
//
// Library decision: none; the FastCGI protocol is implemented here.
// The only maintained-looking candidate, eddic/fastcgipp (LGPL-3.0), has had no
// commit since 2023-04-12 (gh api repos/eddic/fastcgipp, checked 2026-10-17),
// which is past the 3-year threshold, and publishes no release. Its request
// API also does not expose appStatus or let a handler die mid-request, which
// the exitcode and crash fixtures need. Writing the records ourselves gives a
// stack that shares no code with libfcgi (C responder) or the Rust one.
//
// Model: one reader thread per connection assembles a request, hands it to a
// pool of 4 workers and waits for it to finish (no multiplexing, MPXS_CONNS=0).

#include <poll.h>
#include <sqlite3.h>
#include <arpa/inet.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <sys/socket.h>
#include <unistd.h>

#include <algorithm>
#include <array>
#include <atomic>
#include <chrono>
#include <cctype>
#include <condition_variable>
#include <cerrno>
#include <csignal>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <deque>
#include <functional>
#include <map>
#include <mutex>
#include <stdexcept>
#include <string>
#include <thread>
#include <utility>
#include <vector>

namespace {

// ---------------------------------------------------------------- SHA-256

std::string sha256_hex(const std::string& data) {
    static const uint32_t K[64] = {
        0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
        0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
        0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
        0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
        0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
        0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
        0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
        0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2};
    uint32_t h[8] = {0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
                     0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19};
    std::string msg = data;
    const uint64_t bits = static_cast<uint64_t>(data.size()) * 8;
    msg.push_back(static_cast<char>(0x80));
    while (msg.size() % 64 != 56) msg.push_back('\0');
    for (int i = 7; i >= 0; --i) msg.push_back(static_cast<char>((bits >> (i * 8)) & 0xff));
    auto rotr = [](uint32_t x, int n) { return (x >> n) | (x << (32 - n)); };
    for (size_t off = 0; off < msg.size(); off += 64) {
        uint32_t w[64];
        for (int i = 0; i < 16; ++i) {
            const auto* p = reinterpret_cast<const unsigned char*>(msg.data()) + off + i * 4;
            w[i] = (uint32_t(p[0]) << 24) | (uint32_t(p[1]) << 16) | (uint32_t(p[2]) << 8) | p[3];
        }
        for (int i = 16; i < 64; ++i) {
            uint32_t s0 = rotr(w[i - 15], 7) ^ rotr(w[i - 15], 18) ^ (w[i - 15] >> 3);
            uint32_t s1 = rotr(w[i - 2], 17) ^ rotr(w[i - 2], 19) ^ (w[i - 2] >> 10);
            w[i] = w[i - 16] + s0 + w[i - 7] + s1;
        }
        uint32_t a = h[0], b = h[1], c = h[2], d = h[3], e = h[4], f = h[5], g = h[6], hh = h[7];
        for (int i = 0; i < 64; ++i) {
            uint32_t S1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25);
            uint32_t ch = (e & f) ^ (~e & g);
            uint32_t t1 = hh + S1 + ch + K[i] + w[i];
            uint32_t S0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22);
            uint32_t mj = (a & b) ^ (a & c) ^ (b & c);
            uint32_t t2 = S0 + mj;
            hh = g; g = f; f = e; e = d + t1; d = c; c = b; b = a; a = t1 + t2;
        }
        h[0] += a; h[1] += b; h[2] += c; h[3] += d; h[4] += e; h[5] += f; h[6] += g; h[7] += hh;
    }
    char out[65];
    for (int i = 0; i < 8; ++i) std::snprintf(out + i * 8, 9, "%08x", h[i]);
    return std::string(out, 64);
}

// ------------------------------------------------------------------- JSON

void json_escape(std::string& out, const std::string& s) {
    out.push_back('"');
    auto u16 = [&](uint32_t u) {
        char b[8];
        std::snprintf(b, sizeof b, "\\u%04x", u);
        out += b;
    };
    const auto* p = reinterpret_cast<const unsigned char*>(s.data());
    const size_t n = s.size();
    for (size_t i = 0; i < n;) {
        unsigned char c = p[i];
        if (c < 0x80) {
            switch (c) {
            case '"': out += "\\\""; break;
            case '\\': out += "\\\\"; break;
            case '\b': out += "\\b"; break;
            case '\f': out += "\\f"; break;
            case '\n': out += "\\n"; break;
            case '\r': out += "\\r"; break;
            case '\t': out += "\\t"; break;
            default:
                if (c < 0x20 || c == 0x7f) { if (c == 0x7f) out.push_back(char(c)); else u16(c); }
                else out.push_back(char(c));
            }
            ++i;
            continue;
        }
        // strict UTF-8 decode; any malformation substitutes U+FFFD for one byte
        int len = 0;
        uint32_t cp = 0, min = 0;
        if ((c & 0xe0) == 0xc0) { len = 2; cp = c & 0x1f; min = 0x80; }
        else if ((c & 0xf0) == 0xe0) { len = 3; cp = c & 0x0f; min = 0x800; }
        else if ((c & 0xf8) == 0xf0) { len = 4; cp = c & 0x07; min = 0x10000; }
        bool ok = len > 0 && i + len <= n;
        for (int k = 1; ok && k < len; ++k) {
            if ((p[i + k] & 0xc0) != 0x80) ok = false;
            else cp = (cp << 6) | (p[i + k] & 0x3f);
        }
        if (ok && (cp < min || cp > 0x10ffff || (cp >= 0xd800 && cp <= 0xdfff))) ok = false;
        if (!ok) {
            u16(0xfffd);
            ++i;
            continue;
        }
        if (cp >= 0x10000) {
            cp -= 0x10000;
            u16(0xd800 + (cp >> 10));
            u16(0xdc00 + (cp & 0x3ff));
        } else {
            u16(cp);
        }
        i += len;
    }
    out.push_back('"');
}

// ------------------------------------------------------------ FastCGI wire

enum : uint8_t {
    kBeginRequest = 1, kAbortRequest = 2, kEndRequest = 3, kParams = 4, kStdin = 5,
    kStdout = 6, kStderr = 7, kData = 8, kGetValues = 9, kGetValuesResult = 10, kUnknownType = 11,
};
constexpr size_t kMaxContent = 65535;

struct Conn {
    int fd;
    explicit Conn(int f) : fd(f) {}
    ~Conn() { if (fd >= 0) ::close(fd); }
    Conn(const Conn&) = delete;
    Conn& operator=(const Conn&) = delete;

    bool read_exact(void* buf, size_t n) {
        auto* p = static_cast<char*>(buf);
        while (n > 0) {
            ssize_t r = ::recv(fd, p, n, 0);
            if (r > 0) { p += r; n -= size_t(r); continue; }
            if (r < 0 && errno == EINTR) continue;
            return false;
        }
        return true;
    }
    bool write_all(const char* p, size_t n) {
        while (n > 0) {
            ssize_t r = ::send(fd, p, n, MSG_NOSIGNAL);
            if (r > 0) { p += r; n -= size_t(r); continue; }
            if (r < 0 && errno == EINTR) continue;
            return false;
        }
        return true;
    }
    // The gateway sends nothing between the last STDIN record and our
    // response, so EOF or an error here means it went away.
    bool peer_alive() {
        pollfd pf{fd, POLLIN | POLLRDHUP, 0};
        if (::poll(&pf, 1, 0) < 0) return true;
        if (pf.revents & (POLLERR | POLLHUP | POLLNVAL | POLLRDHUP)) return false;
        if (pf.revents & POLLIN) {
            char c;
            ssize_t r = ::recv(fd, &c, 1, MSG_PEEK | MSG_DONTWAIT);
            if (r == 0) return false;
        }
        return true;
    }
    bool record(uint8_t type, uint16_t id, const char* data, size_t len) {
        const size_t pad = (8 - len % 8) % 8;
        std::string buf;
        buf.reserve(8 + len + pad);
        buf.push_back(1);
        buf.push_back(char(type));
        buf.push_back(char(id >> 8));
        buf.push_back(char(id & 0xff));
        buf.push_back(char(len >> 8));
        buf.push_back(char(len & 0xff));
        buf.push_back(char(pad));
        buf.push_back(0);
        buf.append(data, len);
        buf.append(pad, '\0');
        return write_all(buf.data(), buf.size());
    }
    bool end_request(uint16_t id, uint32_t app_status, uint8_t proto_status) {
        char b[8] = {char(app_status >> 24), char(app_status >> 16), char(app_status >> 8),
                     char(app_status), char(proto_status), 0, 0, 0};
        return record(kEndRequest, id, b, 8);
    }
};

struct Request {
    uint16_t id = 0;
    bool keep_conn = false;
    std::vector<std::pair<std::string, std::string>> params;
    std::string stdin_data;

    const std::string* param(const std::string& name) const {
        for (auto& kv : params) if (kv.first == name) return &kv.second;
        return nullptr;
    }
};

// Response writer: stdout is buffered until flush() or 65535 bytes pile up.
class Response {
public:
    Response(Conn& c, uint16_t id) : c_(c), id_(id) {}
    bool failed() const { return failed_; }

    void write(const std::string& s) { write(s.data(), s.size()); }
    void write(const char* p, size_t n) {
        buf_.append(p, n);
        while (buf_.size() >= kMaxContent) {
            send(buf_.data(), kMaxContent);
            buf_.erase(0, kMaxContent);
        }
    }
    void flush() {
        if (!buf_.empty()) send(buf_.data(), buf_.size());
        buf_.clear();
        if (!failed_ && !c_.peer_alive()) failed_ = true;
    }
    void err(const std::string& s) {
        for (size_t o = 0; o < s.size() && !failed_; o += kMaxContent)
            failed_ |= !c_.record(kStderr, id_, s.data() + o, std::min(kMaxContent, s.size() - o));
    }
    // Complete the request normally.
    void finish(uint32_t app_status = 0) {
        flush();
        if (failed_) return;
        failed_ = !c_.record(kStdout, id_, nullptr, 0) || !c_.record(kStderr, id_, nullptr, 0) ||
                  !c_.end_request(id_, app_status, 0);
    }

private:
    void send(const char* p, size_t n) {
        if (!failed_ && !c_.record(kStdout, id_, p, n)) failed_ = true;
    }
    Conn& c_;
    uint16_t id_;
    std::string buf_;
    bool failed_ = false;
};

// ---------------------------------------------------------------- fixtures

using Query = std::map<std::string, std::vector<std::string>>;

std::string urldecode(const std::string& s) {
    std::string o;
    for (size_t i = 0; i < s.size(); ++i) {
        if (s[i] == '+') o.push_back(' ');
        else if (s[i] == '%' && i + 2 < s.size() && std::isxdigit((unsigned char)s[i + 1]) &&
                 std::isxdigit((unsigned char)s[i + 2])) {
            o.push_back(char(std::stoi(s.substr(i + 1, 2), nullptr, 16)));
            i += 2;
        } else o.push_back(s[i]);
    }
    return o;
}

Query parse_query(const std::string& qs) {
    Query q;
    size_t pos = 0;
    while (pos <= qs.size()) {
        size_t amp = qs.find('&', pos);
        if (amp == std::string::npos) amp = qs.size();
        std::string pair = qs.substr(pos, amp - pos);
        pos = amp + 1;
        if (pair.empty()) continue;
        size_t eq = pair.find('=');
        std::string k = urldecode(pair.substr(0, eq));
        std::string v = eq == std::string::npos ? "" : urldecode(pair.substr(eq + 1));
        if (k.size() >= 2 && k.compare(k.size() - 2, 2, "[]") == 0) k.resize(k.size() - 2);
        q[k].push_back(v);
    }
    return q;
}

struct Q {
    Query q;
    bool has(const char* k) const { return q.count(k) > 0; }
    std::string str(const char* k, const std::string& d = "") const {
        auto it = q.find(k);
        return it == q.end() ? d : it->second.back();
    }
    long num(const char* k, long d = 0) const {
        auto it = q.find(k);
        if (it == q.end()) return d;
        return std::strtol(it->second.back().c_str(), nullptr, 10);
    }
    const std::vector<std::string>& list(const char* k) const {
        static const std::vector<std::string> empty;
        auto it = q.find(k);
        return it == q.end() ? empty : it->second;
    }
};

std::string pattern(size_t n, size_t start = 0) {
    std::string s(n, '\0');
    for (size_t i = 0; i < n; ++i) s[i] = char((start + i) % 256);
    return s;
}

void sleep_ms(long ms) {
    if (ms > 0) std::this_thread::sleep_for(std::chrono::milliseconds(ms));
}

std::string head(const std::string& name, const char* content_type = "text/plain") {
    std::string h = "X-Fixture: " + name + "\r\n";
    if (content_type) h += std::string("Content-Type: ") + content_type + "\r\n";
    return h;
}

[[noreturn]] void die_by(const std::string& sig) {
    int n = SIGSEGV;
    if (sig == "ABRT") n = SIGABRT;
    else if (sig == "KILL") n = SIGKILL;
    if (n == SIGKILL) ::kill(::getpid(), SIGKILL);
    else std::raise(n);
    // a signal to ourselves is delivered before raise returns; this is a backstop
    std::this_thread::sleep_for(std::chrono::seconds(5));
    std::abort();
}

void fx_dump(const Request& r, Response& out) {
    std::string j = "{\"env\":{";
    bool first = true;
    for (auto& kv : r.params) {
        if (!first) j.push_back(',');
        first = false;
        json_escape(j, kv.first);
        j.push_back(':');
        json_escape(j, kv.second);
    }
    j += "},\"stdin_length\":" + std::to_string(r.stdin_data.size()) +
         ",\"stdin_sha256\":\"" + sha256_hex(r.stdin_data) + "\"}";
    out.write(head("dump", "application/json") + "\r\n" + j);
}

void fx_respond(const Q& q, Response& out) {
    sleep_ms(q.num("sleep_ms"));
    std::string h = "X-Fixture: respond\r\n";
    if (q.has("raw_status")) h += "Status: " + q.str("raw_status") + "\r\n";
    else if (q.has("status")) h += "Status: " + std::to_string(q.num("status")) + "\r\n";
    bool has_ct = false;
    for (auto& line : q.list("header")) {
        h += line + "\r\n";
        std::string low = line.substr(0, 13);
        std::transform(low.begin(), low.end(), low.begin(), [](unsigned char c) { return std::tolower(c); });
        if (low == "content-type:") has_ct = true;
    }
    if (!has_ct) h += "Content-Type: text/plain\r\n";
    out.write(h + "\r\n");
    if (q.has("body")) out.write(q.str("body"));
    if (q.has("body_size")) out.write(pattern(size_t(std::max(0L, q.num("body_size")))));
    if (q.has("stderr")) out.err(q.str("stderr"));
    if (q.str("flush") == "1") out.flush();
}

void fx_crash(const Q& q, Response& out) {
    out.write(head("crash", "application/octet-stream") + "\r\n");
    out.write(pattern(size_t(std::max(0L, q.num("after_bytes")))));
    if (q.str("flush") == "1") out.flush();
    die_by(q.str("signal", "SEGV"));
}

void fx_slow(const Q& q, Response& out) {
    out.write(head("slow") + "\r\n");
    long chunks = q.num("chunks", 1);
    size_t size = size_t(std::max(0L, q.num("chunk_size", 1)));
    for (long i = 0; i < chunks && !out.failed(); ++i) {
        sleep_ms(q.num("drip_ms"));
        out.write(std::string(size, '.'));
        out.flush();
    }
}

void fx_big(const Q& q, Response& out) {
    out.write(head("big", "application/octet-stream") + "\r\n");
    size_t left = size_t(std::max(0L, q.num("bytes")));
    size_t off = 0;
    while (left > 0 && !out.failed()) {
        size_t n = std::min<size_t>(left, 65536);
        out.write(pattern(n, off));
        off += n;
        left -= n;
    }
}

void fx_stderr(const Q& q, Response& out) {
    out.write(head("stderr") + "\r\n");
    size_t left = size_t(std::max(0L, q.num("bytes")));
    bool inter = q.str("interleave") == "1";
    if (inter) out.flush();
    while (left > 0 && !out.failed()) {
        size_t n = std::min<size_t>(left, 1024);
        out.err(std::string(n, 'e'));
        if (inter) {
            out.write(std::string(1024, 'o'));
            out.flush();
        }
        left -= n;
    }
}

void fx_fatal(Response& out) {
    out.write(head("fatal") + "before");
    out.flush();
    throw std::runtime_error("fixture fatal");  // escapes the worker: std::terminate
}

struct Db {
    sqlite3* h = nullptr;
    ~Db() { if (h) sqlite3_close(h); }
    bool exec(const char* sql) { return sqlite3_exec(h, sql, nullptr, nullptr, nullptr) == SQLITE_OK; }
    bool insert(const std::string& k) {
        sqlite3_stmt* st = nullptr;
        if (sqlite3_prepare_v2(h, "INSERT INTO kv (k, n) VALUES (?, 1)", -1, &st, nullptr) != SQLITE_OK) return false;
        sqlite3_bind_text(st, 1, k.c_str(), int(k.size()), SQLITE_TRANSIENT);
        int rc = sqlite3_step(st);
        sqlite3_finalize(st);
        return rc == SQLITE_DONE;
    }
    long count(const std::string& k) {
        sqlite3_stmt* st = nullptr;
        long n = -1;
        if (sqlite3_prepare_v2(h, "SELECT COUNT(*) FROM kv WHERE k = ?", -1, &st, nullptr) != SQLITE_OK) return -1;
        sqlite3_bind_text(st, 1, k.c_str(), int(k.size()), SQLITE_TRANSIENT);
        if (sqlite3_step(st) == SQLITE_ROW) n = sqlite3_column_int64(st, 0);
        sqlite3_finalize(st);
        return n;
    }
};

void fx_txn(const Q& q, Response& out) {
    const char* dir = std::getenv("FIXTURE_STATE");
    std::string path = std::string(dir ? dir : "/state") + "/state.db";
    Db db;
    auto fail = [&](int status, const std::string& msg) {
        out.write(head("txn") + "Status: " + std::to_string(status) + "\r\n\r\n" + msg);
    };
    if (sqlite3_open(path.c_str(), &db.h) != SQLITE_OK) return fail(500, "open failed");
    sqlite3_busy_timeout(db.h, 5000);
    if (!db.exec("CREATE TABLE IF NOT EXISTS kv (k TEXT PRIMARY KEY, n INTEGER NOT NULL)"))
        return fail(500, sqlite3_errmsg(db.h));
    const std::string op = q.str("op"), k = q.str("k");

    if (op == "reset") {
        if (!db.exec("DELETE FROM kv")) return fail(500, sqlite3_errmsg(db.h));
        out.write(head("txn") + "\r\nreset");
    } else if (op == "count") {
        long n = db.count(k);
        if (n < 0) return fail(500, sqlite3_errmsg(db.h));
        out.write(head("txn") + "\r\n" + std::to_string(n));
    } else if (op == "insert") {
        if (!db.exec("BEGIN IMMEDIATE")) return fail(500, sqlite3_errmsg(db.h));
        if (!db.insert(k)) {
            std::string m = sqlite3_errmsg(db.h);
            db.exec("ROLLBACK");
            return fail(500, m);
        }
        sleep_ms(q.num("sleep_ms"));
        out.write(head("txn") + "\r\ninserted");
        out.flush();
        // the row is only durable once the client demonstrably received the answer
        db.exec(out.failed() ? "ROLLBACK" : "COMMIT");
    } else if (op == "insert_commit_crash") {
        if (!db.exec("BEGIN") || !db.insert(k) || !db.exec("COMMIT")) return fail(500, sqlite3_errmsg(db.h));
        die_by("SEGV");
    } else if (op == "insert_crash") {
        if (!db.exec("BEGIN") || !db.insert(k)) return fail(500, sqlite3_errmsg(db.h));
        die_by("SEGV");
    } else {
        fail(400, "unknown op");
    }
}

// exitcode: the status only shows up in END_REQUEST, so it returns it.
uint32_t run_fixture(const Request& r, Response& out) {
    const std::string* sn = r.param("SCRIPT_NAME");
    std::string name = sn ? *sn : "";
    size_t slash = name.rfind('/');
    if (slash != std::string::npos) name.erase(0, slash + 1);
    size_t dot = name.rfind('.');
    if (dot != std::string::npos && dot > 0) name.erase(dot);

    const std::string* qs = r.param("QUERY_STRING");
    Q q{parse_query(qs ? *qs : "")};

    if (name == "dump") fx_dump(r, out);
    else if (name == "respond") fx_respond(q, out);
    else if (name == "crash") fx_crash(q, out);
    else if (name == "slow") fx_slow(q, out);
    else if (name == "big") fx_big(q, out);
    else if (name == "stderr") fx_stderr(q, out);
    else if (name == "fatal") fx_fatal(out);
    else if (name == "txn") fx_txn(q, out);
    else if (name == "exitcode") {
        out.write(head("exitcode") + "\r\nexit");
        return uint32_t(q.num("code"));
    } else {
        out.write(head(name.empty() ? "unknown" : name) + "Status: 404\r\n\r\nunknown fixture");
    }
    return 0;
}

// -------------------------------------------------------------- server loop

class Pool {
public:
    explicit Pool(int n) {
        for (int i = 0; i < n; ++i) threads_.emplace_back([this] { loop(); });
    }
    void submit(std::function<void()> f) {
        { std::lock_guard<std::mutex> l(m_); q_.push_back(std::move(f)); }
        cv_.notify_one();
    }
private:
    void loop() {
        for (;;) {
            std::function<void()> f;
            {
                std::unique_lock<std::mutex> l(m_);
                cv_.wait(l, [this] { return !q_.empty(); });
                f = std::move(q_.front());
                q_.pop_front();
            }
            f();  // exceptions are deliberately not caught: fatal must terminate
        }
    }
    std::mutex m_;
    std::condition_variable cv_;
    std::deque<std::function<void()>> q_;
    std::vector<std::thread> threads_;
};

void parse_params(const std::string& raw, Request& r) {
    const auto* p = reinterpret_cast<const unsigned char*>(raw.data());
    size_t i = 0, n = raw.size();
    auto len = [&](uint32_t& out) {
        if (i >= n) return false;
        if (p[i] >> 7) {
            if (i + 4 > n) return false;
            out = ((uint32_t(p[i]) & 0x7f) << 24) | (uint32_t(p[i + 1]) << 16) | (uint32_t(p[i + 2]) << 8) | p[i + 3];
            i += 4;
        } else out = p[i++];
        return true;
    };
    while (i < n) {
        uint32_t nl, vl;
        if (!len(nl) || !len(vl) || i + nl + size_t(vl) > n) return;
        r.params.emplace_back(raw.substr(i, nl), raw.substr(i + nl, vl));
        i += nl + size_t(vl);
    }
}

void get_values_result(Conn& c, const std::string& body) {
    // reuse the params name/value parser; answer only names we know
    Request tmp;
    parse_params(body, tmp);
    std::string out;
    auto add = [&](const std::string& k, const std::string& v) {
        out.push_back(char(k.size()));
        out.push_back(char(v.size()));
        out += k + v;
    };
    for (auto& kv : tmp.params) {
        if (kv.first == "FCGI_MAX_CONNS") add(kv.first, "4");
        else if (kv.first == "FCGI_MAX_REQS") add(kv.first, "4");
        else if (kv.first == "FCGI_MPXS_CONNS") add(kv.first, "0");
    }
    c.record(kGetValuesResult, 0, out.data(), out.size());
}

void serve(int fd) {
    Conn c(fd);
    static Pool pool(4);
    Request req;
    bool active = false, params_done = false;
    std::string params_raw;
    for (;;) {
        unsigned char h[8];
        if (!c.read_exact(h, 8)) return;
        uint8_t type = h[1];
        uint16_t id = uint16_t(h[2] << 8 | h[3]);
        size_t clen = size_t(h[4] << 8 | h[5]), plen = h[6];
        std::string body(clen, '\0');
        if (clen && !c.read_exact(body.data(), clen)) return;
        if (plen) { char pad[256]; if (!c.read_exact(pad, plen)) return; }

        if (id == 0) {  // management records
            if (type == kGetValues) get_values_result(c, body);
            else {
                char b[8] = {char(type), 0, 0, 0, 0, 0, 0, 0};
                c.record(kUnknownType, 0, b, 8);
            }
            continue;
        }
        switch (type) {
        case kBeginRequest: {
            uint16_t role = uint16_t(uint8_t(body[0]) << 8 | uint8_t(body[1]));
            if (active) { c.end_request(id, 0, 1); break; }  // CANT_MPX_CONN
            if (role != 1) { c.end_request(id, 0, 3); break; }  // UNKNOWN_ROLE
            req = Request{};
            req.id = id;
            req.keep_conn = uint8_t(body[2]) & 1;
            active = true;
            params_done = false;
            params_raw.clear();
            break;
        }
        case kAbortRequest:
            if (active && id == req.id) { c.end_request(id, 0, 0); active = false; if (!req.keep_conn) return; }
            break;
        case kParams:
            if (!active || id != req.id) break;
            if (clen) params_raw += body;
            else { parse_params(params_raw, req); params_done = true; }
            break;
        case kStdin:
            if (!active || id != req.id || !params_done) break;
            if (clen) { req.stdin_data += body; break; }
            {
                std::mutex m;
                std::condition_variable cv;
                bool done = false;
                bool ok = true;
                pool.submit([&] {
                    Response out(c, req.id);
                    uint32_t status = run_fixture(req, out);
                    out.finish(status);
                    std::lock_guard<std::mutex> l(m);
                    ok = !out.failed();
                    done = true;
                    cv.notify_one();
                });
                std::unique_lock<std::mutex> l(m);
                cv.wait(l, [&] { return done; });
                if (!ok || !req.keep_conn) return;
                active = false;
            }
            break;
        default:
            break;  // DATA and unexpected records are ignored
        }
    }
}

}  // namespace

int main() {
    std::signal(SIGPIPE, SIG_IGN);
    int ls = ::socket(AF_INET, SOCK_STREAM, 0);
    int one = 1;
    ::setsockopt(ls, SOL_SOCKET, SO_REUSEADDR, &one, sizeof one);
    sockaddr_in a{};
    a.sin_family = AF_INET;
    a.sin_addr.s_addr = htonl(INADDR_ANY);
    a.sin_port = htons(9000);
    if (::bind(ls, reinterpret_cast<sockaddr*>(&a), sizeof a) < 0 || ::listen(ls, 128) < 0) {
        std::perror("bind/listen");
        return 1;
    }
    std::fprintf(stderr, "listening on 0.0.0.0:9000\n");
    for (;;) {
        int fd = ::accept(ls, nullptr, nullptr);
        if (fd < 0) { if (errno == EINTR) continue; std::perror("accept"); return 1; }
        int nd = 1;
        ::setsockopt(fd, IPPROTO_TCP, TCP_NODELAY, &nd, sizeof nd);
        std::thread(serve, fd).detach();
    }
}
