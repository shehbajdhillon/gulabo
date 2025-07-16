package telegram

import (
	"context"
	"database/sql"
	"encoding/json"
	"gulabodev/database/postgres"
	"gulabodev/logger"
	"gulabodev/modelapi/cartesiaapi"
	"gulabodev/modelapi/deepgramapi"
	"gulabodev/modelapi/groqapi"
	"io"
	"net/http"
	"os"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

type TelegramConnectProps struct {
	Logger   *logger.LogMiddleware
	Groq     *groqapi.Groq
	Cartesia *cartesiaapi.Cartesia
	Deepgram *deepgramapi.DeepgramAPI
	DB       *postgres.Database
}

type Telegram struct {
	logger   *logger.LogMiddleware
	bot      *tgbotapi.BotAPI
	groq     *groqapi.Groq
	cartesia *cartesiaapi.Cartesia
	deepgram *deepgramapi.DeepgramAPI
	db       *postgres.Database
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
		logger:   args.Logger,
		bot:      bot,
		groq:     args.Groq,
		cartesia: args.Cartesia,
		deepgram: args.Deepgram,
		db:       args.DB,
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
	)

	// Get or create user
	_, err := t.db.GetUserByTelegramUserId(ctx, user.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			// User not found, create new user
			_, err := t.db.SetupNewUser(ctx, postgres.SetupNewUserProps{
				TelegramUserID:    user.ID,
				TelegramFirstName: user.FirstName,
				TelegramUsername:  user.UserName,
				TelegramLastName:  user.LastName,
			})
			if err != nil {
				t.logger.Logger(ctx).Error("Failed to create new user", zap.Error(err), zap.Int64("user_id", user.ID))
				return
			}
		} else {
			t.logger.Logger(ctx).Error("Failed to get user", zap.Error(err), zap.Int64("user_id", user.ID))
			return
		}
	}

	// Get or create conversation
	conversation, err := t.db.GetConversationByTelegramUserId(ctx, user.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			// Conversation not found, create new one
			newConversation, err := t.db.CreateConversation(ctx, user.ID)
			if err != nil {
				t.logger.Logger(ctx).Error("Failed to create conversation", zap.Error(err), zap.Int64("user_id", user.ID))
				return
			}
			conversation = newConversation
		} else {
			t.logger.Logger(ctx).Error("Failed to get conversation", zap.Error(err), zap.Int64("user_id", user.ID))
			return
		}
	}

	// Handle commands
	if message.IsCommand() {
		t.handleCommand(ctx, message)
		return
	}

	// Handle text messages
	if message.Text != "" {
		span.SetAttributes(attribute.String("message.type", "text"))
		t.logger.Logger(ctx).Info("Received text message",
			zap.Int64("user_id", user.ID),
			zap.String("username", user.UserName),
			zap.String("text", message.Text),
		)
		t.processAndRespond(ctx, message, conversation, message.Text)
		return
	}

	// Handle voice messages
	if message.Voice != nil {
		span.SetAttributes(attribute.String("message.type", "voice"))
		t.logger.Logger(ctx).Info("Received voice message",
			zap.Int64("user_id", user.ID),
			zap.String("username", user.UserName),
			zap.Int("duration", message.Voice.Duration),
		)
		t.handleVoiceMessage(ctx, message, conversation)
		return
	}
}

func (t *Telegram) handleCommand(ctx context.Context, message *tgbotapi.Message) {
	command := message.Command()
	var responseText string

	switch command {
	case "start":
		responseText = "Hey there! I'm Gulabo, your AI girlfriend. I'm so excited to get to know you. Send me a message or a voice note and let's have some fun! 😉"
	default:
		responseText = "Sorry, baby. I don't understand that command. Just talk to me normally."
	}

	msg := tgbotapi.NewMessage(message.Chat.ID, responseText)
	_, err := t.bot.Send(msg)
	if err != nil {
		t.logger.Logger(ctx).Error("Failed to send command response", zap.Error(err), zap.String("command", command))
	}
}

func (t *Telegram) processAndRespond(ctx context.Context, message *tgbotapi.Message, conversation postgres.Conversation, userInput string) {
	var conversationHistory []groqapi.ChatCompletionInputMessage
	if err := json.Unmarshal(conversation.Messages, &conversationHistory); err != nil {
		t.logger.Logger(ctx).Error("Failed to unmarshal conversation history", zap.Error(err))
		// Initialize as empty slice if unmarshal fails
		conversationHistory = []groqapi.ChatCompletionInputMessage{}
	}

	// Generate response using Groq
	response, err := t.groq.GetResponse(ctx, conversationHistory, userInput)
	response = strings.Trim(response, `\ '"“”`)

	if err != nil {
		t.logger.Logger(ctx).Error("Failed to generate response", zap.Error(err))
		return
	}

	// Update conversation history
	conversationHistory = append(conversationHistory, groqapi.ChatCompletionInputMessage{
		Role:    groqapi.USER,
		Content: userInput,
	})
	conversationHistory = append(conversationHistory, groqapi.ChatCompletionInputMessage{
		Role:    groqapi.ASSISTANT,
		Content: response,
	})

	updatedMessages, err := json.Marshal(conversationHistory)
	if err != nil {
		t.logger.Logger(ctx).Error("Failed to marshal updated conversation history", zap.Error(err))
	} else {
		_, err = t.db.UpdateConversationMessages(ctx, postgres.UpdateConversationMessagesParams{
			TelegramUserID: message.From.ID,
			Messages:       updatedMessages,
		})
		if err != nil {
			t.logger.Logger(ctx).Error("Failed to update conversation messages", zap.Error(err))
		}
	}

	t.sendVoiceResponse(ctx, message.Chat.ID, response)
}

func (t *Telegram) handleVoiceMessage(ctx context.Context, message *tgbotapi.Message, conversation postgres.Conversation) {
	// Download voice file
	fileURL, err := t.bot.GetFileDirectURL(message.Voice.FileID)
	if err != nil {
		t.logger.Logger(ctx).Error("Failed to get voice file URL", zap.Error(err))
		return
	}

	// Download audio data
	resp, err := http.Get(fileURL)
	if err != nil {
		t.logger.Logger(ctx).Error("Failed to download voice file", zap.Error(err))
		return
	}
	defer resp.Body.Close()

	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		t.logger.Logger(ctx).Error("Failed to read voice data", zap.Error(err))
		return
	}

	// Transcribe voice to text
	transcript, err := t.deepgram.Transcribe(ctx, audioData)
	if err != nil {
		t.logger.Logger(ctx).Error("Failed to transcribe voice", zap.Error(err))
		return
	}

	if transcript == "" {
		t.logger.Logger(ctx).Warn("Empty transcription")
		return
	}

	t.logger.Logger(ctx).Info("Transcribed voice message",
		zap.String("transcript", transcript),
	)

	t.processAndRespond(ctx, message, conversation, transcript)
}

func (t *Telegram) sendVoiceResponse(ctx context.Context, chatID int64, response string) {
	// Generate audio using Cartesia
	audioData, err := t.cartesia.GenerateSpeech(ctx, response)
	if err != nil {
		t.logger.Logger(ctx).Error("Failed to generate speech", zap.Error(err))
		// Fallback to text if audio generation fails
		msg := tgbotapi.NewMessage(chatID, response)
		_, err = t.bot.Send(msg)
		if err != nil {
			t.logger.Logger(ctx).Error("Failed to send text response", zap.Error(err))
		}
		return
	}

	// Send voice message
	voice := tgbotapi.NewVoice(chatID, tgbotapi.FileBytes{
		Name:  "response.mp3",
		Bytes: audioData,
	})
	_, err = t.bot.Send(voice)
	if err != nil {
		t.logger.Logger(ctx).Error("Failed to send voice message", zap.Error(err))
	} else {
		t.logger.Logger(ctx).Info("Sent voice message successfully", zap.Int("audio_size", len(audioData)))
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

