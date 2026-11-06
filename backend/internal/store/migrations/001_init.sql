-- ChamaChain schema v1
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS groups (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                TEXT NOT NULL CHECK (length(name) BETWEEN 2 AND 120),
    currency            TEXT NOT NULL DEFAULT 'KES',
    -- approvals required for a withdrawal, NOT counting the requester
    approval_threshold  INT  NOT NULL CHECK (approval_threshold >= 1),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS members (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id    UUID NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    name        TEXT NOT NULL CHECK (length(name) BETWEEN 2 AND 120),
    phone       TEXT NOT NULL CHECK (phone ~ '^\+?[0-9]{9,15}$'),
    role        TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('chair','treasurer','secretary','member')),
    active      BOOLEAN NOT NULL DEFAULT TRUE,
    joined_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (group_id, phone)
);
CREATE INDEX IF NOT EXISTS members_phone_idx ON members(phone);

-- Append-only, hash-chained ledger. Each row's hash covers the previous row's hash.
CREATE TABLE IF NOT EXISTS ledger_entries (
    id          BIGSERIAL PRIMARY KEY,
    group_id    UUID NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    seq         BIGINT NOT NULL,
    entry_type  TEXT NOT NULL CHECK (entry_type IN ('contribution','withdrawal','adjustment')),
    member_id   UUID REFERENCES members(id),
    amount      BIGINT NOT NULL,          -- signed, whole currency units: + money in, - money out
    reference   TEXT NOT NULL DEFAULT '', -- e.g. M-Pesa receipt number
    prev_hash   TEXT NOT NULL,
    hash        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    UNIQUE (group_id, seq)
);

-- Block UPDATE and DELETE on the ledger at the database level.
CREATE OR REPLACE FUNCTION ledger_is_append_only() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'ledger_entries is append-only';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS ledger_no_update ON ledger_entries;
CREATE TRIGGER ledger_no_update BEFORE UPDATE OR DELETE ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION ledger_is_append_only();

CREATE TABLE IF NOT EXISTS withdrawals (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id            UUID NOT NULL REFERENCES groups(id),
    requested_by        UUID NOT NULL REFERENCES members(id),
    amount              BIGINT NOT NULL CHECK (amount > 0),
    recipient_phone     TEXT NOT NULL,
    reason              TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','executed','rejected','cancelled')),
    required_approvals  INT  NOT NULL,
    risk_score          DOUBLE PRECISION NOT NULL DEFAULT 0,
    risk_level          TEXT NOT NULL DEFAULT 'low',
    risk_reasons        JSONB NOT NULL DEFAULT '[]'::jsonb,
    payout_reference    TEXT NOT NULL DEFAULT '',
    ledger_seq          BIGINT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    executed_at         TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS withdrawals_group_status_idx ON withdrawals(group_id, status);

-- One vote per member per withdrawal: approve or reject
CREATE TABLE IF NOT EXISTS withdrawal_approvals (
    withdrawal_id   UUID NOT NULL REFERENCES withdrawals(id),
    member_id       UUID NOT NULL REFERENCES members(id),
    decision        TEXT NOT NULL DEFAULT 'approve' CHECK (decision IN ('approve','reject')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (withdrawal_id, member_id)
);

-- M-Pesa STK push requests waiting for Safaricom's callback
CREATE TABLE IF NOT EXISTS stk_requests (
    checkout_request_id TEXT PRIMARY KEY,
    group_id            UUID NOT NULL REFERENCES groups(id),
    member_id           UUID NOT NULL REFERENCES members(id),
    amount              BIGINT NOT NULL CHECK (amount > 0),
    status              TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','completed','failed')),
    result_desc         TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Record of on-chain anchors
CREATE TABLE IF NOT EXISTS anchors (
    id          BIGSERIAL PRIMARY KEY,
    group_id    UUID NOT NULL REFERENCES groups(id),
    entries     BIGINT NOT NULL,
    head        TEXT NOT NULL,
    tx_hash     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS anchors_group_idx ON anchors(group_id, entries DESC);
