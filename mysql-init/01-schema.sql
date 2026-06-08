-- Runs once when the MySQL container initialises an empty data directory
-- (mounted at /docker-entrypoint-initdb.d/ via docker-compose.yml). If you
-- change the schema or seed, wipe ./mysql-data and `docker compose up -d`
-- to re-init.

CREATE TABLE IF NOT EXISTS transactions (
    id          VARCHAR(36)     NOT NULL,
    amount      DECIMAL(15,2)   NOT NULL,
    currency    CHAR(3)         NOT NULL,
    status      VARCHAR(20)     NOT NULL,
    created_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT INTO transactions (id, amount, currency, status) VALUES
    ('00000000-0000-0000-0000-000000000001', 100.00, 'USD', 'completed'),
    ('00000000-0000-0000-0000-000000000002', 250.50, 'EUR', 'pending'),
    ('00000000-0000-0000-0000-000000000003',  42.00, 'BRL', 'completed'),
    ('00000000-0000-0000-0000-000000000004', 999.99, 'USD', 'failed'),
    ('00000000-0000-0000-0000-000000000005',  10.00, 'GBP', 'completed'),
    ('00000000-0000-0000-0000-000000000006', 500.00, 'EUR', 'pending'),
    ('00000000-0000-0000-0000-000000000007',  75.25, 'USD', 'completed'),
    ('00000000-0000-0000-0000-000000000008', 320.00, 'BRL', 'completed'),
    ('00000000-0000-0000-0000-000000000009',  15.50, 'EUR', 'refunded'),
    ('00000000-0000-0000-0000-000000000010', 200.00, 'USD', 'completed');
