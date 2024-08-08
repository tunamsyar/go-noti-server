package server

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"strings"
	"time"

	"go-noti-server/internal/log"
	"go-noti-server/internal/notification"
	pbh "go-noti-server/protos/health"
	pb "go-noti-server/protos/notifications"

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

func RunGrpcServer() {
	var (
		port     = os.Getenv("PORT")
		lis, err = net.Listen("tcp", port)
		s        = grpc.NewServer(grpc.UnaryInterceptor(AuthInterceptor))
	)

	pb.RegisterNotificationServiceServer(s, &server{})
	pbh.RegisterHealthServiceServer(s, &healthCheckServer{})

	log.InfoLogger.Printf("server listening at %v\n", lis.Addr())
	log.InfoLogger.Printf("server listening at %v\n", lis.Addr())
	log.InfoLogger.Printf("Hello")

	if err != nil {
		log.ErrorLogger.Fatalf("Failed to listen: %v", err)
	}

	if err := s.Serve(lis); err != nil {
		log.ErrorLogger.Fatalf("failed to serve: %v", err)
	}
}

func (s *server) SendMessage(ctx context.Context, req *pb.NotificationRequest) (*pb.NotificationResponse, error) {
	startTime := time.Now()

	if req.GetNotification() == nil {
		log.InfoLogger.Println("Caught it")
		return nil, status.Errorf(codes.InvalidArgument, "Empty Message")
	}

	data, err := json.Marshal(req.GetNotification().Data)
	if err != nil {
		log.ErrorLogger.Printf("Failed to serialize data: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to serialize data")
	}

	notificationData := notification.Notification{
		Message:        req.GetNotification().Message,
		Title:          req.GetNotification().Title,
		Body:           req.GetNotification().Body,
		Image:          req.GetNotification().Image,
		DeviceTokens:   strings.Join(req.GetNotification().DeviceTokens, ","),
		AnalyticsLabel: req.GetNotification().AnalyticsLabel,
		Data:           string(data),
	}

	err = notification.SaveNotification(notificationData)
	if err != nil {
		log.ErrorLogger.Printf("Failed to save notification: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to save notification")
	}

	log.InfoLogger.Printf(
		"Message: %s, Title: %s, Body: %s, Image: %s, Data: %+v",
		notificationData.Message, notificationData.Title, notificationData.Body,
		notificationData.Image, string(data),
	)

	endTime := time.Now()
	duration := endTime.Sub(startTime)

	log.InfoLogger.Printf("Request Time taken: %v\n", duration)

	return &pb.NotificationResponse{Message: "Message Received"}, nil
}

func (s *healthCheckServer) Check(ctx context.Context, req *pbh.HealthCheckRequest) (*pbh.HealthCheckResponse, error) {
	log.InfoLogger.Printf("Sudah sampai")
	return &pbh.HealthCheckResponse{Message: "Alive"}, nil
}

func AuthInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	// extract token from context
	token := extractFromContext(ctx)
	// validate token
	log.InfoLogger.Println("auth intercept")
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
