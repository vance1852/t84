package services

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"venue-booking-admin/internal/models"
)

// CancellationService 退订规则引擎。
type CancellationService struct {
	DB *gorm.DB
}

// NewCancellationService 创建退订服务。
func NewCancellationService(db *gorm.DB) *CancellationService {
	return &CancellationService{DB: db}
}

// CancelResult 退订计算结果。
type CancelResult struct {
	CanCancel       bool
	RefundAmount    float64
	PenaltyAmount   float64
	RuleID          uint
	RuleName        string
	Reason          string
}

// calculateHoursBeforeStart 计算距开始时间的小时数。
func calculateHoursBeforeStart(booking *models.Booking) (float64, error) {
	bookTime, err := time.ParseInLocation("2006-01-02", booking.BookDate, time.Local)
	if err != nil {
		return 0, err
	}
	bookTime = bookTime.Add(time.Duration(booking.StartHour) * time.Hour)
	return time.Until(bookTime).Hours(), nil
}

// CalculateCancellation 根据预订和规则计算退订方案。
func (s *CancellationService) CalculateCancellation(booking *models.Booking) (*CancelResult, error) {
	if booking.Status == models.BookingCancelled {
		return &CancelResult{CanCancel: false, Reason: "该预订已取消"}, nil
	}
	if booking.Status == models.BookingCompleted {
		return &CancelResult{CanCancel: false, Reason: "该预订已完成"}, nil
	}
	if booking.Status == models.BookingNoShow {
		return &CancelResult{CanCancel: false, Reason: "该预订已判定爽约"}, nil
	}

	hoursBefore, err := calculateHoursBeforeStart(booking)
	if err != nil {
		return nil, err
	}

	// 已开始
	if hoursBefore <= 0 {
		return &CancelResult{CanCancel: false, Reason: "预订已开始，不可取消"}, nil
	}

	// 优先找场馆专属规则，其次找全局规则
	var rules []models.CancellationRule
	s.DB.Where("venue_id = ? OR venue_id = 0", booking.VenueID).
		Order("venue_id desc, priority desc").
		Find(&rules)

	if len(rules) == 0 {
		// 默认规则：提前24h全额退，2-24h退80%收20%违约金，2h内不退
		return s.defaultCancellation(hoursBefore, booking)
	}

	for _, rule := range rules {
		minH := float64(rule.MinHoursBeforeStart)
		maxH := float64(rule.MaxHoursBeforeStart)
		if maxH == 0 {
			maxH = 999999
		}
		if hoursBefore >= minH && hoursBefore < maxH {
			refund := booking.Amount * rule.RefundRate
			penalty := rule.PenaltyFixed
			if penalty <= 0 {
				penalty = booking.Amount * rule.PenaltyRate
			}
			return &CancelResult{
				CanCancel:     true,
				RefundAmount:  refund,
				PenaltyAmount: penalty,
				RuleID:        rule.ID,
				RuleName:      rule.Name,
			}, nil
		}
	}

	return s.defaultCancellation(hoursBefore, booking)
}

// defaultCancellation 默认退订规则。
func (s *CancellationService) defaultCancellation(hoursBefore float64, booking *models.Booking) (*CancelResult, error) {
	switch {
	case hoursBefore >= 24:
		return &CancelResult{
			CanCancel:     true,
			RefundAmount:  booking.Amount,
			PenaltyAmount: 0,
			RuleName:      "默认规则-提前24h以上全额退款",
		}, nil
	case hoursBefore >= 2:
		penalty := booking.Amount * 0.2
		return &CancelResult{
			CanCancel:     true,
			RefundAmount:  booking.Amount - penalty,
			PenaltyAmount: penalty,
			RuleName:      "默认规则-提前2-24h退款80%",
		}, nil
	default:
		return &CancelResult{
			CanCancel:     true,
			RefundAmount:  0,
			PenaltyAmount: booking.Amount,
			RuleName:      "默认规则-不足2h不予退款",
		}, nil
	}
}

// CreateRule 创建退订规则。
func (s *CancellationService) CreateRule(rule *models.CancellationRule) error {
	if rule.RefundRate < 0 || rule.RefundRate > 1 {
		return errors.New("退款比例须在0-1之间")
	}
	if rule.PenaltyRate < 0 || rule.PenaltyRate > 1 {
		return errors.New("违约金比例须在0-1之间")
	}
	return s.DB.Create(rule).Error
}

// ListRules 查询退订规则列表。
func (s *CancellationService) ListRules(venueID uint) ([]models.CancellationRule, error) {
	var rules []models.CancellationRule
	q := s.DB.Order("venue_id desc, priority desc, min_hours_before_start desc")
	if venueID > 0 {
		q = q.Where("venue_id = ? OR venue_id = 0", venueID)
	}
	err := q.Find(&rules).Error
	return rules, err
}

// UpdateRule 更新退订规则。
func (s *CancellationService) UpdateRule(id uint, updates map[string]interface{}) error {
	return s.DB.Model(&models.CancellationRule{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteRule 删除退订规则。
func (s *CancellationService) DeleteRule(id uint) error {
	return s.DB.Delete(&models.CancellationRule{}, id).Error
}

// NoShowResult 爽约计算结果。
type NoShowResult struct {
	PenaltyAmount   float64
	DeductDeposit   bool
}

// CalculateNoShow 计算爽约处理方案。
func (s *CancellationService) CalculateNoShow(booking *models.Booking) (*NoShowResult, error) {
	var rule models.NoShowRule
	err := s.DB.Where("venue_id = ? OR venue_id = 0", booking.VenueID).
		Order("venue_id desc").First(&rule).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// 默认爽约规则：扣50%且扣押金
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &NoShowResult{
			PenaltyAmount: booking.Amount * 0.5,
			DeductDeposit: true,
		}, nil
	}

	penalty := rule.PenaltyFixed
	if penalty <= 0 {
		penalty = booking.Amount * rule.PenaltyRate
	}
	return &NoShowResult{
		PenaltyAmount: penalty,
		DeductDeposit: rule.DeductDeposit,
	}, nil
}

// CreateNoShowRule 创建爽约规则。
func (s *CancellationService) CreateNoShowRule(rule *models.NoShowRule) error {
	return s.DB.Create(rule).Error
}

// ListNoShowRules 查询爽约规则。
func (s *CancellationService) ListNoShowRules(venueID uint) ([]models.NoShowRule, error) {
	var rules []models.NoShowRule
	q := s.DB.Order("venue_id desc")
	if venueID > 0 {
		q = q.Where("venue_id = ? OR venue_id = 0", venueID)
	}
	err := q.Find(&rules).Error
	return rules, err
}
