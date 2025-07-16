package groqapi

import (
	"context"
	"os"
	"testing"
	"time"

	"gulabodev/logger"
)

func TestGetResponse(t *testing.T) {
	// Set the GROQ_SECRET_KEY environment variable for testing
	apiKey := os.Getenv("GROQ_SECRET_KEY")
	if apiKey == "" {
		t.Skip("GROQ_SECRET_KEY environment variable not set, skipping test")
	}

	// Create a logger
	logMiddleware := logger.Connect(logger.LoggerConnectProps{Production: false})

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Connect to Groq API
	groq := Connect(ctx, GroqConnectProps{Logger: logMiddleware})

	// Test message
	testMessage := "Hello, how are you?"

	// Call getResponse function
	response, err := groq.getResponse(ctx, testMessage)
	if err != nil {
		t.Fatalf("getResponse failed: %v", err)
	}

	// Basic validation
	if response == "" {
		t.Error("Expected non-empty response, got empty string")
	}

	t.Logf("Response received: %s", response)
}

func TestGetResponseWithSetEnv(t *testing.T) {
	// This test demonstrates setting environment variable via os.Setenv
	// Set your API key here for testing

	// Example of setting environment variable for testing:
	// os.Setenv("GROQ_SECRET_KEY", "your-test-key-here")
	// defer os.Unsetenv("GROQ_SECRET_KEY")

	// For now, skip this test to avoid exposing keys
	t.Skip("Set GROQ_SECRET_KEY environment variable to run this test")

	// To run this test with a specific key:
	// GROQ_SECRET_KEY=your-key-here go test -v ./backend/modelapi/groqapi/...
}
