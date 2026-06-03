package handler

import (
	"fanapi/internal/db"
	"fanapi/internal/model"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"time"
)

// GetAdminStats GET /admin/stats
func GetAdminStats(c *gin.Context) {
	totalChannels, _ := db.Engine.Count(new(model.Channel))
	activeChannels, _ := db.Engine.Where("is_active = true").Count(new(model.Channel))
	totalUsers, _ := db.Engine.Where("role = 'user'").Count(new(model.User))

	type sumRow struct {
		Revenue int64
		Cost    int64
		Count   int64
	}

	var todayRow, totalRow sumRow

	todayStart, todayEnd := shanghaiDayRange(time.Now())
	// revenue = charge(图片/视频/音频一次性扣费) + settle(LLM实际结算) - refund(退款)
	// cost    = 对应类型的上游成本（refund 抄销对应的预写成本）
	db.Engine.SQL(`SELECT
		COALESCE(SUM(CASE
			WHEN type IN ('charge','settle','hold') THEN credits
			WHEN type = 'refund' THEN -credits
			ELSE 0 END),0) AS revenue,
		COALESCE(SUM(CASE
			WHEN type IN ('charge','settle','hold') THEN cost
			WHEN type = 'refund' THEN -cost
			ELSE 0 END),0) AS cost,
		COUNT(*) AS count
	FROM billing_transactions
	WHERE type IN ('charge','settle','hold','refund') AND created_at >= ? AND created_at < ?`, todayStart, todayEnd).Get(&todayRow)

	db.Engine.SQL(`SELECT
		COALESCE(SUM(CASE
			WHEN type IN ('charge','settle','hold') THEN credits
			WHEN type = 'refund' THEN -credits
			ELSE 0 END),0) AS revenue,
		COALESCE(SUM(CASE
			WHEN type IN ('charge','settle','hold') THEN cost
			WHEN type = 'refund' THEN -cost
			ELSE 0 END),0) AS cost,
		COUNT(*) AS count
	FROM billing_transactions
	WHERE type IN ('charge','settle','hold','refund')`).Get(&totalRow)

	c.JSON(http.StatusOK, gin.H{
		"channels":        totalChannels,
		"active_channels": activeChannels,
		"users":           totalUsers,
		"today": gin.H{
			"revenue": todayRow.Revenue,
			"cost":    todayRow.Cost,
			"profit":  todayRow.Revenue - todayRow.Cost,
			"count":   todayRow.Count,
		},
		"total": gin.H{
			"revenue": totalRow.Revenue,
			"cost":    totalRow.Cost,
			"profit":  totalRow.Revenue - totalRow.Cost,
			"count":   totalRow.Count,
		},
	})
}

// GET /admin/stats/trend?days=7|30&dim=revenue|cost|profit|calls
// 返回近 N 天每日的指定维度数据
func GetAdminStatsTrend(c *gin.Context) {
	days := 7
	if d := c.Query("days"); d == "30" {
		days = 30
	}
	dim := c.DefaultQuery("dim", "revenue") // revenue / cost / profit / calls

	type dayRow struct {
		Day     string `xorm:"day"`
		Revenue int64  `xorm:"revenue"`
		Cost    int64  `xorm:"cost"`
		Calls   int64  `xorm:"calls"`
	}

	var rows []dayRow
	db.Engine.SQL(`
SELECT
  to_char(created_at AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD') AS day,
  COALESCE(SUM(CASE WHEN type IN ('charge','settle','hold') THEN credits WHEN type='refund' THEN -credits ELSE 0 END),0) AS revenue,
  COALESCE(SUM(CASE WHEN type IN ('charge','settle','hold') THEN cost WHEN type='refund' THEN -cost ELSE 0 END),0) AS cost,
  COUNT(*) AS calls
FROM billing_transactions
WHERE type IN ('charge','settle','hold','refund')
  AND created_at >= NOW() - INTERVAL '` + strconv.Itoa(days) + ` days'
GROUP BY day
ORDER BY day ASC
`).Find(&rows)

	type point struct {
		Label string  `json:"label"`
		Value float64 `json:"value"`
	}

	// 补全缺失的天（让曲线连续）
	now := time.Now().In(time.FixedZone("CST", 8*3600))
	dayMap := map[string]dayRow{}
	for _, r := range rows {
		dayMap[r.Day] = r
	}
	result := make([]point, 0, days)
	for i := days - 1; i >= 0; i-- {
		t := now.AddDate(0, 0, -i)
		label := t.Format("01-02")
		key := t.Format("2006-01-02")
		r := dayMap[key]
		var val float64
		switch dim {
		case "cost":
			val = creditsToCNY(r.Cost)
		case "profit":
			val = creditsToCNY(r.Revenue - r.Cost)
		case "calls":
			val = float64(r.Calls)
		default: // revenue
			val = creditsToCNY(r.Revenue)
		}
		result = append(result, point{Label: label, Value: val})
	}
	c.JSON(http.StatusOK, gin.H{"points": result, "dim": dim, "days": days})
}

// GET /admin/stats/top — 今日 TOP10 消耗用户、TOP 模型、TOP 渠道
func GetAdminStatsTop(c *gin.Context) {
	todayStart, todayEnd := shanghaiDayRange(time.Now())

	type topRow struct {
		ID    string  `xorm:"id"`
		Name  string  `xorm:"name"`
		Value float64 `xorm:"value"`
	}

	var topUsers, topModels, topChannels []topRow

	db.Engine.SQL(`
SELECT u.id::text AS id, COALESCE(u.username, u.email::text, u.id::text) AS name,
       COALESCE(SUM(CASE
           WHEN bt.type IN ('charge','settle','hold') THEN bt.credits
           WHEN bt.type = 'refund' THEN -bt.credits
           ELSE 0 END),0)::float8 / 1000000 AS value
FROM billing_transactions bt
JOIN users u ON u.id = bt.user_id
WHERE bt.type IN ('charge','settle','hold','refund')
  AND bt.created_at >= ? AND bt.created_at < ?
GROUP BY u.id, u.username, u.email
HAVING COALESCE(SUM(CASE
           WHEN bt.type IN ('charge','settle','hold') THEN bt.credits
           WHEN bt.type = 'refund' THEN -bt.credits
           ELSE 0 END),0) > 0
ORDER BY value DESC LIMIT 10`, todayStart, todayEnd).Find(&topUsers)

	db.Engine.SQL(`
SELECT COALESCE(ll.model,'(unknown)') AS id, COALESCE(ll.model,'(unknown)') AS name,
       COUNT(*)::float8 AS value
FROM llm_logs ll
WHERE ll.created_at >= ? AND ll.created_at < ?
GROUP BY ll.model
ORDER BY value DESC LIMIT 10`, todayStart, todayEnd).Find(&topModels)

	db.Engine.SQL(`
SELECT c.id::text AS id, COALESCE(c.display_name, c.name, c.id::text) AS name,
       COALESCE(SUM(CASE
           WHEN bt.type IN ('charge','settle','hold') THEN bt.credits
           WHEN bt.type = 'refund' THEN -bt.credits
           ELSE 0 END),0)::float8 / 1000000 AS value
FROM billing_transactions bt
JOIN channels c ON c.id = bt.channel_id
WHERE bt.type IN ('charge','settle','hold','refund')
  AND bt.created_at >= ? AND bt.created_at < ?
GROUP BY c.id, c.display_name, c.name
HAVING COALESCE(SUM(CASE
           WHEN bt.type IN ('charge','settle','hold') THEN bt.credits
           WHEN bt.type = 'refund' THEN -bt.credits
           ELSE 0 END),0) > 0
ORDER BY value DESC LIMIT 10`, todayStart, todayEnd).Find(&topChannels)

	toList := func(rows []topRow) []gin.H {
		out := make([]gin.H, 0, len(rows))
		for _, r := range rows {
			out = append(out, gin.H{"id": r.ID, "name": r.Name, "value": r.Value})
		}
		return out
	}
	c.JSON(http.StatusOK, gin.H{
		"users":    toList(topUsers),
		"models":   toList(topModels),
		"channels": toList(topChannels),
	})
}
