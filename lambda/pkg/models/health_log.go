package models

type HealthLog struct {
	UserID    string `json:"userId" dynamodbav:"userId"`
	Timestamp string `json:"timestamp" dynamodbav:"timestamp"`
	Type      string `json:"type" dynamodbav:"type"`
	Value     string `json:"value" dynamodbav:"value"`
}
