package stores

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"lambda-func/pkg/models"
)

type HealthLogStore struct {
	client    *dynamodb.Client
	tableName string
}

func NewHealthLogStore(client *dynamodb.Client, tableName string) *HealthLogStore {
	return &HealthLogStore{
		client:    client,
		tableName: tableName,
	}
}

// CreateHealthLog inserts a new health log into DynamoDB
func (s *HealthLogStore) CreateHealthLog(ctx context.Context, healthLog *models.HealthLog) error {
	item, err := attributevalue.MarshalMap(healthLog)
	if err != nil {
		return fmt.Errorf("failed to marshal: %w", err)
	}

	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("failed to create log: %w", err)
	}

	return nil
}

// GetHealthLog retrieves a specific health log by userId and timestamp
func (s *HealthLogStore) GetHealthLog(ctx context.Context, userID, timestamp string) (*models.HealthLog, error) {
	result, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"userId":    &types.AttributeValueMemberS{Value: userID},
			"timestamp": &types.AttributeValueMemberS{Value: timestamp},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get log: %w", err)
	}

	if result.Item == nil {
		return nil, fmt.Errorf("log not found")
	}

	var healthLog models.HealthLog
	err = attributevalue.UnmarshalMap(result.Item, &healthLog)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal: %w", err)
	}

	return &healthLog, nil
}

// GetHealthLogsByUserID retrieves all health logs for a specific user
func (s *HealthLogStore) GetHealthLogsByUserID(ctx context.Context, userID string) ([]models.HealthLog, error) {
	result, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.tableName),
		KeyConditionExpression: aws.String("userId = :userId"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":userId": &types.AttributeValueMemberS{Value: userID},
		},
		ScanIndexForward: aws.Bool(false),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get logs: %w", err)
	}

	var healthLogs []models.HealthLog
	for _, item := range result.Items {
		var healthLog models.HealthLog
		err = attributevalue.UnmarshalMap(item, &healthLog)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal: %w", err)
		}
		healthLogs = append(healthLogs, healthLog)
	}

	return healthLogs, nil
}

// GetHealthLogsByUserIDAndType retrieves logs for a user filtered by type
func (s *HealthLogStore) GetHealthLogsByUserIDAndType(ctx context.Context, userID, logType string) ([]models.HealthLog, error) {
	result, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.tableName),
		KeyConditionExpression: aws.String("userId = :userId"),
		FilterExpression:       aws.String("#type = :type"),
		ExpressionAttributeNames: map[string]string{
			"#type": "type",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":userId": &types.AttributeValueMemberS{Value: userID},
			":type":   &types.AttributeValueMemberS{Value: logType},
		},
		ScanIndexForward: aws.Bool(false),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get logs by type: %w", err)
	}

	var logs []models.HealthLog

	for _, item := range result.Items {
		var log models.HealthLog
		err = attributevalue.UnmarshalMap(item, &log)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal: %w", err)
		}
		logs = append(logs, log)
	}

	return logs, nil
}

// UpdateHealthLog updates an existing log
func (s *HealthLogStore) UpdateHealthLog(ctx context.Context, healthLog *models.HealthLog) error {
	item, err := attributevalue.MarshalMap(healthLog)
	if err != nil {
		return fmt.Errorf("failed to marshal: %w", err)
	}

	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("failed to update log: %w", err)
	}

	return nil
}

// DeleteHealthLog deletes a specific log
func (s *HealthLogStore) DeleteHealthLog(ctx context.Context, userID, timestamp string) error {
	_, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"userId":    &types.AttributeValueMemberS{Value: userID},
			"timestamp": &types.AttributeValueMemberS{Value: timestamp},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to delete log: %w", err)
	}

	return nil
}
