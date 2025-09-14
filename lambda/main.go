package main

import (
	"context"
	"log"

	"github.com/aws/aws-lambda-go/lambda"

	"lambda-func/pkg/handlers"
	"lambda-func/pkg/stores"
)

func main() {
	store, err := stores.InitHealthLogStore(context.Background())
	if err != nil {
		log.Fatalf("Failed to initialize health log store: %v", err)
	}

	handler := handlers.NewHealthLogHandler(store)
	lambda.Start(handler.HandleRequest)
}
