package models

import "time"

// User 后台用户（本平台仅 admin 一个管理员角色）。
type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"size:64;uniqueIndex" json:"username"`
	PasswordHash string    `gorm:"size:255" json:"-"`
	DisplayName  string    `gorm:"size:64" json:"display_name"`
	CreatedAt    time.Time `json:"created_at"`
}

// Venue 体育场馆。
type Venue struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	Name           string    `gorm:"size:128" json:"name"`
	SportType      string    `gorm:"size:32" json:"sport_type"` // basketball / football / badminton / swimming ...
	Capacity       int       `json:"capacity"`
	HourlyPrice    float64   `json:"hourly_price"`
	OpenHour       int       `json:"open_hour"`  // 开放起始小时，0-23
	CloseHour      int       `json:"close_hour"` // 关闭小时，1-24
	Status         string    `gorm:"size:16" json:"status"` // open / closed / maintenance
	RequireDeposit bool      `json:"require_deposit"`        // 是否需要押金
	DepositAmount  float64   `json:"deposit_amount"`         // 押金金额
	CreatedAt      time.Time `json:"created_at"`
}

// Customer 预订客户（使用钱包的终端用户）。
type Customer struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:64" json:"name"`
	Phone     string    `gorm:"size:32;uniqueIndex" json:"phone"`
	Status    string    `gorm:"size:16;default:active" json:"status"` // active / blacklisted
	CreatedAt time.Time `json:"created_at"`
}

// Wallet 用户钱包账户，一人一个。
type Wallet struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	CustomerID    uint      `gorm:"uniqueIndex" json:"customer_id"`
	Balance       float64   `gorm:"default:0" json:"balance"`        // 可用余额
	FrozenAmount  float64   `gorm:"default:0" json:"frozen_amount"`  // 已冻结金额
	TotalRecharge float64   `gorm:"default:0" json:"total_recharge"` // 累计充值
	TotalConsume  float64   `gorm:"default:0" json:"total_consume"`  // 累计消费
	TotalRefund   float64   `gorm:"default:0" json:"total_refund"`   // 累计退款
	Version       int       `gorm:"default:0" json:"-"`              // 乐观锁版本号
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// WalletTxType 钱包流水类型。
type WalletTxType string

const (
	TxRecharge       WalletTxType = "recharge"       // 充值
	TxConsume        WalletTxType = "consume"        // 消费扣款
	TxRefund         WalletTxType = "refund"         // 退款入账
	TxFreeze         WalletTxType = "freeze"         // 冻结
	TxUnfreeze       WalletTxType = "unfreeze"       // 解冻
	TxPenalty        WalletTxType = "penalty"        // 违约金扣款
	TxNoShowPenalty  WalletTxType = "no_show_penalty"// 爽约扣款
	TxDepositFreeze  WalletTxType = "deposit_freeze" // 押金冻结
	TxDepositRefund  WalletTxType = "deposit_refund" // 押金退还
	TxDepositDeduct  WalletTxType = "deposit_deduct" // 押金扣减
)

// WalletTransaction 钱包流水记录（所有余额变动全留痕）。
type WalletTransaction struct {
	ID            uint         `gorm:"primaryKey" json:"id"`
	WalletID      uint         `gorm:"index" json:"wallet_id"`
	CustomerID    uint         `gorm:"index" json:"customer_id"`
	TxType        WalletTxType `gorm:"size:32;index" json:"tx_type"`
	Amount        float64      `json:"amount"`         // 变动金额（正数为加，负数为减）
	BalanceBefore float64      `json:"balance_before"` // 变动前可用余额
	BalanceAfter  float64      `json:"balance_after"`  // 变动后可用余额
	FrozenBefore  float64      `json:"frozen_before"`  // 变动前冻结额
	FrozenAfter   float64      `json:"frozen_after"`   // 变动后冻结额
	RelatedType   string       `gorm:"size:32" json:"related_type"` // booking / deposit / reconciliation
	RelatedID     uint         `gorm:"index" json:"related_id"`     // 关联业务ID
	OrderNo       string       `gorm:"size:64;uniqueIndex" json:"order_no"` // 业务单号，幂等防护
	Remark        string       `gorm:"size:255" json:"remark"`
	CreatedAt     time.Time    `json:"created_at"`
}

// WalletFreeze 资金冻结明细（预订/押金冻结的具体记录）。
type WalletFreeze struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	WalletID    uint      `gorm:"index" json:"wallet_id"`
	CustomerID  uint      `gorm:"index" json:"customer_id"`
	Amount      float64   `json:"amount"`
	FreezeType  string    `gorm:"size:32" json:"freeze_type"` // booking / deposit
	RelatedType string    `gorm:"size:32" json:"related_type"`
	RelatedID   uint      `gorm:"index" json:"related_id"`
	Status      string    `gorm:"size:16;default:active" json:"status"` // active / unfrozen / deducted
	OrderNo     string    `gorm:"size:64;uniqueIndex" json:"order_no"`
	Remark      string    `gorm:"size:255" json:"remark"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CancellationRule 退订规则：按距开始时长分阶梯。
type CancellationRule struct {
	ID                  uint    `gorm:"primaryKey" json:"id"`
	VenueID             uint    `gorm:"index;default:0" json:"venue_id"` // 0 表示全局默认
	Name                string  `gorm:"size:128" json:"name"`
	MinHoursBeforeStart int     `json:"min_hours_before_start"` // 距开始最小小时（含）
	MaxHoursBeforeStart int     `json:"max_hours_before_start"` // 距开始最大小时（含），0 表示开始后
	RefundRate          float64 `json:"refund_rate"`            // 退款比例 0-1
	PenaltyRate         float64 `json:"penalty_rate"`           // 违约金比例 0-1（按订单金额）
	PenaltyFixed        float64 `json:"penalty_fixed"`          // 固定违约金（优先于比例）
	IsDefault           bool    `gorm:"default:false" json:"is_default"`
	Priority            int     `gorm:"default:0" json:"priority"`
}

// NoShowRule 爽约规则。
type NoShowRule struct {
	ID          uint    `gorm:"primaryKey" json:"id"`
	VenueID     uint    `gorm:"index;default:0" json:"venue_id"`
	PenaltyRate float64 `json:"penalty_rate"`    // 按订单金额比例扣
	PenaltyFixed float64 `json:"penalty_fixed"`  // 固定金额（优先）
	DeductDeposit bool  `gorm:"default:true" json:"deduct_deposit"` // 是否扣押金
}

// DepositStatus 押金状态。
type DepositStatus string

const (
	DepositFrozen   DepositStatus = "frozen"   // 已冻结
	DepositRefunded DepositStatus = "refunded" // 已退还
	DepositDeducted DepositStatus = "deducted" // 已扣减
	DepositPartial  DepositStatus = "partial"  // 部分扣减部分退还
)

// Deposit 押金记录，独立于普通消费的流转。
type Deposit struct {
	ID             uint          `gorm:"primaryKey" json:"id"`
	BookingID      uint          `gorm:"index;unique" json:"booking_id"`
	CustomerID     uint          `gorm:"index" json:"customer_id"`
	VenueID        uint          `gorm:"index" json:"venue_id"`
	Amount         float64       `json:"amount"`
	RefundedAmount float64       `gorm:"default:0" json:"refunded_amount"`
	DeductedAmount float64       `gorm:"default:0" json:"deducted_amount"`
	Status         DepositStatus `gorm:"size:16" json:"status"`
	FreezeOrderNo  string        `gorm:"size:64;uniqueIndex" json:"freeze_order_no"`
	Reason         string        `gorm:"size:255" json:"reason"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

// BookingStatus 预订状态扩展。
type BookingStatus string

const (
	BookingBooked    BookingStatus = "booked"    // 已预订
	BookingPaid      BookingStatus = "paid"      // 已支付（钱包扣费）
	BookingCancelled BookingStatus = "cancelled" // 已取消（退订）
	BookingCompleted BookingStatus = "completed" // 已完成
	BookingNoShow    BookingStatus = "no_show"   // 爽约
)

// Booking 场地预订。
type Booking struct {
	ID             uint          `gorm:"primaryKey" json:"id"`
	VenueID        uint          `gorm:"index" json:"venue_id"`
	CustomerID     uint          `gorm:"index;default:0" json:"customer_id"`
	CustomerName   string        `gorm:"size:64" json:"customer_name"`
	Phone          string        `gorm:"size:32" json:"phone"`
	BookDate       string        `gorm:"size:10;index" json:"book_date"` // YYYY-MM-DD
	StartHour      int           `json:"start_hour"`
	EndHour        int           `json:"end_hour"`
	Amount         float64       `json:"amount"`
	PaidAmount     float64       `gorm:"default:0" json:"paid_amount"`     // 实际支付金额
	RefundedAmount float64       `gorm:"default:0" json:"refunded_amount"` // 已退款金额
	PenaltyAmount  float64       `gorm:"default:0" json:"penalty_amount"`  // 已扣违约金
	DepositAmount  float64       `gorm:"default:0" json:"deposit_amount"`  // 押金金额
	Status         BookingStatus `gorm:"size:16" json:"status"`
	PayOrderNo     string        `gorm:"size:64" json:"pay_order_no"`
	RefundOrderNo  string        `gorm:"size:64" json:"refund_order_no"`
	CancelReason   string        `gorm:"size:255" json:"cancel_reason"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

// ReconciliationStatus 对账状态。
type ReconciliationStatus string

const (
	ReconMatched   ReconciliationStatus = "matched"
	ReconMismatch  ReconciliationStatus = "mismatch"
	ReconPending   ReconciliationStatus = "pending"
)

// Reconciliation 每日对账汇总。
type Reconciliation struct {
	ID                 uint                 `gorm:"primaryKey" json:"id"`
	ReconDate          string               `gorm:"size:10;uniqueIndex" json:"recon_date"`
	ExpectedIncome     float64              `json:"expected_income"`      // 预订应收
	ActualIncome       float64              `json:"actual_income"`        // 钱包实收
	TotalRefund        float64              `json:"total_refund"`         // 退款总额
	TotalPenalty       float64              `json:"total_penalty"`        // 违约金总额
	TotalDepositIn     float64              `json:"total_deposit_in"`     // 押金冻结
	TotalDepositOut    float64              `json:"total_deposit_out"`    // 押金退还
	TotalDepositDeduct float64              `json:"total_deposit_deduct"` // 押金扣减
	TotalRecharge      float64              `json:"total_recharge"`
	DiffAmount         float64              `json:"diff_amount"`
	Status             ReconciliationStatus `gorm:"size:16" json:"status"`
	CreatedAt          time.Time            `json:"created_at"`
	UpdatedAt          time.Time            `json:"updated_at"`
}

// ReconDiff 对账差异明细。
type ReconDiff struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	ReconID     uint      `gorm:"index" json:"recon_id"`
	ReconDate   string    `gorm:"size:10;index" json:"recon_date"`
	RelatedType string    `gorm:"size:32" json:"related_type"` // booking / wallet_tx / deposit
	RelatedID   uint      `json:"related_id"`
	FieldName   string    `gorm:"size:64" json:"field_name"`
	Expected    float64   `json:"expected"`
	Actual      float64   `json:"actual"`
	Diff        float64   `json:"diff"`
	Remark      string    `gorm:"size:255" json:"remark"`
	CreatedAt   time.Time `json:"created_at"`
}
