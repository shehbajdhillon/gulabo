package deepinfraapi

import (
	"context"
	"gulabodev/logger"
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

const (
	KOKORO_TTS   = "hexgrad/Kokoro-82M"
	KOKORO_VOICE = "hf_beta"
)

type DeepInfra struct {
	logger    *logger.LogMiddleware
	semaphore *semaphore.Weighted
	client    *openai.Client
}

type DeepInfraConnectProps struct {
	Logger *logger.LogMiddleware
}

func Connect(ctx context.Context, args DeepInfraConnectProps) *DeepInfra {
	tracer := otel.Tracer("deepinfraapi/Connect")
	ctx, span := tracer.Start(ctx, "Connect")
	defer span.End()

	maxWorkers := 10
	sem := semaphore.NewWeighted(int64(maxWorkers))

	DEEPINFRA_SECRET_KEY := os.Getenv("DEEPINFRA_SECRET_KEY")

	span.SetAttributes(attribute.Int("maxWorkers", maxWorkers))
	client := openai.NewClient(
		option.WithAPIKey(DEEPINFRA_SECRET_KEY),
		option.WithBaseURL("https://api.deepinfra.com/v1/openai"),
	)

	return &DeepInfra{logger: args.Logger, semaphore: sem, client: &client}
}

func (d *DeepInfra) GenerateSpeech(ctx context.Context, inputText string) ([]byte, error) {
	d.logger.Logger(ctx).Info("[DeepInfraAPI] Generating speech", zap.String("inputText", inputText))

	res, err := d.client.Audio.Speech.New(ctx, openai.AudioSpeechNewParams{
		ResponseFormat: openai.AudioSpeechNewParamsResponseFormatMP3,
		Model:          KOKORO_TTS,
		Input:          inputText,
		Voice:          KOKORO_VOICE,
		Speed:          param.Opt[float64]{Value: 1.15},
	})
	defer res.Body.Close()

	// Read all bytes from the body
	audioBytes, err := io.ReadAll(res.Body)

	return audioBytes, err
}
