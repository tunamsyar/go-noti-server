package main

import (
	"context"
	"fmt"
	pbh "go-noti-server/protos/health"
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

type healthCheckServer struct {
	pbh.UnimplementedHealthServiceServer
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

	log.Printf("Request Time taken: %v\n", duration)

	return &pb.NotificationResponse{Message: "Success"}, nil
}

func (s *healthCheckServer) Check(ctx context.Context, req *pbh.HealthCheckRequest) (*pbh.HealthCheckResponse, error) {
	log.Printf("Sudah sampai")
	return &pbh.HealthCheckResponse{Message: "Alive"}, nil
}

func worker(workerChan <-chan Notification) {
	ctx := context.Background()
	opt := option.WithCredentialsFile("auth.json")
	app, err := firebase.NewApp(ctx, nil, opt)

	if err != nil {
		log.Fatalf("ERROR: %+v", err)
		return
	}

	fcmClient, err := app.Messaging(ctx)

	if err != nil {
		log.Fatalf("ERROR: %+v", err)
		return
	}

	fcmStart := time.Now()

	for notification := range workerChan {
		var messages []*messaging.Message

		for _, deviceToken := range notification.deviceTokens {
			msg := &messaging.Message{
				Notification: &messaging.Notification{
					Title: notification.title,
					Body:  notification.body,
				},
				FCMOptions: &messaging.FCMOptions{
					AnalyticsLabel: notification.analyticsLabel,
				},
				Token: deviceToken, // Assuming a single device token for simplicity. Still janky. Should be a loop.
			}
			messages = append(messages, msg)
		}
		// Send() is very slow.. DO NOT USE..
		msgResponse, err := fcmClient.SendEach(ctx, messages)
		if err != nil {
			log.Printf("ERROR: %+v", err)
			log.Printf("Error sending FCM message to device token: %v\n", err)
			continue
		}

		fcmEnd := time.Now()

		fcmDiff := fcmEnd.Sub(fcmStart)
		log.Printf("FCM Time: %v\n", fcmDiff)
		log.Printf("SuccessCount: %v\n", msgResponse.SuccessCount)
		log.Printf("FailureCount: %v\n", msgResponse.FailureCount)
	}
}

func main() {
	lis, err := net.Listen("tcp", ":50001")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterNotificationServiceServer(s, &server{})
	pbh.RegisterHealthServiceServer(s, &healthCheckServer{})

	log.Printf("server listening at %v\n", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
	fmt.Println("hello")
}
