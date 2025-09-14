package stores

import (
	"context"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

func InitHealthLogStore(ctx context.Context) (*HealthLogStore, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	client := dynamodb.NewFromConfig(cfg)

	tableName := os.Getenv("DYNAMODB_TABLE_NAME")

	if tableName == "" {
		tableName = "verve-health-logs"
	}

	return NewHealthLogStore(client, tableName), nil
}
