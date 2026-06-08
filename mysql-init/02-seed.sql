-- Bulk seed.
--   * 50 000 customers, IDs 11111111-…-000000000001 .. 50000.
--   * 150 000 transactions (3 per customer), IDs 00000000-…-000000000001 .. 150000.
--   * 150 000 cart_snapshots (1 per transaction), IDs 22222222-…-000000000001 .. 150000.
--
-- Transaction N belongs to customer FLOOR((N-1)/3) + 1, so:
--   txs 1..3   → customer 1
--   txs 4..6   → customer 2
--   ...
-- This keeps the existing benchmark/integration target (id …000000000001) on
-- customer 1, just under a generated "Customer #1" name instead of "Leonardo".
--
-- Recursive CTE is fast (single-statement bulk INSERT). Default depth is
-- 1000 — bumped to fit the 150k rows.

SET cte_max_recursion_depth = 200000;

INSERT INTO customer (id, nome)
WITH RECURSIVE seq AS (
    SELECT 1 AS n
    UNION ALL
    SELECT n + 1 FROM seq WHERE n < 50000
)
SELECT
    CONCAT('11111111-1111-1111-1111-', LPAD(n, 12, '0')),
    CONCAT('Customer #', n)
FROM seq;

INSERT INTO `transaction` (id, value, customer_id)
WITH RECURSIVE seq AS (
    SELECT 1 AS n
    UNION ALL
    SELECT n + 1 FROM seq WHERE n < 150000
)
SELECT
    CONCAT('00000000-0000-0000-0000-', LPAD(n, 12, '0')),
    ROUND(RAND() * 1000, 2),
    CONCAT('11111111-1111-1111-1111-', LPAD(FLOOR((n - 1) / 3) + 1, 12, '0'))
FROM seq;

INSERT INTO cart_snapshot (id, transaction_id)
WITH RECURSIVE seq AS (
    SELECT 1 AS n
    UNION ALL
    SELECT n + 1 FROM seq WHERE n < 150000
)
SELECT
    CONCAT('22222222-2222-2222-2222-', LPAD(n, 12, '0')),
    CONCAT('00000000-0000-0000-0000-', LPAD(n, 12, '0'))
FROM seq;
