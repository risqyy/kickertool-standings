# Kickertool Ranking

The project is physically split into one React frontend and one Go backend. The frontend serves the public ranking and protected admin experience; the backend owns crawling, normalization, SQLite/GORM, aggregation, inclusion, player aliases/merges, and JSON APIs.

## Repository layout

- `frontend/` — one React + TypeScript + Vite + Tailwind/shadcn-style app, shared public/admin router, lazy admin routes, API client and UI components.
- `backend/` — one Go module with `cmd/`, `internal/`, domain/application/ports, API/HTML sources, GORM repository, scheduler and JSON HTTP handlers.
- `frontend/nginx.conf` — static SPA serving and `/api/` reverse proxy.
- `docker-compose.yml` — backend with SQLite volume and frontend Nginx service.
- `backend/api/openapi.yaml` — versioned JSON contract.

The backend does not embed or serve React files and contains no HTML/template UI. The frontend contains no Go, GORM, SQLite, crawler, parser, aggregation, or secrets.

## API and security

The versioned public endpoint is `GET /api/v1/public/rankings`. Admin JSON is under `/api/v1/admin/*` and is protected by backend HTTP Basic Authentication. Every mutation additionally requires same-origin JSON, the CSRF cookie/header pair, and the relevant version or merge plan. Basic Auth credentials are never put in the React bundle or browser storage.

`GET /api/standings` remains the public JSON compatibility endpoint. `/` and `/standings` are frontend routes. The frontend calls relative `/api/*` URLs only; Vite proxies them to `localhost:8080` during development and production Nginx proxies them to the backend service. Nginx never lets SPA fallback handle `/api/*`, uses no-cache for `index.html`, long cache headers for hashed assets, a 1 MiB request limit, and bounded proxy timeouts.

## Sources and synchronization

Set `TOURNAMENT_SOURCE` to exactly `api` or `html`. API mode requires `TOURNAMENT_API_TOKEN` and uses Tournament.app Public API. HTML mode requires only the single `TOURNAMENT_HTML_URL` start URL and never receives the API token. Both sources produce the same normalized tournament/hierarchy/entry/player/standing model. Completed Monster DYP tournaments are the ranking scope; the crawler still loads and stores all discovered tournaments, independent of manual ranking inclusion.

The scheduler runs immediately and then every 15 minutes without overlap. It retries temporary HTTP failures, handles cancellation, keeps failed/incomplete snapshots from overwriting complete data, finalizes completed tournaments after two identical complete snapshots, and never deletes missing source rows. Points are integer hundredths; PPG is computed only from known non-zero games and rounded commercially to two decimals. Wins, losses and draws are not part of the model or output.

Player identity is the normalized NFC name: trim, collapse all whitespace runs to one ASCII space, Unicode-aware lowercase, without removing diacritics. Aliases and source identities preserve provenance. Manual merges transfer aliases and allocations transactionally, retain the source as a merged tombstone, deduplicate shared allocations, and recalculate the target aggregate from raw included results. Each new merge also stores an exact recovery snapshot and post-merge fingerprint: administrators can preview and confirm an atomic undo while the participants remain unchanged. Older merges without snapshots are shown as unavailable instead of being partially restored.

## Configuration

The root `.env` is loaded for local Go startup without overriding real process environment variables. It is ignored and must never be committed. Copy `.env.example` and set values without printing secrets.

Required:

- `TOURNAMENT_SOURCE=api` or `html`
- API mode: `TOURNAMENT_API_TOKEN`
- HTML mode: valid `TOURNAMENT_HTML_URL`

Optional defaults:

- `TOURNAMENT_API_BASE_URL=https://api.tournament.io/v1/public`
- `DB_PATH=./data/tournaments.db`
- `CRAWL_INTERVAL=15m`, `HTTP_TIMEOUT=30s`, `PAGE_LIMIT=25`
- `MAX_RETRIES=3`, `RETRY_BACKOFF=1s`, `LOG_LEVEL=info`
- `ADMIN_UI_ENABLED=false`
- `ADMIN_USERNAME` and `ADMIN_PASSWORD` are mandatory when admin UI/API access is enabled

`ADMIN_BIND_ADDR` remains available for local deployment documentation; production administration is served by the frontend and protected by the backend API. Use TLS at a reverse proxy before exposing administration beyond loopback.

## Local development

PowerShell from the repository root:

```powershell
go run ./backend/cmd/crawler
npm --prefix frontend ci
npm --prefix frontend run dev -- --host 127.0.0.1
```

The backend listens on `http://localhost:8080`; Vite serves the frontend on `http://localhost:5173` and proxies `/api`. Public ranking works if the backend is running; the frontend shows an explicit unavailable/error state if it is not.

Make targets:

```text
make dev             # backend and frontend together
make dev-backend
make dev-frontend
make test            # Go and frontend tests
make build           # Go and frontend production builds
make docker-build
make docker-up
```

On Windows without GNU Make, use the PowerShell commands above or run the individual `npm --prefix frontend ...` and `cd backend; go ...` commands.

## Docker Compose

Only the backend receives `.env` and secrets. SQLite is mounted at `/data`; the frontend receives no backend secret at build or runtime.

```powershell
docker compose build
docker compose up
```

Frontend: `http://localhost:5173`. Backend health: `http://localhost:8080/healthz`. The compose healthcheck waits for the backend before starting the frontend.

### Reverse proxy / TLS deployment

Terminate TLS in front of the `frontend` service and proxy the public host to
that service's port 80. The reverse proxy must preserve the public host and
set `X-Forwarded-Proto: https`; for example, its upstream request must include:

```text
Host: <public-hostname>
X-Forwarded-Proto: https
```

The frontend Nginx forwards both values to the backend. They are required for
the admin API's same-origin check and for its secure CSRF cookie. Do not proxy
the browser directly to the backend; expose only the frontend service publicly.

## GHCR release images

Every pushed Git tag triggers `.github/workflows/release-images.yml`. The workflow checks out that exact tag, runs all backend/frontend quality gates, and then publishes only the exact tag (never `latest`) for both images:

- `ghcr.io/risqyy/kickertool-standings-backend:<tag>`
- `ghcr.io/risqyy/kickertool-standings-frontend:<tag>`

Example release and pull commands:

```powershell
git tag v1.0.3
git push origin v1.0.3
docker pull ghcr.io/risqyy/kickertool-standings-backend:v1.0.3
docker pull ghcr.io/risqyy/kickertool-standings-frontend:v1.0.3
```

GHCR package visibility and repository linkage are managed in GitHub. The images contain no local `.env`, API token, admin credential, database, or club-specific configuration; provide deployment configuration at runtime.

## Verification

```powershell
cd backend
go test ./...
go vet ./...
go build ./...

cd ..\frontend
npm ci
npm run typecheck
npm test
npm run build
```

Contract changes must update `backend/api/openapi.yaml` and the typed frontend client together.
