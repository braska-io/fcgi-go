package compliance_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/braska-io/fcgi-go"
	"github.com/braska-io/fcgi-go/compliance"
)

// fcgiTarget wires the library under test with the configuration every case
// assumes (README, "The suite configures the handler the same way").
func fcgiTarget(spool string) compliance.Target {
	return func(r *compliance.Responder) func(compliance.Route) http.Handler {
		gw := fcgi.New(r.Address,
			fcgi.WithDocumentRoot(r.DocumentRoot),
			fcgi.WithMaxConns(4),
			fcgi.WithSpool(spool, 1<<20),
			fcgi.WithProtectedHeaders("X-Auth-Subject", "X-Auth-*"),
			fcgi.WithDropResponseHeaders("X-Powered-By"),
			fcgi.WithTimeout(5*time.Second),
			fcgi.WithMaxBody(10<<20),
		)
		return func(rt compliance.Route) http.Handler {
			var opts []fcgi.ScriptOption
			if rt.PathInfo != "" {
				pi := rt.PathInfo
				opts = append(opts, fcgi.WithPathInfo(func(*http.Request) string { return pi }))
			}
			if rt.QueryString != nil {
				q := *rt.QueryString
				opts = append(opts, fcgi.WithQuery(func(*http.Request) string { return q }))
			}
			if len(rt.Params) > 0 {
				p := rt.Params
				opts = append(opts, fcgi.WithParams(func(*http.Request) map[string]string { return p }))
			}
			if rt.TimeoutMS > 0 {
				opts = append(opts, fcgi.WithTimeout(time.Duration(rt.TimeoutMS)*time.Millisecond))
			}
			return gw.Script(r.Script(rt.Script), opts...)
		}
	}
}

var (
	catalog    = mustLoad()
	responders = mustResponders()
)

func mustLoad() *compliance.Catalog {
	c, err := compliance.Load()
	if err != nil {
		panic(err)
	}
	return c
}

func mustResponders() []compliance.Responder {
	dir := os.Getenv("FCGI_COMPLIANCE_RESPONDERS")
	if dir == "" {
		dir = "fixtures"
	}
	rs, err := compliance.LoadResponders(dir)
	if err != nil {
		panic(err)
	}
	return rs
}

func pendingOr(err error) {
	if errors.Is(err, compliance.ErrPending) {
		Skip("pending: transport not implemented yet")
	}
	Expect(err).ToNot(HaveOccurred())
}

var _ = Describe("fcgi-go", func() {
	It("has a catalog and responders", func() {
		Expect(catalog.Cases).ToNot(BeEmpty())
		Expect(responders).ToNot(BeEmpty())
	})

	for i := range responders {
		r := &responders[i]
		Describe("against the "+r.Name+" responder", func() {
			// One gateway per responder for the whole run, so its connection
			// limit spans every case. Not Ordered: one failure must not stop
			// the remaining cases.
			var (
				once  sync.Once
				route func(compliance.Route) http.Handler
			)
			BeforeEach(func() {
				once.Do(func() {
					spool, err := os.MkdirTemp("", "fcgi-compliance-")
					Expect(err).ToNot(HaveOccurred())
					route = fcgiTarget(spool)(r)
				})
			})

			for j := range catalog.Cases {
				c := &catalog.Cases[j]
				fixture := compliance.Fixture(c.Route.Script)
				if c.Route.Script == "" {
					fixture = compliance.Fixture(compliance.ScriptFor(c.Request.Target))
				}
				for _, tr := range compliance.Runs(c.Transports) {
					It(fmt.Sprintf("%s [%s] %s", c.ID, tr, c.Summary), Label(c.Tier, c.Level, string(tr)), func(ctx SpecContext) {
						if c.Waived != "" {
							Skip("waived: " + c.Waived)
						}
						if !r.Implements(fixture) {
							Skip("pending: responder lacks fixture " + fixture)
						}
						ctx2, cancel := context.WithTimeout(ctx, 30*time.Second)
						defer cancel()
						failures, err := compliance.RunCase(ctx2, c, tr, route)
						pendingOr(err)
						Expect(failures).To(BeEmpty(), "%s (%s %s)", c.ID, c.Source, c.Level)
					})
				}
			}

			for j := range catalog.Scenarios {
				s := &catalog.Scenarios[j]
				for _, tr := range compliance.Runs(s.Transports) {
					It(fmt.Sprintf("%s [%s] %s", s.ID, tr, s.Summary), Label(s.Tier, s.Level, string(tr), "scenario"), func(ctx SpecContext) {
						for _, f := range compliance.Fixtures(s) {
							if !r.Implements(f) {
								Skip("pending: responder lacks fixture " + f)
							}
						}
						ctx2, cancel := context.WithTimeout(ctx, 2*time.Minute)
						defer cancel()
						failures, err := compliance.RunScenario(ctx2, s, tr, route)
						pendingOr(err)
						Expect(failures).To(BeEmpty(), "%s (%s %s)", s.ID, s.Source, s.Level)
					})
				}
			}
		})
	}
})
