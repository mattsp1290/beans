FROM golang:1.25.7-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

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
ENV BN_CORS_ORIGIN=http://localhost:5173

COPY --from=build /out/bean-counter /usr/local/bin/bean-counter

EXPOSE 8080

ENTRYPOINT ["bean-counter"]
