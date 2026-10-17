FROM debian:trixie-slim AS build
RUN apt-get update -qq && apt-get install -qq -y --no-install-recommends g++ libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*
COPY responder.cpp /src/responder.cpp
RUN g++ -std=c++20 -O2 -Wall -Wextra -pthread -o /responder /src/responder.cpp -lsqlite3

FROM debian:trixie-slim
RUN apt-get update -qq && apt-get install -qq -y --no-install-recommends libsqlite3-0 \
    && rm -rf /var/lib/apt/lists/* && mkdir /state
COPY --from=build /responder /usr/local/bin/responder
ENV FIXTURE_STATE=/state
EXPOSE 9000
CMD ["/usr/local/bin/responder"]
