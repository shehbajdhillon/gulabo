package main

import (
	"context"
	"gulabodev/database/postgres"
	"gulabodev/logger"
	"gulabodev/modelapi/cartesiaapi"
	"gulabodev/modelapi/deepgramapi"
	"gulabodev/modelapi/geminiapi"
	"gulabodev/modelapi/groqapi"
	"gulabodev/telegram"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"github.com/hyperdxio/opentelemetry-logs-go/exporters/otlp/otlplogs"
	sdk "github.com/hyperdxio/opentelemetry-logs-go/sdk/logs"
	"github.com/hyperdxio/otel-config-go/otelconfig"
)

const defaultPort = "80"

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}
	godotenv.Load()
	production := os.Getenv("PRODUCTION") != ""

	otelShutdown, err := otelconfig.ConfigureOpenTelemetry()
	if err != nil {
		log.Fatalf("Error setting up OTel SDK - %e", err)
	}
	defer otelShutdown()
	ctx := context.Background()

	logExporter, _ := otlplogs.NewExporter(ctx)
	loggerProvider := sdk.NewLoggerProvider(sdk.WithBatcher(logExporter))
	defer loggerProvider.Shutdown(ctx)

	LogMiddleware := logger.Connect(logger.LoggerConnectProps{Production: false, LoggerProvider: loggerProvider})

	db := postgres.Connect(ctx, postgres.DatabaseConnectProps{Logger: LogMiddleware})
	_ = geminiapi.Connect(ctx, geminiapi.GeminiConnectProps{Logger: LogMiddleware})

	// Connect and start Telegram bot
	groqClient := groqapi.Connect(ctx, groqapi.GroqConnectProps{Logger: LogMiddleware})
	cartesiaClient := cartesiaapi.Connect(ctx, cartesiaapi.CartesiaConnectProps{Logger: LogMiddleware})
	deepgramClient := deepgramapi.Connect(LogMiddleware)
	telegramBot := telegram.Connect(ctx, telegram.TelegramConnectProps{Logger: LogMiddleware, Groq: groqClient, Cartesia: cartesiaClient, Deepgram: deepgramClient, DB: db})

	Logger := LogMiddleware.Logger(ctx)

	if production == false {
		Logger.Info("[Telegram] Bot starting in development mode")
	} else {
		Logger.Info("[Telegram] Bot starting in production mode")
	}

	// Start Telegram bot (blocking call)
	telegramBot.Listen(ctx)
}

func requestLoggerMiddleware(logger *logger.LogMiddleware) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			logger.Logger(ctx).Info("Request Received", zap.String("url", r.URL.Path), zap.String("method", r.Method))
			next.ServeHTTP(w, r)
			logger.Logger(ctx).Info("Request Completed", zap.String("path", r.URL.Path), zap.String("method", r.Method))
		})
	}
}