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

type Notification struct {
	Message        string
	Title          string
	Body           string
	Image          string
	DeviceTokens   []string
	AnalyticsLabel string
	Data           map[string]string
}

func Worker(workerChan <-chan Notification, id int) {
	txn := log.NewRelicApp.StartTransaction(fmt.Sprintf("Worker-%d", id))

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

	for notification := range workerChan {
		var messages []*messaging.Message

		for _, deviceToken := range notification.DeviceTokens {
			msg := &messaging.Message{
				Android: &messaging.AndroidConfig{
					Priority: "high",
					Notification: &messaging.AndroidNotification{
						Title:    notification.Title,
						Body:     notification.Body,
						ImageURL: notification.Image,
					},
					Data: notification.Data,
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
							"data":      notification.Data,
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
				Data:  notification.Data,
			}
			messages = append(messages, msg)
		}
		// Send() is very slow.. DO NOT USE..
		apiCall := time.Now()

		apiCallSegment := txn.StartSegment("SendEach FCM Messages")
		msgResponse, err := fcmClient.SendEach(ctx, messages)
		apiCallSegment.End()

		if err != nil {
			log.ErrorLogger.Printf("ERROR: %+v", err)
			log.ErrorLogger.Printf("Worker-%d: Error sending FCM messages: %+v", id, err)
			txn.AddAttribute("error", fmt.Sprintf("Send FCM error: %v", err))
			continue
		}

		fcmEnd := time.Now()
		apiTrip := fcmEnd.Sub(apiCall)

		fcmDiff := fcmEnd.Sub(fcmStart)

		log.InfoLogger.Printf("Worker-%d: FCM Time: %v, API Round Trip Time: %v, SuccessCount: %v, FailureCount: %v",
			id, fcmDiff, apiTrip, msgResponse.SuccessCount, msgResponse.FailureCount)

		txn.AddAttribute("worker_id", id)
		txn.AddAttribute("fcm_time", fcmDiff.String())
		txn.AddAttribute("api_round_trip", apiTrip.String())
		txn.AddAttribute("success_count", msgResponse.SuccessCount)
		txn.AddAttribute("failure_count", msgResponse.FailureCount)
	}
}
