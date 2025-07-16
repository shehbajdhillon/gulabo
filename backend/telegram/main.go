package telegram

import (
	"context"
	"gulabodev/logger"
	"gulabodev/modelapi/groqapi"
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

type TelegramConnectProps struct {
	Logger *logger.LogMiddleware
	Groq   *groqapi.Groq
}

type Telegram struct {
	logger *logger.LogMiddleware
	bot    *tgbotapi.BotAPI
	groq   *groqapi.Groq
}

func Connect(ctx context.Context, args TelegramConnectProps) *Telegram {
	tracer := otel.Tracer("telegram/Connect")
	ctx, span := tracer.Start(ctx, "Connect")
	defer span.End()

	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		args.Logger.Logger(ctx).Fatal("TELEGRAM_BOT_TOKEN environment variable not set")
	}

	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		args.Logger.Logger(ctx).Fatal("Failed to create Telegram bot", zap.Error(err))
	}

	// Set debug mode based on environment
	debug := os.Getenv("TELEGRAM_DEBUG") == "true"
	bot.Debug = debug

	span.SetAttributes(
		attribute.String("bot.username", bot.Self.UserName),
		attribute.Bool("bot.debug", debug),
	)

	args.Logger.Logger(ctx).Info("Telegram bot connected successfully",
		zap.String("username", bot.Self.UserName),
		zap.Bool("debug", debug),
	)

	return &Telegram{
		logger: args.Logger,
		bot:    bot,
		groq:   args.Groq,
	}
}

func (t *Telegram) Listen(ctx context.Context) {
	tracer := otel.Tracer("telegram/Listen")
	ctx, span := tracer.Start(ctx, "Listen")
	defer span.End()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := t.bot.GetUpdatesChan(u)

	t.logger.Logger(ctx).Info("Starting Telegram bot message listener")

	for {
		select {
		case <-ctx.Done():
			t.logger.Logger(ctx).Info("Shutting down Telegram bot listener")
			return
		case update := <-updates:
			t.handleUpdate(ctx, update)
		}
	}
}

func (t *Telegram) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	tracer := otel.Tracer("telegram/handleUpdate")
	ctx, span := tracer.Start(ctx, "handleUpdate")
	defer span.End()

	switch {
	case update.Message != nil:
		t.handleMessage(ctx, update.Message)
	case update.CallbackQuery != nil:
		t.handleCallbackQuery(ctx, update.CallbackQuery)
	}
}

func (t *Telegram) handleMessage(ctx context.Context, message *tgbotapi.Message) {
	tracer := otel.Tracer("telegram/handleMessage")
	ctx, span := tracer.Start(ctx, "handleMessage")
	defer span.End()

	if message.From == nil {
		return
	}

	user := message.From
	span.SetAttributes(
		attribute.Int64("user.id", user.ID),
		attribute.String("user.username", user.UserName),
		attribute.String("message.type", "text"),
	)

	t.logger.Logger(ctx).Info("Received message",
		zap.Int64("user_id", user.ID),
		zap.String("username", user.UserName),
		zap.String("text", message.Text),
	)

	// Only process text messages
	if message.Text == "" {
		return
	}

	// Generate response using Groq
	response, err := t.groq.GetResponse(ctx, message.Text)
	if err != nil {
		t.logger.Logger(ctx).Error("Failed to generate response", zap.Error(err))
		return
	}

	// Send response back to user
	msg := tgbotapi.NewMessage(message.Chat.ID, response)
	_, err = t.bot.Send(msg)
	if err != nil {
		t.logger.Logger(ctx).Error("Failed to send response", zap.Error(err))
	}
}

func (t *Telegram) handleCallbackQuery(ctx context.Context, query *tgbotapi.CallbackQuery) {
	tracer := otel.Tracer("telegram/handleCallbackQuery")
	ctx, span := tracer.Start(ctx, "handleCallbackQuery")
	defer span.End()

	if query.From == nil {
		return
	}

	span.SetAttributes(
		attribute.Int64("user.id", query.From.ID),
		attribute.String("user.username", query.From.UserName),
		attribute.String("callback.data", query.Data),
	)

	t.logger.Logger(ctx).Info("Received callback query",
		zap.Int64("user_id", query.From.ID),
		zap.String("username", query.From.UserName),
		zap.String("data", query.Data),
	)

	// Acknowledge the callback
	callback := tgbotapi.NewCallback(query.ID, "")
	t.bot.Send(callback)
}
