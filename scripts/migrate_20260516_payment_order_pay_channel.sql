-- 为 payment_orders 表新增 pay_channel 字段
-- 执行方式：psql -U <user> -d <db> -f scripts/migrate_20260516_payment_order_pay_channel.sql

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS pay_channel VARCHAR(64) NOT NULL DEFAULT '';
