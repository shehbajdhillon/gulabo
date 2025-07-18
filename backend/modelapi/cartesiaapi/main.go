package cartesiaapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"gulabodev/httpmiddleware"
	"gulabodev/logger"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

type CartesiaConnectProps struct {
	Logger *logger.LogMiddleware
}

type Cartesia struct {
	logger    *logger.LogMiddleware
	semaphore *semaphore.Weighted
}

const (
	maxRetries = 3
	baseDelay  = 1 * time.Second
)

type VoiceConfig struct {
	Mode string `json:"mode"`
	ID   string `json:"id"`
}

type OutputFormat struct {
	Container  string `json:"container"`
	BitRate    int    `json:"bit_rate"`
	Encoding   string `json:"encoding,omit_empty"`
	SampleRate int    `json:"sample_rate"`
}

type TTSRequest struct {
	ModelID      string       `json:"model_id"`
	Transcript   string       `json:"transcript"`
	Voice        VoiceConfig  `json:"voice"`
	OutputFormat OutputFormat `json:"output_format"`
	Language     string       `json:"language"`
}

func Connect(ctx context.Context, args CartesiaConnectProps) *Cartesia {
	tracer := otel.Tracer("cartesiaapi/Connect")
	ctx, span := tracer.Start(ctx, "Connect")
	defer span.End()

	maxWorkers := 10
	sem := semaphore.NewWeighted(int64(maxWorkers))

	span.SetAttributes(attribute.Int("maxWorkers", maxWorkers))

	return &Cartesia{logger: args.Logger, semaphore: sem}
}

func (c *Cartesia) GenerateSpeech(ctx context.Context, text string) ([]byte, error) {
	tracer := otel.Tracer("cartesiaapi/GenerateSpeech")
	ctx, span := tracer.Start(ctx, "GenerateSpeech")
	defer span.End()

	logger := c.logger.Logger(ctx)

	// Acquire semaphore
	if err := c.semaphore.Acquire(ctx, 1); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer c.semaphore.Release(1)

	apiKey := os.Getenv("CARTESIA_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("CARTESIA_API_KEY environment variable not set")
	}

	// Create request body
	request := TTSRequest{
		ModelID:    "sonic-2",
		Transcript: text,
		Voice: VoiceConfig{
			Mode: "id",
			ID:   INDIAN_WOMAN,
		},
		OutputFormat: OutputFormat{
			Container:  "mp3",
			BitRate:    128000,
			SampleRate: 44100,
		},
		Language: "hi",
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make API request with retries
	var respBody []byte
	maxRetries := 3
	retryDelay := time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		respBody, err = httpmiddleware.HttpRequest(httpmiddleware.HttpRequestStruct{
			Method: "POST",
			Url:    "https://api.cartesia.ai/tts/bytes",
			Body:   bytes.NewBuffer(jsonData),
			Headers: map[string]string{
				"X-API-Key":        apiKey,
				"Cartesia-Version": "2024-06-10",
				"Content-Type":     "application/json",
			},
		})

		if err == nil {
			break
		}

		logger.Warn("Failed to generate speech, retrying",
			zap.Error(err),
			zap.Int("attempt", attempt+1),
			zap.Int("maxRetries", maxRetries))

		if attempt < maxRetries-1 {
			time.Sleep(retryDelay * time.Duration(1<<attempt))
			continue
		}

		return nil, fmt.Errorf("failed to generate speech after %d attempts: %w", maxRetries, err)
	}

	logger.Info("Successfully generated speech",
		zap.Int("audioSize", len(respBody)))

	return respBody, nil
}
