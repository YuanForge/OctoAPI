-- 提现复审上传打款凭证字段
ALTER TABLE withdraw_requests ADD COLUMN IF NOT EXISTS proof_url TEXT NOT NULL DEFAULT '';
ALTER TABLE withdraw_requests ADD COLUMN IF NOT EXISTS proof_note TEXT NOT NULL DEFAULT '';
