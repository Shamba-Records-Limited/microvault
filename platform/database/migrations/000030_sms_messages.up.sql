-- Every outbound SMS with the gateway's message id and its last delivery
-- status. The body is not kept; kind and ref say what it was.
CREATE TABLE IF NOT EXISTS sms_messages (
    id                   uuid          PRIMARY KEY,
    provider             varchar(20)   NOT NULL,
    provider_message_id  varchar(100),
    phone_number         varchar(20)   NOT NULL,
    kind                 varchar(40)   NOT NULL,
    ref                  varchar(64)   NOT NULL DEFAULT '',
    status               varchar(24)   NOT NULL,
    failure_reason       varchar(200)  NOT NULL DEFAULT '',
    network_code         varchar(10)   NOT NULL DEFAULT '',
    cost                 varchar(24)   NOT NULL DEFAULT '',
    created_at           timestamptz   NOT NULL DEFAULT NOW(),
    updated_at           timestamptz   NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_sms_messages_provider_id ON sms_messages (provider_message_id) WHERE provider_message_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sms_messages_ref ON sms_messages (ref, created_at DESC) WHERE ref <> '';
CREATE INDEX IF NOT EXISTS idx_sms_messages_phone ON sms_messages (phone_number, kind, created_at DESC);
