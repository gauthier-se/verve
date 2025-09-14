package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/aws/aws-lambda-go/events"

	"lambda-func/pkg/models"
	"lambda-func/pkg/stores"
)

type HealthLogHandler struct {
	store *stores.HealthLogStore
}

func NewHealthLogHandler(store *stores.HealthLogStore) *HealthLogHandler {
	return &HealthLogHandler{
		store: store,
	}
}

// CreateHealthLogRequest represents the request body for creating a health log
type CreateHealthLogRequest struct {
	UserID string `json:"userId"`
	Type   string `json:"type"`
	Value  string `json:"value"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// SuccessResponse represents a success response
type SuccessResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Message string      `json:"message,omitempty"`
}

// HandleRequest is the main entry point for Lambda requests
func (h *HealthLogHandler) HandleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// Set CORS headers
	headers := map[string]string{
		"Content-Type":                 "application/json",
		"Access-Control-Allow-Origin":  "*",
		"Access-Control-Allow-Headers": "Content-Type,Authorization",
		"Access-Control-Allow-Methods": "GET,POST,PUT,DELETE,OPTIONS",
	}

	// Handle preflight requests
	if request.HTTPMethod == "OPTIONS" {
		return events.APIGatewayProxyResponse{
			StatusCode: 200,
			Headers:    headers,
			Body:       "",
		}, nil
	}

	// Route based on HTTP method and path
	switch request.HTTPMethod {
	case "POST":
		return h.handleCreateHealthLog(ctx, request, headers)
	case "GET":
		return h.handleGetHealthLogs(ctx, request, headers)
	case "DELETE":
		return h.handleDeleteHealthLog(ctx, request, headers)
	default:
		return h.errorResponse(http.StatusMethodNotAllowed, "Method not allowed", "", headers), nil
	}
}

// handleCreateHealthLog creates a new health log
func (h *HealthLogHandler) handleCreateHealthLog(ctx context.Context, request events.APIGatewayProxyRequest, headers map[string]string) (events.APIGatewayProxyResponse, error) {
	var req CreateHealthLogRequest
	if err := json.Unmarshal([]byte(request.Body), &req); err != nil {
		return h.errorResponse(http.StatusBadRequest, "Invalid request body", err.Error(), headers), nil
	}

	// Validate required fields
	if req.UserID == "" {
		return h.errorResponse(http.StatusBadRequest, "Missing required field", "userId is required", headers), nil
	}
	if req.Type == "" {
		return h.errorResponse(http.StatusBadRequest, "Missing required field", "type is required", headers), nil
	}
	if req.Value == "" {
		return h.errorResponse(http.StatusBadRequest, "Missing required field", "value is required", headers), nil
	}

	// Create health log with current timestamp
	healthLog := &models.HealthLog{
		UserID:    req.UserID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Type:      req.Type,
		Value:     req.Value,
	}

	if err := h.store.CreateHealthLog(ctx, healthLog); err != nil {
		return h.errorResponse(http.StatusInternalServerError, "Failed to create health log", err.Error(), headers), nil
	}

	return h.successResponse(http.StatusCreated, healthLog, "Health log created successfully", headers), nil
}

// handleGetHealthLogs retrieves health logs based on query parameters
func (h *HealthLogHandler) handleGetHealthLogs(ctx context.Context, request events.APIGatewayProxyRequest, headers map[string]string) (events.APIGatewayProxyResponse, error) {
	userID := request.QueryStringParameters["userId"]
	if userID == "" {
		return h.errorResponse(http.StatusBadRequest, "Missing required parameter", "userId is required", headers), nil
	}

	logType := request.QueryStringParameters["type"]
	timestamp := request.QueryStringParameters["timestamp"]

	// If timestamp is provided, get specific health log
	if timestamp != "" {
		healthLog, err := h.store.GetHealthLog(ctx, userID, timestamp)
		if err != nil {
			if err.Error() == "health log not found" {
				return h.errorResponse(http.StatusNotFound, "Health log not found", "", headers), nil
			}
			return h.errorResponse(http.StatusInternalServerError, "Failed to get health log", err.Error(), headers), nil
		}
		return h.successResponse(http.StatusOK, healthLog, "", headers), nil
	}

	// If type is provided, get health logs filtered by type
	if logType != "" {
		healthLogs, err := h.store.GetHealthLogsByUserIDAndType(ctx, userID, logType)
		if err != nil {
			return h.errorResponse(http.StatusInternalServerError, "Failed to get health logs", err.Error(), headers), nil
		}
		return h.successResponse(http.StatusOK, healthLogs, "", headers), nil
	}

	// Get all health logs for user
	healthLogs, err := h.store.GetHealthLogsByUserID(ctx, userID)
	if err != nil {
		return h.errorResponse(http.StatusInternalServerError, "Failed to get health logs", err.Error(), headers), nil
	}

	return h.successResponse(http.StatusOK, healthLogs, "", headers), nil
}

// handleDeleteHealthLog deletes a specific health log
func (h *HealthLogHandler) handleDeleteHealthLog(ctx context.Context, request events.APIGatewayProxyRequest, headers map[string]string) (events.APIGatewayProxyResponse, error) {
	userID := request.QueryStringParameters["userId"]
	timestamp := request.QueryStringParameters["timestamp"]

	if userID == "" || timestamp == "" {
		return h.errorResponse(http.StatusBadRequest, "Missing required parameters", "userId and timestamp are required", headers), nil
	}

	if err := h.store.DeleteHealthLog(ctx, userID, timestamp); err != nil {
		return h.errorResponse(http.StatusInternalServerError, "Failed to delete health log", err.Error(), headers), nil
	}

	return h.successResponse(http.StatusOK, nil, "Health log deleted successfully", headers), nil
}

// errorResponse creates a standardized error response
func (h *HealthLogHandler) errorResponse(statusCode int, error, message string, headers map[string]string) events.APIGatewayProxyResponse {
	response := ErrorResponse{
		Error:   error,
		Message: message,
	}

	body, _ := json.Marshal(response)
	return events.APIGatewayProxyResponse{
		StatusCode: statusCode,
		Headers:    headers,
		Body:       string(body),
	}
}

// successResponse creates a standardized success response
func (h *HealthLogHandler) successResponse(statusCode int, data interface{}, message string, headers map[string]string) events.APIGatewayProxyResponse {
	response := SuccessResponse{
		Success: true,
		Data:    data,
		Message: message,
	}

	body, _ := json.Marshal(response)
	return events.APIGatewayProxyResponse{
		StatusCode: statusCode,
		Headers:    headers,
		Body:       string(body),
	}
}
