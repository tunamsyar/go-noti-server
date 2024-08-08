package notification

import (
	"encoding/json"
	"go-noti-server/internal/log"
	"os"
	"strings"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var db *gorm.DB

func InitDB() (*gorm.DB, error) {
	dbFile := "notifications.db"
	if _, err := os.Stat(dbFile); os.IsNotExist(err) {
		file, err := os.Create(dbFile)
		if err != nil {
			log.ErrorLogger.Fatalf("Failed to create database file: %v", err)
		}
		file.Close()
	}

	var err error
	db, err = gorm.Open(sqlite.Open(dbFile), &gorm.Config{})
	if err != nil {
		log.ErrorLogger.Fatalf("Failed to connect to the database: %v", err)
	}

	err = db.AutoMigrate(&Notification{})
	if err != nil {
		log.ErrorLogger.Fatalf("Failed to migrate database: %v", err)
	}
	return db, nil
}

type Notification struct {
	gorm.Model
	Message        string `gorm:"type:string"`
	Title          string `gorm:"type:string"`
	Body           string `gorm:"type:string"`
	Image          string `gorm:"type:string"`
	DeviceTokens   string `gorm:"type:text"`
	AnalyticsLabel string `gorm:"type:text"`
	Data           string `gorm:"type:text"`
	Processed      bool   `gorm:"column:processed"`
	Processing     bool   `gorm:"column:processing"`
}

func SaveNotification(n Notification) error {
	return db.Create(&n).Error
}

func FetchPendingNotifications() ([]Notification, error) {
	var notifications []Notification
	err := db.Where("processed = ?", false).Find(&notifications).Error
	return notifications, err
}

func FetchOnePendingNotification() (Notification, error) {
	var notification Notification

	err := db.Model(&Notification{}).Where("processed = ?", false).First(&notification).Error
	return notification, err
}

func MarkNotificationAsProcessed(id uint) error {
	return db.Model(&Notification{}).Where("id = ?", id).Update("processed", true).Error
}

func split(s, sep string) []string {
	return strings.Split(s, sep)
}

func parseData(data string) map[string]string {
	var result map[string]string
	err := json.Unmarshal([]byte(data), &result)
	if err != nil {
		log.InfoLogger.Printf("Error parsing data: %v", err)
		return nil
	}

	return result
}
