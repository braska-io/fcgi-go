FROM debian:trixie-slim AS build
RUN apt-get update -qq \
 && apt-get install -qq -y --no-install-recommends gcc libc6-dev libfcgi-dev libsqlite3-dev >/dev/null
COPY responder.c /src/responder.c
RUN gcc -std=gnu11 -O2 -Wall -Wextra -pthread -o /responder /src/responder.c -lfcgi -lsqlite3

FROM debian:trixie-slim
RUN apt-get update -qq \
 && apt-get install -qq -y --no-install-recommends libfcgi0t64 libsqlite3-0 >/dev/null \
 && rm -rf /var/lib/apt/lists/* \
 && mkdir /state
COPY --from=build /responder /usr/local/bin/responder
ENV FIXTURE_STATE=/state
VOLUME /state
EXPOSE 9000
# restart: always in compose -- crash fixtures kill the whole process
CMD ["/usr/local/bin/responder"]
