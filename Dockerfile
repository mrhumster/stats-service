FROM golang:1.25.14-alpine AS builder
ARG VERSION=0.0.1
ARG BUILD_DATE=2026-09-16

WORKDIR /app
COPY stats-service/go.mod ./
RUN if [ -f stats-service/go.sum ]; then cp stats-service/go.sum .; fi
RUN go mod download
COPY stats-service/. .
RUN CGO_ENABLED=0 GOOS=linux go build \
  -ldflags="-w -s -X main.version=$VERSION -X main.buildDate=$BUILD_DATE" \
  -o stats-server ./cmd/server/main.go

FROM alpine:3.18
ARG VERSION=0.0.1
ARG BUILD_DATE=2026-09-16
LABEL version=$VERSION \
  build-date=$BUILD_DATE \
  maintainer="me@xomrkob.ru"
RUN apk add --no-cache ca-certificates
RUN addgroup -g 1000 appgroup && \
  adduser -D -u 1000 -G appgroup appuser
WORKDIR /app
COPY --from=builder --chown=appuser:appgroup /app/stats-server .
USER appuser
ENTRYPOINT ["/app/stats-server"]