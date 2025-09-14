package models

type HealthLog struct {
	UserID    string `json:"userId"`
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Value     string `json:"value"`
}
