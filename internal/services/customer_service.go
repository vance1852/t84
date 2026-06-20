package services

import (
	"errors"

	"gorm.io/gorm"

	"venue-booking-admin/internal/models"
)

// CustomerService 客户服务。
type CustomerService struct {
	DB *gorm.DB
}

// NewCustomerService 创建客户服务。
func NewCustomerService(db *gorm.DB) *CustomerService {
	return &CustomerService{DB: db}
}

// CreateCustomer 创建客户。
func (s *CustomerService) CreateCustomer(name, phone string) (*models.Customer, error) {
	if name == "" || phone == "" {
		return nil, errors.New("姓名和手机号不能为空")
	}
	var existing models.Customer
	if err := s.DB.Where("phone = ?", phone).First(&existing).Error; err == nil {
		return &existing, nil
	}
	customer := &models.Customer{
		Name:   name,
		Phone:  phone,
		Status: "active",
	}
	if err := s.DB.Create(customer).Error; err != nil {
		return nil, err
	}
	return customer, nil
}

// GetCustomer 获取客户。
func (s *CustomerService) GetCustomer(id uint) (*models.Customer, error) {
	var customer models.Customer
	if err := s.DB.First(&customer, id).Error; err != nil {
		return nil, err
	}
	return &customer, nil
}

// GetCustomerByPhone 根据手机号查询客户。
func (s *CustomerService) GetCustomerByPhone(phone string) (*models.Customer, error) {
	var customer models.Customer
	if err := s.DB.Where("phone = ?", phone).First(&customer).Error; err != nil {
		return nil, err
	}
	return &customer, nil
}

// ListCustomers 查询客户列表。
func (s *CustomerService) ListCustomers(keyword string, status string, page, pageSize int) ([]models.Customer, int64, error) {
	var customers []models.Customer
	var total int64

	q := s.DB.Model(&models.Customer{})
	if keyword != "" {
		q = q.Where("name LIKE ? OR phone LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	if err := q.Order("id desc").Offset(offset).Limit(pageSize).Find(&customers).Error; err != nil {
		return nil, 0, err
	}
	return customers, total, nil
}

// UpdateCustomer 更新客户信息。
func (s *CustomerService) UpdateCustomer(id uint, name, phone, status string) (*models.Customer, error) {
	var customer models.Customer
	if err := s.DB.First(&customer, id).Error; err != nil {
		return nil, errors.New("客户不存在")
	}
	if name != "" {
		customer.Name = name
	}
	if phone != "" {
		customer.Phone = phone
	}
	if status != "" {
		customer.Status = status
	}
	if err := s.DB.Save(&customer).Error; err != nil {
		return nil, err
	}
	return &customer, nil
}
