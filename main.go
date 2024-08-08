package main

import (
	"go-noti-server/config"
	"go-noti-server/internal/log"
	"go-noti-server/internal/notification"
	"go-noti-server/internal/server"
	"time"
)

func main() {

	config.LoadEnv()
	log.SetupLoggers()

	db, err := notification.InitDB()
	if err != nil {
		log.ErrorLogger.Printf("Error INIT DB: %v", err)
	}

	notificationChan := make(chan notification.Notification, 10)

	for i := 0; i < 10; i++ {
		go notification.Worker(i, notificationChan)
	}

	go func() {
		for {
			var notifications []notification.Notification
			if err := db.Where("processed = ? AND processing = ?", false, false).Find(&notifications).Error; err != nil {
				log.InfoLogger.Printf("Failed to query notifications: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}

			for _, notif := range notifications {
				// Update the notification to indicate it's being processed
				db.Model(&notification.Notification{}).Where("id = ?", notif.ID).Update("processing", true)
				notificationChan <- notif
			}

			time.Sleep(5 * time.Second) // Sleep to avoid busy waiting
		}
	}()

	server.RunGrpcServer()
}
