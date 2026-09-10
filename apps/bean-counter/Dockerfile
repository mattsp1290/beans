# Build context is the REPOSITORY ROOT, not this directory:
#
#   docker build -f apps/bean-counter/Dockerfile -t bean-counter-api .
#
# apps/bean-counter depends on the beans library through a filesystem
# `replace` (=> ../../libs/beans), which lives outside a bean-counter-scoped
# context. go.work is deliberately NOT copied and GOWORK=off is set, so the
# image is built through the replace directive alone and cannot come to depend
# on a workspace file that .dockerignore or a partial COPY could omit.
FROM golang:1.25.7-alpine AS build

WORKDIR /src

# Dependency layer first, so a source-only change does not re-download modules.
# apps/bean-counter's module graph reads the replace target's go.mod, so
# libs/beans/go.mod must be present here even though its source is not needed
# yet.
COPY libs/beans/go.mod libs/beans/go.sum ./libs/beans/
COPY apps/bean-counter/go.mod apps/bean-counter/go.sum ./apps/bean-counter/

WORKDIR /src/apps/bean-counter
ENV GOWORK=off
RUN go mod download

WORKDIR /src
# The whole library is copied, not just model/repo/store: libs/beans/schema
# embeds schema/migrations/*/*.sql via go:embed, and a partial copy breaks the
# embed at build time rather than at runtime.
COPY libs/beans ./libs/beans
COPY apps/bean-counter/cmd ./apps/bean-counter/cmd
COPY apps/bean-counter/internal ./apps/bean-counter/internal

WORKDIR /src/apps/bean-counter
ENV CGO_ENABLED=0
RUN GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bean-counter ./cmd/bean-counter

FROM alpine:3.22

RUN addgroup -S bean-counter \
    && adduser -S -G bean-counter bean-counter \
    && mkdir -p /data \
    && chown bean-counter:bean-counter /data

USER bean-counter
WORKDIR /data

ENV BN_DRIVER=sqlite
ENV BN_DSN=file:/data/bean-counter.db
ENV BN_ADDR=:8080
ENV BN_PROJECT_PREFIX=bean-counter
ENV BN_ACTOR=bean-counter
ENV BN_CORS_ORIGIN=http://localhost:8080

COPY --from=build /out/bean-counter /usr/local/bin/bean-counter

EXPOSE 8080

ENTRYPOINT ["bean-counter"]
