-- 用户专属模型积分表
-- model_name: 渠道的路由键（display_name 非空时为 display_name，否则为 model）
-- credits: 剩余积分（内部单位，1元 = 1,000,000 credits）
CREATE TABLE IF NOT EXISTS user_model_credits (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL,
    model_name  VARCHAR(255) NOT NULL,
    credits     BIGINT NOT NULL DEFAULT 0 CHECK (credits >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_user_model_credits UNIQUE (user_id, model_name)
);
CREATE INDEX IF NOT EXISTS idx_umc_user_id ON user_model_credits (user_id);
