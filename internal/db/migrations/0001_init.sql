CREATE TABLE domains (
    id         bigserial PRIMARY KEY,
    fqdn       text        NOT NULL UNIQUE,
    memo       text        NOT NULL DEFAULT '',
    status     text        NOT NULL DEFAULT 'pending_cname',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE acme_accounts (
    id         bigserial PRIMARY KEY,
    domain_id  bigint      NOT NULL REFERENCES domains (id),
    username   uuid        NOT NULL UNIQUE,
    key_hash   text        NOT NULL,
    subdomain  uuid        NOT NULL UNIQUE,
    allowfrom  text[]      NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz
);

-- 도메인당 활성 계정은 1개. 재발급하면 기존 행에 revoked_at을 기록한다.
CREATE UNIQUE INDEX acme_accounts_one_active ON acme_accounts (domain_id) WHERE revoked_at IS NULL;

CREATE TABLE txt_records (
    id           bigserial PRIMARY KEY,
    account_id   bigint      NOT NULL REFERENCES acme_accounts (id),
    value        text        NOT NULL,
    provider_ref text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (account_id, value)
);

CREATE TABLE audit_logs (
    id        bigserial PRIMARY KEY,
    actor     text        NOT NULL,
    action    text        NOT NULL,
    target    text        NOT NULL DEFAULT '',
    source_ip text        NOT NULL DEFAULT '',
    detail    text        NOT NULL DEFAULT '',
    at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_logs_at ON audit_logs (at DESC);
