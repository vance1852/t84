package services

import (
	"errors"
	"strconv"
	"time"

	"gorm.io/gorm"

	"venue-booking-admin/internal/models"
)

// BookingService 预订服务，接入钱包全流程。
type BookingService struct {
	DB               *gorm.DB
	WalletSvc        *WalletService
	CancellationSvc  *CancellationService
	DepositSvc       *DepositService
}

// NewBookingService 创建预订服务。
func NewBookingService(db *gorm.DB, walletSvc *WalletService, cancelSvc *CancellationService, depositSvc *DepositService) *BookingService {
	return &BookingService{
		DB:              db,
		WalletSvc:       walletSvc,
		CancellationSvc: cancelSvc,
		DepositSvc:      depositSvc,
	}
}

// CreateBookingWithWallet 创建预订并从钱包扣款（或冻结）。
type CreateBookingParams struct {
	VenueID      uint
	CustomerID   uint
	CustomerName string
	Phone        string
	BookDate     string
	StartHour    int
	EndHour      int
	UseWallet    bool
}

func (s *BookingService) CreateBookingWithWallet(p CreateBookingParams) (*models.Booking, error) {
	if p.EndHour <= p.StartHour {
		return nil, errors.New("结束时间须晚于开始时间")
	}
	var venue models.Venue
	if err := s.DB.First(&venue, p.VenueID).Error; err != nil {
		return nil, errors.New("场馆不存在")
	}
	if venue.Status != "open" {
		return nil, errors.New("该场馆当前不可预订")
	}
	if p.StartHour < venue.OpenHour || p.EndHour > venue.CloseHour {
		return nil, errors.New("预订时段超出场馆开放时间")
	}

	var conflict int64
	s.DB.Model(&models.Booking{}).
		Where("venue_id = ? AND book_date = ? AND status <> ?", p.VenueID, p.BookDate, models.BookingCancelled).
		Where("start_hour < ? AND end_hour > ?", p.EndHour, p.StartHour).
		Count(&conflict)
	if conflict > 0 {
		return nil, errors.New("该时段已被预订")
	}

	amount := venue.HourlyPrice * float64(p.EndHour-p.StartHour)
	depositAmount := 0.0
	if venue.RequireDeposit {
		depositAmount = venue.DepositAmount
	}

	var booking *models.Booking

	err := s.DB.Transaction(func(tx *gorm.DB) error {
		booking = &models.Booking{
			VenueID:      p.VenueID,
			CustomerID:   p.CustomerID,
			CustomerName: p.CustomerName,
			Phone:        p.Phone,
			BookDate:     p.BookDate,
			StartHour:    p.StartHour,
			EndHour:      p.EndHour,
			Amount:       amount,
			DepositAmount: depositAmount,
			Status:       models.BookingBooked,
		}
		if err := tx.Create(booking).Error; err != nil {
			return err
		}

		if p.UseWallet && p.CustomerID > 0 {
			totalNeed := amount + depositAmount
			ok, bal, err := s.WalletSvc.CheckBalance(p.CustomerID, totalNeed)
			if err != nil {
				return err
			}
			if !ok {
				return errors.New("钱包余额不足，需要" + floatToStr(totalNeed) + "，当前" + floatToStr(bal))
			}

			payOrderNo := genOrderNo("BK")
			txRecord, err := s.WalletSvc.Deduct(p.CustomerID, amount, "booking", booking.ID, "预订支付")
			if err != nil {
				return err
			}
			booking.PaidAmount = amount
			booking.PayOrderNo = payOrderNo
			_ = txRecord

			if depositAmount > 0 {
				_, err := s.DepositSvc.FreezeDeposit(booking.ID, p.CustomerID, p.VenueID, depositAmount, "预订押金冻结")
				if err != nil {
					return err
				}
			}
			booking.Status = models.BookingPaid
			if err := tx.Save(booking).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return booking, err
}

func floatToStr(f float64) string {
	return strconv.FormatFloat(f, 'f', 2, 64)
}

// CancelBooking 取消预订：按规则计算退款/违约金，退款回钱包，释放时段，退还押金。
func (s *BookingService) CancelBooking(bookingID uint, reason string) (*models.Booking, *CancelResult, error) {
	var booking models.Booking
	if err := s.DB.First(&booking, bookingID).Error; err != nil {
		return nil, nil, errors.New("预订不存在")
	}

	result, err := s.CancellationSvc.CalculateCancellation(&booking)
	if err != nil {
		return nil, nil, err
	}
	if !result.CanCancel {
		return nil, result, errors.New(result.Reason)
	}

	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if booking.CustomerID > 0 && booking.PaidAmount > 0 && result.RefundAmount > 0 {
			refundOrderNo := genOrderNo("RF")
			_, err := s.WalletSvc.Refund(booking.CustomerID, result.RefundAmount, "booking", booking.ID, "退订退款: "+reason)
			if err != nil {
				return err
			}
			booking.RefundedAmount = result.RefundAmount
			booking.RefundOrderNo = refundOrderNo
		}

		if booking.CustomerID > 0 && result.PenaltyAmount > 0 {
			// 优先从退款中抵扣违约金
			if result.PenaltyAmount <= booking.PaidAmount-result.RefundAmount {
				// 已在退款计算中处理
			} else {
				extraPenalty := result.PenaltyAmount - (booking.PaidAmount - result.RefundAmount)
				if extraPenalty > 0 {
					_, err := s.WalletSvc.PenaltyDeduct(booking.CustomerID, extraPenalty, "booking", booking.ID, "退订违约金: "+reason)
					if err != nil {
						return err
					}
				}
			}
		}

		// 处理押金：退订时正常退还
		if booking.DepositAmount > 0 {
			deposit, err := s.DepositSvc.GetDepositByBooking(booking.ID)
			if err == nil && deposit != nil {
				if depErr := s.DepositSvc.RefundDeposit(deposit.ID, "退订押金退还"); depErr != nil {
					return depErr
				}
			}
		}

		booking.PenaltyAmount = result.PenaltyAmount
		booking.Status = models.BookingCancelled
		booking.CancelReason = reason
		return tx.Save(&booking).Error
	})

	return &booking, result, err
}

// MarkNoShow 标记爽约：按规则扣违约金、扣押金。
func (s *BookingService) MarkNoShow(bookingID uint, reason string) (*models.Booking, *NoShowResult, error) {
	var booking models.Booking
	if err := s.DB.First(&booking, bookingID).Error; err != nil {
		return nil, nil, errors.New("预订不存在")
	}
	if booking.Status != models.BookingPaid && booking.Status != models.BookingBooked {
		return nil, nil, errors.New("预订状态不符合爽约判定条件")
	}

	result, err := s.CancellationSvc.CalculateNoShow(&booking)
	if err != nil {
		return nil, nil, err
	}

	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if booking.CustomerID > 0 && result.PenaltyAmount > 0 {
			orderNo := genOrderNo("NS")
			// 先尝试从未使用的余额扣，不足再从押金等扣
			_, err := s.WalletSvc.PenaltyDeduct(booking.CustomerID, result.PenaltyAmount, "booking", booking.ID, "爽约违约金: "+reason)
			if err != nil {
				return err
			}
			_ = orderNo
		}

		if result.DeductDeposit && booking.DepositAmount > 0 {
			deposit, err := s.DepositSvc.GetDepositByBooking(booking.ID)
			if err == nil && deposit != nil {
				if depErr := s.DepositSvc.DeductDeposit(deposit.ID, booking.DepositAmount, "爽约扣押金: "+reason); depErr != nil {
					return depErr
				}
			}
		}

		booking.PenaltyAmount = booking.PenaltyAmount + result.PenaltyAmount
		booking.Status = models.BookingNoShow
		booking.CancelReason = reason
		return tx.Save(&booking).Error
	})

	return &booking, result, err
}

// CompleteBooking 完成预订：退还押金，标记完成。
func (s *BookingService) CompleteBooking(bookingID uint, remark string) (*models.Booking, error) {
	var booking models.Booking
	if err := s.DB.First(&booking, bookingID).Error; err != nil {
		return nil, errors.New("预订不存在")
	}
	if booking.Status != models.BookingPaid && booking.Status != models.BookingBooked {
		return nil, errors.New("预订状态不符合完成条件")
	}

	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if booking.DepositAmount > 0 {
			deposit, err := s.DepositSvc.GetDepositByBooking(booking.ID)
			if err == nil && deposit != nil {
				if depErr := s.DepositSvc.RefundDeposit(deposit.ID, "使用完成押金退还: "+remark); depErr != nil {
					return depErr
				}
			}
		}
		booking.Status = models.BookingCompleted
		return tx.Save(&booking).Error
	})
	return &booking, err
}

// GetBooking 查询预订详情。
func (s *BookingService) GetBooking(bookingID uint) (*models.Booking, error) {
	var booking models.Booking
	if err := s.DB.First(&booking, bookingID).Error; err != nil {
		return nil, err
	}
	return &booking, nil
}

// ListBookings 查询预订列表。
func (s *BookingService) ListBookings(venueID uint, customerID uint, status string, date string, page, pageSize int) ([]models.Booking, int64, error) {
	var bookings []models.Booking
	var total int64

	q := s.DB.Model(&models.Booking{})
	if venueID > 0 {
		q = q.Where("venue_id = ?", venueID)
	}
	if customerID > 0 {
		q = q.Where("customer_id = ?", customerID)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if date != "" {
		q = q.Where("book_date = ?", date)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	if err := q.Order("id desc").Offset(offset).Limit(pageSize).Find(&bookings).Error; err != nil {
		return nil, 0, err
	}
	return bookings, total, nil
}

// CheckAndMarkNoShows 定时任务：检查超时未到场的预订并标记爽约。
func (s *BookingService) CheckAndMarkNoShows() (int64, error) {
	now := time.Now()
	today := now.Format("2006-01-02")
	currentHour := now.Hour()

	var toMark []models.Booking
	err := s.DB.Where("status IN ? AND book_date = ? AND end_hour <= ?",
		[]models.BookingStatus{models.BookingPaid, models.BookingBooked}, today, currentHour).
		Find(&toMark).Error
	if err != nil {
		return 0, err
	}

	marked := int64(0)
	for _, b := range toMark {
		startTime, _ := time.ParseInLocation("2006-01-02 15", b.BookDate+" "+itoa(b.StartHour), time.Local)
		if now.Sub(startTime).Minutes() > 30 {
			_, _, err := s.MarkNoShow(b.ID, "系统自动判定爽约-超时未到场")
			if err == nil {
				marked++
			}
		}
	}
	return marked, nil
}

func itoa(i int) string {
	return strconv.Itoa(i)
}
