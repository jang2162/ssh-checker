TEST_DATABASE_URL ?= postgres://postgres:test@localhost:55432/postgres?sslmode=disable

.PHONY: db-up db-down test script-test-linux web-test web-build

db-up:
	docker run -d --rm --name sslchk-testdb -e POSTGRES_PASSWORD=test -p 55432:5432 postgres:17
	until docker exec sslchk-testdb pg_isready -U postgres -h 127.0.0.1 >/dev/null 2>&1; do sleep 1; done

db-down:
	docker rm -f sslchk-testdb

test:
	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test ./...

# setup.sh 테스트를 Linux(bash 5)에서 돌린다. macOS 기본 bash 3.2는 set -u에서 값이 없는 변수를 ${VAR#패턴}으로 써도
# 오류를 내지 않아서, 서버(Linux)에서만 터지는 문제를 놓친다. Docker가 필요하다.
script-test-linux:
	docker run --rm -v "$(CURDIR)":/src -w /src golang:1.27 go test ./internal/adminapi -run 'Setup|Script' -count=1

# 대시보드(web/). Go 테스트와 따로 돌린다. Node 24가 필요하다.
web-test:
	cd web && npm ci && npm test

# 빌드 결과는 internal/webui/dist로 간다(바이너리가 내장한다).
web-build:
	cd web && npm ci && npm run build
