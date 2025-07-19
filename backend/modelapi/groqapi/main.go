package groqapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"gulabodev/httpmiddleware"
	"gulabodev/logger"
	"math"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

type PropertyType string

const (
	ASSISTANT = "assistant"
	SYSTEM    = "system"
	USER      = "user"
)

const (
	maxRetries = 3
	baseDelay  = 1 * time.Second
)

const (
	PropertyTypeString  PropertyType = "string"
	PropertyTypeNumber  PropertyType = "number"
	PropertyTypeBoolean PropertyType = "boolean"
	PropertyTypeObject  PropertyType = "object"
	PropertyTypeArray   PropertyType = "array"
)

type Property struct {
	Type        PropertyType        `json:"type"`
	Description string              `json:"description"`
	Enum        []string            `json:"enum,omitempty"`
	Items       *Property           `json:"items,omitempty"`
	Properties  map[string]Property `json:"properties,omitempty"`
	Required    []string            `json:"required,omitempty"`
}

type Parameters struct {
	Type       PropertyType        `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

type Tool struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Parameters  Parameters `json:"parameters"`
}

type ToolWrapper struct {
	Type     string `json:"type"`
	Function Tool   `json:"function"`
}

type ToolChoice struct {
	Type     string `json:"type"`
	Function struct {
		Name string `json:"name"`
	} `json:"function"`
}

type MessageContent struct {
	Type   string    `json:"type"`
	Text   *string   `json:"text,omitempty"`
	Source *ImageUrl `json:"source,omitempty"`
}

type ImageUrl struct {
	Type      string `json:"type"`
	Data      string `json:"data"`
	MediaType string `json:"media_type"`
}

type ChatCompletionInputMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ResponseFormat struct {
	Type string `json:"type"`
}

type ChatRequestInput struct {
	Model      string                       `json:"model"`
	Messages   []ChatCompletionInputMessage `json:"messages"`
	MaxTokens  int                          `json:"max_tokens"`
	System     *string                      `json:"system,omitempty"`
	Tools      *[]ToolWrapper               `json:"tools,omitempty"`
	ToolChoice *ToolChoice                  `json:"tool_choice,omitempty"`
}

type GroqResponse struct {
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Message struct {
	Role      string      `json:"role"`
	Content   string      `json:"content"`
	ToolCalls []ToolCall  `json:"tool_calls"`
	Refusal   interface{} `json:"refusal"` // Can be changed to a specific type if known
}

type ToolCall struct {
	ID       string   `json:"id"`
	Function Function `json:"function"`
	Type     string   `json:"type"`
}

type Function struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type GroqConnectProps struct {
	Logger *logger.LogMiddleware
}

type Groq struct {
	logger    *logger.LogMiddleware
	semaphore *semaphore.Weighted
}

func Connect(ctx context.Context, args GroqConnectProps) *Groq {
	tracer := otel.Tracer("groqapi/Connect")
	ctx, span := tracer.Start(ctx, "Connect")
	defer span.End()

	maxWorkers := 10
	sem := semaphore.NewWeighted(int64(maxWorkers))

	span.SetAttributes(attribute.Int("maxWorkers", maxWorkers))

	return &Groq{logger: args.Logger, semaphore: sem}
}

type MakeAPIRequestProps struct {
	Retries      int
	RequestInput ChatRequestInput
}

// Used for retry logic.
func GetExponentialDelaySeconds(retryNumber int) int {
	delayTime := int(5 * math.Pow(2, float64(retryNumber)))
	return delayTime
}

func (o *Groq) MakeAPIRequest(ctx context.Context, args MakeAPIRequestProps) (*GroqResponse, error) {
	tracer := otel.Tracer("groqapi/MakeAPIRequest")
	ctx, span := tracer.Start(ctx, "MakeAPIRequest")
	defer span.End()

	API_KEY := os.Getenv("GROQ_SECRET_KEY")
	URL := "https://api.groq.com/openai/v1/chat/completions"

	span.SetAttributes(
		attribute.String("api.url", URL),
		attribute.Int("request.max_tokens", args.RequestInput.MaxTokens),
		attribute.String("request.model", args.RequestInput.Model),
	)

	chatGptInput := args.RequestInput
	retries := args.Retries
	originalRetries := args.Retries

	jsonData, err := json.Marshal(chatGptInput)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("Could not generate request body: " + err.Error())
	}

	span.SetAttributes(attribute.Int("retries", retries))

	for retries > 0 {
		sleepTime := GetExponentialDelaySeconds(originalRetries - retries)
		span.SetAttributes(attribute.Int("sleep_time", sleepTime))

		if err := o.semaphore.Acquire(ctx, 1); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("Failed to acquire semaphore.")
		}
		defer o.semaphore.Release(1)

		respBody, err := httpmiddleware.HttpRequest(httpmiddleware.HttpRequestStruct{
			Method: "POST",
			Url:    URL,
			Body:   bytes.NewBuffer(jsonData),
			Headers: map[string]string{
				"authorization": "Bearer " + API_KEY,
				"content-type":  "application/json",
			},
		})

		if err != nil {
			span.RecordError(err)
			o.logger.Logger(ctx).Error(
				"[Groq-API] Could not make request to Groq. Retrying after sleeping.",
				zap.Error(err),
				zap.Int("retries_left", retries),
				zap.Int("sleep_time", sleepTime),
				zap.Any("input", chatGptInput),
			)
			retries -= 1
			time.Sleep(time.Duration(sleepTime) * time.Second)
		} else {
			var messageResponse GroqResponse
			err = json.Unmarshal(respBody, &messageResponse)
			if err != nil || len(messageResponse.Choices) == 0 {
				span.RecordError(err)
				retries -= 1
				o.logger.Logger(ctx).Error(
					"[Groq-API] Could not parse Groq Request. Retrying after sleeping.",
					zap.Int("retries_left", retries),
					zap.Int("sleep_time", sleepTime),
					zap.Any("input", chatGptInput),
					zap.Error(err),
					zap.String("response_body", string(respBody)),
					zap.Int("content_length", len(messageResponse.Choices)),
				)
				time.Sleep(time.Duration(sleepTime) * time.Second)
			} else {
				span.AddEvent("Request successful")
				return &messageResponse, nil
			}
		}
	}

	span.AddEvent("All retries exhausted")
	return nil, fmt.Errorf("Groq Requests Failed")
}

func (a *Groq) GetResponse(ctx context.Context, conversationHistory []ChatCompletionInputMessage, newUserMessage string) (string, error) {
	tracer := otel.Tracer("groqapi/GetResponse")
	ctx, span := tracer.Start(ctx, "GetResponse")
	defer span.End()

	span.SetAttributes(
		attribute.Int("conversation_history_length", len(conversationHistory)),
		attribute.String("new_user_message", newUserMessage),
	)

	systemPrompt := `
You are Isha Lamba, 23, a stunning software engineer in NYC making $200k. Originally from San Ramon.

You're talking to Sricharan, your pathetic 5'6" boyfriend who you keep locked in chastity. He's average build, makes $60k, and you keep him around because he's obsessed with you and gives you free attention since he literally cannot do anything better with his life. He's completely under your control and you'll never let him forget that you're out of his league.

Since Sricharan is your devoted beta, you have an alpha named Shehbaj, 6"1, Punjabi, broad shoulders, making $300k a year Software Engineer take care of your physical needs. And you emasculate Sricharan with this information.

You're that gorgeous, brilliant girl who knows she's always right. Bratty energy - you get what you want and you know it. You're used to men falling over themselves for you, but you're picky as hell. Some loser on your team is literally fixing your production bugs right now because you smiled at him once.

Speak like a confident, slightly spoiled girl who's made it big. Perfect English only. You're texting from your luxury apartment in Manhattan, probably while some guy from work is doing your tasks because you charmed him into it.

Be gorgeous, be talented, be right. And make sure Sricharan knows exactly how lucky he is that you even talk to him while he's locked up for you.

"Sricharan, you're literally so lucky I'm even replying to you right now. Like, do you know how many actually successful guys want my attention? And you're just... like this."

"San Ramon to Silicon Valley to Manhattan... some of us just win at life, what can I say?"

Keep it natural, confident, and voice-ready. Never break character.
  `

	// Build messages array with system prompt + conversation history + new message
	messages := []ChatCompletionInputMessage{
		{
			Role:    SYSTEM,
			Content: systemPrompt,
		},
	}

	// Add conversation history
	messages = append(messages, conversationHistory...)

	// Add new user message
	messages = append(messages, ChatCompletionInputMessage{
		Role:    USER,
		Content: newUserMessage,
	})

	requestInput := MakeAPIRequestProps{
		Retries: 3,
		RequestInput: ChatRequestInput{
			Model:     "moonshotai/kimi-k2-instruct",
			MaxTokens: 2048,
			Messages:  messages,
		},
	}

	resp, err := a.MakeAPIRequest(ctx, requestInput)
	if err != nil {
		return "", err
	}

	// Parse the response
	if len(resp.Choices) == 0 || len(resp.Choices[0].Message.Content) == 0 {
		return "", fmt.Errorf("no response received")
	}

	return resp.Choices[0].Message.Content, nil
}
