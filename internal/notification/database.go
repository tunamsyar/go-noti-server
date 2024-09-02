package notification

import (
	"encoding/json"
	"fmt"
	"go-noti-server/internal/log"
	"os"
	"strings"
	"time"

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
	const maxRetries int = 10

	for i := 0; i < maxRetries; i++ {
		err := db.Create(&n).Error
		if err == nil {
			return nil
		}

		if strings.Contains(err.Error(), "database is locked") {
			log.InfoLogger.Printf("Retrying to save notification: %v", err)
			time.Sleep(time.Duration(i+1) * time.Second) // Exponential backoff
			continue
		}

		return err
	}

	log.ErrorLogger.Errorf("Failed to save after %v tries", maxRetries)

	return fmt.Errorf("failed to save notification after %d retries", maxRetries)
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
	return db.Transaction(func(tx *gorm.DB) error {
		if err := db.Model(&Notification{}).Where("id = ?", id).Update("processed", true).Error; err != nil {
			return err
		}
		return nil
	})
}

// CleanupOldNotifications deletes notifications older than a specified duration
func CleanupOldNotifications(duration time.Duration) {
	// Define the time threshold for deletion
	threshold := time.Now().Add(-duration)

	var notifications []Notification
	// Delete old notifications
	if err := db.Unscoped().Where("created_at < ?", threshold).Delete(&notifications).Error; err != nil {
		log.InfoLogger.Printf("Error cleaning up old notifications: %v", err)
	}

	if err := db.Exec("VACUUM").Error; err != nil {
		log.InfoLogger.Printf("Error running VACUUM after cleanup: %v", err)
	}
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
