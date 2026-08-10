CREATE SEQUENCE IF NOT EXISTS account_index_seq;

SELECT setval(
    'account_index_seq',
    GREATEST((SELECT COALESCE(MAX(account_index), 0) FROM accounts), 1),
    (SELECT COALESCE(MAX(account_index), 0) FROM accounts) > 0
);
