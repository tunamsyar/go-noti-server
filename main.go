package main

import (
	"context"
	"fmt"
	pb "go-noti-server/protos/notifications"
	"log"
	"net"
	"time"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
)

type server struct {
	pb.UnimplementedNotificationServiceServer
}

type Notification struct {
	message        string
	analyticsLabel string
	title          string
	body           string
	deviceTokens   []string
}

func (s *server) Send(ctx context.Context, req *pb.NotificationRequest) (*pb.NotificationResponse, error) {
	startTime := time.Now()
	fmt.Printf("%+v\n", req)
	notificationData := Notification{
		message:        req.GetNotification().Message,
		title:          req.GetNotification().Title,
		body:           req.GetNotification().Body,
		deviceTokens:   req.GetNotification().DeviceTokens,
		analyticsLabel: req.GetNotification().AnalyticsLabel,
	}

	numWorkers := 10
	workerChan := make(chan Notification, numWorkers)

	go func() {
		for i := 0; i < numWorkers; i++ {
			go worker(workerChan)
		}
	}()

	workerChan <- notificationData

	close(workerChan)

	endTime := time.Now()
	duration := endTime.Sub(startTime)
	log.Printf("Time taken: %v\n", duration)

	return &pb.NotificationResponse{Message: "Success"}, nil
}

func worker(workerChan <-chan Notification) {
	ctx := context.Background()
	opt := option.WithCredentialsFile("auth.json")
	app, err := firebase.NewApp(ctx, nil, opt)

	if err != nil {
		return
	}

	fcmClient, err := app.Messaging(ctx)
	if err != nil {
		return
	}

	fcmStart := time.Now()

	for notification := range workerChan {
		// for _, deviceToken := range notification.deviceTokens {
		message := &messaging.MulticastMessage{
			Notification: &messaging.Notification{
				Title: notification.title,
				Body:  notification.body,
			},
			Data: map[string]string{
				"analytics_label": notification.analyticsLabel,
			},
			// FCMOptions: &messaging.FCMOptions{
			// 	AnalyticsLabel: notification.analyticsLabel,
			// },
			Tokens: notification.deviceTokens, // Assuming a single device token for simplicity. Still janky. Should be a loop.
		}
		// _, err := fcmClient
		msgResponse, err := fcmClient.SendEachForMulticast(ctx, message)
		if err != nil {
			log.Printf("ERROR: %+v", err)
			// log.Printf("Error sending FCM message to device token %s: %v\n", deviceToken, err)
			continue
		}
		fcmEnd := time.Now()

		fcmDiff := fcmEnd.Sub(fcmStart)
		log.Printf("FCM Time: %v\n", fcmDiff)
		log.Printf("Success count: %v\n", msgResponse.SuccessCount)
		log.Printf("Failure count: %v\n", msgResponse.FailureCount)
		log.Printf("FCM message sent successfully to device tokens\n")
		// }
	}
}

func main() {
	lis, err := net.Listen("tcp", ":50001")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterNotificationServiceServer(s, &server{})

	log.Printf("server listening at %v\n", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
	fmt.Println("hello")
}
