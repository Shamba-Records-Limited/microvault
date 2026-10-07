-- Pilot access list. While ENABLE_PILOT_ACCESS_GATE is on, a number may
-- register, and a registered user may keep using the service, only while an
-- active row matches both its phone number and national ID. One person may
-- hold several rows (a new SIM is re-approved as a new row); revoking acts on
-- every row sharing the national ID. revoked_at set means the row is absent.
-- phone_number has the users.mobile_number form: country code and subscriber
-- digits, no leading +.

CREATE TABLE IF NOT EXISTS pilot_users (
    phone_number      text PRIMARY KEY,
    national_id       text NOT NULL,
    full_name         text NOT NULL,
    language          text NOT NULL DEFAULT 'en',
    note              text,
    added_by          text,
    created_at        timestamp NOT NULL DEFAULT NOW(),
    updated_at        timestamp NOT NULL DEFAULT NOW(),
    revoked_at        timestamp,
    revoked_reason    text,
    invited_at        timestamp,
    invite_count      integer NOT NULL DEFAULT 0,
    last_invite_error text
);

CREATE INDEX IF NOT EXISTS idx_pilot_users_national_id ON pilot_users (national_id);
