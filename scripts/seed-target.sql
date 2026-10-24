-- Development-only seed for the managed target database.
-- Run this against targetdb, not the platform database.

BEGIN;

CREATE TABLE IF NOT EXISTS public.customers (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE
);

ALTER TABLE public.customers
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

INSERT INTO public.customers (name, email)
VALUES
    ('Alice', 'alice@example.com'),
    ('Bob', 'bob@example.com')
ON CONFLICT (email) DO NOTHING;

CREATE TABLE IF NOT EXISTS public.orders (
    id SERIAL PRIMARY KEY,
    customer_id INTEGER NOT NULL REFERENCES public.customers(id),
    order_number TEXT NOT NULL UNIQUE,
    total_cents INTEGER NOT NULL CHECK (total_cents >= 0),
    status TEXT NOT NULL CHECK (status IN ('pending', 'paid', 'cancelled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO public.orders (
    customer_id,
    order_number,
    total_cents,
    status
)
SELECT
    customer.id,
    seed.order_number,
    seed.total_cents,
    seed.status
FROM (
    VALUES
        ('alice@example.com', 'ORD-1001', 129900, 'paid'),
        ('bob@example.com', 'ORD-1002', 49900, 'pending')
) AS seed(email, order_number, total_cents, status)
JOIN public.customers AS customer ON customer.email = seed.email
ON CONFLICT (order_number) DO NOTHING;

COMMIT;
