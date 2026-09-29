CREATE TABLE metric_series (
    id          INTEGER PRIMARY KEY,
    key         TEXT    NOT NULL UNIQUE,
    name        TEXT    NOT NULL,
    unit        TEXT    NOT NULL DEFAULT '',
    source      TEXT    NOT NULL DEFAULT '',
    labels_json TEXT    NOT NULL DEFAULT '{}',
    created_at  INTEGER NOT NULL,
    first_ts    INTEGER,
    last_ts     INTEGER
);

CREATE TABLE metric_samples (
    id        INTEGER PRIMARY KEY,
    series_id INTEGER NOT NULL REFERENCES metric_series(id) ON DELETE CASCADE,
    ts        INTEGER NOT NULL,
    value     REAL    NOT NULL,
    min       REAL    NOT NULL,
    max       REAL    NOT NULL,
    count     INTEGER NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX idx_metric_samples_series_ts ON metric_samples(series_id, ts);
CREATE INDEX idx_metric_samples_ts ON metric_samples(ts);

CREATE VIEW metric_samples_view AS
SELECT m.id, m.ts AS timestamp, s.name AS metric_name, m.value, s.unit, s.source, s.labels_json
FROM metric_samples m JOIN metric_series s ON s.id = m.series_id;

CREATE TABLE service_checks (
    id          INTEGER PRIMARY KEY,
    service_id  TEXT    NOT NULL,
    ts          INTEGER NOT NULL,
    status      TEXT    NOT NULL,
    ok          INTEGER NOT NULL,
    latency_ms  REAL,
    status_code INTEGER,
    error       TEXT
);
CREATE INDEX idx_service_checks_service_ts ON service_checks(service_id, ts);
CREATE INDEX idx_service_checks_ts ON service_checks(ts);

CREATE TABLE alert_rules (
    id               TEXT    PRIMARY KEY,
    name             TEXT    NOT NULL,
    metric_name      TEXT    NOT NULL,
    operator         TEXT    NOT NULL,
    threshold        REAL    NOT NULL,
    duration_seconds INTEGER NOT NULL,
    clear_seconds    INTEGER NOT NULL DEFAULT 0,
    hysteresis       REAL    NOT NULL DEFAULT 0,
    severity         TEXT    NOT NULL,
    enabled          INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);

CREATE TABLE alert_events (
    id              TEXT    PRIMARY KEY,
    rule_id         TEXT    NOT NULL,
    rule_name       TEXT    NOT NULL,
    metric_name     TEXT    NOT NULL,
    severity        TEXT    NOT NULL,
    message         TEXT    NOT NULL,
    threshold       REAL    NOT NULL,
    unit            TEXT    NOT NULL DEFAULT '',
    started_at      INTEGER NOT NULL,
    ended_at        INTEGER,
    acknowledged_at INTEGER,
    peak_value      REAL,
    last_value      REAL
);
CREATE INDEX idx_alert_events_started ON alert_events(started_at);
CREATE INDEX idx_alert_events_open ON alert_events(rule_id) WHERE ended_at IS NULL;
