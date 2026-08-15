.PHONY: dev dev-backend dev-frontend test contract-check test-backend test-frontend build build-backend build-frontend docker-build docker-up

# Export the root .env for local Go/Make commands without printing it.
-include .env
export

dev:
	$(MAKE) -j2 dev-backend dev-frontend

dev-backend:
	go run ./backend/cmd/crawler

dev-frontend:
	npm --prefix frontend run dev -- --host 127.0.0.1

test: contract-check test-backend test-frontend

contract-check:
	node scripts/check-api-contract.mjs

test-backend:
	cd backend && go test ./...

test-frontend:
	npm --prefix frontend test

build: build-backend build-frontend

build-backend:
	cd backend && go build ./...

build-frontend:
	npm --prefix frontend ci
	npm --prefix frontend run build

docker-build:
	docker compose build

docker-up:
	docker compose up --build
