-- Screening provider choice set in the portal. One operator proposes, a
-- different one approves; the latest approved row is in force. With no
-- approved row the deployment's environment setting applies.
CREATE TABLE IF NOT EXISTS screening_provider_settings (
    id             uuid         PRIMARY KEY,
    providers      varchar(120) NOT NULL,
    reason         text         NOT NULL,
    status         varchar(20)  NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed', 'approved', 'rejected')),
    proposed_by    varchar(56)  NOT NULL,
    proposed_at    timestamptz  NOT NULL DEFAULT NOW(),
    decided_by     varchar(56),
    decided_at     timestamptz,
    decision_note  text         NOT NULL DEFAULT '',
    CHECK (status <> 'approved' OR decided_by <> proposed_by)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_screening_provider_settings_open ON screening_provider_settings ((true)) WHERE status = 'proposed';
CREATE INDEX IF NOT EXISTS idx_screening_provider_settings_approved ON screening_provider_settings (decided_at DESC) WHERE status = 'approved';
