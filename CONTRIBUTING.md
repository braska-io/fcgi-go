# Contributing to fcgi-go

Contributions to the library, the compliance suite, and the reference responders are welcome.
[compliance/README.md](compliance/README.md) explains the terms used here: responder, fixture, and case.

## Discuss larger changes first

Open an issue before you start work that changes the API, the design, or more than a few files.
Small fixes, such as a typo or a broken command, can go straight to a pull request.

Ask usage questions in a blank issue.
Report security problems privately, as [SECURITY.md](SECURITY.md) describes.

## Set up your machine

You need three tools.
Check each one:

| Tool | Check | Expected |
| --- | --- | --- |
| Go 1.27 or later | `go version` | `go version go1.27` or later |
| Docker with Compose | `docker compose version` | a version line, no error |
| [Task](https://taskfile.dev) | `task --version` | a version line, no error |

Clone the repository:

```sh
git clone https://github.com/braska-io/fcgi-go.git
cd fcgi-go
```

## Run the checks

Vet both modules:

```sh
task lint
```

Run the library's unit tests:

```sh
task test
```

Check that the compliance catalog loads:

```sh
task catalog
```

Run the compliance suite against the four reference responders:

```sh
task compliance
```

`task compliance` builds and starts the responders with Docker Compose first.
Stop them with `task responders:down`.
Run one case with `task compliance -- -ginkgo.focus "<case id>"`.

On every pull request, CI runs the commands behind `task lint`, `task test`, and `task catalog`.
These must pass.
CI also runs the compliance suite.
The suite fails until the implementation exists.
Reviewers therefore check which cases changed from failing to passing, or back.

## Cases come from specifications, never from the implementation

The compliance suite defines what correct means.
A case is one HTTP request together with the result it must produce.
Cases live as YAML in `compliance/catalog/`.
Each case cites a source document and section.
Sources are RFC 3875, FastCGI 1.0, RFC 9110, 9112, 9113, 9114, and [docs/design.md](docs/design.md).

- Never edit a case to make an implementation pass.
- A failing case means the implementation is wrong, until someone shows the case is wrong.
- A change to a case is its own pull request.
  It cites the document and section that justify the change.
  It contains no implementation code.
- A new case states its `source`, `tier`, and `level`.
  [compliance/README.md](compliance/README.md) defines the schema.

A change to a fixture follows the same rule.
Update [compliance/RESPONDERS.md](compliance/RESPONDERS.md) first.
Then update every reference responder whose `responder.yaml` lists the fixture.

## Add a compliance case

1. Pick the file in `compliance/catalog/` whose source matches your case, such as `rfc3875-request.yaml`.
2. Add the case with `id`, `tier`, `source`, `level`, `summary`, `request`, and `expect`.
   [compliance/README.md](compliance/README.md) describes each field.
3. Check that the catalog still loads:

   ```sh
   task catalog
   ```

4. Run the new case against the reference responders:

   ```sh
   task compliance -- -ginkgo.focus "<case id>"
   ```

   Until the implementation exists, a case that reaches the handler fails, because the handler answers 501.
5. Open a pull request of type `test(compliance)` that contains only the case.

## Write code a reader can follow

- Format Go code with `gofmt`.
- The library module stays free of dependencies outside the standard library.
  Test dependencies belong in the `compliance` module.
- Comments explain why the code does something, not what it does.
- Log messages are lowercase and end without a period.
- Add or change a test with every behaviour change.

## Write commits as Conventional Commits

Use the form `type(scope): description`, as described at [conventionalcommits.org](https://www.conventionalcommits.org/en/v1.0.0/).

| Type | Use for |
| --- | --- |
| `feat` | new behaviour in the library |
| `fix` | a bug fix |
| `test` | compliance cases, fixtures, unit tests |
| `docs` | documentation only |
| `refactor` | code changes without behaviour changes |
| `chore` | tooling, dependencies |
| `ci` | workflow changes |

The scope names the area: `fcgi`, `compliance`, `fixtures/php`, `fixtures/rust`, and so on.
Keep one logical change per commit.

Examples:

```text
feat(fcgi): drop client headers whose raw name contains an underscore
test(compliance): add RFC 3875 section 4.1.5 PATH_INFO decoding case
```

## Open a pull request

1. Fork the repository and create a branch from `main`.
2. Make your change and run the checks above.
3. Push the branch and open a pull request against `main`.
4. Fill in the pull request template.

A maintainer reviews the pull request.
Expect questions; they are part of the review, not a rejection.

## Licensing of contributions

fcgi-go uses the [MIT License](LICENSE).
By submitting a contribution, you agree to license it under the same terms.
The project does not require a Developer Certificate of Origin sign-off or a contributor licence agreement.

## Code of conduct

Everyone in the project follows the [code of conduct](CODE_OF_CONDUCT.md).
