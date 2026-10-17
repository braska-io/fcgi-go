//! Rust reference FastCGI responder for the compliance suite.
//! Contract: compliance/RESPONDERS.md. Protocol code is in proto.rs.

mod proto;

use std::io;
use std::sync::Arc;
use std::time::Duration;

use sha2::{Digest, Sha256};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::tcp::{OwnedReadHalf, OwnedWriteHalf};
use tokio::net::{TcpListener, TcpStream};
use tokio::sync::Semaphore;
use tokio::time::sleep;

const MAX_CONCURRENT: usize = 4;

struct Request {
    id: u16,
    keep_conn: bool,
    params: Vec<(Vec<u8>, Vec<u8>)>,
    stdin_len: u64,
    stdin_sha256: String,
}

impl Request {
    fn param(&self, name: &str) -> Option<String> {
        self.params
            .iter()
            .rev()
            .find(|(k, _)| k == name.as_bytes())
            .map(|(_, v)| String::from_utf8_lossy(v).into_owned())
    }

    fn fixture(&self) -> String {
        let script = self.param("SCRIPT_NAME").unwrap_or_default();
        let base = script.rsplit('/').next().unwrap_or("");
        match base.rfind('.') {
            Some(i) if i > 0 => base[..i].to_string(),
            _ => base.to_string(),
        }
    }
}

struct Query(Vec<(String, String)>);

fn form_decode(s: &str) -> String {
    let b = s.as_bytes();
    let mut out = Vec::with_capacity(b.len());
    let mut i = 0;
    while i < b.len() {
        match b[i] {
            b'+' => out.push(b' '),
            b'%' if i + 2 < b.len() && hex_val(b[i + 1]).is_some() && hex_val(b[i + 2]).is_some() => {
                out.push(hex_val(b[i + 1]).unwrap() * 16 + hex_val(b[i + 2]).unwrap());
                i += 2;
            }
            c => out.push(c),
        }
        i += 1;
    }
    String::from_utf8_lossy(&out).into_owned()
}

fn hex_val(c: u8) -> Option<u8> {
    match c {
        b'0'..=b'9' => Some(c - b'0'),
        b'a'..=b'f' => Some(c - b'a' + 10),
        b'A'..=b'F' => Some(c - b'A' + 10),
        _ => None,
    }
}

impl Query {
    fn parse(qs: &str) -> Query {
        let mut v = Vec::new();
        for pair in qs.split('&') {
            if pair.is_empty() {
                continue;
            }
            let (k, val) = pair.split_once('=').unwrap_or((pair, ""));
            let k = form_decode(k);
            let k = k.strip_suffix("[]").map(str::to_string).unwrap_or(k);
            v.push((k, form_decode(val)));
        }
        Query(v)
    }
    fn get(&self, k: &str) -> Option<&str> {
        self.0.iter().rev().find(|(n, _)| n == k).map(|(_, v)| v.as_str())
    }
    fn all(&self, k: &str) -> Vec<&str> {
        self.0.iter().filter(|(n, _)| n == k).map(|(_, v)| v.as_str()).collect()
    }
    fn num(&self, k: &str, default: u64) -> u64 {
        self.get(k).and_then(|v| v.trim().parse().ok()).unwrap_or(default)
    }
    fn flag(&self, k: &str) -> bool {
        self.get(k) == Some("1")
    }
}

fn pattern(offset: usize, n: usize) -> Vec<u8> {
    (offset..offset + n).map(|i| (i % 256) as u8).collect()
}

/// Buffered stdout; `flush` puts the buffer on the wire as STDOUT records.
struct Out<'a> {
    wr: &'a mut OwnedWriteHalf,
    id: u16,
    buf: Vec<u8>,
    used_stderr: bool,
}

impl Out<'_> {
    fn write(&mut self, b: &[u8]) {
        self.buf.extend_from_slice(b);
    }

    async fn flush(&mut self) -> io::Result<()> {
        if !self.buf.is_empty() {
            let bytes = proto::stream_records(proto::STDOUT, self.id, &self.buf);
            self.buf.clear();
            self.wr.write_all(&bytes).await?;
        }
        self.wr.flush().await
    }

    async fn stderr(&mut self, b: &[u8]) -> io::Result<()> {
        self.used_stderr = true;
        let bytes = proto::stream_records(proto::STDERR, self.id, b);
        self.wr.write_all(&bytes).await
    }

    async fn raw(&mut self, bytes: &[u8]) -> io::Result<()> {
        self.wr.write_all(bytes).await?;
        self.wr.flush().await
    }
}

enum Action {
    Done(u32),
    /// The handler wrote everything itself; the connection must close.
    CloseConn,
}

fn die(sig: i32) -> ! {
    // Rust's runtime may install its own SIGSEGV handler; restore the default
    // so raise() really kills the process.
    unsafe {
        libc::signal(sig, libc::SIG_DFL);
        libc::raise(sig);
    }
    std::thread::sleep(Duration::from_secs(5));
    std::process::abort()
}

fn signal_number(name: Option<&str>) -> i32 {
    match name {
        Some("ABRT") => libc::SIGABRT,
        Some("KILL") => libc::SIGKILL,
        _ => libc::SIGSEGV,
    }
}

fn client_gone(rd: &OwnedReadHalf) -> bool {
    let mut b = [0u8; 1];
    match rd.try_read(&mut b) {
        Ok(0) => true,
        Ok(_) => false,
        Err(e) if e.kind() == io::ErrorKind::WouldBlock => false,
        Err(_) => true,
    }
}

fn head(status: Option<&str>, extra: &[&str]) -> Vec<u8> {
    let mut s = String::new();
    if let Some(st) = status {
        s.push_str(&format!("Status: {st}\r\n"));
    }
    for l in extra {
        s.push_str(l);
        s.push_str("\r\n");
    }
    s.push_str("\r\n");
    s.into_bytes()
}

async fn handle(req: &Request, rd: &OwnedReadHalf, out: &mut Out<'_>) -> io::Result<Action> {
    let name = req.fixture();
    let q = Query::parse(&req.param("QUERY_STRING").unwrap_or_default());
    let xf = format!("X-Fixture: {name}");
    match name.as_str() {
        "dump" => {
            let mut env = serde_json::Map::new();
            for (k, v) in &req.params {
                env.insert(
                    String::from_utf8_lossy(k).into_owned(),
                    serde_json::Value::String(String::from_utf8_lossy(v).into_owned()),
                );
            }
            let body = serde_json::json!({
                "env": env,
                "stdin_length": req.stdin_len,
                "stdin_sha256": req.stdin_sha256,
            });
            out.write(&head(None, &[&xf, "Content-Type: application/json"]));
            out.write(body.to_string().as_bytes());
        }
        "respond" => {
            let ms = q.num("sleep_ms", 0);
            if ms > 0 {
                sleep(Duration::from_millis(ms)).await;
            }
            let status = q.get("raw_status").or_else(|| q.get("status"));
            let headers = q.all("header");
            let mut lines: Vec<&str> = vec![&xf];
            lines.extend(headers.iter().copied());
            let has_ct = headers.iter().any(|h| h.to_ascii_lowercase().starts_with("content-type:"));
            if !has_ct {
                lines.push("Content-Type: text/plain");
            }
            out.write(&head(status, &lines));
            if let Some(b) = q.get("body") {
                out.write(b.as_bytes());
            }
            if let Some(n) = q.get("body_size") {
                out.write(&pattern(0, n.trim().parse().unwrap_or(0)));
            }
            if let Some(s) = q.get("stderr") {
                out.stderr(format!("{s}\n").as_bytes()).await?;
            }
            if q.flush_flag() {
                out.flush().await?;
            }
        }
        "crash" => {
            out.write(&head(None, &[&xf, "Content-Type: application/octet-stream"]));
            out.write(&pattern(0, q.num("after_bytes", 0) as usize));
            if q.flag("flush") {
                out.flush().await?;
            }
            die(signal_number(q.get("signal")));
        }
        "slow" => {
            out.write(&head(None, &[&xf, "Content-Type: text/plain"]));
            let size = q.num("chunk_size", 1) as usize;
            for _ in 0..q.num("chunks", 1) {
                sleep(Duration::from_millis(q.num("drip_ms", 0))).await;
                out.write(&vec![b'.'; size]);
                out.flush().await?;
            }
        }
        "big" => {
            out.write(&head(None, &[&xf, "Content-Type: application/octet-stream"]));
            let total = q.num("bytes", 0) as usize;
            let mut sent = 0;
            while sent < total {
                let n = (total - sent).min(proto::MAX_CONTENT);
                out.write(&pattern(sent, n));
                sent += n;
                out.flush().await?;
            }
        }
        "stderr" => {
            out.write(&head(None, &[&xf, "Content-Type: text/plain"]));
            let mut left = q.num("bytes", 0) as usize;
            if q.flag("interleave") {
                while left > 0 {
                    let n = left.min(1024);
                    out.stderr(&vec![b'e'; n]).await?;
                    out.write(&vec![b'o'; 1024]);
                    out.flush().await?;
                    left -= n;
                }
            } else if left > 0 {
                out.stderr(&vec![b'e'; left]).await?;
                left = 0;
            }
            let _ = left;
        }
        "fatal" => {
            out.write(&head(None, &[&xf, "Content-Type: text/plain"]));
            out.write(b"before");
            let _ = out.flush().await;
            std::process::abort();
        }
        "exitcode" => {
            out.write(&head(None, &[&xf, "Content-Type: text/plain"]));
            out.write(b"ok");
            return Ok(Action::Done(q.num("code", 0) as u32));
        }
        "txn" => return txn(&q, &xf, rd, out).await,
        "malformed" => return malformed(&q, req.id, &xf, rd, out).await,
        _ => {
            out.write(&head(Some("404 Not Found"), &[&xf, "Content-Type: text/plain"]));
            out.write(b"unknown fixture");
        }
    }
    Ok(Action::Done(0))
}

trait FlushFlag {
    fn flush_flag(&self) -> bool;
}
impl FlushFlag for Query {
    fn flush_flag(&self) -> bool {
        self.flag("flush")
    }
}

fn io_err(e: rusqlite::Error) -> io::Error {
    io::Error::new(io::ErrorKind::Other, e)
}

async fn txn(q: &Query, xf: &str, rd: &OwnedReadHalf, out: &mut Out<'_>) -> io::Result<Action> {
    let dir = std::env::var("FIXTURE_STATE").unwrap_or_else(|_| "/state".into());
    let conn = rusqlite::Connection::open(format!("{dir}/state.db")).map_err(io_err)?;
    // Mutex makes the handle Sync so the future stays Send across awaits.
    let db = std::sync::Mutex::new(conn);
    db.lock().unwrap().busy_timeout(Duration::from_millis(5000)).map_err(io_err)?;
    db.lock().unwrap().execute_batch("CREATE TABLE IF NOT EXISTS kv (k TEXT PRIMARY KEY, n INTEGER NOT NULL)")
        .map_err(io_err)?;
    let k = q.get("k").unwrap_or("");
    let ok = |out: &mut Out<'_>, body: &str| {
        out.write(&head(None, &[xf, "Content-Type: text/plain"]));
        out.write(body.as_bytes());
    };
    match q.get("op").unwrap_or("") {
        "reset" => {
            db.lock().unwrap().execute("DELETE FROM kv", []).map_err(io_err)?;
            ok(out, "reset");
        }
        "count" => {
            let n: i64 = db
                .lock()
                .unwrap()
                .query_row("SELECT COUNT(*) FROM kv WHERE k = ?1", [k], |r| r.get(0))
                .map_err(io_err)?;
            ok(out, &n.to_string());
        }
        "insert" => {
            db.lock().unwrap().execute_batch("BEGIN IMMEDIATE").map_err(io_err)?;
            let r: io::Result<()> = async {
                db.lock().unwrap().execute("INSERT INTO kv (k, n) VALUES (?1, 1)", [k]).map_err(io_err)?;
                sleep(Duration::from_millis(q.num("sleep_ms", 0))).await;
                // A closed peer is only reported by a write after the RST
                // arrives, so look at the socket before writing as well.
                if client_gone(rd) {
                    return Err(io::Error::new(io::ErrorKind::BrokenPipe, "client gone"));
                }
                ok(out, "inserted");
                out.flush().await
            }
            .await;
            match r {
                Ok(()) => db.lock().unwrap().execute_batch("COMMIT").map_err(io_err)?,
                Err(e) => {
                    let _ = db.lock().unwrap().execute_batch("ROLLBACK");
                    return Err(e);
                }
            }
        }
        "insert_commit_crash" => {
            db.lock().unwrap().execute_batch("BEGIN").map_err(io_err)?;
            db.lock().unwrap().execute("INSERT INTO kv (k, n) VALUES (?1, 1)", [k]).map_err(io_err)?;
            db.lock().unwrap().execute_batch("COMMIT").map_err(io_err)?;
            die(libc::SIGSEGV);
        }
        "insert_crash" => {
            db.lock().unwrap().execute_batch("BEGIN").map_err(io_err)?;
            db.lock().unwrap().execute("INSERT INTO kv (k, n) VALUES (?1, 1)", [k]).map_err(io_err)?;
            die(libc::SIGSEGV);
        }
        _ => {
            out.write(&head(Some("400 Bad Request"), &[xf, "Content-Type: text/plain"]));
            out.write(b"unknown op");
        }
    }
    Ok(Action::Done(0))
}

async fn malformed(q: &Query, id: u16, xf: &str, rd: &OwnedReadHalf, out: &mut Out<'_>) -> io::Result<Action> {
    use proto::*;
    let block = head(None, &[xf, "Content-Type: text/plain"]);
    let mut body = block.clone();
    body.extend_from_slice(b"malformed");
    match q.get("kind").unwrap_or("") {
        "bad_version" => {
            let mut b = record_with_version(2, STDOUT, id, &body);
            b.extend_from_slice(&record(STDOUT, id, b""));
            b.extend_from_slice(&end_request(id, 0, REQUEST_COMPLETE));
            out.raw(&b).await?;
        }
        "truncated_record" => {
            // The 10 bytes are the start of the X-Fixture header line.
            let mut b = vec![VERSION, STDOUT];
            b.extend_from_slice(&id.to_be_bytes());
            b.extend_from_slice(&[0, 100, 0, 0]);
            b.extend_from_slice(&xf.as_bytes()[..10]);
            out.raw(&b).await?;
        }
        "missing_end_request" => {
            let mut b = record(STDOUT, id, &body);
            b.extend_from_slice(&record(STDOUT, id, b""));
            out.raw(&b).await?;
        }
        "wrong_request_id" => {
            let other = id.wrapping_add(1);
            let mut b = record(STDOUT, other, &body);
            b.extend_from_slice(&record(STDOUT, other, b""));
            b.extend_from_slice(&end_request(id, 0, REQUEST_COMPLETE));
            out.raw(&b).await?;
        }
        "stdout_after_end" => {
            let mut b = record(STDOUT, id, &body);
            b.extend_from_slice(&record(STDOUT, id, b""));
            b.extend_from_slice(&end_request(id, 0, REQUEST_COMPLETE));
            b.extend_from_slice(&record(STDOUT, id, b"late"));
            out.raw(&b).await?;
        }
        "no_header_terminator" => {
            let mut data = format!("{xf}\r\nContent-Type: text/plain\r\nno terminator").into_bytes();
            data.extend_from_slice(b"");
            let mut b = record(STDOUT, id, &data);
            b.extend_from_slice(&record(STDOUT, id, b""));
            b.extend_from_slice(&end_request(id, 0, REQUEST_COMPLETE));
            out.raw(&b).await?;
        }
        "oversized_header_block" => {
            let mut data = format!("{xf}\r\n").into_bytes();
            let mut i = 0;
            while data.len() < (1 << 20) {
                data.extend_from_slice(format!("X-Pad-{i}: {}\r\n", "a".repeat(900)).as_bytes());
                i += 1;
            }
            data.extend_from_slice(b"\r\n");
            let mut b = stream_records(STDOUT, id, &data);
            b.extend_from_slice(&record(STDOUT, id, b""));
            b.extend_from_slice(&end_request(id, 0, REQUEST_COMPLETE));
            out.raw(&b).await?;
        }
        "stall" => {
            out.raw(&record(STDOUT, id, &block)).await?;
            while !client_gone(rd) {
                sleep(Duration::from_millis(50)).await;
            }
        }
        _ => {
            out.write(&head(Some("400 Bad Request"), &[xf, "Content-Type: text/plain"]));
            out.write(b"unknown kind");
            return Ok(Action::Done(0));
        }
    }
    Ok(Action::CloseConn)
}

async fn read_request(rd: &mut OwnedReadHalf, wr: &mut OwnedWriteHalf) -> io::Result<Option<Request>> {
    let mut id = 0u16;
    let mut keep_conn = false;
    let mut begun = false;
    let mut params_raw: Vec<u8> = Vec::new();
    let mut hasher = Sha256::new();
    let mut stdin_len = 0u64;
    loop {
        let mut hb = [0u8; 8];
        match rd.read_exact(&mut hb).await {
            Ok(_) => {}
            Err(e) if e.kind() == io::ErrorKind::UnexpectedEof && !begun => return Ok(None),
            Err(e) => return Err(e),
        }
        let h = proto::parse_header(&hb);
        if h.version != proto::VERSION {
            return Err(io::Error::new(io::ErrorKind::InvalidData, "bad version"));
        }
        let mut content = vec![0u8; h.content_len + h.padding_len];
        rd.read_exact(&mut content).await?;
        content.truncate(h.content_len);

        if h.id == 0 {
            if h.kind == proto::GET_VALUES {
                let mut res = Vec::new();
                for (name, _) in proto::parse_pairs(&content) {
                    let v: Option<&[u8]> = match name.as_slice() {
                        b"FCGI_MPXS_CONNS" => Some(b"0"),
                        b"FCGI_MAX_CONNS" | b"FCGI_MAX_REQS" => Some(b"4"),
                        _ => None,
                    };
                    if let Some(v) = v {
                        proto::encode_pair(&mut res, &name, v);
                    }
                }
                wr.write_all(&proto::record(proto::GET_VALUES_RESULT, 0, &res)).await?;
            } else {
                let mut b = [0u8; 8];
                b[0] = h.kind;
                wr.write_all(&proto::record(proto::UNKNOWN_TYPE, 0, &b)).await?;
            }
            continue;
        }

        match h.kind {
            proto::BEGIN_REQUEST if !begun => {
                if content.len() < 8 {
                    return Err(io::Error::new(io::ErrorKind::InvalidData, "short begin"));
                }
                let role = u16::from_be_bytes([content[0], content[1]]);
                if role != proto::ROLE_RESPONDER {
                    wr.write_all(&proto::end_request(h.id, 0, proto::UNKNOWN_ROLE)).await?;
                    continue;
                }
                id = h.id;
                keep_conn = content[2] & proto::KEEP_CONN != 0;
                begun = true;
            }
            proto::PARAMS if begun && h.id == id => params_raw.extend_from_slice(&content),
            proto::STDIN if begun && h.id == id => {
                if content.is_empty() {
                    return Ok(Some(Request {
                        id,
                        keep_conn,
                        params: proto::parse_pairs(&params_raw),
                        stdin_len,
                        stdin_sha256: hasher.finalize().iter().map(|b| format!("{b:02x}")).collect(),
                    }));
                }
                stdin_len += content.len() as u64;
                hasher.update(&content);
            }
            _ => {}
        }
    }
}

async fn serve(stream: TcpStream, sem: Arc<Semaphore>) {
    let _ = stream.set_nodelay(true);
    let (mut rd, mut wr) = stream.into_split();
    loop {
        let req = match read_request(&mut rd, &mut wr).await {
            Ok(Some(r)) => r,
            _ => return,
        };
        let _permit = sem.acquire().await.expect("semaphore closed");
        let mut out = Out { wr: &mut wr, id: req.id, buf: Vec::new(), used_stderr: false };
        let action = match handle(&req, &rd, &mut out).await {
            Ok(a) => a,
            Err(_) => return,
        };
        match action {
            Action::CloseConn => return,
            Action::Done(status) => {
                let r: io::Result<()> = async {
                    out.flush().await?;
                    let mut tail = Vec::new();
                    if out.used_stderr {
                        tail.extend_from_slice(&proto::record(proto::STDERR, req.id, b""));
                    }
                    tail.extend_from_slice(&proto::record(proto::STDOUT, req.id, b""));
                    tail.extend_from_slice(&proto::end_request(req.id, status, proto::REQUEST_COMPLETE));
                    out.raw(&tail).await
                }
                .await;
                if r.is_err() || !req.keep_conn {
                    return;
                }
            }
        }
    }
}

fn main() {
    let rt = tokio::runtime::Builder::new_multi_thread()
        .worker_threads(8)
        .enable_all()
        .build()
        .expect("runtime");
    rt.block_on(async {
        let listener = TcpListener::bind("0.0.0.0:9000").await.expect("bind");
        let sem = Arc::new(Semaphore::new(MAX_CONCURRENT));
        loop {
            match listener.accept().await {
                Ok((s, _)) => {
                    tokio::spawn(serve(s, sem.clone()));
                }
                Err(_) => sleep(Duration::from_millis(50)).await,
            }
        }
    });
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn form_decoding() {
        assert_eq!(form_decode("a+b%20c%2"), "a b c%2");
        assert_eq!(form_decode("%e4"), "\u{fffd}");
    }

    #[test]
    fn repeated_header_array() {
        let q = Query::parse("header%5B%5D=A%3A+1&header[]=B%3A+2&status=201&status=202");
        assert_eq!(q.all("header"), ["A: 1", "B: 2"]);
        assert_eq!(q.get("status"), Some("202"));
    }

    #[test]
    fn fixture_name_from_script_name() {
        let mk = |s: &str| Request {
            id: 1,
            keep_conn: false,
            params: vec![(b"SCRIPT_NAME".to_vec(), s.as_bytes().to_vec())],
            stdin_len: 0,
            stdin_sha256: String::new(),
        };
        assert_eq!(mk("/dump.php").fixture(), "dump");
        assert_eq!(mk("/a/b/dump").fixture(), "dump");
    }

    #[test]
    fn pattern_wraps() {
        assert_eq!(pattern(254, 4), [254, 255, 0, 1]);
    }
}
