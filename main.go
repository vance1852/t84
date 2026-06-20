package main

import (
	"log"

	"github.com/gin-gonic/gin"

	"venue-booking-admin/internal/auth"
	"venue-booking-admin/internal/config"
	"venue-booking-admin/internal/db"
	"venue-booking-admin/internal/handlers"
	"venue-booking-admin/internal/seed"
)

func main() {
	cfg := config.Load()
	auth.SetSecret(cfg.JWTSecret)

	database, err := db.Connect(cfg.DSN)
	if err != nil {
		log.Fatalf("无法连接数据库: %v", err)
	}
	if err := db.Migrate(database); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	if err := seed.Run(database, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		log.Fatalf("种子数据初始化失败: %v", err)
	}

	h := &handlers.Handler{DB: database}
	h.InitServices()

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	api := r.Group("/api")
	{
		api.GET("/health", h.Health)
		api.POST("/auth/login", h.Login)

		secured := api.Group("")
		secured.Use(auth.Middleware(database))
		{
			secured.GET("/auth/me", h.Me)

			// 场馆
			secured.GET("/venues", h.ListVenues)
			secured.POST("/venues", h.CreateVenue)
			secured.GET("/venues/:id", h.GetVenue)
			secured.PUT("/venues/:id", h.UpdateVenue)
			secured.DELETE("/venues/:id", h.DeleteVenue)

			// 客户
			secured.GET("/customers", h.ListCustomers)
			secured.POST("/customers", h.CreateCustomer)
			secured.GET("/customers/:id", h.GetCustomer)
			secured.PUT("/customers/:id", h.UpdateCustomer)

			// 钱包
			secured.GET("/wallet", h.GetWallet)
			secured.GET("/wallet/:customer_id", h.GetWallet)
			secured.POST("/wallet/recharge", h.WalletRecharge)
			secured.POST("/wallet/deduct", h.WalletDeduct)
			secured.POST("/wallet/freeze", h.WalletFreeze)
			secured.POST("/wallet/unfreeze", h.WalletUnfreeze)
			secured.POST("/wallet/refund", h.WalletRefund)
			secured.GET("/wallet/transactions", h.ListWalletTransactions)

			// 预订（接入钱包全流程）
			secured.GET("/bookings", h.ListBookings)
			secured.POST("/bookings", h.CreateBooking)
			secured.POST("/bookings/:id/cancel", h.CancelBooking)
			secured.GET("/bookings/:id/cancel-preview", h.PreviewCancellation)
			secured.POST("/bookings/:id/no-show", h.MarkBookingNoShow)
			secured.POST("/bookings/:id/complete", h.CompleteBooking)

			// 退订规则
			secured.GET("/cancellation-rules", h.ListCancellationRules)
			secured.POST("/cancellation-rules", h.CreateCancellationRule)
			secured.PUT("/cancellation-rules/:id", h.UpdateCancellationRule)
			secured.DELETE("/cancellation-rules/:id", h.DeleteCancellationRule)

			// 爽约规则
			secured.GET("/no-show-rules", h.ListNoShowRules)
			secured.POST("/no-show-rules", h.CreateNoShowRule)

			// 押金管理
			secured.GET("/deposits", h.ListDeposits)
			secured.GET("/deposits/booking/:booking_id", h.GetDepositByBooking)
			secured.POST("/deposits/:id/refund", h.RefundDeposit)
			secured.POST("/deposits/:id/deduct", h.DeductDeposit)

			// 对账
			secured.POST("/reconciliation/run", h.DoReconciliation)
			secured.GET("/reconciliation", h.ListReconciliations)
			secured.GET("/reconciliation/:id/diffs", h.GetReconciliationDiffs)
			secured.GET("/reconciliation/wallet-check", h.WalletBalanceCheck)

			// 统计报表
			secured.GET("/stats/cancellation", h.CancellationStats)
			secured.GET("/stats/no-show", h.NoShowStats)
			secured.GET("/stats/revenue", h.RevenueStats)
			secured.GET("/stats/by-venue", h.VenueStats)

			secured.GET("/dashboard/stats", h.DashboardStats)
		}
	}

	log.Printf("venue-booking-admin listening on :%s", cfg.Port)
	if err := r.Run("0.0.0.0:" + cfg.Port); err != nil {
		log.Fatal(err)
	}
}
