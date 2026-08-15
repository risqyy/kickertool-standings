FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/crawler ./cmd/crawler

FROM alpine:3.24
RUN apk add --no-cache ca-certificates \
    && addgroup -S app \
    && adduser -S -G app app
WORKDIR /app
COPY --from=build /out/crawler /app/crawler
RUN mkdir -p /data && chown app:app /data
ENV DB_PATH=/data/tournaments.db
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
USER app:app
ENTRYPOINT ["/app/crawler"]
