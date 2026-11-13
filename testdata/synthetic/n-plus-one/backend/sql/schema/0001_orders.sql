-- +goose Up
CREATE TABLE customers (id UUID PRIMARY KEY, name TEXT NOT NULL);
CREATE TABLE orders (
    id UUID PRIMARY KEY,
    customer_id UUID NOT NULL REFERENCES customers(id),
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_orders_customer ON orders (customer_id, created_at);
-- +goose Down
DROP TABLE orders;
DROP TABLE customers;
