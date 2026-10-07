-- 모니터링: 도메인별 점검 대상과 점검 기록, ACME 계정의 CNAME·갱신 상태.
-- 도메인 상태는 대상과 ACME에서 계산하므로 저장하지 않는다.
ALTER TABLE domains DROP COLUMN status;

CREATE TABLE monitor_targets (
    id         bigserial PRIMARY KEY,
    domain_id  bigint      NOT NULL REFERENCES domains (id) ON DELETE CASCADE,
    hostname   text        NOT NULL,
    port       integer     NOT NULL DEFAULT 443 CHECK (port BETWEEN 1 AND 65535),
    sni        text        NOT NULL DEFAULT '',
    ips        text[]      NOT NULL DEFAULT '{}',
    memo       text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (domain_id, hostname, port, sni)
);

-- 인증서를 받지 못한 행도 넣기 쉽게 not_before/not_after만 NULL을 허용한다.
CREATE TABLE check_results (
    id          bigserial PRIMARY KEY,
    target_id   bigint      NOT NULL REFERENCES monitor_targets (id) ON DELETE CASCADE,
    address     text        NOT NULL,
    checked_at  timestamptz NOT NULL DEFAULT now(),
    status      text        NOT NULL,
    reasons     text[]      NOT NULL DEFAULT '{}',
    tls_version text        NOT NULL DEFAULT '',
    cipher      text        NOT NULL DEFAULT '',
    subject_cn  text        NOT NULL DEFAULT '',
    sans        text[]      NOT NULL DEFAULT '{}',
    issuer      text        NOT NULL DEFAULT '',
    serial      text        NOT NULL DEFAULT '',
    not_before  timestamptz,
    not_after   timestamptz,
    key_alg     text        NOT NULL DEFAULT '',
    key_bits    integer     NOT NULL DEFAULT 0,
    chain_ok    boolean     NOT NULL DEFAULT false,
    hostname_ok boolean     NOT NULL DEFAULT false,
    error       text        NOT NULL DEFAULT ''
);

CREATE INDEX check_results_target_time ON check_results (target_id, checked_at DESC);
-- 최신 결과 조회(주소별 LIMIT 1)용. target_time은 이력과 호스트명 모드 조회, time은 정리에 쓴다.
CREATE INDEX check_results_target_address_time ON check_results (target_id, address, checked_at DESC, id DESC);
CREATE INDEX check_results_time ON check_results (checked_at);

ALTER TABLE acme_accounts
    ADD COLUMN cname_status         text    NOT NULL DEFAULT 'unknown',
    ADD COLUMN cname_value          text    NOT NULL DEFAULT '',
    ADD COLUMN cname_checked_at     timestamptz,
    ADD COLUMN last_update_at       timestamptz,
    ADD COLUMN last_failure_at      timestamptz,
    ADD COLUMN consecutive_failures integer NOT NULL DEFAULT 0;

-- 도메인을 지우면 계정과 TXT 기록도 함께 지운다. 감사 로그는 텍스트라 남는다.
ALTER TABLE acme_accounts
    DROP CONSTRAINT acme_accounts_domain_id_fkey,
    ADD CONSTRAINT acme_accounts_domain_id_fkey FOREIGN KEY (domain_id) REFERENCES domains (id) ON DELETE CASCADE;
ALTER TABLE txt_records
    DROP CONSTRAINT txt_records_account_id_fkey,
    ADD CONSTRAINT txt_records_account_id_fkey FOREIGN KEY (account_id) REFERENCES acme_accounts (id) ON DELETE CASCADE;
