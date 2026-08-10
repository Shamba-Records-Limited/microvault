-- Optional SEP-9 customer fields, used to prefill MoneyGram's cash-pickup KYC
-- webview. All nullable — collected opt-in during USSD registration.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS birth_date        DATE,
    ADD COLUMN IF NOT EXISTS address           VARCHAR(255),
    ADD COLUMN IF NOT EXISTS city              VARCHAR(255),
    ADD COLUMN IF NOT EXISTS postal_code       VARCHAR(20);
