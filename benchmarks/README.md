# Benchmarks compare fcgi-go with nginx in front of the same php-fpm

These benchmarks measure what fcgi-go costs compared with nginx when both pass requests to php-fpm.
The workloads cover small and I/O-bound scripts, a typical application page, multipart uploads, large downloads, and a mix.
Each runs at several client counts.

## The setup gives both proxies the same resources

[testcontainers](https://golang.testcontainers.org) starts three containers on one Docker network:

| Container | Image | CPUs | Role |
| --- | --- | ---: | --- |
| `fpm` | `php:8.5-fpm`, [php/](php/); the results below ran on 8.3 | 8 | 16 static children, opcache on |
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

Run on 2026-10-17, fcgi-go `2301cb5`. Host: darwin/arm64, 16 CPUs. Docker: docker-desktop 29.8.0, Docker Desktop, 16 CPUs, 24 GiB. Each cell ran 10s after a 2s warm-up.

### hello

A script that prints 3 bytes: the cost of the proxy itself.

| Clients | Proxy | req/s | MiB/s | p50 | p99 | max | proxy CPU per req | proxy peak memory | errors |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | nginx | 4560 | 0.0 | 214 µs | 331 µs | 3.6 ms | 66 µs | 9 MiB | 0 |
| 1 | fcgi-go | 3516 | 0.0 | 278 µs | 427 µs | 6.1 ms | 137 µs | 14 MiB | 0 |
| 16 | nginx | 19842 | 0.1 | 694 µs | 1.5 ms | 9.0 ms | 100 µs | 10 MiB | 0 |
| 16 | fcgi-go | 19937 | 0.1 | 774 µs | 1.5 ms | 14.8 ms | 99 µs | 19 MiB | 0 |
| 64 | nginx | 25620 | 0.1 | 2.0 ms | 5.7 ms | 25.1 ms | 77 µs | 10 MiB | 0 |
| 64 | fcgi-go | 24464 | 0.1 | 2.5 ms | 5.1 ms | 37.3 ms | 80 µs | 39 MiB | 0 |
| 256 | nginx | 23735 | 0.1 | 10.0 ms | 14.5 ms | 63.8 ms | 83 µs | 14 MiB | 0 |
| 256 | fcgi-go | 26685 | 0.1 | 9.4 ms | 13.4 ms | 34.5 ms | 75 µs | 61 MiB | 0 |

### io-20ms

A script that waits 20 ms on I/O; 16 php-fpm children cap it at 800 req/s.

| Clients | Proxy | req/s | MiB/s | p50 | p99 | max | proxy CPU per req | proxy peak memory | errors |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | nginx | 46 | 0.0 | 21.4 ms | 28.2 ms | 30.2 ms | 251 µs | 11 MiB | 0 |
| 1 | fcgi-go | 46 | 0.0 | 21.5 ms | 27.3 ms | 27.8 ms | 455 µs | 59 MiB | 0 |
| 16 | nginx | 748 | 0.0 | 21.1 ms | 24.7 ms | 28.0 ms | 133 µs | 12 MiB | 0 |
| 16 | fcgi-go | 751 | 0.0 | 21.1 ms | 24.4 ms | 30.9 ms | 185 µs | 60 MiB | 0 |
| 64 | nginx | 778 | 0.0 | 81.9 ms | 86.2 ms | 96.9 ms | 116 µs | 11 MiB | 0 |
| 64 | fcgi-go | 777 | 0.0 | 82.1 ms | 85.8 ms | 90.6 ms | 184 µs | 62 MiB | 0 |
| 256 | nginx | 781 | 0.0 | 326.7 ms | 334.3 ms | 354.2 ms | 87 µs | 14 MiB | 0 |
| 256 | fcgi-go | 777 | 0.0 | 328.2 ms | 334.2 ms | 339.8 ms | 175 µs | 81 MiB | 0 |

### page

A session, a 5 ms simulated query, 400 rows sorted and rendered to 60 KiB of HTML.

| Clients | Proxy | req/s | MiB/s | p50 | p99 | max | proxy CPU per req | proxy peak memory | errors |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | nginx | 145 | 7.5 | 6.7 ms | 8.8 ms | 9.6 ms | 286 µs | 10 MiB | 0 |
| 1 | fcgi-go | 143 | 7.4 | 6.8 ms | 9.2 ms | 9.9 ms | 364 µs | 82 MiB | 0 |
| 16 | nginx | 2469 | 127.9 | 6.2 ms | 10.2 ms | 14.1 ms | 192 µs | 11 MiB | 0 |
| 16 | fcgi-go | 2537 | 131.5 | 6.2 ms | 7.4 ms | 13.8 ms | 252 µs | 55 MiB | 0 |
| 64 | nginx | 2753 | 142.7 | 23.1 ms | 25.3 ms | 30.6 ms | 203 µs | 11 MiB | 0 |
| 64 | fcgi-go | 2795 | 144.8 | 22.8 ms | 24.8 ms | 37.9 ms | 226 µs | 68 MiB | 0 |
| 256 | nginx | 2748 | 142.4 | 92.6 ms | 96.0 ms | 138.8 ms | 186 µs | 13 MiB | 29 (Get "http://localhost:55072/page.php": dial tcp [::1]:55072: connect: connection reset by peer) |
| 256 | fcgi-go | 2795 | 144.8 | 91.4 ms | 93.9 ms | 112.3 ms | 230 µs | 91 MiB | 0 |

### upload-1m

A 1 MiB multipart upload that PHP stores and hashes.

| Clients | Proxy | req/s | MiB/s | p50 | p99 | max | proxy CPU per req | proxy peak memory | errors |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | nginx | 89 | 88.9 | 11.2 ms | 13.5 ms | 20.7 ms | 3.0 ms | 12 MiB | 0 |
| 1 | fcgi-go | 88 | 88.4 | 11.2 ms | 13.6 ms | 20.9 ms | 2.8 ms | 88 MiB | 0 |
| 16 | nginx | 347 | 346.9 | 33.8 ms | 189.7 ms | 2.52 s | 3.7 ms | 15 MiB | 0 |
| 16 | fcgi-go | 354 | 353.8 | 36.5 ms | 232.4 ms | 4.10 s | 2.5 ms | 55 MiB | 0 |
| 64 | nginx | 332 | 332.4 | 143.5 ms | 812.2 ms | 21.42 s | 2.9 ms | 38 MiB | 0 |
| 64 | fcgi-go | 344 | 344.2 | 101.9 ms | 1.66 s | 39.67 s | 2.9 ms | 67 MiB | 0 |
| 256 | nginx | 332 | 331.8 | 524.8 ms | 3.71 s | 22.16 s | 2.6 ms | 23 MiB | 0 |
| 256 | fcgi-go | 336 | 336.1 | 536.0 ms | 3.19 s | 9.28 s | 2.9 ms | 94 MiB | 0 |

### upload-16m

A 16 MiB multipart upload that PHP stores and hashes.
Each cell ran 30s.

| Clients | Proxy | req/s | MiB/s | p50 | p99 | max | proxy CPU per req | proxy peak memory | errors |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | nginx | 6 | 90.7 | 175.9 ms | 198.9 ms | 199.8 ms | 49.3 ms | 19 MiB | 0 |
| 1 | fcgi-go | 6 | 90.7 | 176.1 ms | 197.9 ms | 198.0 ms | 45.7 ms | 78 MiB | 0 |
| 4 | nginx | 15 | 239.5 | 242.1 ms | 475.6 ms | 554.1 ms | 63.3 ms | 44 MiB | 0 |
| 4 | fcgi-go | 16 | 262.4 | 239.1 ms | 421.7 ms | 570.1 ms | 39.9 ms | 77 MiB | 0 |
| 16 | nginx | 21 | 331.2 | 553.5 ms | 3.02 s | 3.48 s | 54.3 ms | 126 MiB | 0 |
| 16 | fcgi-go | 22 | 359.5 | 522.9 ms | 2.80 s | 5.86 s | 37.1 ms | 62 MiB | 0 |

### download-1m

A 1 MiB generated download.

| Clients | Proxy | req/s | MiB/s | p50 | p99 | max | proxy CPU per req | proxy peak memory | errors |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | nginx | 188 | 187.7 | 5.3 ms | 6.2 ms | 13.3 ms | 863 µs | 13 MiB | 0 |
| 1 | fcgi-go | 180 | 180.1 | 5.5 ms | 6.5 ms | 13.7 ms | 889 µs | 56 MiB | 0 |
| 16 | nginx | 209 | 209.3 | 72.4 ms | 143.6 ms | 710.1 ms | 581 µs | 26 MiB | 0 |
| 16 | fcgi-go | 211 | 211.0 | 75.4 ms | 136.0 ms | 200.1 ms | 827 µs | 72 MiB | 0 |
| 64 | nginx | 210 | 210.0 | 254.2 ms | 926.4 ms | 1.99 s | 435 µs | 53 MiB | 0 |
| 64 | fcgi-go | 214 | 213.6 | 240.3 ms | 1.90 s | 3.98 s | 807 µs | 125 MiB | 0 |
| 256 | nginx | 192 | 191.7 | 1.21 s | 2.49 s | 3.55 s | 424 µs | 195 MiB | 0 |
| 256 | fcgi-go | 197 | 196.9 | 1.25 s | 3.60 s | 5.25 s | 859 µs | 288 MiB | 0 |

### download-64m

A 64 MiB generated download.
Each cell ran 30s.

| Clients | Proxy | req/s | MiB/s | p50 | p99 | max | proxy CPU per req | proxy peak memory | errors |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | nginx | 3 | 217.6 | 291.1 ms | 324.8 ms | 330.6 ms | 71.5 ms | 16 MiB | 0 |
| 1 | fcgi-go | 3 | 206.9 | 308.5 ms | 330.5 ms | 330.5 ms | 34.3 ms | 76 MiB | 0 |
| 4 | nginx | 3 | 213.3 | 1.17 s | 1.65 s | 1.95 s | 54.9 ms | 32 MiB | 0 |
| 4 | fcgi-go | 3 | 213.3 | 1.18 s | 1.25 s | 1.27 s | 39.2 ms | 85 MiB | 0 |
| 16 | nginx | 3 | 194.1 | 4.01 s | 13.69 s | 17.68 s | 46.7 ms | 96 MiB | 0 |
| 16 | fcgi-go | 3 | 198.4 | 4.72 s | 7.46 s | 9.07 s | 40.1 ms | 152 MiB | 0 |

### mixed

70 % page, 20 % io-20ms, 5 % upload-1m, 5 % download-1m.

| Clients | Proxy | req/s | MiB/s | p50 | p99 | max | proxy CPU per req | proxy peak memory | errors |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | nginx | 101 | 15.9 | 6.7 ms | 24.0 ms | 29.7 ms | 431 µs | 11 MiB | 0 |
| 1 | fcgi-go | 102 | 16.1 | 6.7 ms | 23.8 ms | 29.7 ms | 503 µs | 67 MiB | 0 |
| 16 | nginx | 1458 | 193.0 | 7.6 ms | 26.2 ms | 45.0 ms | 383 µs | 13 MiB | 0 |
| 16 | fcgi-go | 1498 | 198.2 | 7.4 ms | 24.2 ms | 61.7 ms | 403 µs | 61 MiB | 0 |
| 64 | nginx | 1494 | 197.2 | 40.9 ms | 85.9 ms | 25.71 s | 541 µs | 18 MiB | 0 |
| 64 | fcgi-go | 1878 | 245.6 | 31.4 ms | 66.0 ms | 350.3 ms | 368 µs | 83 MiB | 0 |
| 256 | nginx | 1667 | 218.1 | 151.2 ms | 198.3 ms | 5.50 s | 480 µs | 32 MiB | 0 |
| 256 | fcgi-go | 1861 | 244.4 | 135.3 ms | 174.6 ms | 4.84 s | 379 µs | 111 MiB | 0 |

<!-- results:end -->
