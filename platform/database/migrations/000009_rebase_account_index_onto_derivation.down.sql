-- Restore the pre-rebase offset. Only valid alongside a rollback of the code
-- change: the +4 has to live either in the column or in deriveChildKeypair,
-- never in both and never in neither.
UPDATE accounts SET account_index = account_index - 4;

SELECT setval('account_index_seq', (SELECT COALESCE(MAX(account_index), 1) FROM accounts));
