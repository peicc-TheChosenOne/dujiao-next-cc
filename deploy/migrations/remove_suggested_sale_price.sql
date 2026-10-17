-- Run once, after all application instances have stopped using the old schema.
-- Compatible with PostgreSQL and the application's SQLite runtime.
ALTER TABLE product_skus DROP COLUMN suggested_sale_price_amount;
