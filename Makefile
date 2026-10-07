TEST_DATABASE_URL ?= postgres://postgres:test@localhost:55432/postgres?sslmode=disable

.PHONY: db-up db-down test web-test web-build

db-up:
	docker run -d --rm --name sslchk-testdb -e POSTGRES_PASSWORD=test -p 55432:5432 postgres:17
	until docker exec sslchk-testdb pg_isready -U postgres -h 127.0.0.1 >/dev/null 2>&1; do sleep 1; done

db-down:
	docker rm -f sslchk-testdb

test:
	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test ./...

# 대시보드(web/). Go 테스트와 따로 돌린다. Node 24가 필요하다.
web-test:
	cd web && npm ci && npm test

# 빌드 결과는 internal/webui/dist로 간다(바이너리가 내장한다).
web-build:
	cd web && npm ci && npm run build
