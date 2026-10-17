//! FastCGI 1.0 record framing. No I/O here, so it is unit-testable.

pub const VERSION: u8 = 1;
pub const BEGIN_REQUEST: u8 = 1;
pub const END_REQUEST: u8 = 3;
pub const PARAMS: u8 = 4;
pub const STDIN: u8 = 5;
pub const STDOUT: u8 = 6;
pub const STDERR: u8 = 7;
pub const GET_VALUES: u8 = 9;
pub const GET_VALUES_RESULT: u8 = 10;
pub const UNKNOWN_TYPE: u8 = 11;

pub const KEEP_CONN: u8 = 1;
pub const ROLE_RESPONDER: u16 = 1;
pub const REQUEST_COMPLETE: u8 = 0;
pub const UNKNOWN_ROLE: u8 = 3;

pub const MAX_CONTENT: usize = 65535;

pub struct Header {
    pub version: u8,
    pub kind: u8,
    pub id: u16,
    pub content_len: usize,
    pub padding_len: usize,
}

pub fn parse_header(b: &[u8; 8]) -> Header {
    Header {
        version: b[0],
        kind: b[1],
        id: u16::from_be_bytes([b[2], b[3]]),
        content_len: u16::from_be_bytes([b[4], b[5]]) as usize,
        padding_len: b[6] as usize,
    }
}

fn padding_for(len: usize) -> usize {
    (8 - len % 8) % 8
}

/// Encodes one record. `data` must not exceed MAX_CONTENT.
pub fn record_with_version(version: u8, kind: u8, id: u16, data: &[u8]) -> Vec<u8> {
    assert!(data.len() <= MAX_CONTENT);
    let pad = padding_for(data.len());
    let mut out = Vec::with_capacity(8 + data.len() + pad);
    out.push(version);
    out.push(kind);
    out.extend_from_slice(&id.to_be_bytes());
    out.extend_from_slice(&(data.len() as u16).to_be_bytes());
    out.push(pad as u8);
    out.push(0);
    out.extend_from_slice(data);
    out.resize(out.len() + pad, 0);
    out
}

pub fn record(kind: u8, id: u16, data: &[u8]) -> Vec<u8> {
    record_with_version(VERSION, kind, id, data)
}

/// Splits `data` into records of at most MAX_CONTENT bytes. Empty data yields
/// no record: the empty terminator record is always an explicit decision.
pub fn stream_records(kind: u8, id: u16, data: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(data.len() + 16);
    for chunk in data.chunks(MAX_CONTENT) {
        out.extend_from_slice(&record(kind, id, chunk));
    }
    out
}

pub fn end_request(id: u16, app_status: u32, protocol_status: u8) -> Vec<u8> {
    let mut body = [0u8; 8];
    body[..4].copy_from_slice(&app_status.to_be_bytes());
    body[4] = protocol_status;
    record(END_REQUEST, id, &body)
}

pub fn encode_length(out: &mut Vec<u8>, n: usize) {
    if n < 128 {
        out.push(n as u8);
    } else {
        out.extend_from_slice(&((n as u32) | 0x8000_0000).to_be_bytes());
    }
}

pub fn encode_pair(out: &mut Vec<u8>, name: &[u8], value: &[u8]) {
    encode_length(out, name.len());
    encode_length(out, value.len());
    out.extend_from_slice(name);
    out.extend_from_slice(value);
}

fn decode_length(b: &[u8], pos: &mut usize) -> Option<usize> {
    let first = *b.get(*pos)?;
    if first < 128 {
        *pos += 1;
        return Some(first as usize);
    }
    let raw = b.get(*pos..*pos + 4)?;
    *pos += 4;
    Some((u32::from_be_bytes([raw[0], raw[1], raw[2], raw[3]]) & 0x7fff_ffff) as usize)
}

/// Decodes a complete, reassembled PARAMS (or GET_VALUES) payload. A truncated
/// trailing pair is dropped.
pub fn parse_pairs(b: &[u8]) -> Vec<(Vec<u8>, Vec<u8>)> {
    let mut pos = 0;
    let mut out = Vec::new();
    while pos < b.len() {
        let (Some(nl), Some(vl)) = (decode_length(b, &mut pos), decode_length(b, &mut pos)) else {
            break;
        };
        let Some(end) = pos.checked_add(nl).and_then(|x| x.checked_add(vl)) else {
            break;
        };
        if end > b.len() {
            break;
        }
        out.push((b[pos..pos + nl].to_vec(), b[pos + nl..end].to_vec()));
        pos = end;
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn short_and_long_length_forms() {
        let mut v = Vec::new();
        encode_length(&mut v, 127);
        assert_eq!(v, [127]);
        v.clear();
        encode_length(&mut v, 128);
        assert_eq!(v, [0x80, 0, 0, 128]);
        v.clear();
        encode_length(&mut v, 70000);
        assert_eq!(v, (70000u32 | 0x8000_0000).to_be_bytes());
    }

    #[test]
    fn pairs_round_trip_mixed_forms() {
        let long_value = vec![b'x'; 300];
        let mut enc = Vec::new();
        encode_pair(&mut enc, b"A", b"1");
        encode_pair(&mut enc, b"LONG", &long_value);
        encode_pair(&mut enc, &vec![b'n'; 200], b"");
        encode_pair(&mut enc, b"EMPTY", b"");
        let got = parse_pairs(&enc);
        assert_eq!(got.len(), 4);
        assert_eq!(got[0], (b"A".to_vec(), b"1".to_vec()));
        assert_eq!(got[1].1, long_value);
        assert_eq!(got[2].0.len(), 200);
        assert_eq!(got[3], (b"EMPTY".to_vec(), vec![]));
    }

    #[test]
    fn truncated_pair_is_dropped() {
        let mut enc = Vec::new();
        encode_pair(&mut enc, b"A", b"1");
        encode_pair(&mut enc, b"B", b"22");
        enc.truncate(enc.len() - 1);
        assert_eq!(parse_pairs(&enc).len(), 1);
    }

    #[test]
    fn params_split_across_records_reassemble() {
        let mut enc = Vec::new();
        encode_pair(&mut enc, b"SCRIPT_NAME", b"/dump.php");
        let (a, b) = enc.split_at(5);
        let mut joined = Vec::new();
        joined.extend_from_slice(a);
        joined.extend_from_slice(b);
        assert_eq!(parse_pairs(&joined)[0].0, b"SCRIPT_NAME");
    }

    #[test]
    fn record_padding_is_multiple_of_eight() {
        for n in 0..40usize {
            let r = record(STDOUT, 1, &vec![7u8; n]);
            assert_eq!(r.len() % 8, 0, "len {n}");
            let h = parse_header(r[..8].try_into().unwrap());
            assert_eq!(h.content_len, n);
            assert_eq!(h.padding_len, (8 - n % 8) % 8);
            assert_eq!(r.len(), 8 + n + h.padding_len);
            assert_eq!(h.version, 1);
            assert_eq!(h.id, 1);
        }
    }

    #[test]
    fn stream_splits_at_65535() {
        let data = vec![1u8; 65535 * 2 + 10];
        let out = stream_records(STDOUT, 3, &data);
        let mut pos = 0;
        let mut sizes = Vec::new();
        while pos < out.len() {
            let h = parse_header(out[pos..pos + 8].try_into().unwrap());
            sizes.push(h.content_len);
            pos += 8 + h.content_len + h.padding_len;
        }
        assert_eq!(sizes, [65535, 65535, 10]);
        assert_eq!(pos, out.len());
    }

    #[test]
    fn empty_stream_has_no_records() {
        assert!(stream_records(STDOUT, 1, b"").is_empty());
    }

    #[test]
    fn end_request_layout() {
        let r = end_request(9, 0x01020304, REQUEST_COMPLETE);
        assert_eq!(r.len(), 16);
        assert_eq!(r[1], END_REQUEST);
        assert_eq!(&r[8..12], &[1, 2, 3, 4]);
        assert_eq!(r[12], REQUEST_COMPLETE);
    }
}
