-- 添加 card_batch_id 字段，建立 cards 和 card_batches 的直接关联
ALTER TABLE cards ADD COLUMN IF NOT EXISTS card_batch_id BIGINT DEFAULT 0;

-- 为新数据和后续操作建立索引，提升作废操作性能
CREATE INDEX IF NOT EXISTS idx_cards_card_batch_id ON cards(card_batch_id) WHERE status != 'used';
