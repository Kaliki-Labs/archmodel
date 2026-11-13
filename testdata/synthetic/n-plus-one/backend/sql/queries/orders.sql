-- name: ListOrderRows :many
SELECT id, customer_id FROM orders ORDER BY created_at DESC LIMIT 50;

-- name: GetCustomer :one
SELECT id, name FROM customers WHERE id = $1;

-- name: GetOrderRow :one
SELECT id, customer_id FROM orders WHERE id = $1;

-- name: ListCustomersByIDs :many
SELECT id, name FROM customers WHERE id = ANY($1::uuid[]);

-- name: CountOrders :one
SELECT count(*) FROM orders;
