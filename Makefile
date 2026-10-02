TEST_DATABASE_URL ?= postgres://postgres:test@localhost:55432/postgres?sslmode=disable

.PHONY: db-up db-down test

db-up:
	docker run -d --rm --name sslchk-testdb -e POSTGRES_PASSWORD=test -p 55432:5432 postgres:17
	until docker exec sslchk-testdb pg_isready -U postgres -h 127.0.0.1 >/dev/null 2>&1; do sleep 1; done

db-down:
	docker rm -f sslchk-testdb

test:
	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test ./...
