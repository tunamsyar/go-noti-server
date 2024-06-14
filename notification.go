package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/newrelic/go-agent/v3/newrelic"
	"google.golang.org/api/option"
)

type Notification struct {
	message        string
	analyticsLabel string
	title          string
	body           string
	deviceTokens   []string
	image          string
	data           map[string]string
}

func (n *Notification) SendToFcm(id int) {
	InfoLogger.Printf(
		"Message: %s, Title: %s, Body: %s, Analytics Label: %s, Data: %+v",
		n.message, n.title, n.body,
		n.analyticsLabel, n.data,
	)

	txn := newRelicApp.StartTransaction(fmt.Sprintf("Worker-%d", id))

	defer txn.End()

	var (
		auth = os.Getenv("AUTH_FILE")
		ctx  = newrelic.NewContext(context.Background(), txn)
		opt  = option.WithCredentialsFile(auth)
	)

	app, err := firebase.NewApp(ctx, nil, opt)
	if err != nil {
		txn.NoticeError(err)
		ErrorLogger.Printf("ERROR: %+v", err)
		txn.AddAttribute("error", fmt.Sprintf("Firebase init error: %v", err))
		return
	}

	fcmClient, err := app.Messaging(ctx)
	if err != nil {
		ErrorLogger.Printf("ERROR: %+v", err)
		txn.AddAttribute("error", fmt.Sprintf("FCM Error: %v", err))
		return
	}

	fcmStart := time.Now()

	var messages []*messaging.Message

	for _, deviceToken := range n.deviceTokens {
		msg := &messaging.Message{
			Android: &messaging.AndroidConfig{
				Priority: "high",
				Notification: &messaging.AndroidNotification{
					Title:    n.title,
					Body:     n.body,
					ImageURL: n.image,
				},
			},
			APNS: &messaging.APNSConfig{
				Headers: map[string]string{
					"apns-priority": "10",
				},
				Payload: &messaging.APNSPayload{
					Aps: &messaging.Aps{
						Alert: &messaging.ApsAlert{
							Title:       n.title,
							Body:        n.body,
							LaunchImage: n.image,
						},
						Sound: "default",
					},
					CustomData: map[string]interface{}{
						"image-url": n.image, // Custom key to handle image URL in your app
					},
				},
			},
			Notification: &messaging.Notification{
				Title:    n.title,
				Body:     n.body,
				ImageURL: n.image,
			},
			FCMOptions: &messaging.FCMOptions{
				AnalyticsLabel: n.analyticsLabel,
			},
			Token: deviceToken,
			Data:  n.data,
		}
		messages = append(messages, msg)
	}
	// Send() is very slow.. DO NOT USE..
	apiCall := time.Now()

	apiCallSegment := txn.StartSegment("SendEach FCM Messages")
	msgResponse, err := fcmClient.SendEach(ctx, messages)
	apiCallSegment.End()

	if err != nil {
		ErrorLogger.Printf("ERROR: %+v", err)
		ErrorLogger.Printf("Worker-%d: Error sending FCM messages: %+v", id, err)
		txn.AddAttribute("error", fmt.Sprintf("Send FCM error: %v", err))
	}

	fcmEnd := time.Now()
	apiTrip := fcmEnd.Sub(apiCall)

	fcmDiff := fcmEnd.Sub(fcmStart)

	log.Printf("Worker-%d: FCM Time: %v, API Round Trip Time: %v, SuccessCount: %v, FailureCount: %v",
		id, fcmDiff, apiTrip, msgResponse.SuccessCount, msgResponse.FailureCount)

	txn.AddAttribute("worker_id", id)
	txn.AddAttribute("fcm_time", fcmDiff.String())
	txn.AddAttribute("api_round_trip", apiTrip.String())
	txn.AddAttribute("success_count", msgResponse.SuccessCount)
	txn.AddAttribute("failure_count", msgResponse.FailureCount)

	InfoLogger.Printf("Worker-%d: FCM Time: %v, API Round Trip Time: %v, SuccessCount: %v, FailureCount: %v",
		id, fcmDiff, apiTrip, msgResponse.SuccessCount, msgResponse.FailureCount)
}
