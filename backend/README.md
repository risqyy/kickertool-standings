# Backend

The Go backend owns crawling, source adapters, normalized domain data, GORM/SQLite persistence, aggregation, inclusion, manual player merges, zerolog logging, and the public/admin JSON APIs. It never serves React assets or HTML templates.

Run from this directory with `go test ./...`, `go vet ./...`, `go build ./...`, or `go run ./cmd/crawler`. Configuration is read from the process environment; the root `.env` is loaded by the composition root when started from the repository root.

The backend listens on `:8080`, exposes `GET /healthz`, public `GET /api/v1/public/rankings`, protected `/api/v1/admin/*`, and the compatibility JSON endpoint `GET /api/standings`. Admin APIs require Basic Auth; mutations additionally require same-origin JSON and the CSRF cookie/header pair.

Manual ranking corrections are append-only administrative bookings. Their
effective date/year, additive deltas, reason, administrator and revision hash
are stored separately from source standings. Preview/confirm uses the player's
`rankingCorrectionVersion` for optimistic concurrency; revocation keeps the
original row and appends a revision. Materialized, annual and snapshot readers
apply active corrections through the selected date boundary and derive
points-per-game from the resulting points and games.
