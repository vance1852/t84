package services

import (
	"gorm.io/gorm"

	"venue-booking-admin/internal/models"
)

// StatsService 统计报表服务。
type StatsService struct {
	DB *gorm.DB
}

// NewStatsService 创建统计服务。
func NewStatsService(db *gorm.DB) *StatsService {
	return &StatsService{DB: db}
}

// CancellationStats 退订统计。
type CancellationStats struct {
	TotalBookings    int64   `json:"total_bookings"`
	CancelledCount   int64   `json:"cancelled_count"`
	CancellationRate float64 `json:"cancellation_rate"`
	TotalRefund      float64 `json:"total_refund"`
	TotalPenalty     float64 `json:"total_penalty"`
}

// GetCancellationStats 退订统计。
func (s *StatsService) GetCancellationStats(startDate, endDate string, venueID uint) (*CancellationStats, error) {
	stats := &CancellationStats{}

	q := s.DB.Model(&models.Booking{})
	if startDate != "" {
		q = q.Where("book_date >= ?", startDate)
	}
	if endDate != "" {
		q = q.Where("book_date <= ?", endDate)
	}
	if venueID > 0 {
		q = q.Where("venue_id = ?", venueID)
	}
	q.Count(&stats.TotalBookings)

	cancelQ := s.DB.Model(&models.Booking{}).Where("status = ?", models.BookingCancelled)
	if startDate != "" {
		cancelQ = cancelQ.Where("book_date >= ?", startDate)
	}
	if endDate != "" {
		cancelQ = cancelQ.Where("book_date <= ?", endDate)
	}
	if venueID > 0 {
		cancelQ = cancelQ.Where("venue_id = ?", venueID)
	}
	cancelQ.Count(&stats.CancelledCount)
	cancelQ.Select("COALESCE(SUM(refunded_amount), 0)").Scan(&stats.TotalRefund)
	cancelQ.Select("COALESCE(SUM(penalty_amount), 0)").Scan(&stats.TotalPenalty)

	if stats.TotalBookings > 0 {
		stats.CancellationRate = float64(stats.CancelledCount) / float64(stats.TotalBookings)
	}
	return stats, nil
}

// NoShowStats 爽约统计。
type NoShowStats struct {
	TotalBookings    int64   `json:"total_bookings"`
	NoShowCount      int64   `json:"no_show_count"`
	NoShowRate       float64 `json:"no_show_rate"`
	TotalPenalty     float64 `json:"total_penalty"`
	DepositDeducted  float64 `json:"deposit_deducted"`
}

// GetNoShowStats 爽约统计。
func (s *StatsService) GetNoShowStats(startDate, endDate string, venueID uint) (*NoShowStats, error) {
	stats := &NoShowStats{}

	q := s.DB.Model(&models.Booking{})
	if startDate != "" {
		q = q.Where("book_date >= ?", startDate)
	}
	if endDate != "" {
		q = q.Where("book_date <= ?", endDate)
	}
	if venueID > 0 {
		q = q.Where("venue_id = ?", venueID)
	}
	q.Count(&stats.TotalBookings)

	nsQ := s.DB.Model(&models.Booking{}).Where("status = ?", models.BookingNoShow)
	if startDate != "" {
		nsQ = nsQ.Where("book_date >= ?", startDate)
	}
	if endDate != "" {
		nsQ = nsQ.Where("book_date <= ?", endDate)
	}
	if venueID > 0 {
		nsQ = nsQ.Where("venue_id = ?", venueID)
	}
	nsQ.Count(&stats.NoShowCount)
	nsQ.Select("COALESCE(SUM(penalty_amount), 0)").Scan(&stats.TotalPenalty)

	if stats.TotalBookings > 0 {
		stats.NoShowRate = float64(stats.NoShowCount) / float64(stats.TotalBookings)
	}

	depQ := s.DB.Model(&models.Deposit{}).Where("status IN ?", []models.DepositStatus{models.DepositDeducted, models.DepositPartial})
	if venueID > 0 {
		depQ = depQ.Where("venue_id = ?", venueID)
	}
	depQ.Select("COALESCE(SUM(deducted_amount), 0)").Scan(&stats.DepositDeducted)

	return stats, nil
}

// RevenueStats 营收统计。
type RevenueStats struct {
	TotalIncome       float64 `json:"total_income"`        // 预订收入
	TotalRecharge     float64 `json:"total_recharge"`      // 充值总额
	TotalRefund       float64 `json:"total_refund"`        // 退款总额
	TotalPenalty      float64 `json:"total_penalty"`       // 违约金收入
	NetIncome         float64 `json:"net_income"`          // 净收入 = 收入+违约金-退款
	CompletedCount    int64   `json:"completed_count"`     // 已完成订单数
	PaidCount         int64   `json:"paid_count"`          // 已支付订单数
	DepositHeld       float64 `json:"deposit_held"`        // 仍在冻结的押金
}

// GetRevenueStats 营收统计。
func (s *StatsService) GetRevenueStats(startDate, endDate string, venueID uint) (*RevenueStats, error) {
	stats := &RevenueStats{}

	paidQ := s.DB.Model(&models.Booking{}).Where("status IN ?",
		[]models.BookingStatus{models.BookingPaid, models.BookingCompleted, models.BookingNoShow})
	if startDate != "" {
		paidQ = paidQ.Where("created_at >= ?", startDate)
	}
	if endDate != "" {
		paidQ = paidQ.Where("created_at <= ?", endDate)
	}
	if venueID > 0 {
		paidQ = paidQ.Where("venue_id = ?", venueID)
	}
	paidQ.Count(&stats.PaidCount)
	paidQ.Select("COALESCE(SUM(paid_amount), 0)").Scan(&stats.TotalIncome)

	compQ := s.DB.Model(&models.Booking{}).Where("status = ?", models.BookingCompleted)
	if venueID > 0 {
		compQ = compQ.Where("venue_id = ?", venueID)
	}
	if startDate != "" {
		compQ = compQ.Where("created_at >= ?", startDate)
	}
	if endDate != "" {
		compQ = compQ.Where("created_at <= ?", endDate)
	}
	compQ.Count(&stats.CompletedCount)

	cancelQ := s.DB.Model(&models.Booking{}).Where("status = ?", models.BookingCancelled)
	if venueID > 0 {
		cancelQ = cancelQ.Where("venue_id = ?", venueID)
	}
	if startDate != "" {
		cancelQ = cancelQ.Where("created_at >= ?", startDate)
	}
	if endDate != "" {
		cancelQ = cancelQ.Where("created_at <= ?", endDate)
	}
	cancelQ.Select("COALESCE(SUM(refunded_amount), 0)").Scan(&stats.TotalRefund)

	penaltyQ := s.DB.Model(&models.Booking{})
	if venueID > 0 {
		penaltyQ = penaltyQ.Where("venue_id = ?", venueID)
	}
	if startDate != "" {
		penaltyQ = penaltyQ.Where("created_at >= ?", startDate)
	}
	if endDate != "" {
		penaltyQ = penaltyQ.Where("created_at <= ?", endDate)
	}
	penaltyQ.Select("COALESCE(SUM(penalty_amount), 0)").Scan(&stats.TotalPenalty)

	rechargeQ := s.DB.Model(&models.WalletTransaction{}).Where("tx_type = ?", models.TxRecharge)
	if startDate != "" {
		rechargeQ = rechargeQ.Where("created_at >= ?", startDate)
	}
	if endDate != "" {
		rechargeQ = rechargeQ.Where("created_at <= ?", endDate)
	}
	rechargeQ.Select("COALESCE(SUM(amount), 0)").Scan(&stats.TotalRecharge)

	depQ := s.DB.Model(&models.Deposit{}).Where("status = ?", models.DepositFrozen)
	if venueID > 0 {
		depQ = depQ.Where("venue_id = ?", venueID)
	}
	depQ.Select("COALESCE(SUM(amount - refunded_amount - deducted_amount), 0)").Scan(&stats.DepositHeld)

	stats.NetIncome = stats.TotalIncome + stats.TotalPenalty - stats.TotalRefund
	return stats, nil
}

// VenueBookingStats 按场馆维度的统计。
type VenueBookingStats struct {
	VenueID       uint    `json:"venue_id"`
	VenueName     string  `json:"venue_name"`
	BookingCount  int64   `json:"booking_count"`
	Revenue       float64 `json:"revenue"`
	CancelCount   int64   `json:"cancel_count"`
	NoShowCount   int64   `json:"no_show_count"`
}

// GetStatsByVenue 按场馆统计。
func (s *StatsService) GetStatsByVenue(startDate, endDate string) ([]VenueBookingStats, error) {
	var results []VenueBookingStats

	type rawStat struct {
		VenueID      uint
		BookingCount int64
		Revenue      float64
		CancelCount  int64
		NoShowCount  int64
	}

	var raws []rawStat
	q := s.DB.Model(&models.Booking{}).
		Select("venue_id, COUNT(*) as booking_count, SUM(paid_amount) as revenue, " +
			"SUM(CASE WHEN status = 'cancelled' THEN 1 ELSE 0 END) as cancel_count, " +
			"SUM(CASE WHEN status = 'no_show' THEN 1 ELSE 0 END) as no_show_count")
	if startDate != "" {
		q = q.Where("book_date >= ?", startDate)
	}
	if endDate != "" {
		q = q.Where("book_date <= ?", endDate)
	}
	q.Group("venue_id").Scan(&raws)

	for _, r := range raws {
		var venue models.Venue
		s.DB.Select("name").First(&venue, r.VenueID)
		results = append(results, VenueBookingStats{
			VenueID:      r.VenueID,
			VenueName:    venue.Name,
			BookingCount: r.BookingCount,
			Revenue:      r.Revenue,
			CancelCount:  r.CancelCount,
			NoShowCount:  r.NoShowCount,
		})
	}
	return results, nil
}
