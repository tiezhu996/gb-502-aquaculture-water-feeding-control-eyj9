package database

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"aquaculture-water-feeding-control/backend/internal/model"
	"fmt"
	"log"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Open(databaseURL, environment string) (*gorm.DB, error) {
	level := logger.Warn
	if environment == "development" {
		level = logger.Info
	}
	db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{
		Logger:  logger.Default.LogMode(level),
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.AutoMigrate(
		&model.User{},
		&model.Pond{},
		&model.WaterReading{},
		&model.FeedingPlan{},
		&model.ControlExecution{},
		&model.FeedBatch{},
		&model.FeedConsumption{},
		&model.AuditLog{},
	); err != nil {
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	if err := seed(db); err != nil {
		return nil, fmt.Errorf("seed database: %w", err)
	}
	return db, nil
}

func seed(db *gorm.DB) error {
	users := []struct {
		Username, DisplayName, Password string
		Role                            constants.Role
	}{
		{"admin", "系统管理员", "admin123", constants.RoleAdmin},
		{"manager", "生产主管", "manager123", constants.RoleManager},
		{"operator", "值班操作员", "operator123", constants.RoleOperator},
		{"viewer", "观察员", "viewer123", constants.RoleViewer},
	}
	for _, entry := range users {
		var count int64
		if err := db.Model(&model.User{}).Where("username = ?", entry.Username).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(entry.Password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		user := model.User{Username: entry.Username, DisplayName: entry.DisplayName, PasswordHash: string(hash), Role: entry.Role, Active: true}
		if err := db.Create(&user).Error; err != nil {
			return err
		}
	}
	var pondCount int64
	if err := db.Model(&model.Pond{}).Count(&pondCount).Error; err != nil {
		return err
	}
	if pondCount == 0 {
		ponds := []model.Pond{
			{Code: "P-A01", Name: "东区一号塘", Species: "南美白对虾", AreaSquareMeters: 3600, CapacityKg: 12000, GrowthStage: "成长期", Status: constants.PondStatusActive, Manager: "李海", Notes: "主生产塘"},
			{Code: "P-B03", Name: "西区三号塘", Species: "加州鲈鱼", AreaSquareMeters: 2800, CapacityKg: 8500, GrowthStage: "幼鱼期", Status: constants.PondStatusQuarantine, Manager: "王宁", Notes: "近期氨氮偏高"},
		}
		if err := db.Create(&ponds).Error; err != nil {
			return err
		}
		log.Printf("seeded %d ponds", len(ponds))
	}
	var batchCount int64
	if err := db.Model(&model.FeedBatch{}).Count(&batchCount).Error; err != nil {
		return err
	}
	if batchCount == 0 {
		today := time.Now().UTC()
		day := func(offsetDays int) time.Time {
			value := today.AddDate(0, 0, offsetDays)
			return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
		}
		batches := []model.FeedBatch{
			{BatchNo: "FB-2026-0901", FeedType: "对虾配合饲料", InboundKg: 500, ExpireDate: day(25), Enabled: true, Notes: "东区主用，先到期先出"},
			{BatchNo: "FB-2026-0902", FeedType: "对虾配合饲料", InboundKg: 800, ExpireDate: day(120), Enabled: true, Notes: "储备批次"},
			{BatchNo: "FB-2026-0801", FeedType: "对虾配合饲料", InboundKg: 120, ExpireDate: day(-10), Enabled: true, Notes: "已过期批次，用于拦截演示"},
			{BatchNo: "FB-2026-0903", FeedType: "鱼用膨化饲料", InboundKg: 400, ExpireDate: day(60), Enabled: true, Notes: "加州鲈鱼专用"},
			{BatchNo: "FB-2026-0701", FeedType: "鱼用膨化饲料", InboundKg: 200, ExpireDate: day(90), Enabled: false, Notes: "停用批次，等待供应商换货"},
		}
		if err := db.Create(&batches).Error; err != nil {
			return err
		}
		log.Printf("seeded %d feed batches", len(batches))
	}
	return nil
}
