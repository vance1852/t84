package seed

import (
	"log"
	"time"

	"gorm.io/gorm"

	"venue-booking-admin/internal/auth"
	"venue-booking-admin/internal/models"
	"venue-booking-admin/internal/services"
)

// Run 初始化内置管理员与种子业务数据（幂等）。
func Run(database *gorm.DB, adminUser, adminPass string) error {
	var count int64
	database.Model(&models.User{}).Where("username = ?", adminUser).Count(&count)
	if count == 0 {
		hash, err := auth.HashPassword(adminPass)
		if err != nil {
			return err
		}
		database.Create(&models.User{Username: adminUser, PasswordHash: hash, DisplayName: "平台管理员"})
		log.Println("已创建管理员账号")
	}

	// ---------- 场馆种子数据 ----------
	var venueCount int64
	database.Model(&models.Venue{}).Count(&venueCount)
	venues := []models.Venue{}
	if venueCount == 0 {
		venues = []models.Venue{
			{Name: "城北全民健身中心篮球馆", SportType: "basketball", Capacity: 200, HourlyPrice: 160, OpenHour: 8, CloseHour: 22, Status: "open", RequireDeposit: true, DepositAmount: 300},
			{Name: "奥体中心游泳馆", SportType: "swimming", Capacity: 400, HourlyPrice: 80, OpenHour: 6, CloseHour: 21, Status: "open", RequireDeposit: false, DepositAmount: 0},
			{Name: "市民广场羽毛球馆", SportType: "badminton", Capacity: 60, HourlyPrice: 50, OpenHour: 9, CloseHour: 22, Status: "maintenance", RequireDeposit: false, DepositAmount: 0},
			{Name: "滨江足球公园", SportType: "football", Capacity: 500, HourlyPrice: 300, OpenHour: 8, CloseHour: 20, Status: "open", RequireDeposit: true, DepositAmount: 500},
		}
		if err := database.Create(&venues).Error; err != nil {
			return err
		}
		log.Println("已创建场馆种子数据")
	} else {
		database.Find(&venues)
	}

	// ---------- 退订规则种子数据 ----------
	var ruleCount int64
	database.Model(&models.CancellationRule{}).Count(&ruleCount)
	if ruleCount == 0 {
		rules := []models.CancellationRule{
			{VenueID: 0, Name: "全局-提前24h以上", MinHoursBeforeStart: 24, MaxHoursBeforeStart: 999999, RefundRate: 1.0, PenaltyRate: 0, PenaltyFixed: 0, IsDefault: true, Priority: 100},
			{VenueID: 0, Name: "全局-提前4-24h", MinHoursBeforeStart: 4, MaxHoursBeforeStart: 24, RefundRate: 0.8, PenaltyRate: 0.2, PenaltyFixed: 0, IsDefault: true, Priority: 90},
			{VenueID: 0, Name: "全局-提前2-4h", MinHoursBeforeStart: 2, MaxHoursBeforeStart: 4, RefundRate: 0.5, PenaltyRate: 0.5, PenaltyFixed: 0, IsDefault: true, Priority: 80},
			{VenueID: 0, Name: "全局-不足2h不退", MinHoursBeforeStart: 0, MaxHoursBeforeStart: 2, RefundRate: 0, PenaltyRate: 1.0, PenaltyFixed: 0, IsDefault: true, Priority: 70},
			{VenueID: venues[3].ID, Name: "足球场-提前48h以上", MinHoursBeforeStart: 48, MaxHoursBeforeStart: 999999, RefundRate: 1.0, PenaltyRate: 0, IsDefault: false, Priority: 200},
		}
		if err := database.Create(&rules).Error; err != nil {
			return err
		}
		log.Println("已创建退订规则种子数据")
	}

	// ---------- 爽约规则种子数据 ----------
	var noShowRuleCount int64
	database.Model(&models.NoShowRule{}).Count(&noShowRuleCount)
	if noShowRuleCount == 0 {
		noShowRules := []models.NoShowRule{
			{VenueID: 0, PenaltyRate: 0.5, PenaltyFixed: 0, DeductDeposit: true},
			{VenueID: venues[0].ID, PenaltyRate: 0, PenaltyFixed: 100, DeductDeposit: true},
		}
		if err := database.Create(&noShowRules).Error; err != nil {
			return err
		}
		log.Println("已创建爽约规则种子数据")
	}

	// ---------- 客户种子数据 ----------
	var customerCount int64
	database.Model(&models.Customer{}).Count(&customerCount)
	customers := []models.Customer{}
	if customerCount == 0 {
		customers = []models.Customer{
			{Name: "陈刚", Phone: "13700001111", Status: "active"},
			{Name: "周敏", Phone: "13700002222", Status: "active"},
			{Name: "黄磊", Phone: "13700003333", Status: "active"},
			{Name: "吴静", Phone: "13700004444", Status: "active"},
			{Name: "李勇", Phone: "13700005555", Status: "active"},
			{Name: "赵敏", Phone: "13700006666", Status: "active"},
			{Name: "孙强", Phone: "13700007777", Status: "blacklisted"},
		}
		if err := database.Create(&customers).Error; err != nil {
			return err
		}
		log.Println("已创建客户种子数据")
	} else {
		database.Find(&customers)
	}

	// ---------- 钱包与流水种子数据 ----------
	var walletCount int64
	database.Model(&models.Wallet{}).Count(&walletCount)
	if walletCount == 0 {
		walletSvc := services.NewWalletService(database)

		for i, customer := range customers {
			initialAmounts := []float64{2000, 1500, 800, 3500, 500, 1200, 100}
			if i < len(initialAmounts) && initialAmounts[i] > 0 {
				_, _, err := walletSvc.Recharge(customer.ID, initialAmounts[i], "开户赠送初始余额")
				if err != nil {
					log.Printf("客户 %s 充值失败: %v", customer.Name, err)
				}
			}
		}

		// 再给几个客户加几笔消费/退款流水，模拟历史
		time.Sleep(10 * time.Millisecond)
		walletSvc.Recharge(customers[0].ID, 500, "用户充值-微信支付")
		time.Sleep(10 * time.Millisecond)
		walletSvc.Recharge(customers[3].ID, 1000, "用户充值-支付宝")
		time.Sleep(10 * time.Millisecond)
		walletSvc.Deduct(customers[1].ID, 200, "booking", 0, "历史消费扣款")
		time.Sleep(10 * time.Millisecond)
		walletSvc.Refund(customers[3].ID, 80, "booking", 0, "历史退订退款")
		time.Sleep(10 * time.Millisecond)
		walletSvc.PenaltyDeduct(customers[2].ID, 50, "booking", 0, "历史违约金扣款")

		log.Println("已创建钱包与流水种子数据")
	}

	// ---------- 预订种子数据（带钱包、押金、历史状态） ----------
	var bookingCount int64
	database.Model(&models.Booking{}).Count(&bookingCount)
	if bookingCount == 0 {
		walletSvc := services.NewWalletService(database)
		depositSvc := services.NewDepositService(database, walletSvc)

		bookings := []models.Booking{}

		// 1. 陈刚-篮球馆-已支付-未来预订
		b1 := models.Booking{
			VenueID: venues[0].ID, CustomerID: customers[0].ID,
			CustomerName: "陈刚", Phone: "13700001111",
			BookDate: "2026-06-22", StartHour: 18, EndHour: 20,
			Amount: 320, DepositAmount: venues[0].DepositAmount,
		}
		database.Create(&b1)
		walletSvc.Deduct(customers[0].ID, 320, "booking", b1.ID, "预订城北篮球馆18-20点")
		depositSvc.FreezeDeposit(b1.ID, customers[0].ID, venues[0].ID, venues[0].DepositAmount, "篮球馆预订押金")
		b1.Status = models.BookingPaid
		b1.PaidAmount = 320
		database.Save(&b1)
		bookings = append(bookings, b1)

		// 2. 周敏-篮球馆-已支付-未来预订
		b2 := models.Booking{
			VenueID: venues[0].ID, CustomerID: customers[1].ID,
			CustomerName: "周敏", Phone: "13700002222",
			BookDate: "2026-06-21", StartHour: 20, EndHour: 21,
			Amount: 160, DepositAmount: venues[0].DepositAmount,
		}
		database.Create(&b2)
		walletSvc.Deduct(customers[1].ID, 160, "booking", b2.ID, "预订城北篮球馆20-21点")
		depositSvc.FreezeDeposit(b2.ID, customers[1].ID, venues[0].ID, venues[0].DepositAmount, "篮球馆预订押金")
		b2.Status = models.BookingPaid
		b2.PaidAmount = 160
		database.Save(&b2)
		bookings = append(bookings, b2)

		// 3. 黄磊-游泳馆-已完成
		b3 := models.Booking{
			VenueID: venues[1].ID, CustomerID: customers[2].ID,
			CustomerName: "黄磊", Phone: "13700003333",
			BookDate: "2026-06-18", StartHour: 7, EndHour: 9,
			Amount: 160, PaidAmount: 160, Status: models.BookingCompleted,
		}
		database.Create(&b3)
		walletSvc.Deduct(customers[2].ID, 160, "booking", b3.ID, "预订奥体游泳馆07-09点")
		bookings = append(bookings, b3)

		// 4. 吴静-足球公园-已取消（退订退款80%，扣违约金20%）
		b4 := models.Booking{
			VenueID: venues[3].ID, CustomerID: customers[3].ID,
			CustomerName: "吴静", Phone: "13700004444",
			BookDate: "2026-06-15", StartHour: 15, EndHour: 17,
			Amount: 600, PaidAmount: 600, RefundedAmount: 480, PenaltyAmount: 120,
			Status: models.BookingCancelled, CancelReason: "临时有事，提前10h取消",
		}
		database.Create(&b4)
		walletSvc.Deduct(customers[3].ID, 600, "booking", b4.ID, "预订滨江足球公园15-17点")
		walletSvc.Refund(customers[3].ID, 480, "booking", b4.ID, "退订退款-提前10h退80%")
		walletSvc.PenaltyDeduct(customers[3].ID, 120, "booking", b4.ID, "退订违约金-扣20%")
		bookings = append(bookings, b4)

		// 5. 李勇-羽毛球馆-已取消（不足2h，全额扣违约金）
		b5 := models.Booking{
			VenueID: venues[2].ID, CustomerID: customers[4].ID,
			CustomerName: "李勇", Phone: "13700005555",
			BookDate: "2026-06-19", StartHour: 19, EndHour: 21,
			Amount: 100, PaidAmount: 100, RefundedAmount: 0, PenaltyAmount: 100,
			Status: models.BookingCancelled, CancelReason: "临近取消，不足2h",
		}
		database.Create(&b5)
		walletSvc.Deduct(customers[4].ID, 100, "booking", b5.ID, "预订羽毛球馆19-21点")
		walletSvc.PenaltyDeduct(customers[4].ID, 100, "booking", b5.ID, "退订违约金-不足2h全额扣")
		bookings = append(bookings, b5)

		// 6. 赵敏-篮球馆-爽约
		b6 := models.Booking{
			VenueID: venues[0].ID, CustomerID: customers[5].ID,
			CustomerName: "赵敏", Phone: "13700006666",
			BookDate: "2026-06-17", StartHour: 14, EndHour: 16,
			Amount: 320, PaidAmount: 320, PenaltyAmount: 100,
			Status: models.BookingNoShow, CancelReason: "到点未到场，系统判定爽约",
			DepositAmount: venues[0].DepositAmount,
		}
		database.Create(&b6)
		walletSvc.Deduct(customers[5].ID, 320, "booking", b6.ID, "预订篮球馆14-16点")
		depositSvc.FreezeDeposit(b6.ID, customers[5].ID, venues[0].ID, venues[0].DepositAmount, "篮球馆预订押金")
		walletSvc.PenaltyDeduct(customers[5].ID, 100, "booking", b6.ID, "爽约违约金-固定100元")
		dep, _ := depositSvc.GetDepositByBooking(b6.ID)
		if dep != nil {
			depositSvc.DeductDeposit(dep.ID, venues[0].DepositAmount, "爽约扣押金")
		}
		bookings = append(bookings, b6)

		log.Println("已创建预订与关联流水种子数据")
	}

	log.Println("种子数据初始化完成")
	return nil
}
