FRONTEND_DIR = ./web/default
FRONTEND_CLASSIC_DIR = ./web/classic
BACKEND_DIR = .

.PHONY: all build-frontend build-frontend-classic build-all-frontends start-backend dev dev-api dev-web dev-web-classic

all: build-all-frontends start-backend

build-frontend:
	@echo "Building default frontend..."
	@[ -f VERSION ] || { echo "missing VERSION file" >&2; exit 1; }; \
	build_version="$$(cat VERSION)" && \
	build_commit="$${BUILD_COMMIT:-$$(git rev-parse HEAD 2>/dev/null || echo unknown)}" && \
	build_date="$${BUILD_DATE:-$$(date -u +%Y-%m-%dT%H:%M:%SZ)}" && \
	rm -rf "$(FRONTEND_DIR)/dist" && \
	(cd $(FRONTEND_DIR) && bun install --frozen-lockfile && DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION="$$build_version" bun run build) && \
	sh scripts/write-frontend-release-metadata.sh default "$(FRONTEND_DIR)/dist" "$$build_version" "$$build_commit" "$$build_date"

build-frontend-classic:
	@echo "Building classic frontend..."
	@[ -f VERSION ] || { echo "missing VERSION file" >&2; exit 1; }; \
	build_version="$$(cat VERSION)" && \
	build_commit="$${BUILD_COMMIT:-$$(git rev-parse HEAD 2>/dev/null || echo unknown)}" && \
	build_date="$${BUILD_DATE:-$$(date -u +%Y-%m-%dT%H:%M:%SZ)}" && \
	rm -rf "$(FRONTEND_CLASSIC_DIR)/dist" && \
	(cd $(FRONTEND_CLASSIC_DIR) && bun install --frozen-lockfile && DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION="$$build_version" bun run build) && \
	sh scripts/write-frontend-release-metadata.sh classic "$(FRONTEND_CLASSIC_DIR)/dist" "$$build_version" "$$build_commit" "$$build_date"

build-all-frontends:
	@echo "Building all frontends..."
	@[ -f VERSION ] || { echo "missing VERSION file" >&2; exit 1; }; \
	bash scripts/build-release-frontends.sh "$$(cat VERSION)"

start-backend:
	@echo "Starting backend dev server..."
	@cd $(BACKEND_DIR) && go run main.go &

dev-api:
	@echo "Starting backend services (docker)..."
	@docker compose -f docker-compose.dev.yml up -d

dev-web:
	@echo "Starting frontend dev server..."
	@cd $(FRONTEND_DIR) && bun install && bun run dev

dev-web-classic:
	@echo "Starting classic frontend dev server..."
	@cd $(FRONTEND_CLASSIC_DIR) && bun install && bun run dev

dev: dev-api dev-web
