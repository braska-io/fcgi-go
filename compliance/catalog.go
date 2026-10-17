// Package compliance holds the specification-derived cases for a CGI gateway
// and the code that runs them against any gateway and responder.
package compliance

import (
	"embed"
	"encoding/base64"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

//go:embed catalog
var catalogFS embed.FS

// Transport is the HTTP version a case is sent with.
type Transport string

// The transports the schema knows. H3 runs are pending until the library
// adds HTTP/3.
const (
	H1 Transport = "h1"
	H2 Transport = "h2"
	H3 Transport = "h3"
)

// AllTransports is the default of a case without a transports list.
var AllTransports = []Transport{H1, H2, H3}

// Case is one request with its expected observation.
type Case struct {
	ID         string      `yaml:"id"`
	Tier       string      `yaml:"tier"`
	Source     string      `yaml:"source"`
	Level      string      `yaml:"level"`
	Summary    string      `yaml:"summary"`
	Waived     string      `yaml:"waived"`
	Transports []Transport `yaml:"transports"`
	Request    Request     `yaml:"request"`
	Route      Route       `yaml:"route"`
	Expect     Expect      `yaml:"expect"`
}

// Scenario is a sequence of steps that one request cannot show.
type Scenario struct {
	ID         string      `yaml:"id"`
	Tier       string      `yaml:"tier"`
	Source     string      `yaml:"source"`
	Level      string      `yaml:"level"`
	Summary    string      `yaml:"summary"`
	Transports []Transport `yaml:"transports"`
	CookieJar  bool        `yaml:"cookie_jar"`
	Steps      []Step      `yaml:"steps"`
}

// Step is a request, a pause, or a group of steps that start together.
type Step struct {
	Request  *Request `yaml:"request"`
	Route    Route    `yaml:"route"`
	Expect   Expect   `yaml:"expect"`
	WaitMS   int      `yaml:"wait_ms"`
	DelayMS  int      `yaml:"delay_ms"`
	Parallel []Step   `yaml:"parallel"`
}

// Header is one request header field; order and duplicates are kept.
type Header [2]string

// Request is what the client sends.
type Request struct {
	Method     string   `yaml:"method"`
	Target     string   `yaml:"target"`
	Authority  string   `yaml:"authority"`
	Headers    []Header `yaml:"headers"`
	Body       *string  `yaml:"body"`
	BodyBase64 string   `yaml:"body_base64"`
	BodySize   int64    `yaml:"body_size"`
	Framing    string   `yaml:"framing"`
}

// Route is what the caller's router hands to the gateway.
type Route struct {
	Script      string            `yaml:"script"`
	PathInfo    string            `yaml:"path_info"`
	QueryString *string           `yaml:"query_string"`
	Params      map[string]string `yaml:"params"`
	TimeoutMS   int               `yaml:"timeout_ms"`
	MaxResponse int64             `yaml:"max_response_bytes"`
}

// Expect is what the client and the responder must observe.
type Expect struct {
	ReachesResponder *bool               `yaml:"reaches_responder"`
	Status           int                 `yaml:"status"`
	StatusIn         []int               `yaml:"status_in"`
	OrReset          bool                `yaml:"or_reset"` // a reset stream also passes: RFC 9113 8.1.1, RFC 9114 4.1.2
	Env              map[string]string   `yaml:"env"`
	EnvMatch         map[string]string   `yaml:"env_match"`
	EnvAbsent        []string            `yaml:"env_absent"`
	StdinLength      *int64              `yaml:"stdin_length"`
	StdinSHA256      string              `yaml:"stdin_sha256"`
	Headers          map[string]string   `yaml:"headers"`
	HeadersAll       map[string][]string `yaml:"headers_all"`
	HeadersAbsent    []string            `yaml:"headers_absent"`
	Body             *string             `yaml:"body"`
	BodyContains     string              `yaml:"body_contains"`
	BodyLength       *int64              `yaml:"body_length"`
	BodySHA256       string              `yaml:"body_sha256"`
	DurationMinMS    int                 `yaml:"duration_min_ms"`
	DurationMaxMS    int                 `yaml:"duration_max_ms"`
}

// Catalog is every case and scenario in the embedded catalog.
type Catalog struct {
	Cases     []Case
	Scenarios []Scenario
}

// Load reads and validates the embedded catalog.
func Load() (*Catalog, error) { return LoadFS(catalogFS) }

// LoadFS reads and validates every catalog/**/*.yaml in fsys.
func LoadFS(fsys fs.FS) (*Catalog, error) {
	var files []string
	err := fs.WalkDir(fsys, "catalog", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && path.Ext(p) == ".yaml" {
			files = append(files, p)
		}
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("walk catalog: %w", err)
	}
	sort.Strings(files)

	cat := &Catalog{}
	for _, f := range files {
		b, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f, err)
		}
		var doc struct {
			Cases     []Case     `yaml:"cases"`
			Scenarios []Scenario `yaml:"scenarios"`
		}
		if err := yaml.Unmarshal(b, &doc); err != nil {
			return nil, fmt.Errorf("parse %s: %w", f, err)
		}
		cat.Cases = append(cat.Cases, doc.Cases...)
		cat.Scenarios = append(cat.Scenarios, doc.Scenarios...)
	}
	if errs := cat.Validate(); len(errs) > 0 {
		return nil, fmt.Errorf("invalid catalog:\n  %s", strings.Join(errs, "\n  "))
	}
	return cat, nil
}

var (
	idPattern  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9.]+)*$`)
	tiers      = map[string]bool{"spec": true, "gateway": true, "server": true, "app": true}
	levels     = map[string]bool{"MUST": true, "SHOULD": true, "MAY": true, "OBSERVED": true}
	transports = map[Transport]bool{H1: true, H2: true, H3: true}
	framings   = map[string]bool{"": true, "length": true, "chunked": true, "none": true}
)

// Validate reports every schema violation, one line each.
func (c *Catalog) Validate() []string {
	var errs []string
	seen := map[string]bool{}
	head := func(id, tier, source, level, summary string, ts []Transport) {
		switch {
		case !idPattern.MatchString(id):
			errs = append(errs, fmt.Sprintf("%q: id must be lowercase kebab-case", id))
		case seen[id]:
			errs = append(errs, fmt.Sprintf("%s: duplicate id", id))
		}
		seen[id] = true
		if !tiers[tier] {
			errs = append(errs, fmt.Sprintf("%s: unknown tier %q", id, tier))
		}
		if !levels[level] {
			errs = append(errs, fmt.Sprintf("%s: unknown level %q", id, level))
		}
		if source == "" || summary == "" {
			errs = append(errs, fmt.Sprintf("%s: source and summary are required", id))
		}
		for _, t := range ts {
			if !transports[t] {
				errs = append(errs, fmt.Sprintf("%s: unknown transport %q", id, t))
			}
		}
	}
	for i := range c.Cases {
		k := &c.Cases[i]
		head(k.ID, k.Tier, k.Source, k.Level, k.Summary, k.Transports)
		errs = append(errs, validateRequest(k.ID, &k.Request, &k.Expect)...)
	}
	for i := range c.Scenarios {
		s := &c.Scenarios[i]
		head(s.ID, s.Tier, s.Source, s.Level, s.Summary, s.Transports)
		if len(s.Steps) == 0 {
			errs = append(errs, fmt.Sprintf("%s: a scenario needs steps", s.ID))
		}
		errs = append(errs, validateSteps(s.ID, s.Steps)...)
	}
	return errs
}

func validateSteps(id string, steps []Step) []string {
	var errs []string
	for i := range steps {
		st := &steps[i]
		kinds := 0
		if st.Request != nil {
			kinds++
			errs = append(errs, validateRequest(fmt.Sprintf("%s step %d", id, i+1), st.Request, &st.Expect)...)
		}
		if st.WaitMS > 0 {
			kinds++
		}
		if len(st.Parallel) > 0 {
			kinds++
			errs = append(errs, validateSteps(fmt.Sprintf("%s step %d", id, i+1), st.Parallel)...)
		}
		if kinds != 1 {
			errs = append(errs, fmt.Sprintf("%s step %d: exactly one of request, wait_ms, parallel", id, i+1))
		}
	}
	return errs
}

func validateRequest(id string, r *Request, e *Expect) []string {
	var errs []string
	if r.Method == "" || r.Target == "" {
		errs = append(errs, fmt.Sprintf("%s: request needs method and target", id))
	}
	if !framings[r.Framing] {
		errs = append(errs, fmt.Sprintf("%s: unknown framing %q", id, r.Framing))
	}
	bodies := 0
	for _, set := range []bool{r.Body != nil, r.BodyBase64 != "", r.BodySize > 0} {
		if set {
			bodies++
		}
	}
	if bodies > 1 {
		errs = append(errs, fmt.Sprintf("%s: body, body_base64 and body_size exclude each other", id))
	}
	if r.BodyBase64 != "" {
		if _, err := base64.StdEncoding.DecodeString(r.BodyBase64); err != nil {
			errs = append(errs, fmt.Sprintf("%s: body_base64: %v", id, err))
		}
	}
	if e.Status != 0 && len(e.StatusIn) > 0 {
		errs = append(errs, fmt.Sprintf("%s: status and status_in exclude each other", id))
	}
	for k, v := range e.EnvMatch {
		if _, err := regexp.Compile(v); err != nil {
			errs = append(errs, fmt.Sprintf("%s: env_match %s: %v", id, k, err))
		}
	}
	return errs
}

// Runs reports the transports of a case: its list, or all.
func Runs(ts []Transport) []Transport {
	if len(ts) == 0 {
		return AllTransports
	}
	return ts
}

// script is the route's script, or the first path segment of the target.
func (r Route) script(target string) string {
	if r.Script != "" {
		return r.Script
	}
	return ScriptFor(target)
}

// ScriptFor is the first path segment of a request-target.
func ScriptFor(target string) string {
	p := strings.TrimPrefix(strings.SplitN(target, "?", 2)[0], "/")
	if i := strings.Index(p, ".php"); i >= 0 {
		return p[:i+len(".php")] // app scripts live in subdirectories
	}
	return strings.SplitN(p, "/", 2)[0]
}

// Fixtures lists the fixtures a scenario's steps use.
func Fixtures(s *Scenario) []string {
	seen := map[string]bool{}
	var out []string
	var walk func([]Step)
	walk = func(steps []Step) {
		for i := range steps {
			st := &steps[i]
			walk(st.Parallel)
			if st.Request == nil {
				continue
			}
			f := Fixture(st.Route.script(st.Request.Target))
			if !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	walk(s.Steps)
	return out
}

// Fixture is the script path without leading slash and extension: "dump.php"
// selects "dump", "app/session/start.php" selects "app/session/start".
func Fixture(script string) string {
	p := strings.TrimPrefix(script, "/")
	return strings.TrimSuffix(p, path.Ext(p))
}
