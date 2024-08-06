package main

import (
	"go-noti-server/internal/log"
	"go-noti-server/internal/server"

	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.ErrorLogger.Fatal(err)
	}

	log.SetupLoggers()
	go server.RunGrpcServer()
	server.RunHttpServer()
}
