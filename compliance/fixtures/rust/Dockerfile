FROM rust:1-slim AS build
WORKDIR /src
COPY Cargo.toml ./
COPY src ./src
RUN cargo test --release
RUN cargo build --release

FROM debian:trixie-slim
COPY --from=build /src/target/release/fcgi-compliance-rust /usr/local/bin/responder
RUN mkdir /state
ENV FIXTURE_STATE=/state
EXPOSE 9000
CMD ["/usr/local/bin/responder"]
