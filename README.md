# fcgi-go

fcgi-go is a Go library that passes HTTP requests to a FastCGI responder, such as php-fpm, and returns its response.
You mount one `http.HandlerFunc` per script in your own router.
Routing, rewrites, static files and authentication stay in your code.

## Mount one handler per script

```sh
go get github.com/braska-io/fcgi-go
```

```go
package main

import (
	"log"
	"net/http"
	"time"

	fcgi "github.com/braska-io/fcgi-go"
)

func main() {
	gw := fcgi.New("unix:/run/php/fpm.sock",
		fcgi.WithMaxConns(30),
		fcgi.WithTimeout(120*time.Second),
		fcgi.WithDocumentRoot("/var/www/html"),
		fcgi.WithProtectedHeaders("X-Auth-*"),
		fcgi.WithParam("APP_ENV", "production"),
	)

	mux := http.NewServeMux()
	mux.Handle("/api/", gw.Script("api.php",
		fcgi.WithParams(func(r *http.Request) map[string]string {
			return map[string]string{"HTTP_X_AUTH_SUBJECT": subjectFrom(r)}
		}),
	))
	mux.Handle("/export/", gw.Script("export.php",
		fcgi.WithTimeout(600*time.Second),
		fcgi.WithParam("APP_ENV", "export"),
	))

	log.Fatal(http.ListenAndServe("127.0.0.1:8080", mux))
}

// subjectFrom returns the authenticated user. Your authentication
// middleware decides how; a fixed value keeps the example short.
func subjectFrom(r *http.Request) string { return "anonymous" }
```

The address is `unix:<path>` or `tcp:<host:port>`.
`WithProtectedHeaders("X-Auth-*")` drops client headers that start with `X-Auth-`, so only your code sets `HTTP_X_AUTH_SUBJECT`.

[docs/design.md](docs/design.md) lists every option, its default, and the error statuses.
[compliance/README.md](compliance/README.md) describes the compliance suite and how to run it.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) before you open a pull request.
Report security problems privately, as [SECURITY.md](SECURITY.md) describes.
Everyone in the project follows the [code of conduct](CODE_OF_CONDUCT.md).

## License

fcgi-go is released under the [MIT License](LICENSE).
