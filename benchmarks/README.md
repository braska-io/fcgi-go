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
| `gateway` | [gateway/](gateway/) | 2 | fcgi-go with `WithMaxConns(16)` |

Both proxies open a new connection to php-fpm per request and spool request and response bodies.
The load generator in [cmd/bench](cmd/bench/main.go) runs on the host.
It reaches each proxy through its mapped port, so both pay the same port mapping.
Clients use HTTP/1.1 with keep-alive; one client sends one request at a time.
Each cell warms up, measures, and then switches to the other proxy.
Drift on the host thus spreads over both.

The columns:

- **req/s** and **MiB/s**: completed requests and bytes in both directions per second.
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

## Results

<!-- results:start -->

Run on 2026-10-17, fcgi-go `98438be`. Host: darwin/arm64, 16 CPUs. Docker: docker-desktop 29.8.0, Docker Desktop, 16 CPUs, 24 GiB. Each cell ran 3s after a 1s warm-up.

### hello

A script that prints 3 bytes: the cost of the proxy itself.

| Clients | Proxy | req/s | MiB/s | p50 | p99 | max | proxy CPU per req | proxy peak memory | errors |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | nginx | 4539 | 0.0 | 215 µs | 338 µs | 2.7 ms | 66 µs | 8 MiB | 0 |
| 1 | fcgi-go | 2408 | 0.0 | 401 µs | 671 µs | 2.8 ms | 249 µs | 11 MiB | 0 |
| 16 | nginx | 20100 | 0.1 | 685 µs | 1.5 ms | 5.2 ms | 99 µs | 9 MiB | 0 |
| 16 | fcgi-go | 11901 | 0.0 | 1.3 ms | 2.3 ms | 4.5 ms | 155 µs | 13 MiB | 0 |
| 64 | nginx | 25810 | 0.1 | 1.8 ms | 6.2 ms | 15.3 ms | 74 µs | 10 MiB | 0 |
| 64 | fcgi-go | 9833 | 0.0 | 6.4 ms | 8.8 ms | 17.2 ms | 189 µs | 15 MiB | 0 |
| 256 | nginx | 26668 | 0.1 | 7.8 ms | 16.6 ms | 26.9 ms | 67 µs | 14 MiB | 0 |
| 256 | fcgi-go | 10477 | 0.0 | 24.0 ms | 35.5 ms | 44.6 ms | 185 µs | 28 MiB | 0 |

### upload-1m

A 1 MiB multipart upload that PHP stores and hashes.

| Clients | Proxy | req/s | MiB/s | p50 | p99 | max | proxy CPU per req | proxy peak memory | errors |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | nginx | 89 | 89.0 | 11.1 ms | 13.7 ms | 15.9 ms | 2.8 ms | 12 MiB | 0 |
| 1 | fcgi-go | 86 | 86.3 | 11.5 ms | 13.9 ms | 14.5 ms | 3.6 ms | 19 MiB | 0 |
| 16 | nginx | 346 | 345.8 | 30.9 ms | 261.0 ms | 618.6 ms | 3.0 ms | 16 MiB | 0 |
| 16 | fcgi-go | 358 | 357.8 | 40.6 ms | 105.7 ms | 136.8 ms | 2.9 ms | 38 MiB | 0 |
| 64 | nginx | 351 | 351.5 | 129.0 ms | 789.5 ms | 1.29 s | 2.4 ms | 12 MiB | 0 |
| 64 | fcgi-go | 348 | 348.6 | 121.6 ms | 817.0 ms | 1.29 s | 2.7 ms | 82 MiB | 0 |
| 256 | nginx | 341 | 341.1 | 554.6 ms | 2.28 s | 3.02 s | 2.8 ms | 30 MiB | 0 |
| 256 | fcgi-go | 97 | 97.1 | 517.8 ms | 3.19 s | 10.83 s | 2.9 ms | 283 MiB | 0 |

### download-64m

A 64 MiB generated download.

| Clients | Proxy | req/s | MiB/s | p50 | p99 | max | proxy CPU per req | proxy peak memory | errors |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | nginx | 4 | 225.2 | 285.7 ms | 297.9 ms | 297.9 ms | 58.7 ms | 17 MiB | 0 |
| 1 | fcgi-go | 3 | 207.9 | 306.8 ms | 333.7 ms | 333.7 ms | 40.7 ms | 23 MiB | 0 |
| 16 | nginx | 3 | 220.4 | 4.16 s | 5.52 s | 5.52 s | 54.0 ms | 96 MiB | 0 |
| 16 | fcgi-go | 3 | 213.9 | 5.28 s | 6.28 s | 6.28 s | 51.7 ms | 107 MiB | 0 |
| 64 | nginx | 3 | 215.3 | 17.51 s | 19.02 s | 19.02 s | 72.8 ms | 345 MiB | 0 |
| 64 | fcgi-go | 3 | 220.2 | 17.41 s | 18.90 s | 18.90 s | 52.3 ms | 369 MiB | 0 |

<!-- results:end -->
