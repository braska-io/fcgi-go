// Command bench compares fcgi-go with nginx in front of the same php-fpm and
// writes the results into the benchmarks README.
//
// testcontainers starts php-fpm, nginx and the fcgi-go gateway on one Docker
// network. Both proxies get the same CPU limit; the load comes from this
// process through each proxy's mapped port, so both pay the same port
// mapping.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	proxyCPUs = 2
	fpmCPUs   = 8
)

func main() {
	var (
		duration = flag.Duration("duration", 10*time.Second, "measured time per cell")
		warmup   = flag.Duration("warmup", 2*time.Second, "unmeasured time before each cell")
		only     = flag.String("only", "", "comma-separated workload names to run; empty runs all")
		out      = flag.String("out", "README.md", "README whose results block is replaced")
		rev      = flag.String("rev", "", "fcgi-go revision for the report")
		cpuprof  = flag.String("cpuprofile", "", "write the gateway's CPU profile over the whole run to this file")
		maxConns = flag.Int("max-conns", 64, "the gateway's WithMaxConns")
	)
	flag.Parse()
	if err := run(*duration, *warmup, *only, *out, *rev, *cpuprof, *maxConns); err != nil {
		log.Fatal(err)
	}
}

func run(duration, warmup time.Duration, only, out, rev, cpuprof string, maxConns int) error {
	ctx := context.Background()
	repo, err := filepath.Abs("..")
	if err != nil {
		return err
	}

	nw, err := network.New(ctx)
	if err != nil {
		return fmt.Errorf("network: %w", err)
	}
	defer func() { _ = nw.Remove(ctx) }()

	log.Print("starting php-fpm")
	fpm, err := testcontainers.Run(ctx, "",
		testcontainers.WithDockerfile(testcontainers.FromDockerfile{Context: filepath.Join(repo, "benchmarks/php"), KeepImage: true}),
		network.WithNetwork([]string{"fpm"}, nw),
		testcontainers.WithHostConfigModifier(cpus(fpmCPUs)),
		testcontainers.WithWaitStrategy(wait.ForLog("ready to handle connections")),
	)
	defer terminate(fpm)
	if err != nil {
		return fmt.Errorf("php-fpm: %w", err)
	}

	log.Print("starting nginx")
	nginx, err := testcontainers.Run(ctx, "nginx:1.29",
		testcontainers.WithFiles(testcontainers.ContainerFile{
			HostFilePath: filepath.Join(repo, "benchmarks/nginx/nginx.conf"), ContainerFilePath: "/etc/nginx/nginx.conf", FileMode: 0o644,
		}),
		testcontainers.WithExposedPorts("8080/tcp"),
		network.WithNetwork([]string{"nginx"}, nw),
		testcontainers.WithHostConfigModifier(cpus(proxyCPUs)),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("8080/tcp")),
	)
	defer terminate(nginx)
	if err != nil {
		return fmt.Errorf("nginx: %w", err)
	}

	log.Print("building and starting the fcgi-go gateway")
	gwOpts := []testcontainers.ContainerCustomizer{
		testcontainers.WithDockerfile(testcontainers.FromDockerfile{Context: repo, Dockerfile: "benchmarks/gateway/Dockerfile"}),
		testcontainers.WithExposedPorts("8080/tcp"),
		network.WithNetwork([]string{"gateway"}, nw),
		testcontainers.WithHostConfigModifier(cpus(proxyCPUs)),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("8080/tcp")),
		testcontainers.WithEnv(map[string]string{"MAX_CONNS": strconv.Itoa(maxConns)}),
	}
	if cpuprof != "" {
		gwOpts = append(gwOpts, testcontainers.WithEnv(map[string]string{"CPU_PROFILE": "/tmp/cpu.pprof"}))
	}
	gw, err := testcontainers.Run(ctx, "", gwOpts...)
	defer terminate(gw)
	if err != nil {
		return fmt.Errorf("gateway: %w", err)
	}

	docker, err := client.New(client.FromEnv)
	if err != nil {
		return fmt.Errorf("docker client: %w", err)
	}
	defer docker.Close()

	var proxies []*proxy
	for _, p := range []struct {
		name string
		c    *testcontainers.DockerContainer
	}{{"nginx", nginx}, {"fcgi-go", gw}} {
		ep, err := p.c.PortEndpoint(ctx, "8080/tcp", "http")
		if err != nil {
			return fmt.Errorf("%s endpoint: %w", p.name, err)
		}
		proxies = append(proxies, &proxy{name: p.name, base: ep, id: p.c.GetContainerID()})
	}

	var results []result
	for _, w := range workloads() {
		if only != "" && !slices.Contains(strings.Split(only, ","), w.name) {
			continue
		}
		for _, c := range w.loads {
			// Alternating the proxies per cell spreads drift in the host
			// over both instead of favouring whichever runs first.
			for _, p := range proxies {
				log.Printf("%s c=%d %s", w.name, c, p.name)
				d := duration * time.Duration(max(w.scale, 1))
				_ = load(ctx, p.base, w, c, warmup)
				r, err := measure(ctx, docker, p, w, c, d)
				if err != nil {
					return fmt.Errorf("%s c=%d %s: %w", w.name, c, p.name, err)
				}
				results = append(results, r)
			}
		}
	}

	if cpuprof != "" {
		if err := copyProfile(ctx, gw, cpuprof); err != nil {
			return err
		}
	}

	env, err := environment(ctx, docker, rev, duration, warmup)
	if err != nil {
		return err
	}
	return writeReport(out, env, results)
}

// copyProfile stops the gateway, which makes it write its profile, and
// copies the profile out of the stopped container.
func copyProfile(ctx context.Context, gw *testcontainers.DockerContainer, path string) error {
	timeout := 10 * time.Second
	if err := gw.Stop(ctx, &timeout); err != nil {
		return fmt.Errorf("stop gateway: %w", err)
	}
	rc, err := gw.CopyFileFromContainer(ctx, "/tmp/cpu.pprof")
	if err != nil {
		return fmt.Errorf("copy profile: %w", err)
	}
	defer rc.Close()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, rc); err != nil {
		_ = f.Close()
		return fmt.Errorf("write profile: %w", err)
	}
	log.Printf("gateway CPU profile: %s", path)
	return f.Close()
}

func cpus(n int64) func(*container.HostConfig) {
	return func(hc *container.HostConfig) { hc.NanoCPUs = n * 1e9 }
}

func terminate(c *testcontainers.DockerContainer) {
	if c != nil {
		_ = testcontainers.TerminateContainer(c)
	}
}

type proxy struct {
	name string
	base string
	id   string
}

// workload builds one request per call. The bodies are built once, so the
// generator spends its time on the network, not on producing data.
type workload struct {
	name    string
	summary string
	loads   []int
	// scale multiplies the cell duration, so cells whose requests take
	// seconds still complete many of them.
	scale int
	next  func(base string, rnd *rand.Rand) (*http.Request, error)
	// check sees the first bytes of the response body and its full length.
	check func(head []byte, n int64) error
}

func workloads() []workload {
	small := []int{1, 16, 64, 256}
	large := []int{1, 4, 16}
	get := func(path string) func(string, *rand.Rand) (*http.Request, error) {
		return func(base string, _ *rand.Rand) (*http.Request, error) {
			return http.NewRequest(http.MethodGet, base+path, nil)
		}
	}
	upload := func(size int) func(string, *rand.Rand) (*http.Request, error) {
		body, ctype := multipartFile(size)
		return func(base string, _ *rand.Rand) (*http.Request, error) {
			req, err := http.NewRequest(http.MethodPost, base+"/upload.php", bytes.NewReader(body))
			if err == nil {
				req.Header.Set("Content-Type", ctype)
			}
			return req, err
		}
	}
	page := get("/page.php")
	io20 := get("/io.php?ms=20")
	up1 := upload(1 << 20)
	down1 := get("/download.php?bytes=1048576")

	return []workload{
		{name: "hello", summary: "a script that prints 3 bytes: the cost of the proxy itself", loads: small, next: get("/hello.php")},
		{name: "io-20ms", summary: "a script that waits 20 ms on I/O; 16 php-fpm children cap it at 800 req/s", loads: small, next: io20},
		{name: "page", summary: "a session, a 5 ms simulated query, 400 rows sorted and rendered to 60 KiB of HTML", loads: small, next: page},
		{name: "upload-1m", summary: "a 1 MiB multipart upload that PHP stores and hashes", loads: small, next: up1, check: uploadCheck(1 << 20)},
		{name: "upload-16m", summary: "a 16 MiB multipart upload that PHP stores and hashes", loads: large, scale: 3, next: upload(16 << 20), check: uploadCheck(16 << 20)},
		{name: "download-1m", summary: "a 1 MiB generated download", loads: small, next: down1, check: lengthCheck(1 << 20)},
		{name: "download-64m", summary: "a 64 MiB generated download", loads: large, scale: 3, next: get("/download.php?bytes=67108864"), check: lengthCheck(64 << 20)},
		{
			name: "mixed", summary: "70 % page, 20 % io-20ms, 5 % upload-1m, 5 % download-1m", loads: small,
			next: func(base string, rnd *rand.Rand) (*http.Request, error) {
				switch n := rnd.IntN(100); {
				case n < 70:
					return page(base, rnd)
				case n < 90:
					return io20(base, rnd)
				case n < 95:
					return up1(base, rnd)
				default:
					return down1(base, rnd)
				}
			},
		},
	}
}

func multipartFile(size int) ([]byte, string) {
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	fw, _ := mw.CreateFormFile("file", "upload.bin") // writes to a bytes.Buffer cannot fail
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i)
	}
	_, _ = fw.Write(data)
	_ = mw.Close()
	return b.Bytes(), mw.FormDataContentType()
}

func uploadCheck(size int) func([]byte, int64) error {
	want := strconv.Itoa(size) + " "
	return func(head []byte, _ int64) error {
		if !strings.HasPrefix(string(head), want) {
			return fmt.Errorf("upload: want size %d, got %q", size, head)
		}
		return nil
	}
}

func lengthCheck(size int64) func([]byte, int64) error {
	return func(_ []byte, n int64) error {
		if n != size {
			return fmt.Errorf("download: want %d bytes, got %d", size, n)
		}
		return nil
	}
}

type result struct {
	workload string
	summary  string
	load     int
	proxy    string
	requests int
	errors   int
	firstErr string
	elapsed  time.Duration
	bytes    int64
	lat      []time.Duration
	cpu      time.Duration
	peakMem  uint64
	window   time.Duration
}

func (r result) rps() float64 { return float64(r.requests) / r.elapsed.Seconds() }

func (r result) pct(p float64) time.Duration {
	if len(r.lat) == 0 {
		return 0
	}
	return r.lat[min(len(r.lat)-1, int(p*float64(len(r.lat))))]
}

// measure runs one cell and samples the proxy container's CPU and memory.
func measure(ctx context.Context, docker *client.Client, p *proxy, w workload, c int, d time.Duration) (result, error) {
	sctx, stop := context.WithCancel(ctx)
	samples := make(chan sample, 1)
	go func() { samples <- sampleStats(sctx, docker, p.id) }()
	time.Sleep(1500 * time.Millisecond) // the first stats sample arrives after about a second

	r := load(ctx, p.base, w, c, d)
	time.Sleep(1500 * time.Millisecond)
	stop()
	s := <-samples
	if s.err != nil {
		return r, s.err
	}
	r.proxy, r.cpu, r.peakMem, r.window = p.name, s.cpu, s.peakMem, d
	return r, nil
}

// load runs c clients against base for d and collects every latency.
func load(ctx context.Context, base string, w workload, c int, d time.Duration) result {
	tr := &http.Transport{MaxConnsPerHost: c, MaxIdleConnsPerHost: c, DisableCompression: true}
	defer tr.CloseIdleConnections()
	hc := &http.Client{Transport: tr, Timeout: 150 * time.Second}

	var (
		mu       sync.Mutex
		lat      []time.Duration
		errs     atomic.Int64
		total    atomic.Int64
		firstErr atomic.Value
		wg       sync.WaitGroup
	)
	deadline := time.Now().Add(d)
	start := time.Now()
	for i := range c {
		wg.Go(func() {
			rnd := rand.New(rand.NewPCG(uint64(i), 1)) //nolint:gosec // workload mix, not security
			var mine []time.Duration
			for time.Now().Before(deadline) && ctx.Err() == nil {
				t0 := time.Now()
				n, err := once(hc, base, w, rnd)
				if err != nil {
					errs.Add(1)
					firstErr.CompareAndSwap(nil, err.Error())
					continue
				}
				total.Add(n)
				mine = append(mine, time.Since(t0))
			}
			mu.Lock()
			lat = append(lat, mine...)
			mu.Unlock()
		})
	}
	wg.Wait()
	slices.Sort(lat)
	fe, _ := firstErr.Load().(string)
	return result{
		workload: w.name, summary: w.summary, load: c,
		requests: len(lat), errors: int(errs.Load()), firstErr: fe,
		elapsed: time.Since(start), bytes: total.Load(), lat: lat,
	}
}

// once sends one request and returns the bytes moved in both directions.
func once(hc *http.Client, base string, w workload, rnd *rand.Rand) (int64, error) {
	req, err := w.next(base, rnd)
	if err != nil {
		return 0, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	head := &headWriter{}
	n, err := io.Copy(head, resp.Body)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status %d", resp.StatusCode)
	}
	if w.check != nil {
		if err := w.check(head.b, n); err != nil {
			return 0, err
		}
	}
	return n + max(req.ContentLength, 0), nil
}

// headWriter keeps the first 256 bytes written to it and drops the rest.
type headWriter struct{ b []byte }

func (h *headWriter) Write(p []byte) (int, error) {
	if room := 256 - len(h.b); room > 0 {
		h.b = append(h.b, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

type sample struct {
	cpu     time.Duration
	peakMem uint64
	err     error
}

// sampleStats streams docker stats until ctx ends. CPU is the difference
// between the first and the last sample; memory excludes the page cache,
// as docker stats does, so spool files on disk do not count.
func sampleStats(ctx context.Context, docker *client.Client, id string) sample {
	res, err := docker.ContainerStats(ctx, id, client.ContainerStatsOptions{Stream: true})
	if err != nil {
		return sample{err: fmt.Errorf("stats: %w", err)}
	}
	defer res.Body.Close()
	dec := json.NewDecoder(res.Body)
	var first, last uint64
	var s sample
	for {
		var st container.StatsResponse
		if err := dec.Decode(&st); err != nil {
			if ctx.Err() != nil || errors.Is(err, io.EOF) {
				break
			}
			return sample{err: fmt.Errorf("stats decode: %w", err)}
		}
		if first == 0 {
			first = st.CPUStats.CPUUsage.TotalUsage
		}
		last = st.CPUStats.CPUUsage.TotalUsage
		s.peakMem = max(s.peakMem, st.MemoryStats.Usage-st.MemoryStats.Stats["inactive_file"])
	}
	s.cpu = time.Duration(last - first) //nolint:gosec // nanoseconds of CPU time
	return s
}

type env struct {
	date, host, docker, rev string
	duration, warmup        time.Duration
}

func environment(ctx context.Context, docker *client.Client, rev string, duration, warmup time.Duration) (env, error) {
	info, err := docker.Info(ctx, client.InfoOptions{})
	if err != nil {
		return env{}, fmt.Errorf("docker info: %w", err)
	}
	i := info.Info
	return env{
		date:     time.Now().UTC().Format(time.DateOnly),
		host:     fmt.Sprintf("%s/%s, %d CPUs", runtime.GOOS, runtime.GOARCH, runtime.NumCPU()),
		docker:   fmt.Sprintf("%s %s, %s, %d CPUs, %.0f GiB", i.Name, i.ServerVersion, i.OperatingSystem, i.NCPU, float64(i.MemTotal)/(1<<30)),
		rev:      rev,
		duration: duration, warmup: warmup,
	}, nil
}

const (
	startMarker = "<!-- results:start -->"
	endMarker   = "<!-- results:end -->"
)

func writeReport(path string, e env, results []result) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	s := string(b)
	i, j := strings.Index(s, startMarker), strings.Index(s, endMarker)
	if i < 0 || j < i {
		return fmt.Errorf("%s: results markers missing", path)
	}
	var r strings.Builder
	r.WriteString(startMarker + "\n\n")
	fmt.Fprintf(&r, "Run on %s, fcgi-go `%s`. Host: %s. Docker: %s. Each cell ran %s after a %s warm-up.\n",
		e.date, e.rev, e.host, e.docker, e.duration, e.warmup)

	byWorkload := map[string][]result{}
	var order []string
	for _, x := range results {
		if _, ok := byWorkload[x.workload]; !ok {
			order = append(order, x.workload)
		}
		byWorkload[x.workload] = append(byWorkload[x.workload], x)
	}
	for _, name := range order {
		rs := byWorkload[name]
		fmt.Fprintf(&r, "\n### %s\n\n%s.\n", name, capitalize(rs[0].summary))
		if rs[0].window != e.duration {
			fmt.Fprintf(&r, "Each cell ran %s.\n", rs[0].window)
		}
		r.WriteString("\n")
		r.WriteString("| Clients | Proxy | req/s | MiB/s | p50 | p99 | max | proxy CPU per req | proxy peak memory | errors |\n")
		r.WriteString("| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
		for _, x := range rs {
			cpuPerReq := "n/a"
			if x.requests > 0 {
				cpuPerReq = fmtDur(x.cpu / time.Duration(x.requests))
			}
			errs := strconv.Itoa(x.errors)
			if x.firstErr != "" {
				errs += " (" + x.firstErr + ")"
			}
			fmt.Fprintf(&r, "| %d | %s | %.0f | %.1f | %s | %s | %s | %s | %.0f MiB | %s |\n",
				x.load, x.proxy, x.rps(), float64(x.bytes)/(1<<20)/x.elapsed.Seconds(),
				fmtDur(x.pct(0.50)), fmtDur(x.pct(0.99)), fmtDur(x.pct(1)), cpuPerReq,
				float64(x.peakMem)/(1<<20), errs)
		}
	}
	r.WriteString("\n")
	s = s[:i] + r.String() + s[j:]
	return os.WriteFile(path, []byte(s), 0o644) //nolint:gosec // a README
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func fmtDur(d time.Duration) string {
	switch {
	case d >= time.Second:
		return fmt.Sprintf("%.2f s", d.Seconds())
	case d >= time.Millisecond:
		return fmt.Sprintf("%.1f ms", float64(d)/float64(time.Millisecond))
	default:
		return fmt.Sprintf("%.0f µs", float64(d)/float64(time.Microsecond))
	}
}
