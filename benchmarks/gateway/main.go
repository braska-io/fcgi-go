// Command gateway serves the benchmark scripts through fcgi-go, configured
// like the nginx it is compared with.
package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"runtime/pprof"
	"strconv"
	"sync"
	"syscall"
	"time"

	fcgi "github.com/braska-io/fcgi-go"
)

func main() {
	if path := os.Getenv("CPU_PROFILE"); path != "" {
		profileUntilStop(path)
	}
	maxConns := 16
	if v, err := strconv.Atoi(os.Getenv("MAX_CONNS")); err == nil {
		maxConns = v
	}
	gw := fcgi.New("tcp:fpm:9000",
		fcgi.WithMaxConns(maxConns),
		fcgi.WithDocumentRoot("/srv/bench"),
		fcgi.WithTimeout(120*time.Second),
		fcgi.WithMaxBody(64<<20),
	)
	var (
		mu      sync.Mutex
		scripts = map[string]http.Handler{}
	)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Base(r.URL.Path)
		if path.Ext(name) != ".php" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		s, ok := scripts[name]
		if !ok {
			s = gw.Script(name)
			scripts[name] = s
		}
		mu.Unlock()
		s.ServeHTTP(w, r)
	})
	log.Fatal(http.ListenAndServe(":8080", h))
}

// profileUntilStop records a CPU profile to path until the container is
// stopped; the bench copies it out of the stopped container.
func profileUntilStop(path string) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		log.Fatal(err)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, os.Interrupt)
	go func() {
		<-stop
		pprof.StopCPUProfile()
		_ = f.Close()
		os.Exit(0)
	}()
}
