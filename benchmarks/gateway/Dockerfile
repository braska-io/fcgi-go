# Built with the repository root as context; see /.dockerignore.
FROM golang:1.27-trixie AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY internal ./internal
COPY benchmarks/gateway ./benchmarks/gateway
WORKDIR /src/benchmarks/gateway
RUN CGO_ENABLED=0 go build -trimpath -o /gateway .

FROM debian:trixie-slim
COPY --from=build /gateway /usr/local/bin/gateway
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/gateway"]
