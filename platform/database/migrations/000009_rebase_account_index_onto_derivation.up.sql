-- Rebase accounts.account_index onto the actual BIP-44 derivation index.
--
-- Child keypairs have been derived at m/44'/148'/(account_index + 4)' since
-- 051d0b3, where the +4 was a manual index-skip during early testing —
-- introduced with the comment "Remove incremented index (used for testing)"
-- and never removed. account_index_seq later solved the same problem properly
-- (monotonic, never rewinds on row deletion or rollback), making the offset
-- redundant.
--
-- The consequence was that account_index did NOT identify the key: the stored
-- value was always four short of the index the address was derived at. This
-- adds the offset into the column so the invariant finally holds:
--
--     accounts.public_key == derive(m/44'/148'/account_index')
--
-- Run this together with the code change that drops the two +4 offsets. In a
-- window where the DB is migrated but the old code still runs, derivation
-- computes (stored+4)+4 and every address check fails.
UPDATE accounts SET account_index = account_index + 4;

-- The sequence hands out stored indices, so it has to clear the rebased
-- maximum. Advancing it also steps past derivation indices already burned
-- on-chain, which cannot be reclaimed by deleting rows.
SELECT setval('account_index_seq', (SELECT COALESCE(MAX(account_index), 1) FROM accounts));

-- Deliberately NOT touched: loans.ramp_child_account_index.
--
-- That column is a per-loan copy taken at initiation and is what the MoneyGram
-- poller re-derives the SEP-10 child memo from. Leaving it frozen keeps every
-- in-flight withdrawal on the exact memo MoneyGram already holds. Future loans
-- will copy the rebased index and therefore compute a different memo — a new
-- anchor-side scope for that user, which is expected.
