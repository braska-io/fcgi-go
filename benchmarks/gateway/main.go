// Command gateway serves the benchmark scripts through fcgi-go, configured
// like the nginx it is compared with.
package main

import (
	"log"
	"net/http"
	"path"
	"sync"
	"time"

	fcgi "github.com/braska-io/fcgi-go"
)

func main() {
	gw := fcgi.New("tcp:fpm:9000",
		// As many calls as php-fpm has children; more would only queue there.
		fcgi.WithMaxConns(16),
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
