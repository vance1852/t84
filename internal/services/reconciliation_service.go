package services

import (
	"time"

	"gorm.io/gorm"

	"venue-booking-admin/internal/models"
)

// ReconciliationService 对账与差异定位服务。
type ReconciliationService struct {
	DB *gorm.DB
}

// NewReconciliationService 创建对账服务。
func NewReconciliationService(db *gorm.DB) *ReconciliationService {
	return &ReconciliationService{DB: db}
}

// DailyStats 某一天的各项数据。
type DailyStats struct {
	Date               string
	ExpectedIncome     float64 // 预订应收（新创建且非取消的订单支付金额）
	ActualIncome       float64 // 钱包实收（consume 类型流水金额）
	TotalRefund        float64 // 退款总额
	TotalPenalty       float64 // 违约金总额
	TotalDepositIn     float64 // 押金冻结
	TotalDepositOut    float64 // 押金退还
	TotalDepositDeduct float64 // 押金扣减
	TotalRecharge      float64 // 充值总额
}

// collectDailyStats 收集某日的实际数据。
func (s *ReconciliationService) collectDailyStats(date string) (*DailyStats, error) {
	dayStart, _ := time.ParseInLocation("2006-01-02 15:04:05", date+" 00:00:00", time.Local)
	dayEnd := dayStart.Add(24 * time.Hour)

	stats := &DailyStats{Date: date}

	// 预订应收：当天创建的非取消订单的支付金额
	var bookings []models.Booking
	s.DB.Where("created_at >= ? AND created_at < ?", dayStart, dayEnd).Find(&bookings)
	for _, b := range bookings {
		if b.Status != models.BookingCancelled {
			if b.PaidAmount > 0 {
				stats.ExpectedIncome += b.PaidAmount
			} else {
				stats.ExpectedIncome += b.Amount
			}
		}
	}

	// 钱包流水统计
	var txs []models.WalletTransaction
	s.DB.Where("created_at >= ? AND created_at < ?", dayStart, dayEnd).Find(&txs)
	for _, tx := range txs {
		switch tx.TxType {
		case models.TxConsume:
			stats.ActualIncome += -tx.Amount
		case models.TxRefund:
			stats.TotalRefund += tx.Amount
		case models.TxPenalty, models.TxNoShowPenalty:
			stats.TotalPenalty += -tx.Amount
		case models.TxRecharge:
			stats.TotalRecharge += tx.Amount
		case models.TxDepositFreeze:
			frozenDiff := tx.FrozenAfter - tx.FrozenBefore
			if frozenDiff > 0 {
				stats.TotalDepositIn += frozenDiff
			}
		case models.TxDepositRefund:
			frozenDiff := tx.FrozenBefore - tx.FrozenAfter
			if frozenDiff > 0 {
				stats.TotalDepositOut += frozenDiff
			}
		case models.TxDepositDeduct:
			frozenDiff := tx.FrozenBefore - tx.FrozenAfter
			if frozenDiff > 0 {
				stats.TotalDepositDeduct += frozenDiff
			}
		}
	}

	// 押金流水再从 Deposit 表独立校验
	var deposits []models.Deposit
	s.DB.Where("created_at >= ? AND created_at < ?", dayStart, dayEnd).Find(&deposits)
	for _, d := range deposits {
		if stats.TotalDepositIn == 0 {
			stats.TotalDepositIn += d.Amount
		}
	}

	return stats, nil
}

// DoReconciliation 执行某一天的对账，生成对账记录和差异明细。
func (s *ReconciliationService) DoReconciliation(date string) (*models.Reconciliation, []models.ReconDiff, error) {
	stats, err := s.collectDailyStats(date)
	if err != nil {
		return nil, nil, err
	}

	diffAmount := stats.ActualIncome + stats.TotalRecharge - stats.TotalRefund - stats.TotalPenalty - stats.ExpectedIncome
	status := models.ReconMatched
	if diffAmount > 0.01 || diffAmount < -0.01 {
		status = models.ReconMismatch
	}

	var recon models.Reconciliation
	var diffs []models.ReconDiff

	err = s.DB.Transaction(func(tx *gorm.DB) error {
		// 检查是否已存在当日对账记录
		var existing models.Reconciliation
		hasExisting := false
		if err := tx.Where("recon_date = ?", date).First(&existing).Error; err == nil {
			hasExisting = true
			recon = existing
		}

		updates := map[string]interface{}{
			"expected_income":      stats.ExpectedIncome,
			"actual_income":        stats.ActualIncome,
			"total_refund":         stats.TotalRefund,
			"total_penalty":        stats.TotalPenalty,
			"total_deposit_in":     stats.TotalDepositIn,
			"total_deposit_out":    stats.TotalDepositOut,
			"total_deposit_deduct": stats.TotalDepositDeduct,
			"total_recharge":       stats.TotalRecharge,
			"diff_amount":          diffAmount,
			"status":               status,
		}

		if hasExisting {
			if err := tx.Model(&recon).Updates(updates).Error; err != nil {
				return err
			}
			// 清除旧的差异明细
			if err := tx.Where("recon_id = ?", recon.ID).Delete(&models.ReconDiff{}).Error; err != nil {
				return err
			}
		} else {
			recon = models.Reconciliation{
				ReconDate:          date,
				ExpectedIncome:     stats.ExpectedIncome,
				ActualIncome:       stats.ActualIncome,
				TotalRefund:        stats.TotalRefund,
				TotalPenalty:       stats.TotalPenalty,
				TotalDepositIn:     stats.TotalDepositIn,
				TotalDepositOut:    stats.TotalDepositOut,
				TotalDepositDeduct: stats.TotalDepositDeduct,
				TotalRecharge:      stats.TotalRecharge,
				DiffAmount:         diffAmount,
				Status:             status,
			}
			if err := tx.Create(&recon).Error; err != nil {
				return err
			}
		}

		// 差异1：预订收入 vs 钱包消费流水
		if (stats.ExpectedIncome - stats.ActualIncome) > 0.01 || (stats.ActualIncome - stats.ExpectedIncome) > 0.01 {
			diffs = append(diffs, models.ReconDiff{
				ReconID:     recon.ID,
				ReconDate:   date,
				RelatedType: "booking",
				FieldName:   "income_match",
				Expected:    stats.ExpectedIncome,
				Actual:      stats.ActualIncome,
				Diff:        stats.ActualIncome - stats.ExpectedIncome,
				Remark:      "预订应收与钱包实收不一致",
			})
		}

		// 差异2：检查每笔已支付预订是否有对应的消费流水
		dayStart, _ := time.ParseInLocation("2006-01-02 15:04:05", date+" 00:00:00", time.Local)
		dayEnd := dayStart.Add(24 * time.Hour)
		var paidBookings []models.Booking
		tx.Where("created_at >= ? AND created_at < ? AND status <> ?",
			dayStart, dayEnd, models.BookingCancelled).Find(&paidBookings)
		for _, b := range paidBookings {
			if b.CustomerID > 0 && b.PaidAmount > 0 {
				var count int64
				tx.Model(&models.WalletTransaction{}).
					Where("related_type = ? AND related_id = ? AND tx_type = ?", "booking", b.ID, models.TxConsume).
					Count(&count)
				if count == 0 {
					diffs = append(diffs, models.ReconDiff{
						ReconID:     recon.ID,
						ReconDate:   date,
						RelatedType: "booking",
						RelatedID:   b.ID,
						FieldName:   "missing_consume_tx",
						Expected:    b.PaidAmount,
						Actual:      0,
						Diff:        -b.PaidAmount,
						Remark:      "预订已支付但无对应消费流水",
					})
				}
			}
		}

		// 差异3：检查每笔取消预订是否有对应的退款流水
		var cancelledBookings []models.Booking
		tx.Where("created_at >= ? AND created_at < ? AND status = ?",
			dayStart, dayEnd, models.BookingCancelled).Find(&cancelledBookings)
		for _, b := range cancelledBookings {
			if b.CustomerID > 0 && b.RefundedAmount > 0 {
				var count int64
				tx.Model(&models.WalletTransaction{}).
					Where("related_type = ? AND related_id = ? AND tx_type = ?", "booking", b.ID, models.TxRefund).
					Count(&count)
				if count == 0 {
					diffs = append(diffs, models.ReconDiff{
						ReconID:     recon.ID,
						ReconDate:   date,
						RelatedType: "booking",
						RelatedID:   b.ID,
						FieldName:   "missing_refund_tx",
						Expected:    b.RefundedAmount,
						Actual:      0,
						Diff:        -b.RefundedAmount,
						Remark:      "预订已退款但无对应退款流水",
					})
				}
			}
		}

		// 批量写入差异明细
		if len(diffs) > 0 {
			if err := tx.Create(&diffs).Error; err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, nil, err
	}

	// 重新查询最新的差异明细
	s.DB.Where("recon_id = ?", recon.ID).Find(&diffs)
	return &recon, diffs, nil
}

// ListReconciliations 查询对账记录。
func (s *ReconciliationService) ListReconciliations(startDate, endDate string, status string, page, pageSize int) ([]models.Reconciliation, int64, error) {
	var list []models.Reconciliation
	var total int64

	q := s.DB.Model(&models.Reconciliation{})
	if startDate != "" {
		q = q.Where("recon_date >= ?", startDate)
	}
	if endDate != "" {
		q = q.Where("recon_date <= ?", endDate)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	if err := q.Order("recon_date desc").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// GetReconciliationDiffs 查询某次对账的差异明细。
func (s *ReconciliationService) GetReconciliationDiffs(reconID uint) ([]models.ReconDiff, error) {
	var diffs []models.ReconDiff
	err := s.DB.Where("recon_id = ?", reconID).Order("id").Find(&diffs).Error
	return diffs, err
}

// WalletBalanceRecon 全量钱包余额校验：
// 校验1：所有钱包的 balance 总和 = 所有流水的 amount 代数和
// 校验2：所有钱包的 frozen 总和 = 当前所有有效冻结金额
func (s *ReconciliationService) WalletBalanceRecon() (map[string]interface{}, []uint, error) {
	var allWallets []models.Wallet
	s.DB.Find(&allWallets)

	result := map[string]interface{}{}
	var totalBalance, totalFrozen float64
	for _, w := range allWallets {
		totalBalance += w.Balance
		totalFrozen += w.FrozenAmount
	}
	result["total_balance"] = totalBalance
	result["total_frozen"] = totalFrozen
	result["total_assets"] = totalBalance + totalFrozen

	// 校验1：所有流水 amount 总和 vs 所有钱包 balance 总和
	var sumTxAmount float64
	s.DB.Model(&models.WalletTransaction{}).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&sumTxAmount)
	result["sum_tx_amount"] = sumTxAmount
	result["balance_diff"] = totalBalance - sumTxAmount
	balanceOk := (totalBalance - sumTxAmount) < 0.01 && (sumTxAmount - totalBalance) < 0.01
	result["balance_match"] = balanceOk

	// 校验2：所有 active 状态 WalletFreeze 的金额总和 vs 所有钱包 frozen 总和
	var sumActiveFreeze float64
	s.DB.Model(&models.WalletFreeze{}).
		Where("status = ?", "active").
		Select("COALESCE(SUM(amount), 0)").
		Scan(&sumActiveFreeze)
	result["sum_active_freeze"] = sumActiveFreeze
	result["frozen_diff"] = totalFrozen - sumActiveFreeze
	frozenOk := (totalFrozen - sumActiveFreeze) < 0.01 && (sumActiveFreeze - totalFrozen) < 0.01
	result["frozen_match"] = frozenOk

	// 校验3：每笔钱包单独校验
	var mismatchWallets []uint
	for _, w := range allWallets {
		var wSum float64
		s.DB.Model(&models.WalletTransaction{}).
			Where("wallet_id = ?", w.ID).
			Select("COALESCE(SUM(amount), 0)").
			Scan(&wSum)
		diff := w.Balance - wSum
		if diff > 0.01 || diff < -0.01 {
			mismatchWallets = append(mismatchWallets, w.ID)
		}
	}
	result["mismatch_wallet_count"] = len(mismatchWallets)
	result["overall_match"] = balanceOk && frozenOk && len(mismatchWallets) == 0

	return result, mismatchWallets, nil
}
