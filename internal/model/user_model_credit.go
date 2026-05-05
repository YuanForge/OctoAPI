package model

import "time"

// UserModelCredit 记录用户针对某个模型（渠道展示名/路由键）的专属积分余额。
// model_name 与渠道的路由键对应：display_name 非空时为 display_name，否则为 model。
type UserModelCredit struct {
	ID        int64     `xorm:"pk autoincr 'id'" json:"id"`
	UserID    int64     `xorm:"notnull index 'user_id'" json:"user_id"`
	ModelName string    `xorm:"notnull 'model_name'" json:"model_name"`
	Credits   int64     `xorm:"notnull default(0) 'credits'" json:"credits"`
	CreatedAt time.Time `xorm:"created 'created_at'" json:"created_at"`
	UpdatedAt time.Time `xorm:"updated 'updated_at'" json:"updated_at"`
}

func (*UserModelCredit) TableName() string { return "user_model_credits" }
