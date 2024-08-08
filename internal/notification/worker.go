package notification

import (
	"context"
	"fmt"
	"os"
	"time"

	"go-noti-server/internal/log"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/newrelic/go-agent/v3/newrelic"
	"google.golang.org/api/option"
)

func Worker(id int, notificationChan <-chan Notification) {
	for notification := range notificationChan {

		processNotification(notification, id)

		if err := MarkNotificationAsProcessed(notification.ID); err != nil {
			log.InfoLogger.Printf("ERROR: %+v", err)
		}
	}
}

func processNotification(notification Notification, workerId int) {
	txn := log.NewRelicApp.StartTransaction(fmt.Sprintf("Worker-%d", workerId))

	defer txn.End()

	var (
		auth = os.Getenv("AUTH_FILE")
		ctx  = newrelic.NewContext(context.Background(), txn)
		opt  = option.WithCredentialsFile(auth)
	)

	app, err := firebase.NewApp(ctx, nil, opt)
	if err != nil {
		txn.NoticeError(err)
		log.ErrorLogger.Printf("ERROR: %+v", err)
		txn.AddAttribute("error", fmt.Sprintf("Firebase init error: %v", err))
		return
	}

	fcmClient, err := app.Messaging(ctx)
	if err != nil {
		log.ErrorLogger.Printf("ERROR: %+v", err)
		txn.AddAttribute("error", fmt.Sprintf("FCM Error: %v", err))
		return
	}

	fcmStart := time.Now()

	var messages []*messaging.Message

	deviceTokens := split(notification.DeviceTokens, ",")
	if deviceTokens == nil {
		log.ErrorLogger.Printf("Worker-%d: deviceTokens is nil", workerId)
		txn.AddAttribute("error", "deviceTokens is nil")
		return
	}

	data := parseData(notification.Data)

	for _, deviceToken := range deviceTokens {
		msg := &messaging.Message{
			Android: &messaging.AndroidConfig{
				Priority: "high",
				Notification: &messaging.AndroidNotification{
					Title:    notification.Title,
					Body:     notification.Body,
					ImageURL: notification.Image,
				},
				Data: data,
			},
			APNS: &messaging.APNSConfig{
				Headers: map[string]string{
					"apns-priority": "10",
				},
				Payload: &messaging.APNSPayload{
					Aps: &messaging.Aps{
						Alert: &messaging.ApsAlert{
							Title:       notification.Title,
							Body:        notification.Body,
							LaunchImage: notification.Image,
						},
						Sound: "default",
					},
					CustomData: map[string]interface{}{
						"image-url": notification.Image, // Custom key to handle image URL in your app
						"data":      data,
					},
				},
			},
			Notification: &messaging.Notification{
				Title:    notification.Title,
				Body:     notification.Body,
				ImageURL: notification.Image,
			},
			FCMOptions: &messaging.FCMOptions{
				AnalyticsLabel: notification.AnalyticsLabel,
			},
			Token: deviceToken, // Assuming a single device token for simplicity. Still janky. Should be a loop.
			Data:  data,
		}
		messages = append(messages, msg)
	}
	// Send() is very slow.. DO NOT USE..
	apiCall := time.Now()

	apiCallSegment := txn.StartSegment("SendEach FCM Messages")

	msgResponse, err := fcmClient.SendEach(ctx, messages)
	if msgResponse == nil {
		log.ErrorLogger.Printf("Worker-%d: msgResponse is nil", workerId)
		txn.AddAttribute("error", "msgResponse is nil")
		return
	}
	if err != nil {
		log.ErrorLogger.Printf("ERROR: %+v", err)
		log.ErrorLogger.Printf("Worker-%d: Error sending FCM messages: %+v", workerId, err)
		txn.AddAttribute("error", fmt.Sprintf("Send FCM error: %v", err))
	}
	apiCallSegment.End()

	fcmEnd := time.Now()
	apiTrip := fcmEnd.Sub(apiCall)

	fcmDiff := fcmEnd.Sub(fcmStart)

	log.InfoLogger.Printf("Worker-%d: FCM Time: %v, API Round Trip Time: %v, SuccessCount: %v, FailureCount: %v",
		workerId, fcmDiff, apiTrip, msgResponse.SuccessCount, msgResponse.FailureCount)

	txn.AddAttribute("worker_id", workerId)
	txn.AddAttribute("fcm_time", fcmDiff.String())
	txn.AddAttribute("api_round_trip", apiTrip.String())
	txn.AddAttribute("success_count", msgResponse)
	txn.AddAttribute("failure_count", msgResponse.FailureCount)
}
