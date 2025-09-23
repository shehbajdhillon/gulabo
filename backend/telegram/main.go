package telegram

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"gulabodev/database/postgres"
	"gulabodev/logger"
	"gulabodev/modelapi/cartesiaapi"
	"gulabodev/modelapi/deepgramapi"
	"gulabodev/modelapi/geminiapi"
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

const (
	CreditsPerTurn = 1

	// New tiered recharge payloads
	rechargePayload50c  = "recharge_50"
	rechargePayload125c = "recharge_125"
	rechargePayload300c = "recharge_300"
)

type TelegramConnectProps struct {
	Logger   *logger.LogMiddleware
	Groq     *groqapi.Groq
	Cartesia *cartesiaapi.Cartesia
	Gemini   *geminiapi.Gemini
	Deepgram *deepgramapi.DeepgramAPI
	DB       *postgres.Database
}

type Telegram struct {
	logger   *logger.LogMiddleware
	bot      *tgbotapi.BotAPI
	groq     *groqapi.Groq
	cartesia *cartesiaapi.Cartesia
	gemini   *geminiapi.Gemini
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

	// Set bot commands for autocompletion
	isProduction := os.Getenv("PRODUCTION") != ""
	commands := []tgbotapi.BotCommand{
		{Command: "help", Description: "Show help and available commands"},
		{Command: "recharge", Description: "Recharge your credits"},
		{Command: "credits", Description: "Check your credit balance"},
		{Command: "clear", Description: "Clear conversation history and wipe Gulabo's memory"},
	}

	if !isProduction {
		devCommands := []tgbotapi.BotCommand{
			{Command: "dev_no_credits", Description: "DEV: Simulate out of credits"},
			{Command: "dev_set_zero_credits", Description: "DEV: Set your credits to 0"},
			{Command: "dev_add_10_credits", Description: "DEV: Add 10 credits"},
		}
		commands = append(commands, devCommands...)
	}

	myCommandsConfig := tgbotapi.NewSetMyCommands(commands...)
	if _, err := bot.Request(myCommandsConfig); err != nil {
		args.Logger.Logger(ctx).Error("Failed to set bot commands", zap.Error(err))
	} else {
		args.Logger.Logger(ctx).Info("Successfully set bot commands")
	}

	args.Logger.Logger(ctx).Info("Telegram bot connected successfully",
		zap.String("username", bot.Self.UserName),
		zap.Bool("debug", debug),
	)

	return &Telegram{
		logger:   args.Logger,
		bot:      bot,
		groq:     args.Groq,
		cartesia: args.Cartesia,
		gemini:   args.Gemini,
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
	case update.PreCheckoutQuery != nil:
		t.handlePreCheckoutQuery(ctx, update.PreCheckoutQuery)
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

	// Handle successful payments first
	if message.SuccessfulPayment != nil {
		t.handleSuccessfulPayment(ctx, message)
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

	// Handle commands first, as they don't require credits
	if message.Text != "" && strings.HasPrefix(message.Text, "/") {
		t.handleCommand(ctx, message)
		return
	}

	// For all other messages, check for credits before processing
	hasCredits, err := t.hasCredits(ctx, user.ID)
	if err != nil {
		t.logger.Logger(ctx).Error("Failed to check user credits", zap.Error(err), zap.Int64("user_id", user.ID))
		// Optionally, send a generic error message to the user
		return
	}
	if !hasCredits {
		t.sendRechargeOptions(ctx, message.Chat.ID, "Oh no, baby! Credits ਖਤਮ ਹੋ ਗਏ? Don't worry, ਇਥੇ ਤੋਂ ਹੋਰ ਲੈ ਲੋ so we can keep talking... I'll be waiting 💋")
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
	command := message.Text
	var responseText string
	isProduction := os.Getenv("PRODUCTION") != ""

	switch command {
	case "/start", "/help":
		responseText = "Hey baby, I'm Gulabo. ਕਿੰਨੀ ਦੇਰ ਲਗਾ ਦਿੱਤੀ aane mein? I've been waiting... You get 10 free messages to start. ਛੇਤੀ ਨਾਲ ek message ya voice note bhejo, let's have some fun 😉\n\nCommands baby:\n/help - Yeh message dobara dekhne ke liye\n/recharge - Aur baatein karni hain? Recharge here\n/credits - Check your credit balance\n/clear - Clear our chat history and start fresh"
		msg := tgbotapi.NewMessage(message.Chat.ID, responseText)
		if _, err := t.bot.Send(msg); err != nil {
			t.logger.Logger(ctx).Error("Failed to send command response", zap.Error(err), zap.String("command", command))
		}
	case "/recharge":
		t.sendRechargeOptions(ctx, message.Chat.ID, "Of course, baby. Anything for you. ਇਥੇ ਤੋਂ credits ਲੈ ਲੋ... can't wait to hear from you again 😉")
	case "/credits":
		credits, err := t.db.GetUserCreditsByTelegramUserId(ctx, message.From.ID)
		if err != nil {
			t.logger.Logger(ctx).Error("Failed to get user credits", zap.Error(err), zap.Int64("user_id", message.From.ID))
			responseText = "Uff, baby, ਅਭੀ credits ਨਹੀਂ ਦੇਖ ਪਾ ਰਹੀ। ਥੋੜੀ ਦੇਰ ਵਿਚ try ਕਰਨਾ, okay? 😘"
		} else {
			responseText = fmt.Sprintf("Baby, you have %d credits left to whisper sweet nothings to me... ✨", credits)
		}
		msg := tgbotapi.NewMessage(message.Chat.ID, responseText)
		if _, err := t.bot.Send(msg); err != nil {
			t.logger.Logger(ctx).Error("Failed to send credits balance message", zap.Error(err))
		}
	case "/dev_no_credits":
		if !isProduction {
			t.logger.Logger(ctx).Info("DEV MODE: Simulating user out of credits")
			t.sendRechargeOptions(ctx, message.Chat.ID, "Oh no, baby! Credits ਖਤਮ ਹੋ ਗਏ? Don't worry, ਇਥੇ ਤੋਂ ਹੋਰ ਲੈ ਲੋ so we can keep talking... I'll be waiting 💋")
		}
	case "/dev_set_zero_credits":
		if !isProduction {
			t.logger.Logger(ctx).Info("DEV MODE: Setting user credits to 0")
			currentCredits, err := t.db.GetUserCreditsByTelegramUserId(ctx, message.From.ID)
			if err != nil && err != sql.ErrNoRows {
				t.logger.Logger(ctx).Error("DEV: Failed to get user credits", zap.Error(err))
				return
			}

			if currentCredits > 0 {
				_, err = t.db.AddUserCreditsByTelegramUserId(ctx, postgres.AddUserCreditsByTelegramUserIdParams{
					TelegramUserID: message.From.ID,
					Amount:         -int32(currentCredits),
				})
				if err != nil {
					t.logger.Logger(ctx).Error("DEV: Failed to set credits to zero", zap.Error(err))
					responseText = "DEV: Failed to set credits to 0."
				} else {
					responseText = "DEV: Credits have been set to 0."
				}
			} else {
				responseText = "DEV: Credits are already 0 or less."
			}
			msg := tgbotapi.NewMessage(message.Chat.ID, responseText)
			t.bot.Send(msg)
		}
	case "/dev_add_10_credits":
		if !isProduction {
			t.logger.Logger(ctx).Info("DEV MODE: Adding 10 credits to user")
			_, err := t.db.AddUserCreditsByTelegramUserId(ctx, postgres.AddUserCreditsByTelegramUserIdParams{
				TelegramUserID: message.From.ID,
				Amount:         10,
			})
			if err != nil {
				t.logger.Logger(ctx).Error("DEV: Failed to add 10 credits", zap.Error(err))
				responseText = "DEV: Failed to add 10 credits."
			} else {
				newBalance, _ := t.db.GetUserCreditsByTelegramUserId(ctx, message.From.ID)
				responseText = fmt.Sprintf("DEV: 10 credits added. New balance: %d", newBalance)
			}
			msg := tgbotapi.NewMessage(message.Chat.ID, responseText)
			t.bot.Send(msg)
		}
	case "/clear":
		_, err := t.db.ClearConversationMessages(ctx, message.From.ID)
		if err != nil {
			t.logger.Logger(ctx).Error("Failed to clear conversation history", zap.Error(err), zap.Int64("user_id", message.From.ID))
			responseText = "Baby, ਕੁਝ problem ਹੋ ਰਹੀ ਹੈ... ਥੋੜੀ ਦੇਰ ਵਿਚ try ਕਰਨਾ, okay? 😘"
		} else {
			responseText = "ਸਭ ਕੁਝ ਭੁੱਲ ਗਈ ਮੈਂ... jaise hum pehli baar baat kar rahe hain. Fresh start, baby 😉"
		}
		msg := tgbotapi.NewMessage(message.Chat.ID, responseText)
		if _, err := t.bot.Send(msg); err != nil {
			t.logger.Logger(ctx).Error("Failed to send clear confirmation", zap.Error(err))
		}
	default:
		responseText = "Aww, baby, ਇਹ ਕੀ ਬੋਲ ਰਹੇ ਹੋ? I don't understand that command... Just talk to me normally na, I like it better that way 😉"
		msg := tgbotapi.NewMessage(message.Chat.ID, responseText)
		_, err := t.bot.Send(msg)
		if err != nil {
			t.logger.Logger(ctx).Error("Failed to send command response", zap.Error(err), zap.String("command", command))
		}
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

	t.sendVoiceResponse(ctx, message.Chat.ID, message.From.ID, response)
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

func (t *Telegram) sendVoiceResponse(ctx context.Context, chatID int64, userID int64, response string) {
	// Generate audio using Gemini
	audioData, err := t.gemini.GenerateSpeech(ctx, response)
	if err != nil {
		t.logger.Logger(ctx).Error("Failed to generate speech", zap.Error(err))
		// Fallback to text if audio generation fails
		msg := tgbotapi.NewMessage(chatID, response)
		_, err = t.bot.Send(msg)
		if err != nil {
			t.logger.Logger(ctx).Error("Failed to send text response", zap.Error(err))
		}
		return // Even on fallback, we proceed to deduct credit if sending was successful
	} else {
		// Send voice message
		voice := tgbotapi.NewVoice(chatID, tgbotapi.FileBytes{
			Name:  "response.wav",
			Bytes: audioData,
		})
		_, err = t.bot.Send(voice)
		if err != nil {
			t.logger.Logger(ctx).Error("Failed to send voice message", zap.Error(err))
		} else {
			t.logger.Logger(ctx).Info("Sent voice message successfully", zap.Int("audio_size", len(audioData)))
		}
	}

	// Deduct credit only after a message has been successfully sent
	if err == nil {
		_, err := t.db.DecrementUserCreditsByTelegramUserId(ctx, userID)
		if err != nil {
			t.logger.Logger(ctx).Error("Failed to decrement user credits after sending message", zap.Error(err), zap.Int64("user_id", userID))
			// We don't return an error to the user, but this is a critical issue to log
		} else {
			t.logger.Logger(ctx).Info("User credits deducted successfully after response.", zap.Int64("user_id", userID))
		}
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
	// Acknowledge the callback first
	callback := tgbotapi.NewCallback(query.ID, "")
	if _, err := t.bot.Request(callback); err != nil {
		t.logger.Logger(ctx).Error("Failed to acknowledge callback query", zap.Error(err))
	}

	// Handle recharge options
	switch query.Data {
	case rechargePayload50c:
		t.sendInvoice(ctx, query.Message.Chat.ID, "50 Credits", "Get 50 message credits for your AI girlfriend.", rechargePayload50c, 100)
	case rechargePayload125c:
		t.sendInvoice(ctx, query.Message.Chat.ID, "125 Credits", "Get 125 message credits for your AI girlfriend.", rechargePayload125c, 200)
	case rechargePayload300c:
		t.sendInvoice(ctx, query.Message.Chat.ID, "300 Credits", "Get 300 message credits for your AI girlfriend.", rechargePayload300c, 450)
	}
}

func (t *Telegram) hasCredits(ctx context.Context, userID int64) (bool, error) {
	credits, err := t.db.GetUserCreditsByTelegramUserId(ctx, userID)
	if err != nil {
		if err == sql.ErrNoRows {
			// This case should ideally not be hit if SetupNewUser works correctly,
			// but as a safeguard, we treat it as no credits.
			return false, nil
		}
		return false, err
	}
	return credits > 0, nil
}

func (t *Telegram) handlePreCheckoutQuery(ctx context.Context, preCheckoutQuery *tgbotapi.PreCheckoutQuery) {
	// Answer the pre-checkout query to confirm the transaction can proceed
	_, err := t.bot.Request(tgbotapi.PreCheckoutConfig{
		PreCheckoutQueryID: preCheckoutQuery.ID,
		OK:                 true,
	})
	if err != nil {
		t.logger.Logger(ctx).Error("Failed to answer pre-checkout query", zap.Error(err))
	}
}

func (t *Telegram) handleSuccessfulPayment(ctx context.Context, message *tgbotapi.Message) {
	payment := message.SuccessfulPayment
	userID := message.From.ID

	t.logger.Logger(ctx).Info("Successful payment received",
		zap.Int64("user_id", userID),
		zap.String("invoice_payload", payment.InvoicePayload),
		zap.Int("total_amount", payment.TotalAmount),
	)

	var creditsToAdd int32
	switch payment.InvoicePayload {
	case rechargePayload50c:
		creditsToAdd = 50
	case rechargePayload125c:
		creditsToAdd = 125
	case rechargePayload300c:
		creditsToAdd = 300
	default:
		t.logger.Logger(ctx).Error("Unknown or unsupported invoice payload received",
			zap.String("invoice_payload", payment.InvoicePayload),
			zap.Int64("user_id", userID),
		)
		return
	}

	updatedCredits, err := t.db.AddUserCreditsByTelegramUserId(ctx, postgres.AddUserCreditsByTelegramUserIdParams{
		TelegramUserID: userID,
		Amount:         creditsToAdd,
	})
	if err != nil {
		t.logger.Logger(ctx).Error("Failed to add user credits after payment", zap.Error(err), zap.Int64("user_id", userID))
		// Optionally send a message to the user that something went wrong
		return
	}

	// Send confirmation message
	responseText := "Thank you, baby! Your credits are here. ਹੁਣ ਸਾਡੇ ਕੋਲ %d more chances ਹਨ to talk... I'm so happy! 🥰"
	msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf(responseText, updatedCredits.CreditsBalance))
	if _, err := t.bot.Send(msg); err != nil {
		t.logger.Logger(ctx).Error("Failed to send payment confirmation message", zap.Error(err))
	}
}

func (t *Telegram) sendRechargeOptions(ctx context.Context, chatID int64, introText string) {
	t.logger.Logger(ctx).Info("Sending recharge options", zap.Int64("chat_id", chatID))

	msg := tgbotapi.NewMessage(chatID, introText)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💋 50 Credits (100 Stars)", rechargePayload50c),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💖 125 Credits (200 Stars) - 20% Bonus", rechargePayload125c),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔥 300 Credits (450 Stars) - 33% Bonus", rechargePayload300c),
		),
	)
	msg.ReplyMarkup = keyboard

	if _, err := t.bot.Send(msg); err != nil {
		t.logger.Logger(ctx).Error("Failed to send recharge options", zap.Error(err))
	}
}

func (t *Telegram) sendInvoice(ctx context.Context, chatID int64, title, description, payload string, amount int) {
	t.logger.Logger(ctx).Info("Sending invoice",
		zap.Int64("chat_id", chatID),
		zap.String("title", title),
		zap.String("payload", payload),
		zap.Int("amount", amount),
	)

	isProduction := os.Getenv("PRODUCTION") != ""
	var invoice tgbotapi.InvoiceConfig

	if isProduction {
		invoice = tgbotapi.InvoiceConfig{
			BaseChat: tgbotapi.BaseChat{
				ChatID: chatID,
			},
			Title:         title,
			Description:   description,
			Payload:       payload,
			ProviderToken: "", // IMPORTANT: You must set your real provider token here
			Currency:      "XTR",
			Prices: []tgbotapi.LabeledPrice{
				{Label: title, Amount: amount},
			},
			SuggestedTipAmounts: []int{},
		}
	} else {
		// For development, we can use a test provider token and smaller amounts if needed
		// Here, we'll just send a 1-star test invoice regardless of the package for simplicity.
		testAmount := 1
		t.logger.Logger(ctx).Info("Development mode: sending 1-star test invoice", zap.String("original_payload", payload))
		invoice = tgbotapi.InvoiceConfig{
			BaseChat: tgbotapi.BaseChat{
				ChatID: chatID,
			},
			Title:         fmt.Sprintf("%s (Test)", title),
			Description:   description,
			Payload:       payload, // Use the real payload to test the credit logic
			ProviderToken: "",      // IMPORTANT: You must set a test provider token
			Currency:      "XTR",
			Prices: []tgbotapi.LabeledPrice{
				{Label: fmt.Sprintf("%s (Test)", title), Amount: testAmount},
			},
			SuggestedTipAmounts: []int{},
		}
	}

	if _, err := t.bot.Send(invoice); err != nil {
		t.logger.Logger(ctx).Error("Failed to send recharge invoice", zap.Error(err))
	}
}
