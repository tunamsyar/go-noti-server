package main

import (
	"context"
	"fmt"
	pbh "go-noti-server/protos/health"
	pb "go-noti-server/protos/notifications"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
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
	image          string
}

func AuthInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	// extract token from context
	token := extractFromContext(ctx)
	// validate token
	fmt.Println("auth intercept")
	if !isTokenValid(token) {
		return nil, status.Errorf(codes.Unauthenticated, "Token invalid")
	}
	// handle it
	return handler(ctx, req)
}

func extractFromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}

	tokens := md.Get("Authorization")
	if len(tokens) == 0 {
		return ""
	}
	return tokens[0]
}

func isTokenValid(token string) bool {
	authed := os.Getenv("AUTHED")
	return token == authed
}

func (s *server) SendMessage(ctx context.Context, req *pb.NotificationRequest) (*pb.NotificationResponse, error) {
	startTime := time.Now()

	if req.GetNotification() == nil {
		fmt.Println("Caught it")
		return nil, status.Errorf(codes.InvalidArgument, "Empty Message")
	}

	fmt.Printf("%+v\n", req)

  notificationData := Notification{
		message:        req.GetNotification().Message,
		title:          req.GetNotification().Title,
		body:           req.GetNotification().Body,
		image:          req.GetNotification().Image,
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

	return &pb.NotificationResponse{Message: "Message Received"}, nil
}

func (s *healthCheckServer) Check(ctx context.Context, req *pbh.HealthCheckRequest) (*pbh.HealthCheckResponse, error) {
	log.Printf("Sudah sampai")
	return &pbh.HealthCheckResponse{Message: "Alive"}, nil
}

func worker(workerChan <-chan Notification) {
	auth := os.Getenv("AUTH_FILE")
	ctx := context.Background()
	opt := option.WithCredentialsFile(auth)

	app, err := firebase.NewApp(ctx, nil, opt)
	if err != nil {
		log.Printf("ERROR: %+v", err)
		return
	}

	fcmClient, err := app.Messaging(ctx)
	if err != nil {
		log.Printf("ERROR: %+v", err)
		return
	}

	fcmStart := time.Now()

	for notification := range workerChan {
		var messages []*messaging.Message

		for _, deviceToken := range notification.deviceTokens {
			msg := &messaging.Message{
				Notification: &messaging.Notification{
					Title:    notification.title,
					Body:     notification.body,
					ImageURL: notification.image,
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

func runGrpcServer() {
	var (
		port     = os.Getenv("PORT")
		lis, err = net.Listen("tcp", port)
		s        = grpc.NewServer(grpc.UnaryInterceptor(AuthInterceptor))
	)

	pb.RegisterNotificationServiceServer(s, &server{})
	pbh.RegisterHealthServiceServer(s, &healthCheckServer{})

	log.Printf("server listening at %v\n", lis.Addr())
	log.Printf("Hello")

	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func runHttpServer() {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "Server is healthy\n")

		log.Printf("Server is Running")
	})

	http.ListenAndServe(":8080", nil)
}

func main() {
	go runGrpcServer()
	runHttpServer()
}
