package services

import (
	"errors"

	"gorm.io/gorm"

	"venue-booking-admin/internal/models"
)

// DepositService 押金管理服务。
type DepositService struct {
	DB          *gorm.DB
	WalletSvc   *WalletService
}

// NewDepositService 创建押金服务。
func NewDepositService(db *gorm.DB, walletSvc *WalletService) *DepositService {
	return &DepositService{DB: db, WalletSvc: walletSvc}
}

// FreezeDeposit 预订时冻结押金。
func (s *DepositService) FreezeDeposit(bookingID, customerID, venueID uint, amount float64, reason string) (*models.Deposit, error) {
	if amount <= 0 {
		return nil, errors.New("押金金额必须大于0")
	}

	var existing models.Deposit
	if err := s.DB.Where("booking_id = ?", bookingID).First(&existing).Error; err == nil {
		return nil, errors.New("该预订已存在押金记录")
	}

	freeze, err := s.WalletSvc.Freeze(customerID, amount, "deposit", "deposit", bookingID, reason)
	if err != nil {
		return nil, err
	}

	deposit := &models.Deposit{
		BookingID:     bookingID,
		CustomerID:    customerID,
		VenueID:       venueID,
		Amount:        amount,
		Status:        models.DepositFrozen,
		FreezeOrderNo: freeze.OrderNo,
		Reason:        reason,
	}
	if err := s.DB.Create(deposit).Error; err != nil {
		return nil, err
	}
	return deposit, nil
}

// RefundDeposit 退还押金（使用后无违规）。
func (s *DepositService) RefundDeposit(depositID uint, remark string) error {
	var deposit models.Deposit
	if err := s.DB.First(&deposit, depositID).Error; err != nil {
		return errors.New("押金记录不存在")
	}
	if deposit.Status != models.DepositFrozen && deposit.Status != models.DepositPartial {
		return errors.New("押金状态不允许退还")
	}

	remaining := deposit.Amount - deposit.DeductedAmount - deposit.RefundedAmount
	if remaining <= 0 {
		return errors.New("无剩余押金可退还")
	}

	var freeze models.WalletFreeze
	if err := s.DB.Where("order_no = ?", deposit.FreezeOrderNo).First(&freeze).Error; err == nil {
		if err := s.WalletSvc.Unfreeze(freeze.ID, remark); err != nil {
			return err
		}
	}

	return s.DB.Model(&deposit).Updates(map[string]interface{}{
		"refunded_amount": deposit.RefundedAmount + remaining,
		"status":          models.DepositRefunded,
	}).Error
}

// DeductDeposit 扣减押金（损坏或爽约等）。
func (s *DepositService) DeductDeposit(depositID uint, amount float64, remark string) error {
	if amount <= 0 {
		return errors.New("扣减金额必须大于0")
	}

	var deposit models.Deposit
	if err := s.DB.First(&deposit, depositID).Error; err != nil {
		return errors.New("押金记录不存在")
	}
	if deposit.Status != models.DepositFrozen && deposit.Status != models.DepositPartial {
		return errors.New("押金状态不允许扣减")
	}

	remaining := deposit.Amount - deposit.DeductedAmount - deposit.RefundedAmount
	if remaining < amount {
		return errors.New("押金余额不足")
	}

	var freeze models.WalletFreeze
	if err := s.DB.Where("order_no = ?", deposit.FreezeOrderNo).First(&freeze).Error; err == nil {
		if err := s.WalletSvc.DeductFromFreeze(freeze.ID, amount, models.TxDepositDeduct, remark); err != nil {
			return err
		}
	}

	newDeducted := deposit.DeductedAmount + amount
	newRemaining := deposit.Amount - newDeducted - deposit.RefundedAmount
	status := models.DepositPartial
	if newRemaining <= 0 {
		status = models.DepositDeducted
	}

	return s.DB.Model(&deposit).Updates(map[string]interface{}{
		"deducted_amount": newDeducted,
		"status":          status,
	}).Error
}

// PartialRefundDeposit 部分退还押金。
func (s *DepositService) PartialRefundDeposit(depositID uint, refundAmount float64, remark string) error {
	if refundAmount <= 0 {
		return errors.New("退还金额必须大于0")
	}

	var deposit models.Deposit
	if err := s.DB.First(&deposit, depositID).Error; err != nil {
		return errors.New("押金记录不存在")
	}
	if deposit.Status != models.DepositFrozen && deposit.Status != models.DepositPartial {
		return errors.New("押金状态不允许操作")
	}

	remaining := deposit.Amount - deposit.DeductedAmount - deposit.RefundedAmount
	if remaining < refundAmount {
		return errors.New("押金余额不足")
	}

	var freeze models.WalletFreeze
	if err := s.DB.Where("order_no = ?", deposit.FreezeOrderNo).First(&freeze).Error; err == nil {
		if refundAmount == freeze.Amount {
			if err := s.WalletSvc.Unfreeze(freeze.ID, remark); err != nil {
				return err
			}
		} else {
			return errors.New("部分退还押金需要先解冻相应金额，当前操作需走全额解冻")
		}
	}

	newRefunded := deposit.RefundedAmount + refundAmount
	newRemaining := deposit.Amount - deposit.DeductedAmount - newRefunded
	status := models.DepositPartial
	if newRemaining <= 0 && deposit.DeductedAmount == 0 {
		status = models.DepositRefunded
	}

	return s.DB.Model(&deposit).Updates(map[string]interface{}{
		"refunded_amount": newRefunded,
		"status":          status,
	}).Error
}

// GetDepositByBooking 根据预订查询押金。
func (s *DepositService) GetDepositByBooking(bookingID uint) (*models.Deposit, error) {
	var deposit models.Deposit
	err := s.DB.Where("booking_id = ?", bookingID).First(&deposit).Error
	if err != nil {
		return nil, err
	}
	return &deposit, nil
}

// ListDeposits 查询押金列表。
func (s *DepositService) ListDeposits(customerID, venueID uint, status string, page, pageSize int) ([]models.Deposit, int64, error) {
	var deposits []models.Deposit
	var total int64

	q := s.DB.Model(&models.Deposit{})
	if customerID > 0 {
		q = q.Where("customer_id = ?", customerID)
	}
	if venueID > 0 {
		q = q.Where("venue_id = ?", venueID)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	if err := q.Order("id desc").Offset(offset).Limit(pageSize).Find(&deposits).Error; err != nil {
		return nil, 0, err
	}
	return deposits, total, nil
}
