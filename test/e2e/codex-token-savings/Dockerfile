# Build rtk-go with the pinned Go 1.25 toolchain.
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go/ /src/
RUN CGO_ENABLED=0 GOTOOLCHAIN=local GOPROXY=off \
    go build -mod=readonly -trimpath -o /out/rtk-go ./cmd/rtk-go
COPY test/e2e/codex-token-savings/bench.go /build/bench.go
RUN CGO_ENABLED=0 go build -trimpath -o /out/bench /build/bench.go

FROM rtk-original:0.49.0 AS rtkbin

FROM node:22-bookworm-slim
COPY --from=build /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:/usr/local/bin:$PATH
RUN ln -sf /usr/local/go/bin/go /usr/local/bin/go
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates git ripgrep \
 && rm -rf /var/lib/apt/lists/*
RUN npm install -g @openai/codex@0.163.0
COPY --from=build /out/rtk-go /usr/local/bin/rtk-go
COPY --from=build /out/bench /usr/local/bin/bench
COPY --from=rtkbin /src/target/release/rtk /usr/local/bin/rtk
COPY test/e2e/codex-token-savings/codex-config.toml /root/.codex/config.toml
ARG FIXTURE=fixture.sh
COPY test/e2e/codex-token-savings/${FIXTURE} /usr/local/bin/make-fixture
RUN chmod +x /usr/local/bin/make-fixture && /usr/local/bin/make-fixture
WORKDIR /work
