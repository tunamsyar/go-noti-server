package main

import (
	"context"
	"fmt"
	pbh "go-noti-server/protos/health"
	pb "go-noti-server/protos/notifications"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/newrelic/go-agent/v3/integrations/logcontext-v2/nrlogrus"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/sirupsen/logrus"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type server struct {
	pb.UnimplementedNotificationServiceServer
	wp *WorkerPool
}

type healthCheckServer struct {
	pbh.UnimplementedHealthServiceServer
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

	task := Task{
		ID: rand.Intn(101),
		Notification: &Notification{
			message:        req.GetNotification().Message,
			title:          req.GetNotification().Title,
			body:           req.GetNotification().Body,
			image:          req.GetNotification().Image,
			deviceTokens:   req.GetNotification().DeviceTokens,
			analyticsLabel: req.GetNotification().AnalyticsLabel,
			data:           req.GetNotification().Data,
		},
	}

	s.wp.AddTask(task)

	endTime := time.Now()
	duration := endTime.Sub(startTime)

	InfoLogger.Printf("Request Time taken: %v\n", duration)

	return &pb.NotificationResponse{Message: "Message Received"}, nil
}

func (s *healthCheckServer) Check(ctx context.Context, req *pbh.HealthCheckRequest) (*pbh.HealthCheckResponse, error) {
	InfoLogger.Printf("Sudah sampai")
	return &pbh.HealthCheckResponse{Message: "Alive"}, nil
}

func runGrpcServer(wp *WorkerPool) {
	var (
		port     = os.Getenv("PORT")
		lis, err = net.Listen("tcp", port)
		s        = grpc.NewServer(grpc.UnaryInterceptor(AuthInterceptor))
	)

	pb.RegisterNotificationServiceServer(s, &server{wp: wp})
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
	wp := NewWorkerPool(10)
	go wp.Run()

	go runGrpcServer(wp)
	runHttpServer()
}
