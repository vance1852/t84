package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"venue-booking-admin/internal/auth"
	"venue-booking-admin/internal/models"
	"venue-booking-admin/internal/services"
)

// Handler 持有数据库句柄与各业务服务。
type Handler struct {
	DB              *gorm.DB
	WalletSvc       *services.WalletService
	CancellationSvc *services.CancellationService
	DepositSvc      *services.DepositService
	BookingSvc      *services.BookingService
	ReconSvc        *services.ReconciliationService
	StatsSvc        *services.StatsService
	CustomerSvc     *services.CustomerService
}

// InitServices 初始化所有业务服务。
func (h *Handler) InitServices() {
	h.WalletSvc = services.NewWalletService(h.DB)
	h.CancellationSvc = services.NewCancellationService(h.DB)
	h.DepositSvc = services.NewDepositService(h.DB, h.WalletSvc)
	h.BookingSvc = services.NewBookingService(h.DB, h.WalletSvc, h.CancellationSvc, h.DepositSvc)
	h.ReconSvc = services.NewReconciliationService(h.DB)
	h.StatsSvc = services.NewStatsService(h.DB)
	h.CustomerSvc = services.NewCustomerService(h.DB)
}

// parsePage 解析分页参数。
func parsePage(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}

// parseUintParam 解析 uint 参数。
func parseUintParam(c *gin.Context, name string) (uint, error) {
	v, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(v), nil
}

// parseUintQuery 解析 uint query 参数。
func parseUintQuery(c *gin.Context, name string) uint {
	v, err := strconv.ParseUint(c.Query(name), 10, 64)
	if err != nil {
		return 0
	}
	return uint(v)
}

// ---------- 认证 ----------

type loginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (h *Handler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	var user models.User
	if err := h.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "用户名或密码错误"})
		return
	}
	if !auth.VerifyPassword(req.Password, user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "用户名或密码错误"})
		return
	}
	token, err := auth.CreateToken(user.ID, user.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "签发令牌失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"access_token": token, "token_type": "bearer"})
}

func (h *Handler) Me(c *gin.Context) {
	user := c.MustGet("user").(models.User)
	c.JSON(http.StatusOK, gin.H{"id": user.ID, "username": user.Username, "display_name": user.DisplayName})
}

// ---------- 场馆 ----------

type venueReq struct {
	Name           string  `json:"name" binding:"required"`
	SportType      string  `json:"sport_type"`
	Capacity       int     `json:"capacity"`
	HourlyPrice    float64 `json:"hourly_price"`
	OpenHour       int     `json:"open_hour"`
	CloseHour      int     `json:"close_hour"`
	Status         string  `json:"status"`
	RequireDeposit bool    `json:"require_deposit"`
	DepositAmount  float64 `json:"deposit_amount"`
}

func (h *Handler) ListVenues(c *gin.Context) {
	var venues []models.Venue
	h.DB.Order("id").Find(&venues)
	c.JSON(http.StatusOK, venues)
}

func (h *Handler) CreateVenue(c *gin.Context) {
	var req venueReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	if req.CloseHour <= req.OpenHour {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "关闭时间须晚于开放时间"})
		return
	}
	status := req.Status
	if status == "" {
		status = "open"
	}
	venue := models.Venue{
		Name: req.Name, SportType: req.SportType, Capacity: req.Capacity,
		HourlyPrice: req.HourlyPrice, OpenHour: req.OpenHour, CloseHour: req.CloseHour, Status: status,
		RequireDeposit: req.RequireDeposit, DepositAmount: req.DepositAmount,
	}
	h.DB.Create(&venue)
	c.JSON(http.StatusCreated, venue)
}

func (h *Handler) GetVenue(c *gin.Context) {
	var venue models.Venue
	if err := h.DB.First(&venue, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "场馆不存在"})
		return
	}
	c.JSON(http.StatusOK, venue)
}

func (h *Handler) UpdateVenue(c *gin.Context) {
	var venue models.Venue
	if err := h.DB.First(&venue, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "场馆不存在"})
		return
	}
	var req venueReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	venue.Name = req.Name
	venue.SportType = req.SportType
	venue.Capacity = req.Capacity
	venue.HourlyPrice = req.HourlyPrice
	if req.OpenHour != 0 || req.CloseHour != 0 {
		venue.OpenHour = req.OpenHour
		venue.CloseHour = req.CloseHour
	}
	if req.Status != "" {
		venue.Status = req.Status
	}
	venue.RequireDeposit = req.RequireDeposit
	venue.DepositAmount = req.DepositAmount
	h.DB.Save(&venue)
	c.JSON(http.StatusOK, venue)
}

func (h *Handler) DeleteVenue(c *gin.Context) {
	var venue models.Venue
	if err := h.DB.First(&venue, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "场馆不存在"})
		return
	}
	h.DB.Delete(&venue)
	c.Status(http.StatusNoContent)
}

// ---------- 客户 ----------

type customerReq struct {
	Name   string `json:"name" binding:"required"`
	Phone  string `json:"phone" binding:"required"`
	Status string `json:"status"`
}

func (h *Handler) CreateCustomer(c *gin.Context) {
	var req customerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	customer, err := h.CustomerSvc.CreateCustomer(req.Name, req.Phone)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, customer)
}

func (h *Handler) ListCustomers(c *gin.Context) {
	keyword := c.Query("keyword")
	status := c.Query("status")
	page, pageSize := parsePage(c)
	customers, total, err := h.CustomerSvc.ListCustomers(keyword, status, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": customers, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) GetCustomer(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "ID不合法"})
		return
	}
	customer, err := h.CustomerSvc.GetCustomer(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "客户不存在"})
		return
	}
	c.JSON(http.StatusOK, customer)
}

func (h *Handler) UpdateCustomer(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "ID不合法"})
		return
	}
	var req customerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	customer, err := h.CustomerSvc.UpdateCustomer(id, req.Name, req.Phone, req.Status)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, customer)
}

// ---------- 钱包 ----------

type rechargeReq struct {
	CustomerID uint    `json:"customer_id" binding:"required"`
	Amount     float64 `json:"amount" binding:"required"`
	Remark     string  `json:"remark"`
}

func (h *Handler) WalletRecharge(c *gin.Context) {
	var req rechargeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	wallet, tx, err := h.WalletSvc.Recharge(req.CustomerID, req.Amount, req.Remark)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"wallet": wallet, "transaction": tx})
}

type deductReq struct {
	CustomerID  uint    `json:"customer_id" binding:"required"`
	Amount      float64 `json:"amount" binding:"required"`
	RelatedType string  `json:"related_type"`
	RelatedID   uint    `json:"related_id"`
	Remark      string  `json:"remark"`
}

func (h *Handler) WalletDeduct(c *gin.Context) {
	var req deductReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	tx, err := h.WalletSvc.Deduct(req.CustomerID, req.Amount, req.RelatedType, req.RelatedID, req.Remark)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, tx)
}

type freezeReq struct {
	CustomerID  uint    `json:"customer_id" binding:"required"`
	Amount      float64 `json:"amount" binding:"required"`
	FreezeType  string  `json:"freeze_type"`
	RelatedType string  `json:"related_type"`
	RelatedID   uint    `json:"related_id"`
	Remark      string  `json:"remark"`
}

func (h *Handler) WalletFreeze(c *gin.Context) {
	var req freezeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	freeze, err := h.WalletSvc.Freeze(req.CustomerID, req.Amount, req.FreezeType, req.RelatedType, req.RelatedID, req.Remark)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, freeze)
}

type unfreezeReq struct {
	FreezeID uint   `json:"freeze_id" binding:"required"`
	Remark   string `json:"remark"`
}

func (h *Handler) WalletUnfreeze(c *gin.Context) {
	var req unfreezeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	if err := h.WalletSvc.Unfreeze(req.FreezeID, req.Remark); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

type refundReq struct {
	CustomerID  uint    `json:"customer_id" binding:"required"`
	Amount      float64 `json:"amount" binding:"required"`
	RelatedType string  `json:"related_type"`
	RelatedID   uint    `json:"related_id"`
	Remark      string  `json:"remark"`
}

func (h *Handler) WalletRefund(c *gin.Context) {
	var req refundReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	tx, err := h.WalletSvc.Refund(req.CustomerID, req.Amount, req.RelatedType, req.RelatedID, req.Remark)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, tx)
}

func (h *Handler) GetWallet(c *gin.Context) {
	customerID := parseUintQuery(c, "customer_id")
	if customerID == 0 {
		id, err := parseUintParam(c, "customer_id")
		if err != nil {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "客户ID不合法"})
			return
		}
		customerID = id
	}
	wallet, err := h.WalletSvc.GetWallet(customerID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, wallet)
}

func (h *Handler) ListWalletTransactions(c *gin.Context) {
	customerID := parseUintQuery(c, "customer_id")
	txType := c.Query("tx_type")
	page, pageSize := parsePage(c)
	txs, total, err := h.WalletSvc.ListTransactions(customerID, txType, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": txs, "total": total, "page": page, "page_size": pageSize})
}

// ---------- 预订（接入钱包） ----------

type bookingReq struct {
	VenueID      uint   `json:"venue_id" binding:"required"`
	CustomerID   uint   `json:"customer_id"`
	CustomerName string `json:"customer_name" binding:"required"`
	Phone        string `json:"phone"`
	BookDate     string `json:"book_date" binding:"required"`
	StartHour    int    `json:"start_hour"`
	EndHour      int    `json:"end_hour"`
	UseWallet    bool   `json:"use_wallet"`
}

func (h *Handler) ListBookings(c *gin.Context) {
	venueID := parseUintQuery(c, "venue_id")
	customerID := parseUintQuery(c, "customer_id")
	status := c.Query("status")
	date := c.Query("date")
	page, pageSize := parsePage(c)
	bookings, total, err := h.BookingSvc.ListBookings(venueID, customerID, status, date, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": bookings, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) CreateBooking(c *gin.Context) {
	var req bookingReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	booking, err := h.BookingSvc.CreateBookingWithWallet(services.CreateBookingParams{
		VenueID:      req.VenueID,
		CustomerID:   req.CustomerID,
		CustomerName: req.CustomerName,
		Phone:        req.Phone,
		BookDate:     req.BookDate,
		StartHour:    req.StartHour,
		EndHour:      req.EndHour,
		UseWallet:    req.UseWallet,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, booking)
}

type cancelBookingReq struct {
	Reason string `json:"reason"`
}

func (h *Handler) CancelBooking(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "ID不合法"})
		return
	}
	var req cancelBookingReq
	if err := c.ShouldBindJSON(&req); err != nil {
		req.Reason = "用户主动取消"
	}
	booking, result, err := h.BookingSvc.CancelBooking(id, req.Reason)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error(), "result": result})
		return
	}
	c.JSON(http.StatusOK, gin.H{"booking": booking, "cancel_result": result})
}

type noShowReq struct {
	Reason string `json:"reason"`
}

func (h *Handler) MarkBookingNoShow(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "ID不合法"})
		return
	}
	var req noShowReq
	if err := c.ShouldBindJSON(&req); err != nil {
		req.Reason = "爽约"
	}
	booking, result, err := h.BookingSvc.MarkNoShow(id, req.Reason)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"booking": booking, "no_show_result": result})
}

type completeBookingReq struct {
	Remark string `json:"remark"`
}

func (h *Handler) CompleteBooking(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "ID不合法"})
		return
	}
	var req completeBookingReq
	c.ShouldBindJSON(&req)
	booking, err := h.BookingSvc.CompleteBooking(id, req.Remark)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, booking)
}

func (h *Handler) PreviewCancellation(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "ID不合法"})
		return
	}
	booking, err := h.BookingSvc.GetBooking(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "预订不存在"})
		return
	}
	result, err := h.CancellationSvc.CalculateCancellation(booking)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// ---------- 退订规则 ----------

type cancelRuleReq struct {
	VenueID             uint    `json:"venue_id"`
	Name                string  `json:"name" binding:"required"`
	MinHoursBeforeStart int     `json:"min_hours_before_start"`
	MaxHoursBeforeStart int     `json:"max_hours_before_start"`
	RefundRate          float64 `json:"refund_rate"`
	PenaltyRate         float64 `json:"penalty_rate"`
	PenaltyFixed        float64 `json:"penalty_fixed"`
	IsDefault           bool    `json:"is_default"`
	Priority            int     `json:"priority"`
}

func (h *Handler) ListCancellationRules(c *gin.Context) {
	venueID := parseUintQuery(c, "venue_id")
	rules, err := h.CancellationSvc.ListRules(venueID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rules)
}

func (h *Handler) CreateCancellationRule(c *gin.Context) {
	var req cancelRuleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	rule := &models.CancellationRule{
		VenueID:             req.VenueID,
		Name:                req.Name,
		MinHoursBeforeStart: req.MinHoursBeforeStart,
		MaxHoursBeforeStart: req.MaxHoursBeforeStart,
		RefundRate:          req.RefundRate,
		PenaltyRate:         req.PenaltyRate,
		PenaltyFixed:        req.PenaltyFixed,
		IsDefault:           req.IsDefault,
		Priority:            req.Priority,
	}
	if err := h.CancellationSvc.CreateRule(rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, rule)
}

func (h *Handler) UpdateCancellationRule(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "ID不合法"})
		return
	}
	var req cancelRuleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	updates := map[string]interface{}{
		"name":                  req.Name,
		"min_hours_before_start": req.MinHoursBeforeStart,
		"max_hours_before_start": req.MaxHoursBeforeStart,
		"refund_rate":           req.RefundRate,
		"penalty_rate":          req.PenaltyRate,
		"penalty_fixed":         req.PenaltyFixed,
		"is_default":            req.IsDefault,
		"priority":              req.Priority,
	}
	if err := h.CancellationSvc.UpdateRule(id, updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "status": "updated"})
}

func (h *Handler) DeleteCancellationRule(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "ID不合法"})
		return
	}
	if err := h.CancellationSvc.DeleteRule(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// ---------- 爽约规则 ----------

type noShowRuleReq struct {
	VenueID       uint    `json:"venue_id"`
	PenaltyRate   float64 `json:"penalty_rate"`
	PenaltyFixed  float64 `json:"penalty_fixed"`
	DeductDeposit bool    `json:"deduct_deposit"`
}

func (h *Handler) ListNoShowRules(c *gin.Context) {
	venueID := parseUintQuery(c, "venue_id")
	rules, err := h.CancellationSvc.ListNoShowRules(venueID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rules)
}

func (h *Handler) CreateNoShowRule(c *gin.Context) {
	var req noShowRuleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	rule := &models.NoShowRule{
		VenueID:       req.VenueID,
		PenaltyRate:   req.PenaltyRate,
		PenaltyFixed:  req.PenaltyFixed,
		DeductDeposit: req.DeductDeposit,
	}
	if err := h.CancellationSvc.CreateNoShowRule(rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, rule)
}

// ---------- 押金 ----------

type depositDeductReq struct {
	Amount float64 `json:"amount" binding:"required"`
	Remark string  `json:"remark"`
}

type depositRefundReq struct {
	Amount float64 `json:"amount"`
	Remark string  `json:"remark"`
}

func (h *Handler) ListDeposits(c *gin.Context) {
	customerID := parseUintQuery(c, "customer_id")
	venueID := parseUintQuery(c, "venue_id")
	status := c.Query("status")
	page, pageSize := parsePage(c)
	deposits, total, err := h.DepositSvc.ListDeposits(customerID, venueID, status, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": deposits, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) GetDepositByBooking(c *gin.Context) {
	bookingID, err := parseUintParam(c, "booking_id")
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "预订ID不合法"})
		return
	}
	deposit, err := h.DepositSvc.GetDepositByBooking(bookingID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "押金记录不存在"})
		return
	}
	c.JSON(http.StatusOK, deposit)
}

func (h *Handler) RefundDeposit(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "ID不合法"})
		return
	}
	var req depositRefundReq
	c.ShouldBindJSON(&req)
	if req.Amount > 0 {
		if err := h.DepositSvc.PartialRefundDeposit(id, req.Amount, req.Remark); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
			return
		}
	} else {
		if err := h.DepositSvc.RefundDeposit(id, req.Remark); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "refunded"})
}

func (h *Handler) DeductDeposit(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "ID不合法"})
		return
	}
	var req depositDeductReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "请求参数不合法"})
		return
	}
	if err := h.DepositSvc.DeductDeposit(id, req.Amount, req.Remark); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deducted"})
}

// ---------- 对账 ----------

func (h *Handler) DoReconciliation(c *gin.Context) {
	date := c.Query("date")
	if date == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "必须指定对账日期"})
		return
	}
	recon, diffs, err := h.ReconSvc.DoReconciliation(date)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"reconciliation": recon, "diffs": diffs})
}

func (h *Handler) ListReconciliations(c *gin.Context) {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	status := c.Query("status")
	page, pageSize := parsePage(c)
	list, total, err := h.ReconSvc.ListReconciliations(startDate, endDate, status, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": list, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) GetReconciliationDiffs(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"detail": "ID不合法"})
		return
	}
	diffs, err := h.ReconSvc.GetReconciliationDiffs(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, diffs)
}

func (h *Handler) WalletBalanceCheck(c *gin.Context) {
	result, mismatches, err := h.ReconSvc.WalletBalanceRecon()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"summary": result, "mismatch_wallet_ids": mismatches})
}

// ---------- 统计报表 ----------

func (h *Handler) CancellationStats(c *gin.Context) {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	venueID := parseUintQuery(c, "venue_id")
	stats, err := h.StatsSvc.GetCancellationStats(startDate, endDate, venueID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

func (h *Handler) NoShowStats(c *gin.Context) {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	venueID := parseUintQuery(c, "venue_id")
	stats, err := h.StatsSvc.GetNoShowStats(startDate, endDate, venueID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

func (h *Handler) RevenueStats(c *gin.Context) {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	venueID := parseUintQuery(c, "venue_id")
	stats, err := h.StatsSvc.GetRevenueStats(startDate, endDate, venueID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

func (h *Handler) VenueStats(c *gin.Context) {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	stats, err := h.StatsSvc.GetStatsByVenue(startDate, endDate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// ---------- 仪表盘 ----------

func (h *Handler) DashboardStats(c *gin.Context) {
	var venueTotal, venueOpen, bookingTotal, bookingActive int64
	h.DB.Model(&models.Venue{}).Count(&venueTotal)
	h.DB.Model(&models.Venue{}).Where("status = ?", "open").Count(&venueOpen)
	h.DB.Model(&models.Booking{}).Count(&bookingTotal)
	h.DB.Model(&models.Booking{}).Where("status = ? OR status = ?", models.BookingBooked, models.BookingPaid).Count(&bookingActive)

	var revenue float64
	h.DB.Model(&models.Booking{}).Where("status <> ?", models.BookingCancelled).
		Select("COALESCE(SUM(paid_amount), 0)").Scan(&revenue)
	if revenue == 0 {
		h.DB.Model(&models.Booking{}).Where("status <> ?", models.BookingCancelled).
			Select("COALESCE(SUM(amount), 0)").Scan(&revenue)
	}

	var totalWalletBalance, totalFrozen float64
	h.DB.Model(&models.Wallet{}).Select("COALESCE(SUM(balance), 0)").Scan(&totalWalletBalance)
	h.DB.Model(&models.Wallet{}).Select("COALESCE(SUM(frozen_amount), 0)").Scan(&totalFrozen)

	var cancelCount, noShowCount int64
	h.DB.Model(&models.Booking{}).Where("status = ?", models.BookingCancelled).Count(&cancelCount)
	h.DB.Model(&models.Booking{}).Where("status = ?", models.BookingNoShow).Count(&noShowCount)

	c.JSON(http.StatusOK, gin.H{
		"venue_total":         venueTotal,
		"venue_open":          venueOpen,
		"booking_total":       bookingTotal,
		"booking_active":      bookingActive,
		"revenue_total":       revenue,
		"total_wallet_balance": totalWalletBalance,
		"total_frozen":        totalFrozen,
		"cancel_count":        cancelCount,
		"no_show_count":       noShowCount,
	})
}

// Health 健康检查。
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "venue-booking-admin"})
}
