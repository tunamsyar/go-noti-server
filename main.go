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
	"github.com/newrelic/go-agent/v3/integrations/logcontext-v2/nrlogrus"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/sirupsen/logrus"

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
	InfoLogger  *logrus.Logger
	ErrorLogger *logrus.Logger
	newRelicApp *newrelic.Application
)

func init() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal(err)
	}

	InfoLogger = logrus.New()
	ErrorLogger = logrus.New()

	newRelicApp, err = newrelic.NewApplication(
		newrelic.ConfigAppName(os.Getenv("NEW_RELIC_APP_NAME")),
		newrelic.ConfigLicense(os.Getenv("NEW_RELIC_LICENSE_KEY")),
		newrelic.ConfigDistributedTracerEnabled(true),
	)

	if err != nil {
		log.Fatal(err)
	}

	nrlogrusFormatter := nrlogrus.NewFormatter(newRelicApp, &logrus.TextFormatter{})

	// Set Logrus log level and formatter
	InfoLogger.SetLevel(logrus.InfoLevel)
	InfoLogger.SetFormatter(nrlogrusFormatter)

	ErrorLogger.SetLevel(logrus.ErrorLevel)
	ErrorLogger.SetFormatter(nrlogrusFormatter)
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
			go worker(workerChan, i)
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

func worker(workerChan <-chan Notification, id int) {
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

	for notification := range workerChan {
		var messages []*messaging.Message

		for _, deviceToken := range notification.deviceTokens {
			msg := &messaging.Message{
				Android: &messaging.AndroidConfig{
					Priority: "high",
					Notification: &messaging.AndroidNotification{
						Title:    notification.title,
						Body:     notification.body,
						ImageURL: notification.image,
					},
				},
				APNS: &messaging.APNSConfig{
					Headers: map[string]string{
						"apns-priority": "10",
					},
					Payload: &messaging.APNSPayload{
						Aps: &messaging.Aps{
							Alert: &messaging.ApsAlert{
								Title:       notification.title,
								Body:        notification.body,
								LaunchImage: notification.image,
							},
							Sound: "default",
						},
						CustomData: map[string]interface{}{
							"image-url": notification.image, // Custom key to handle image URL in your app
						},
					},
				},
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

		apiCallSegment := txn.StartSegment("SendEach FCM Messages")
		msgResponse, err := fcmClient.SendEach(ctx, messages)
		apiCallSegment.End()

		if err != nil {
			ErrorLogger.Printf("ERROR: %+v", err)
			ErrorLogger.Printf("Worker-%d: Error sending FCM messages: %+v", id, err)
			txn.AddAttribute("error", fmt.Sprintf("Send FCM error: %v", err))
			continue
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
	go runGrpcServer()
	runHttpServer()
}
