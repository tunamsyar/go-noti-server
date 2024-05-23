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

	"github.com/joho/godotenv"
	"github.com/newrelic/go-agent/v3/newrelic"

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
	data           map[string]string
}

// LOGGERS
var (
	InfoLogger  *log.Logger
	ErrorLogger *log.Logger
	file        *os.File
	errFile     *os.File
	newRelicApp *newrelic.Application
)

func init() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal(err)
	}

	file, err := os.OpenFile("info_logs.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal(err)
	}

	errFile, err := os.OpenFile("error_logs.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal(err)
	}

	InfoLogger = log.New(file, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
	ErrorLogger = log.New(errFile, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)

	newRelicApp, err := newrelic.NewApplication(
		newrelic.ConfigAppName(os.Getenv("NEW_RELIC_APP_NAME")),
		newrelic.ConfigLicense(os.Getenv("NEW_RELIC_LICENSE_KEY")),
		newrelic.ConfigAppLogForwardingEnabled(true),
	)
}

func closeLogFiles() {
	if err := file.Close(); err != nil {
		log.Printf("Failed to close info log file: %v", err)
	}
	if err := errFile.Close(); err != nil {
		log.Printf("Failed to close error log file: %v", err)
	}
}

func AuthInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	// extract token from context
	token := extractFromContext(ctx)
	// validate token
	InfoLogger.Println("auth intercept")
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
		InfoLogger.Println("Caught it")
		return nil, status.Errorf(codes.InvalidArgument, "Empty Message")
	}

	notificationData := Notification{
		message:        req.GetNotification().Message,
		title:          req.GetNotification().Title,
		body:           req.GetNotification().Body,
		image:          req.GetNotification().Image,
		deviceTokens:   req.GetNotification().DeviceTokens,
		analyticsLabel: req.GetNotification().AnalyticsLabel,
		data:           req.GetNotification().Data,
	}

	InfoLogger.Printf(
		"Message: %s, Title: %s, Body: %s, Analytics Label: %s, Data: %+v",
		notificationData.message, notificationData.title, notificationData.body,
		notificationData.analyticsLabel, notificationData.data,
	)

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

	InfoLogger.Printf("Request Time taken: %v\n", duration)

	return &pb.NotificationResponse{Message: "Message Received"}, nil
}

func (s *healthCheckServer) Check(ctx context.Context, req *pbh.HealthCheckRequest) (*pbh.HealthCheckResponse, error) {
	InfoLogger.Printf("Sudah sampai")
	return &pbh.HealthCheckResponse{Message: "Alive"}, nil
}

func worker(workerChan <-chan Notification) {
	txn := newRelicApp.StartTransaction("")
	defer txn.End()

	var (
		auth = os.Getenv("AUTH_FILE")
		ctx  = context.Background()
		opt  = option.WithCredentialsFile(auth)
	)

	app, err := firebase.NewApp(ctx, nil, opt)
	if err != nil {
		ErrorLogger.Printf("ERROR: %+v", err)
		return
	}

	fcmClient, err := app.Messaging(ctx)
	if err != nil {
		ErrorLogger.Printf("ERROR: %+v", err)
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
				Data:  notification.data,
			}
			messages = append(messages, msg)
		}
		// Send() is very slow.. DO NOT USE..
		apiCall := time.Now()
		msgResponse, err := fcmClient.SendEach(ctx, messages)
		if err != nil {
			ErrorLogger.Printf("ERROR: %+v", err)
			ErrorLogger.Printf("Error sending FCM message to device token: %v\n", err)
			continue
		}

		fcmEnd := time.Now()
		apiTrip := fcmEnd.Sub(apiCall)

		fcmDiff := fcmEnd.Sub(fcmStart)

		log.Printf("FCM Time: %v\n", fcmDiff)
		log.Printf("API Round Trip Time: %v\n", apiTrip)
		log.Printf("SuccessCount: %v\n", msgResponse.SuccessCount)
		log.Printf("FailureCount: %v\n", msgResponse.FailureCount)

		InfoLogger.Printf("FCM Time: %v\n", fcmDiff)
		InfoLogger.Printf("API Round Trip Time: %v\n", apiTrip)
		InfoLogger.Printf("SuccessCount: %v\n", msgResponse.SuccessCount)
		InfoLogger.Printf("FailureCount: %v\n", msgResponse.FailureCount)
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
	InfoLogger.Printf("server listening at %v\n", lis.Addr())
	InfoLogger.Printf("Hello")

	if err != nil {
		ErrorLogger.Fatalf("Failed to listen: %v", err)
	}

	if err := s.Serve(lis); err != nil {
		ErrorLogger.Fatalf("failed to serve: %v", err)
	}
}

func runHttpServer() {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "Server is healthy\n")

		InfoLogger.Printf("Server is Running")
	})

	http.ListenAndServe(":8080", nil)
}

func main() {
	defer closeLogFiles()
	go runGrpcServer()
	runHttpServer()
}
