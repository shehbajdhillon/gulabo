package openaiapi

import (
	"context"
	"gulabodev/logger"
	"gulabodev/modelapi"
	"io"
	"os"

	// imported as openai
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"

	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	"github.com/openai/openai-go/v2/packages/param"
)

type OpenAI struct {
	logger    *logger.LogMiddleware
	semaphore *semaphore.Weighted
	client    *openai.Client
}

type OpenAIConnectProps struct {
	Logger *logger.LogMiddleware
}

func Connect(ctx context.Context, args OpenAIConnectProps) *OpenAI {
	tracer := otel.Tracer("openaiapi/Connect")
	ctx, span := tracer.Start(ctx, "Connect")
	defer span.End()

	maxWorkers := 10
	sem := semaphore.NewWeighted(int64(maxWorkers))

	OPENAI_SECRET_KEY := os.Getenv("OPENAI_SECRET_KEY")

	span.SetAttributes(attribute.Int("maxWorkers", maxWorkers))
	client := openai.NewClient(
		option.WithAPIKey(OPENAI_SECRET_KEY),
	)

	return &OpenAI{logger: args.Logger, semaphore: sem, client: &client}
}

func (d *OpenAI) GenerateSpeech(ctx context.Context, inputText string) ([]byte, error) {
	d.logger.Logger(ctx).Info("[OpenAIAPI] Generating speech", zap.String("inputText", inputText))

	res, err := d.client.Audio.Speech.New(ctx, openai.AudioSpeechNewParams{
		ResponseFormat: openai.AudioSpeechNewParamsResponseFormatMP3,
		Model:          openai.SpeechModelGPT4oMiniTTS,
		Input:          inputText,
		Voice:          openai.AudioSpeechNewParamsVoiceSage,
		Instructions:   param.Opt[string]{Value: modelapi.STYLE_INSTRUCTION},
	})
	defer res.Body.Close()

	// Read all bytes from the body
	audioBytes, err := io.ReadAll(res.Body)

	return audioBytes, err
}
