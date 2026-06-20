package services

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"venue-booking-admin/internal/models"
)

// WalletService 钱包服务。
type WalletService struct {
	DB *gorm.DB
}

// NewWalletService 创建钱包服务。
func NewWalletService(db *gorm.DB) *WalletService {
	return &WalletService{DB: db}
}

// genOrderNo 生成业务单号。
func genOrderNo(prefix string) string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%s%s%s", prefix, time.Now().Format("20060102150405"), hex.EncodeToString(b)[:8])
}

// GetOrCreateWallet 获取或创建用户钱包。
func (s *WalletService) GetOrCreateWallet(customerID uint) (*models.Wallet, error) {
	var wallet models.Wallet
	err := s.DB.Where("customer_id = ?", customerID).First(&wallet).Error
	if err == nil {
		return &wallet, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	wallet = models.Wallet{
		CustomerID: customerID,
		Balance:    0,
		FrozenAmount: 0,
	}
	if err := s.DB.Create(&wallet).Error; err != nil {
		return nil, err
	}
	return &wallet, nil
}

// createTx 写入钱包流水。
func (s *WalletService) createTx(tx *gorm.DB, walletID, customerID uint, txType models.WalletTxType,
	amount float64, balBefore, balAfter, fzBefore, fzAfter float64,
	relatedType string, relatedID uint, orderNo, remark string) error {
	return tx.Create(&models.WalletTransaction{
		WalletID:      walletID,
		CustomerID:    customerID,
		TxType:        txType,
		Amount:        amount,
		BalanceBefore: balBefore,
		BalanceAfter:  balAfter,
		FrozenBefore:  fzBefore,
		FrozenAfter:   fzAfter,
		RelatedType:   relatedType,
		RelatedID:     relatedID,
		OrderNo:       orderNo,
		Remark:        remark,
	}).Error
}

// Recharge 钱包充值。
func (s *WalletService) Recharge(customerID uint, amount float64, remark string) (*models.Wallet, *models.WalletTransaction, error) {
	if amount <= 0 {
		return nil, nil, errors.New("充值金额必须大于0")
	}
	orderNo := genOrderNo("RC")

	var wallet *models.Wallet
	var txRecord *models.WalletTransaction

	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		wallet, err = s.GetOrCreateWallet(customerID)
		if err != nil {
			return err
		}

		var existing models.WalletTransaction
		if err := tx.Where("order_no = ?", orderNo).First(&existing).Error; err == nil {
			return errors.New("重复的充值请求")
		}

		balBefore := wallet.Balance
		balAfter := wallet.Balance + amount
		fzBefore := wallet.FrozenAmount

		result := tx.Model(&models.Wallet{}).
			Where("id = ? AND version = ?", wallet.ID, wallet.Version).
			Updates(map[string]interface{}{
				"balance":        balAfter,
				"total_recharge": gorm.Expr("total_recharge + ?", amount),
				"version":        wallet.Version + 1,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("钱包更新冲突，请重试")
		}

		if err := s.createTx(tx, wallet.ID, customerID, models.TxRecharge, amount,
			balBefore, balAfter, fzBefore, fzBefore, "wallet", wallet.ID, orderNo, remark); err != nil {
			return err
		}

		wallet.Balance = balAfter
		wallet.TotalRecharge += amount
		wallet.Version++
		txRecord = &models.WalletTransaction{
			WalletID:      wallet.ID,
			CustomerID:    customerID,
			TxType:        models.TxRecharge,
			Amount:        amount,
			BalanceBefore: balBefore,
			BalanceAfter:  balAfter,
			FrozenBefore:  fzBefore,
			FrozenAfter:   fzBefore,
			OrderNo:       orderNo,
			Remark:        remark,
		}
		return nil
	})
	return wallet, txRecord, err
}

// Deduct 钱包扣款（消费）。
func (s *WalletService) Deduct(customerID uint, amount float64, relatedType string, relatedID uint, remark string) (*models.WalletTransaction, error) {
	if amount <= 0 {
		return nil, errors.New("扣款金额必须大于0")
	}
	orderNo := genOrderNo("DC")

	var txRecord *models.WalletTransaction

	err := s.DB.Transaction(func(tx *gorm.DB) error {
		wallet, err := s.GetOrCreateWallet(customerID)
		if err != nil {
			return err
		}

		var existing models.WalletTransaction
		if err := tx.Where("order_no = ?", orderNo).First(&existing).Error; err == nil {
			return errors.New("重复的扣款请求")
		}

		if wallet.Balance < amount {
			return errors.New("钱包余额不足")
		}

		balBefore := wallet.Balance
		balAfter := wallet.Balance - amount
		fzBefore := wallet.FrozenAmount

		result := tx.Model(&models.Wallet{}).
			Where("id = ? AND version = ? AND balance >= ?", wallet.ID, wallet.Version, amount).
			Updates(map[string]interface{}{
				"balance":       balAfter,
				"total_consume": gorm.Expr("total_consume + ?", amount),
				"version":       wallet.Version + 1,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("钱包余额不足或更新冲突，请重试")
		}

		if err := s.createTx(tx, wallet.ID, customerID, models.TxConsume, -amount,
			balBefore, balAfter, fzBefore, fzBefore, relatedType, relatedID, orderNo, remark); err != nil {
			return err
		}

		txRecord = &models.WalletTransaction{
			WalletID:      wallet.ID,
			CustomerID:    customerID,
			TxType:        models.TxConsume,
			Amount:        -amount,
			BalanceBefore: balBefore,
			BalanceAfter:  balAfter,
			OrderNo:       orderNo,
		}
		return nil
	})
	return txRecord, err
}

// Freeze 钱包资金冻结。
func (s *WalletService) Freeze(customerID uint, amount float64, freezeType, relatedType string, relatedID uint, remark string) (*models.WalletFreeze, error) {
	if amount <= 0 {
		return nil, errors.New("冻结金额必须大于0")
	}
	orderNo := genOrderNo("FZ")

	var freeze *models.WalletFreeze

	err := s.DB.Transaction(func(tx *gorm.DB) error {
		wallet, err := s.GetOrCreateWallet(customerID)
		if err != nil {
			return err
		}

		var existing models.WalletFreeze
		if err := tx.Where("order_no = ?", orderNo).First(&existing).Error; err == nil {
			return errors.New("重复的冻结请求")
		}

		if wallet.Balance < amount {
			return errors.New("钱包可用余额不足，无法冻结")
		}

		balBefore := wallet.Balance
		balAfter := wallet.Balance - amount
		fzBefore := wallet.FrozenAmount
		fzAfter := wallet.FrozenAmount + amount

		result := tx.Model(&models.Wallet{}).
			Where("id = ? AND version = ? AND balance >= ?", wallet.ID, wallet.Version, amount).
			Updates(map[string]interface{}{
				"balance":       balAfter,
				"frozen_amount": fzAfter,
				"version":       wallet.Version + 1,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("余额不足或更新冲突，请重试")
		}

		freeze = &models.WalletFreeze{
			WalletID:    wallet.ID,
			CustomerID:  customerID,
			Amount:      amount,
			FreezeType:  freezeType,
			RelatedType: relatedType,
			RelatedID:   relatedID,
			Status:      "active",
			OrderNo:     orderNo,
			Remark:      remark,
		}
		if err := tx.Create(freeze).Error; err != nil {
			return err
		}

		txType := models.TxFreeze
		if freezeType == "deposit" {
			txType = models.TxDepositFreeze
		}
		if err := s.createTx(tx, wallet.ID, customerID, txType, -amount,
			balBefore, balAfter, fzBefore, fzAfter, relatedType, relatedID, orderNo, remark); err != nil {
			return err
		}
		return nil
	})
	return freeze, err
}

// Unfreeze 钱包资金解冻。
func (s *WalletService) Unfreeze(freezeID uint, remark string) error {
	var freeze models.WalletFreeze
	if err := s.DB.First(&freeze, freezeID).Error; err != nil {
		return errors.New("冻结记录不存在")
	}
	if freeze.Status != "active" {
		return errors.New("冻结记录状态异常，无法解冻")
	}
	unfreezeOrderNo := genOrderNo("UF")

	return s.DB.Transaction(func(tx *gorm.DB) error {
		wallet, err := s.GetOrCreateWallet(freeze.CustomerID)
		if err != nil {
			return err
		}

		balBefore := wallet.Balance
		balAfter := wallet.Balance + freeze.Amount
		fzBefore := wallet.FrozenAmount
		fzAfter := wallet.FrozenAmount - freeze.Amount
		if fzAfter < 0 {
			fzAfter = 0
		}

		result := tx.Model(&models.Wallet{}).
			Where("id = ? AND version = ?", wallet.ID, wallet.Version).
			Updates(map[string]interface{}{
				"balance":       balAfter,
				"frozen_amount": fzAfter,
				"version":       wallet.Version + 1,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("钱包更新冲突，请重试")
		}

		if err := tx.Model(&freeze).Updates(map[string]interface{}{
			"status": "unfrozen",
		}).Error; err != nil {
			return err
		}

		txType := models.TxUnfreeze
		if freeze.FreezeType == "deposit" {
			txType = models.TxDepositRefund
		}
		return s.createTx(tx, wallet.ID, freeze.CustomerID, txType, freeze.Amount,
			balBefore, balAfter, fzBefore, fzAfter, freeze.RelatedType, freeze.RelatedID, unfreezeOrderNo, remark)
	})
}

// DeductFromFreeze 从冻结金额中扣减（不退回余额，直接从冻结额核销）。
func (s *WalletService) DeductFromFreeze(freezeID uint, amount float64, txType models.WalletTxType, remark string) error {
	if amount <= 0 {
		return errors.New("扣减金额必须大于0")
	}
	var freeze models.WalletFreeze
	if err := s.DB.First(&freeze, freezeID).Error; err != nil {
		return errors.New("冻结记录不存在")
	}
	if freeze.Status != "active" {
		return errors.New("冻结记录状态异常")
	}
	if freeze.Amount < amount {
		return errors.New("冻结金额不足")
	}
	deductOrderNo := genOrderNo("DD")

	return s.DB.Transaction(func(tx *gorm.DB) error {
		wallet, err := s.GetOrCreateWallet(freeze.CustomerID)
		if err != nil {
			return err
		}

		balBefore := wallet.Balance
		fzBefore := wallet.FrozenAmount
		fzAfter := wallet.FrozenAmount - amount
		if fzAfter < 0 {
			fzAfter = 0
		}

		result := tx.Model(&models.Wallet{}).
			Where("id = ? AND version = ?", wallet.ID, wallet.Version).
			Updates(map[string]interface{}{
				"frozen_amount": fzAfter,
				"version":       wallet.Version + 1,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("钱包更新冲突，请重试")
		}

		status := "deducted"
		if freeze.Amount > amount {
			status = "active"
		}
		if err := tx.Model(&freeze).Updates(map[string]interface{}{
			"amount": freeze.Amount - amount,
			"status": status,
		}).Error; err != nil {
			return err
		}

		return s.createTx(tx, wallet.ID, freeze.CustomerID, txType, 0,
			balBefore, balBefore, fzBefore, fzAfter, freeze.RelatedType, freeze.RelatedID, deductOrderNo, remark)
	})
}

// PartialUnfreeze 部分解冻，冻结金额退回到可用余额。
func (s *WalletService) PartialUnfreeze(freezeID uint, amount float64, txType models.WalletTxType, remark string) error {
	if amount <= 0 {
		return errors.New("解冻金额必须大于0")
	}
	var freeze models.WalletFreeze
	if err := s.DB.First(&freeze, freezeID).Error; err != nil {
		return errors.New("冻结记录不存在")
	}
	if freeze.Status != "active" {
		return errors.New("冻结记录状态异常")
	}
	if freeze.Amount < amount {
		return errors.New("冻结金额不足")
	}
	unfreezeOrderNo := genOrderNo("PU")

	return s.DB.Transaction(func(tx *gorm.DB) error {
		wallet, err := s.GetOrCreateWallet(freeze.CustomerID)
		if err != nil {
			return err
		}

		balBefore := wallet.Balance
		balAfter := wallet.Balance + amount
		fzBefore := wallet.FrozenAmount
		fzAfter := wallet.FrozenAmount - amount
		if fzAfter < 0 {
			fzAfter = 0
		}

		result := tx.Model(&models.Wallet{}).
			Where("id = ? AND version = ?", wallet.ID, wallet.Version).
			Updates(map[string]interface{}{
				"balance":       balAfter,
				"frozen_amount": fzAfter,
				"version":       wallet.Version + 1,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("钱包更新冲突，请重试")
		}

		status := "unfrozen"
		if freeze.Amount > amount {
			status = "active"
		}
		if err := tx.Model(&freeze).Updates(map[string]interface{}{
			"amount": freeze.Amount - amount,
			"status": status,
		}).Error; err != nil {
			return err
		}

		return s.createTx(tx, wallet.ID, freeze.CustomerID, txType, amount,
			balBefore, balAfter, fzBefore, fzAfter, freeze.RelatedType, freeze.RelatedID, unfreezeOrderNo, remark)
	})
}

// Refund 钱包退款入账。
func (s *WalletService) Refund(customerID uint, amount float64, relatedType string, relatedID uint, remark string) (*models.WalletTransaction, error) {
	if amount <= 0 {
		return nil, errors.New("退款金额必须大于0")
	}
	orderNo := genOrderNo("RF")

	var txRecord *models.WalletTransaction

	err := s.DB.Transaction(func(tx *gorm.DB) error {
		wallet, err := s.GetOrCreateWallet(customerID)
		if err != nil {
			return err
		}

		var existing models.WalletTransaction
		if err := tx.Where("order_no = ?", orderNo).First(&existing).Error; err == nil {
			return errors.New("重复的退款请求")
		}

		balBefore := wallet.Balance
		balAfter := wallet.Balance + amount
		fzBefore := wallet.FrozenAmount

		result := tx.Model(&models.Wallet{}).
			Where("id = ? AND version = ?", wallet.ID, wallet.Version).
			Updates(map[string]interface{}{
				"balance":      balAfter,
				"total_refund": gorm.Expr("total_refund + ?", amount),
				"version":      wallet.Version + 1,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("钱包更新冲突，请重试")
		}

		if err := s.createTx(tx, wallet.ID, customerID, models.TxRefund, amount,
			balBefore, balAfter, fzBefore, fzBefore, relatedType, relatedID, orderNo, remark); err != nil {
			return err
		}

		txRecord = &models.WalletTransaction{
			WalletID:      wallet.ID,
			CustomerID:    customerID,
			TxType:        models.TxRefund,
			Amount:        amount,
			BalanceBefore: balBefore,
			BalanceAfter:  balAfter,
			OrderNo:       orderNo,
		}
		return nil
	})
	return txRecord, err
}

// PenaltyDeduct 违约金扣款（从可用余额）。
func (s *WalletService) PenaltyDeduct(customerID uint, amount float64, relatedType string, relatedID uint, remark string) (*models.WalletTransaction, error) {
	if amount <= 0 {
		return nil, errors.New("扣款金额必须大于0")
	}
	orderNo := genOrderNo("PN")

	var txRecord *models.WalletTransaction

	err := s.DB.Transaction(func(tx *gorm.DB) error {
		wallet, err := s.GetOrCreateWallet(customerID)
		if err != nil {
			return err
		}

		var existing models.WalletTransaction
		if err := tx.Where("order_no = ?", orderNo).First(&existing).Error; err == nil {
			return errors.New("重复的扣款请求")
		}

		if wallet.Balance < amount {
			return errors.New("钱包余额不足，违约金扣款失败")
		}

		balBefore := wallet.Balance
		balAfter := wallet.Balance - amount
		fzBefore := wallet.FrozenAmount

		result := tx.Model(&models.Wallet{}).
			Where("id = ? AND version = ? AND balance >= ?", wallet.ID, wallet.Version, amount).
			Updates(map[string]interface{}{
				"balance": balAfter,
				"version": wallet.Version + 1,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("余额不足或更新冲突，请重试")
		}

		if err := s.createTx(tx, wallet.ID, customerID, models.TxPenalty, -amount,
			balBefore, balAfter, fzBefore, fzBefore, relatedType, relatedID, orderNo, remark); err != nil {
			return err
		}

		txRecord = &models.WalletTransaction{
			WalletID:      wallet.ID,
			CustomerID:    customerID,
			TxType:        models.TxPenalty,
			Amount:        -amount,
			BalanceBefore: balBefore,
			BalanceAfter:  balAfter,
			OrderNo:       orderNo,
		}
		return nil
	})
	return txRecord, err
}

// ListTransactions 查询钱包流水。
func (s *WalletService) ListTransactions(customerID uint, txType string, page, pageSize int) ([]models.WalletTransaction, int64, error) {
	var txs []models.WalletTransaction
	var total int64

	q := s.DB.Model(&models.WalletTransaction{})
	if customerID > 0 {
		q = q.Where("customer_id = ?", customerID)
	}
	if txType != "" {
		q = q.Where("tx_type = ?", txType)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	if err := q.Order("id desc").Offset(offset).Limit(pageSize).Find(&txs).Error; err != nil {
		return nil, 0, err
	}
	return txs, total, nil
}

// GetWallet 获取钱包详情。
func (s *WalletService) GetWallet(customerID uint) (*models.Wallet, error) {
	return s.GetOrCreateWallet(customerID)
}

// CheckBalance 检查钱包余额是否充足。
func (s *WalletService) CheckBalance(customerID uint, amount float64) (bool, float64, error) {
	wallet, err := s.GetOrCreateWallet(customerID)
	if err != nil {
		return false, 0, err
	}
	return wallet.Balance >= amount, wallet.Balance, nil
}
