package deepgramapi

import (
	"bytes"
	"context"
	"fmt"
	"gulabodev/logger"

	api "github.com/deepgram/deepgram-go-sdk/pkg/api/listen/v1/rest"
	interfaces "github.com/deepgram/deepgram-go-sdk/pkg/client/interfaces"
	client "github.com/deepgram/deepgram-go-sdk/pkg/client/listen"
	"go.uber.org/zap"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type DeepgramAPI struct {
	logger *logger.LogMiddleware
	dg     *api.Client
}

func Connect(logger *logger.LogMiddleware) *DeepgramAPI {
	c := client.NewRESTWithDefaults()
	dg := api.New(c)

	return &DeepgramAPI{logger: logger, dg: dg}
}

func (d *DeepgramAPI) Transcribe(ctx context.Context, audioData []byte) (string, error) {
	tracer := otel.Tracer("deepgramapi")
	ctx, span := tracer.Start(ctx, "Transcribe")
	defer span.End()

	span.SetAttributes(attribute.Int("audio.data.size", len(audioData)))

	logger := d.logger.Logger(ctx)

	options := &interfaces.PreRecordedTranscriptionOptions{
		Punctuate:  true,
		Diarize:    false,
		Language:   "multi",
		Utterances: true,
		Model:      "nova-3",
	}

	audioReader := bytes.NewReader(audioData)

	span.AddEvent("Calling Deepgram API")
	res, err := d.dg.FromStream(ctx, audioReader, options)
	if err != nil {
		logger.Error("Deepgram transcription failed",
			zap.Error(err))
		span.RecordError(err)
		span.AddEvent("Deepgram API call failed")
		return "", fmt.Errorf("deepgram transcription failed: %w", err)
	}

	if res != nil && res.Results != nil && res.Results.Channels != nil && len(res.Results.Channels) > 0 {
		channel := res.Results.Channels[0]
		if channel.Alternatives != nil && len(channel.Alternatives) > 0 {
			transcription := channel.Alternatives[0].Transcript
			logger.Info("Successfully transcribed audio",
				zap.String("transcription", transcription))
			span.AddEvent("Transcription successful", trace.WithAttributes(attribute.Int("transcription.length", len(transcription))))
			return transcription, nil
		}
	}

	logger.Warn("No transcription found in response")
	span.AddEvent("No transcription found in Deepgram response")
	return "", fmt.Errorf("no transcription found in response")
}
