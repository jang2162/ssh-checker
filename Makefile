TEST_DATABASE_URL ?= postgres://postgres:test@localhost:55432/postgres?sslmode=disable

.PHONY: db-up db-down test script-test-linux web-test web-build

db-up:
	docker run -d --rm --name sslchk-testdb -e POSTGRES_PASSWORD=test -p 55432:5432 postgres:17
	until docker exec sslchk-testdb pg_isready -U postgres -h 127.0.0.1 >/dev/null 2>&1; do sleep 1; done

db-down:
	docker rm -f sslchk-testdb

test:
	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test ./...

# Run the setup.sh tests on Linux (bash 5). The macOS default bash 3.2 does not fail when an unset variable is used as
# ${VAR#pattern} under set -u, so it misses problems that only appear on servers (Linux). Needs Docker.
script-test-linux:
	docker run --rm -v "$(CURDIR)":/src -w /src golang:1.27 go test ./internal/adminapi -run 'Setup|Script' -count=1

# Dashboard (web/). Run separately from the Go tests. Needs Node 24.
web-test:
	cd web && npm ci && npm test

# The build output goes to internal/webui/dist (the binary embeds it).
web-build:
	cd web && npm ci && npm run build
