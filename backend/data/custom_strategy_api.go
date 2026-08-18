package data

import (
	"errors"
	"fmt"
	"go-stock/backend/db"
	"go-stock/backend/models"

	"gorm.io/gorm"
)

// ErrCustomStrategyNotFound 表示指定的自定义策略不存在或已被删除。
var ErrCustomStrategyNotFound = errors.New("自定义策略不存在")

type CustomStrategyApi struct{}

func NewCustomStrategyApi() *CustomStrategyApi {
	return &CustomStrategyApi{}
}

func (a *CustomStrategyApi) GetCustomStrategyList(query *models.CustomStrategyQuery) (*models.CustomStrategyPageData, error) {
	var list []models.CustomStrategy
	var total int64

	q := db.Dao.Model(&models.CustomStrategy{})

	if query.Name != "" {
		q = q.Where("name LIKE ?", "%"+query.Name+"%")
	}

	err := q.Count(&total).Error
	if err != nil {
		return nil, err
	}

	page := query.Page
	pageSize := query.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	offset := (page - 1) * pageSize
	err = q.Offset(offset).Limit(pageSize).Order("sort_order ASC, created_at DESC").Find(&list).Error
	if err != nil {
		return nil, err
	}

	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))

	return &models.CustomStrategyPageData{
		List:       list,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}, nil
}

func (a *CustomStrategyApi) GetAllCustomStrategies() *[]models.CustomStrategy {
	var list []models.CustomStrategy
	db.Dao.Model(&models.CustomStrategy{}).Order("sort_order ASC, created_at DESC").Find(&list)
	return &list
}

// GetByID 按稳定 ID 读取策略，供定时任务在每次执行时获取最新条件。
func (a *CustomStrategyApi) GetByID(id uint) (*models.CustomStrategy, error) {
	if id == 0 {
		return nil, ErrCustomStrategyNotFound
	}
	var strategy models.CustomStrategy
	if err := db.Dao.First(&strategy, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: id=%d", ErrCustomStrategyNotFound, id)
		}
		return nil, fmt.Errorf("查询自定义策略失败: %w", err)
	}
	return &strategy, nil
}

func (a *CustomStrategyApi) SaveCustomStrategy(strategy models.CustomStrategy) string {
	if strategy.ID == 0 {
		err := db.Dao.Model(&models.CustomStrategy{}).Create(&models.CustomStrategy{
			Name:        strategy.Name,
			Query:       strategy.Query,
			Description: strategy.Description,
			SortOrder:   strategy.SortOrder,
		}).Error
		if err != nil {
			return "添加失败"
		}
		return "添加成功"
	}
	var existing models.CustomStrategy
	db.Dao.Model(&models.CustomStrategy{}).Where("id=?", strategy.ID).First(&existing)
	if existing.ID == 0 {
		return "策略不存在"
	}
	err := db.Dao.Model(&models.CustomStrategy{}).Where("id=?", strategy.ID).Updates(map[string]any{
		"name":        strategy.Name,
		"query":       strategy.Query,
		"description": strategy.Description,
		"sort_order":  strategy.SortOrder,
	}).Error
	if err != nil {
		return "更新失败"
	}
	return "更新成功"
}

func (a *CustomStrategyApi) DeleteCustomStrategy(id uint) string {
	var strategy models.CustomStrategy
	db.Dao.Model(&models.CustomStrategy{}).Where("id=?", id).First(&strategy)
	if strategy.ID == 0 {
		return "策略不存在"
	}
	err := db.Dao.Model(&models.CustomStrategy{}).Delete(&strategy).Error
	if err != nil {
		return "删除失败"
	}
	return "删除成功"
}
