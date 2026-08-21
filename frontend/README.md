# Frontend

This directory contains the one React + TypeScript + Vite application. It owns public ranking, the admin dashboard/tournament inclusion UI/player merge and merge-undo UI, routing, the AdminGuard, API client, and the local shadcn/ui-style design system. It contains no Go, SQLite, crawler, parser, aggregation, or secrets.

Use `npm ci`, `npm run dev`, `npm run typecheck`, `npm test`, and `npm run build`. Vite proxies relative `/api/*` requests to the backend at `http://localhost:8080`; production Nginx performs the same reverse proxy. Routes are `/`, `/standings`, `/admin`, `/admin/tournaments`, and `/admin/players/merge`.
