-- DDL only. Bulk seed lives in 02-seed.sql so it can be skipped/swapped
-- independently. Both files run once when the MySQL container initialises
-- an empty data directory.
--
-- To re-apply schema/seed: `docker compose down && rm -rf mysql-data &&
-- docker compose up -d`.
--
-- Notes:
--   * `transaction` is a MySQL reserved word — always backtick when used as
--     a table identifier.
--   * `value` is DOUBLE (maps cleanly to Go float64). DECIMAL would be
--     correct for money but slower; DOUBLE was chosen per the benchmark
--     goal.
--   * cart_snapshot is 1:1 with `transaction` (UNIQUE FK).

CREATE TABLE customer (
    id          CHAR(36)        NOT NULL,
    nome        VARCHAR(100)    NOT NULL,
    create_date DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `transaction` (
    id          CHAR(36)        NOT NULL,
    value       DOUBLE          NOT NULL,
    customer_id CHAR(36)        NOT NULL,
    create_date DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_transaction_customer (customer_id),
    CONSTRAINT fk_transaction_customer
        FOREIGN KEY (customer_id) REFERENCES customer (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE cart_snapshot (
    id             CHAR(36)     NOT NULL,
    transaction_id CHAR(36)     NOT NULL,
    create_date    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uniq_cart_snapshot_transaction (transaction_id),
    CONSTRAINT fk_cart_snapshot_transaction
        FOREIGN KEY (transaction_id) REFERENCES `transaction` (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
