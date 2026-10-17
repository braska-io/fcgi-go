# Benchmarks compare fcgi-go with nginx in front of the same php-fpm

These benchmarks measure what fcgi-go costs compared with nginx when both pass requests to php-fpm.
The workloads cover small and I/O-bound scripts, a typical application page, multipart uploads, large downloads, and a mix.
Each runs at several client counts.

## The setup gives both proxies the same resources

[testcontainers](https://golang.testcontainers.org) starts three containers on one Docker network:

| Container | Image | CPUs | Role |
| --- | --- | ---: | --- |
| `fpm` | `php:8.3-fpm`, [php/](php/) | 8 | 16 static children, opcache on |
| `nginx` | `nginx:1.29`, [nginx/nginx.conf](nginx/nginx.conf) | 2 | 2 workers, defaults otherwise |
| `gateway` | [gateway/](gateway/) | 2 | fcgi-go with `WithMaxConns(64)` |

Both proxies open a new connection to php-fpm per request and spool request and response bodies.
The load generator in [cmd/bench](cmd/bench/main.go) runs on the host.
It reaches each proxy through its mapped port, so both pay the same port mapping.
Clients use HTTP/1.1 with keep-alive; one client sends one request at a time.
Each cell warms up and measures one proxy, then the other.
The proxy that goes first alternates per cell, so drift on the host spreads over both.
Each client keeps its cookies, so the page workload reuses one PHP session per client.

The columns:

- **req/s** and **MiB/s**: requests completed within the measured window, and their bytes in both directions, per second.
  A request still running when the window ends counts only in the latency columns.
- **p50**, **p99**, **max**: latency of one request as the client sees it.
- **proxy CPU per req**: CPU time of the proxy container divided by completed requests.
- **proxy peak memory**: highest memory use of the proxy container without the page cache.
  Spool files on disk therefore do not count.
- **errors**: requests with a transport error, a status other than 200, or a wrong body.

## Run the benchmarks

You need Go 1.27, Docker, and [Task](https://taskfile.dev).

```sh
task bench
```

A full run takes about 20 minutes and rewrites the results below.
Pass flags after `--` to shorten it, for example `task bench -- -only hello,page -duration 5s`.

Numbers from Docker Desktop include its virtual machine and say little about absolute throughput on Linux.
Compare the two proxies within one run, not runs on different machines.
Uploads and the mixed workload write to disk inside the virtual machine.
There, single requests of either proxy sometimes stall for seconds.
Those rows vary between runs by up to a factor of two.

## Results

<!-- results:start -->

Run on 2026-10-17, fcgi-go `db5ddc2`. Host: darwin/arm64, 16 CPUs. Docker: docker-desktop 29.8.0, Docker Desktop, 16 CPUs, 24 GiB. Each cell ran 10s after a 2s warm-up.

### hello

A script that prints 3 bytes: the cost of the proxy itself.

| Clients | Proxy | req/s | MiB/s | p50 | p99 | max | proxy CPU per req | proxy peak memory | errors |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | nginx | 4512 | 0.0 | 215 µs | 336 µs | 5.3 ms | 66 µs | 9 MiB | 0 |
| 1 | fcgi-go | 3535 | 0.0 | 275 µs | 427 µs | 3.5 ms | 137 µs | 12 MiB | 0 |
| 16 | nginx | 20349 | 0.1 | 793 µs | 1.2 ms | 5.0 ms | 96 µs | 10 MiB | 0 |
| 16 | fcgi-go | 19808 | 0.1 | 772 µs | 1.6 ms | 29.4 ms | 99 µs | 19 MiB | 0 |
| 64 | nginx | 25776 | 0.1 | 2.0 ms | 5.7 ms | 9.3 ms | 77 µs | 11 MiB | 0 |
| 64 | fcgi-go | 24480 | 0.1 | 2.4 ms | 5.2 ms | 107.2 ms | 78 µs | 39 MiB | 0 |
| 256 | nginx | 23758 | 0.1 | 10.2 ms | 14.5 ms | 66.8 ms | 84 µs | 14 MiB | 0 |
| 256 | fcgi-go | 26654 | 0.1 | 9.4 ms | 12.5 ms | 60.7 ms | 74 µs | 61 MiB | 0 |

### io-20ms

A script that waits 20 ms on I/O; 16 php-fpm children cap it at 800 req/s.

| Clients | Proxy | req/s | MiB/s | p50 | p99 | max | proxy CPU per req | proxy peak memory | errors |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | nginx | 46 | 0.0 | 21.4 ms | 27.3 ms | 29.3 ms | 227 µs | 11 MiB | 0 |
| 1 | fcgi-go | 46 | 0.0 | 21.5 ms | 27.4 ms | 28.4 ms | 472 µs | 59 MiB | 0 |
| 16 | nginx | 753 | 0.0 | 21.1 ms | 24.1 ms | 28.5 ms | 123 µs | 11 MiB | 0 |
| 16 | fcgi-go | 751 | 0.0 | 21.2 ms | 23.5 ms | 27.0 ms | 172 µs | 60 MiB | 0 |
| 64 | nginx | 780 | 0.0 | 81.7 ms | 85.2 ms | 90.7 ms | 101 µs | 12 MiB | 0 |
| 64 | fcgi-go | 779 | 0.0 | 82.0 ms | 84.6 ms | 90.7 ms | 174 µs | 64 MiB | 0 |
| 256 | nginx | 780 | 0.0 | 327.4 ms | 334.1 ms | 354.9 ms | 107 µs | 14 MiB | 0 |
| 256 | fcgi-go | 779 | 0.0 | 327.6 ms | 334.6 ms | 356.6 ms | 194 µs | 84 MiB | 0 |

### page

A session, a 5 ms simulated query, 400 rows sorted and rendered to 60 KiB of HTML.

| Clients | Proxy | req/s | MiB/s | p50 | p99 | max | proxy CPU per req | proxy peak memory | errors |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | nginx | 145 | 7.5 | 6.8 ms | 9.1 ms | 11.6 ms | 284 µs | 10 MiB | 0 |
| 1 | fcgi-go | 141 | 7.3 | 6.9 ms | 10.4 ms | 17.3 ms | 352 µs | 81 MiB | 0 |
| 16 | nginx | 2050 | 106.2 | 6.5 ms | 23.3 ms | 85.6 ms | 182 µs | 11 MiB | 0 |
| 16 | fcgi-go | 1603 | 83.1 | 6.6 ms | 69.0 ms | 156.3 ms | 249 µs | 49 MiB | 0 |
| 64 | nginx | 1448 | 75.0 | 41.3 ms | 129.0 ms | 275.9 ms | 179 µs | 11 MiB | 0 |
| 64 | fcgi-go | 1212 | 62.8 | 49.7 ms | 188.5 ms | 402.4 ms | 236 µs | 67 MiB | 0 |
| 256 | nginx | 987 | 51.1 | 238.6 ms | 490.6 ms | 761.6 ms | 182 µs | 14 MiB | 0 |
| 256 | fcgi-go | 1123 | 58.2 | 221.5 ms | 453.1 ms | 818.1 ms | 229 µs | 89 MiB | 0 |

### upload-1m

A 1 MiB multipart upload that PHP stores and hashes.

| Clients | Proxy | req/s | MiB/s | p50 | p99 | max | proxy CPU per req | proxy peak memory | errors |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | nginx | 88 | 88.5 | 11.2 ms | 13.7 ms | 22.7 ms | 2.4 ms | 11 MiB | 0 |
| 1 | fcgi-go | 88 | 88.5 | 11.2 ms | 13.6 ms | 20.6 ms | 2.8 ms | 88 MiB | 0 |
| 16 | nginx | 135 | 134.7 | 29.7 ms | 254.1 ms | 21.00 s | 4.0 ms | 19 MiB | 0 |
| 16 | fcgi-go | 355 | 355.4 | 35.0 ms | 210.5 ms | 1.84 s | 2.5 ms | 51 MiB | 0 |
| 64 | nginx | 349 | 349.2 | 130.4 ms | 708.2 ms | 4.18 s | 2.5 ms | 28 MiB | 0 |
| 64 | fcgi-go | 262 | 261.9 | 124.7 ms | 785.4 ms | 5.54 s | 3.5 ms | 58 MiB | 0 |
| 256 | nginx | 344 | 344.0 | 585.7 ms | 2.54 s | 3.55 s | 2.7 ms | 19 MiB | 0 |
| 256 | fcgi-go | 342 | 341.9 | 569.8 ms | 2.75 s | 4.27 s | 3.2 ms | 80 MiB | 0 |

### upload-16m

A 16 MiB multipart upload that PHP stores and hashes.
Each cell ran 30s.

| Clients | Proxy | req/s | MiB/s | p50 | p99 | max | proxy CPU per req | proxy peak memory | errors |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | nginx | 6 | 90.9 | 176.5 ms | 201.4 ms | 207.0 ms | 52.3 ms | 14 MiB | 0 |
| 1 | fcgi-go | 6 | 90.3 | 176.7 ms | 197.6 ms | 204.1 ms | 43.5 ms | 72 MiB | 0 |
| 4 | nginx | 11 | 183.8 | 347.6 ms | 534.6 ms | 648.0 ms | 74.4 ms | 47 MiB | 0 |
| 4 | fcgi-go | 17 | 264.5 | 237.2 ms | 418.1 ms | 820.9 ms | 39.9 ms | 72 MiB | 0 |
| 16 | nginx | 22 | 350.9 | 572.0 ms | 3.51 s | 3.88 s | 49.1 ms | 123 MiB | 0 |
| 16 | fcgi-go | 19 | 306.8 | 491.5 ms | 3.41 s | 7.49 s | 38.5 ms | 60 MiB | 0 |

### download-1m

A 1 MiB generated download.

| Clients | Proxy | req/s | MiB/s | p50 | p99 | max | proxy CPU per req | proxy peak memory | errors |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | nginx | 183 | 183.3 | 5.5 ms | 6.4 ms | 7.7 ms | 883 µs | 14 MiB | 0 |
| 1 | fcgi-go | 178 | 178.3 | 5.6 ms | 6.7 ms | 14.5 ms | 932 µs | 54 MiB | 0 |
| 16 | nginx | 203 | 203.4 | 74.5 ms | 181.1 ms | 900.7 ms | 585 µs | 25 MiB | 0 |
| 16 | fcgi-go | 205 | 205.4 | 77.7 ms | 111.3 ms | 158.1 ms | 939 µs | 71 MiB | 0 |
| 64 | nginx | 209 | 208.6 | 268.3 ms | 878.9 ms | 2.20 s | 456 µs | 56 MiB | 0 |
| 64 | fcgi-go | 214 | 214.4 | 227.7 ms | 1.95 s | 3.26 s | 834 µs | 112 MiB | 0 |
| 256 | nginx | 213 | 212.9 | 1.14 s | 2.63 s | 4.93 s | 479 µs | 192 MiB | 0 |
| 256 | fcgi-go | 211 | 211.1 | 1.12 s | 2.98 s | 4.01 s | 827 µs | 282 MiB | 0 |

### download-64m

A 64 MiB generated download.
Each cell ran 30s.

| Clients | Proxy | req/s | MiB/s | p50 | p99 | max | proxy CPU per req | proxy peak memory | errors |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | nginx | 3 | 218.6 | 292.3 ms | 310.6 ms | 316.4 ms | 60.5 ms | 16 MiB | 0 |
| 1 | fcgi-go | 3 | 208.3 | 306.5 ms | 352.8 ms | 352.8 ms | 34.8 ms | 68 MiB | 0 |
| 4 | nginx | 3 | 216.8 | 1.18 s | 1.27 s | 1.58 s | 53.7 ms | 32 MiB | 0 |
| 4 | fcgi-go | 3 | 217.9 | 1.12 s | 2.26 s | 2.32 s | 39.3 ms | 77 MiB | 0 |
| 16 | nginx | 3 | 222.0 | 4.01 s | 11.70 s | 13.08 s | 45.0 ms | 96 MiB | 0 |
| 16 | fcgi-go | 3 | 218.3 | 4.62 s | 7.22 s | 9.47 s | 37.2 ms | 144 MiB | 0 |

### mixed

70 % page, 20 % io-20ms, 5 % upload-1m, 5 % download-1m.

| Clients | Proxy | req/s | MiB/s | p50 | p99 | max | proxy CPU per req | proxy peak memory | errors |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | nginx | 84 | 13.6 | 6.5 ms | 178.3 ms | 193.0 ms | 360 µs | 11 MiB | 0 |
| 1 | fcgi-go | 85 | 13.6 | 6.9 ms | 173.8 ms | 196.4 ms | 517 µs | 57 MiB | 0 |
| 16 | nginx | 879 | 114.4 | 8.4 ms | 114.7 ms | 654.4 ms | 324 µs | 15 MiB | 0 |
| 16 | fcgi-go | 875 | 114.0 | 7.9 ms | 81.1 ms | 509.5 ms | 402 µs | 59 MiB | 0 |
| 64 | nginx | 969 | 128.3 | 45.8 ms | 230.1 ms | 850.0 ms | 336 µs | 17 MiB | 0 |
| 64 | fcgi-go | 778 | 102.1 | 54.6 ms | 361.9 ms | 887.4 ms | 384 µs | 80 MiB | 0 |
| 256 | nginx | 790 | 103.5 | 296.1 ms | 636.7 ms | 1.47 s | 359 µs | 34 MiB | 0 |
| 256 | fcgi-go | 765 | 100.2 | 317.8 ms | 801.7 ms | 1.44 s | 402 µs | 102 MiB | 0 |

<!-- results:end -->
