package cleanup

import (
	"time"

	"go-noti-server/internal/notification"
)

// ScheduleDailyCleanup schedules the CleanupOldNotifications function to run at a specified time every day
func ScheduleDailyCleanup(duration time.Duration) {
	go func() {
		for {
			// Calculate the next run time
			location, _ := time.LoadLocation("Asia/Singapore")
			now := time.Now().In(location)

			nextRun := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).Add(24 * time.Hour)
			if now.After(nextRun) {
				nextRun = nextRun.Add(24 * time.Hour)
			}

			// Sleep until the next run time
			time.Sleep(time.Until(nextRun))

			// Perform the cleanup
			notification.CleanupOldNotifications(duration)
		}
	}()
}
