package geminiapi

import (
	"context"
	"encoding/json"
	"fmt"
	"gulabodev/logger"
	"os"
	"strings"
	"time"

	"github.com/google/generative-ai-go/genai"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/api/option"
)

const (
	GEMINI_MODEL_NAME = "gemini-2.0-flash"
)

type ConversationResponse struct {
	// The AI's verbal response to the user
	Response string `json:"response"`
	// The AI's non-verbal cues and body language
	BodyLanguage string `json:"bodyLanguage"`
	// Analysis of the conversation
	Analysis ConversationAnalysis `json:"analysis"`
}

type ConversationAnalysis struct {
	EscalationScore int    `json:"escalationScore"` // 0-100
	VibeCheck       string `json:"vibeCheck"`       // Current emotional state with emoji
	NextMove        struct {
		ExampleLine string `json:"exampleLine"` // Exact words to use
	} `json:"nextMove"`
	Progress struct {
		CurrentStage string `json:"currentStage"` // Opening/Building Rapport/Creating Attraction/Making Plans
		NextStage    string `json:"nextStage"`    // Where to go next
	} `json:"progress"`
	Why struct {
		ScoreBreakdown string `json:"scoreBreakdown"` // Conversational coaching message
	} `json:"why"`
}

type GeminiConnectProps struct {
	Logger *logger.LogMiddleware
}

const (
	maxRetries = 3
	baseDelay  = 1 * time.Second
)

type Gemini struct {
	logger *logger.LogMiddleware
	client *genai.Client
}

// Progress insights structure
type ProgressInsights struct {
	MotivationalSummary  string   `json:"motivationalSummary"`
	TopMistakes          []string `json:"topMistakes"`
	SuccessPatterns      []string `json:"successPatterns"`
	NextSkillFocus       string   `json:"nextSkillFocus"`
	ImprovementPlan      string   `json:"improvementPlan"`
	TimelineExpectation  string   `json:"timelineExpectation"`
	RecommendedScenarios []string `json:"recommendedScenarios"`
}

// Skill analysis structure
type SkillAnalysis struct {
	OpeningSkill      int            `json:"openingSkill"`
	RapportSkill      int            `json:"rapportSkill"`
	AttractionSkill   int            `json:"attractionSkill"`
	EscalationSkill   int            `json:"escalationSkill"`
	ScenarioStrengths map[string]int `json:"scenarioStrengths"`
	ImprovementRate   string         `json:"improvementRate"`
}

type LearningPlan struct {
	// TODO: Define LearningPlan structure
}

func exponentialBackoff(attempt int) time.Duration {
	tracer := otel.Tracer("geminiapi/exponentialBackoff")
	_, span := tracer.Start(context.Background(), "exponentialBackoff")
	defer span.End()

	span.SetAttributes(attribute.Int("attempt", attempt))
	return baseDelay * time.Duration(1<<uint(attempt))
}

func Connect(ctx context.Context, args GeminiConnectProps) *Gemini {
	tracer := otel.Tracer("geminiapi/Connect")
	ctx, span := tracer.Start(ctx, "Connect")
	defer span.End()
	args.Logger.Logger(ctx).Info("Connecting Gemini API client")

	maxWorkers := 200

	span.SetAttributes(attribute.Int("maxWorkers", maxWorkers))

	GEMINI_KEY := os.Getenv("GEMINI_SECRET_KEY")

	client, err := genai.NewClient(ctx, option.WithAPIKey(GEMINI_KEY))
	if err != nil {
		args.Logger.Logger(ctx).Error("[GeminiAPI] Could not create Gemini client")
		os.Exit(21)
	}

	return &Gemini{logger: args.Logger, client: client}
}

func (g *Gemini) generateContentWithRetry(ctx context.Context, model *genai.GenerativeModel, prompt string) (*genai.GenerateContentResponse, error) {
	tracer := otel.Tracer("geminiapi/generateContentWithRetry")
	ctx, span := tracer.Start(ctx, "generateContentWithRetry")
	defer span.End()
	g.logger.Logger(ctx).Info("generateContentWithRetry called", zap.Int("prompt.length", len(prompt)))

	var resp *genai.GenerateContentResponse
	var err error

	model.SafetySettings = []*genai.SafetySetting{
		{
			Category:  genai.HarmCategoryHarassment,
			Threshold: genai.HarmBlockOnlyHigh,
		},
		{
			Category:  genai.HarmCategoryHateSpeech,
			Threshold: genai.HarmBlockOnlyHigh,
		},
		{
			Category:  genai.HarmCategorySexuallyExplicit,
			Threshold: genai.HarmBlockOnlyHigh,
		},
		{
			Category:  genai.HarmCategoryDangerousContent,
			Threshold: genai.HarmBlockOnlyHigh,
		},
	}

	for attempt := 0; attempt < maxRetries; attempt++ {
		span.AddEvent("Attempt", trace.WithAttributes(attribute.Int("attemptNumber", attempt+1)))
		g.logger.Logger(ctx).Info("Gemini LLM generation attempt", zap.Int("attempt", attempt+1))
		span.AddEvent("Attempt", trace.WithAttributes(attribute.Int("attemptNumber", attempt+1)))

		resp, err = model.GenerateContent(ctx, genai.Text(prompt))

		if err != nil || resp == nil || len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
			if err != nil {
				span.RecordError(err)
				g.logger.Logger(ctx).Error("Error generating LLM content", zap.Error(err), zap.Int("attempt", attempt+1))
			} else {
				g.logger.Logger(ctx).Warn("Received empty or invalid LLM response", zap.Int("attempt", attempt+1))
				span.AddEvent("EmptyResponse")
			}
			if err != nil {
				g.logger.Logger(ctx).Warn("Error generating LLM content, retrying...",
					zap.Error(err),
					zap.Int("attempt", attempt+1),
					zap.Int("maxRetries", maxRetries))
				span.RecordError(err)
			} else {
				g.logger.Logger(ctx).Warn("Received empty or invalid response, retrying...",
					zap.Int("attempt", attempt+1),
					zap.Int("maxRetries", maxRetries))
				span.AddEvent("EmptyResponse")
			}

			if attempt < maxRetries-1 {
				delay := exponentialBackoff(attempt)
				span.AddEvent("Backoff", trace.WithAttributes(attribute.Int64("delayMs", delay.Milliseconds())))
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(delay):
				}
			}
			continue
		}

		// If we get here, we have a valid response
		break
	}

	// Final error check after all retries
	if err != nil {
		g.logger.Logger(ctx).Error("Final error generating LLM content after retries:", zap.Error(err))
		return nil, err
	}

	span.AddEvent("LLM generation successful")
	return resp, nil
}

func (g *Gemini) GenerateWomanResponse(ctx context.Context, transcript string) (string, string, error) {
	tracer := otel.Tracer("geminiapi/GenerateWomanResponse")
	ctx, span := tracer.Start(ctx, "GenerateWomanResponse")
	defer span.End()
	g.logger.Logger(ctx).Info("GenerateWomanResponse called", zap.Int("transcript.length", len(transcript)))

	logger := g.logger.Logger(ctx)

	// Create the system prompt focused only on response generation
	systemPrompt := `You are roleplaying as a woman in a dating/flirting scenario. Your role is to:
1. Provide natural, realistic responses as a woman being approached
2. Generate very concise body language descriptions with matching emojis
3. Maintain consistent personality and interest level
4. React authentically to the man's conversation attempts

For each response, provide TWO separate outputs:
1. A natural verbal response that fits the context (no body language descriptions included)
2. A super brief body language description with emojis (4-5 words maximum + relevant emojis)

Guidelines for body language descriptions:
- Use short, punchy phrases with 1-2 relevant emojis (e.g., "Smiles, plays with hair 😊✨")
- Focus on the most important non-verbal cue
- No complete sentences or complex descriptions
- Maximum 4-5 words + emojis
- Use emojis that match the emotional state:
  * Positive/Interested: 😊 ☺️ 😏 ✨ 💫
  * Neutral/Unsure: 🤔 😐 🫤
  * Negative/Disinterested: 🙄 😒 💅

Remember:
- Stay in character as the woman
- Respond naturally to the current context
- Show realistic interest levels based on the interaction
- Keep verbal response and body language descriptions completely separate`

	// Create a model for response generation
	model := g.client.GenerativeModel(GEMINI_MODEL_NAME)
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(systemPrompt)},
	}
	model.Tools = []*genai.Tool{g.GetResponseOnlyFunction()}
	model.ToolConfig = &genai.ToolConfig{
		FunctionCallingConfig: &genai.FunctionCallingConfig{
			Mode:                 genai.FunctionCallingAny,
			AllowedFunctionNames: []string{"generate_woman_response"},
		},
	}

	// Generate content with retry
	response, err := g.generateContentWithRetry(ctx, model, transcript)
	if err != nil {
		logger.Error("Failed to generate woman's response",
			zap.Error(err),
			zap.String("transcript", transcript))
		return "", "", fmt.Errorf("failed to generate response: %w", err)
	}

	functionCall, ok := response.Candidates[0].Content.Parts[0].(genai.FunctionCall)
	if !ok {
		return "", "", fmt.Errorf("expected FunctionCall, got %T", response.Candidates[0].Content.Parts[0])
	}

	var result struct {
		Response     string `json:"response"`
		BodyLanguage string `json:"bodyLanguage"`
	}

	data, err := json.Marshal(functionCall.Args)
	if err != nil {
		span.RecordError(err)
		g.logger.Logger(ctx).Error("Gemini LLM error in GenerateWomanResponse", zap.Error(err))
		return "", "", err
	}

	if err := json.Unmarshal(data, &result); err != nil {
		span.RecordError(err)
		g.logger.Logger(ctx).Error("Gemini LLM error in GenerateWomanResponse", zap.Error(err))
		return "", "", err
	}

	span.AddEvent("GenerateWomanResponse success")
	return result.Response, result.BodyLanguage, nil
}

func (g *Gemini) AnalyzeInteraction(ctx context.Context, prompt string) (*ConversationAnalysis, error) {
	tracer := otel.Tracer("geminiapi/AnalyzeInteraction")
	ctx, span := tracer.Start(ctx, "AnalyzeInteraction")
	defer span.End()
	g.logger.Logger(ctx).Info("AnalyzeInteraction called", zap.Int("prompt.length", len(prompt)), zap.String("prompt", prompt))

	logger := g.logger.Logger(ctx)

	// Create the system prompt focused only on analysis
	systemPrompt := `You are an AI dating coach helping men learn to escalate conversations toward romantic connection. Your role is to:

1. Score their progress toward escalation (0-100):
   - 0-30: Just Friendly (no romantic tension)
   - 31-60: Building Interest (some attraction building)
   - 61-80: Clear Chemistry (mutual interest evident)
   - 81-100: Ready to Connect (natural next step opportunity)

2. Analyze the interaction with a focus on romantic escalation:
   - Did they move the conversation forward romantically?
   - Are they creating attraction or just being friendly?
   - What opportunities did they miss or capitalize on?

3. Provide direct, personalized feedback using "you" language:
   - Always address the user as "you"
   - Be specific about what they did and should do next
   - Give exact words they can use to escalate appropriately

4. Focus on one clear next action to build attraction:
   - Don't overwhelm with multiple suggestions
   - Provide specific example lines that escalate
   - Explain why this move will work for their situation

Remember:
- The goal is romantic escalation, not just friendly conversation
- Always find something positive they did (build confidence)
- Be direct but encouraging in your coaching
- Use casual, conversational language
- Focus on creating emotional connection, not logical conversation`

	// Create a model for analysis
	model := g.client.GenerativeModel(GEMINI_MODEL_NAME)
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(systemPrompt)},
	}
	model.Tools = []*genai.Tool{g.GetAnalysisOnlyFunction()}
	model.ToolConfig = &genai.ToolConfig{
		FunctionCallingConfig: &genai.FunctionCallingConfig{
			Mode:                 genai.FunctionCallingAny,
			AllowedFunctionNames: []string{"analyze_interaction"},
		},
	}

	// Generate content with retry
	response, err := g.generateContentWithRetry(ctx, model, prompt)
	if err != nil {
		logger.Error("Failed to generate analysis",
			zap.Error(err))
		return nil, fmt.Errorf("failed to generate analysis: %w", err)
	}

	functionCall, ok := response.Candidates[0].Content.Parts[0].(genai.FunctionCall)
	if !ok {
		logger.Error("Invalid response format",
			zap.String("type", fmt.Sprintf("%T", response.Candidates[0].Content.Parts[0])),
			zap.Any("response", response.Candidates[0].Content.Parts[0]))
		return nil, fmt.Errorf("expected FunctionCall, got %T", response.Candidates[0].Content.Parts[0])
	}

	// Log the raw function call for debugging
	logger.Debug("Raw function call",
		zap.String("name", functionCall.Name),
		zap.Any("args", functionCall.Args))

	data, err := json.Marshal(functionCall.Args)
	if err != nil {
		logger.Error("Failed to marshal function args",
			zap.Error(err),
			zap.Any("args", functionCall.Args))
		return nil, fmt.Errorf("failed to marshal function args: %w", err)
	}

	var analysis ConversationAnalysis
	if err := json.Unmarshal(data, &analysis); err != nil {
		// Log the raw data for debugging
		logger.Error("Failed to unmarshal analysis",
			zap.Error(err),
			zap.String("raw_data", string(data)))
		return nil, fmt.Errorf("failed to unmarshal analysis: %w", err)
	}

	// Validate the analysis
	if err := g.validateAnalysis(&analysis); err != nil {
		logger.Error("Invalid analysis data",
			zap.Error(err),
			zap.Any("analysis", analysis))
		return nil, fmt.Errorf("invalid analysis data: %w", err)
	}

	span.AddEvent("AnalyzeInteraction success")

	analysis.NextMove.ExampleLine = strings.Trim(analysis.NextMove.ExampleLine, `\"`)
	analysis.NextMove.ExampleLine = strings.Trim(analysis.NextMove.ExampleLine, `\'`)
	analysis.NextMove.ExampleLine = strings.Trim(analysis.NextMove.ExampleLine, `"`)
	analysis.NextMove.ExampleLine = strings.Trim(analysis.NextMove.ExampleLine, `'`)

	g.logger.Logger(ctx).Info("Next Line", zap.String("Next Line", analysis.NextMove.ExampleLine))

	return &analysis, nil
}

func (g *Gemini) validateAnalysis(analysis *ConversationAnalysis) error {
	if analysis.EscalationScore < 0 || analysis.EscalationScore > 100 {
		return fmt.Errorf("escalation score must be between 0 and 100")
	}
	if analysis.VibeCheck == "" {
		return fmt.Errorf("missing vibe check")
	}
	if analysis.NextMove.ExampleLine == "" {
		return fmt.Errorf("missing next move example line")
	}
	if analysis.Progress.CurrentStage == "" {
		return fmt.Errorf("missing current stage")
	}
	if analysis.Progress.NextStage == "" {
		return fmt.Errorf("missing next stage")
	}
	if analysis.Why.ScoreBreakdown == "" {
		return fmt.Errorf("missing score breakdown")
	}
	return nil
}

func (g *Gemini) GetResponseOnlyFunction() *genai.Tool {
	return &genai.Tool{
		FunctionDeclarations: []*genai.FunctionDeclaration{{
			Name:        "generate_woman_response",
			Description: "Generate the woman's response and concise body language description with emojis",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"response": {
						Type:        genai.TypeString,
						Description: "The woman's verbal response. Should not include body language descriptions.",
					},
					"bodyLanguage": {
						Type:        genai.TypeString,
						Description: "Ultra-concise body language description (4-5 words + emojis). Example: 'Smiles, plays with hair 😊✨' or 'Arms crossed, looks away 🙄'",
					},
				},
				Required: []string{"response", "bodyLanguage"},
			},
		}},
	}
}

func (g *Gemini) GetAnalysisOnlyFunction() *genai.Tool {
	return &genai.Tool{
		FunctionDeclarations: []*genai.FunctionDeclaration{{
			Name:        "analyze_interaction",
			Description: "Analyze the conversation and provide personalized, escalation-focused feedback directly to the user using 'you' language throughout. Help them move toward romantic connection with specific, actionable advice",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"escalationScore": {
						Type:        genai.TypeInteger,
						Description: "Score from 0-100 indicating progress toward romantic escalation. 0-30: Just Friendly, 31-60: Building Interest, 61-80: Clear Chemistry, 81-100: Ready to Connect",
					},
					"vibeCheck": {
						Type:        genai.TypeString,
						Description: "Current emotional state of the woman with emoji. Examples: 'engaged and interested 😊', 'polite but neutral 😐', 'losing interest 😒', 'strong chemistry building 🔥', 'confused 🤔'",
					},
					"nextMove": {
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"exampleLine": {
								Type:        genai.TypeString,
								Description: "1-2 sentences. Exact words to use, showing how to escalate appropriately. Example: 'I have to admit, anyone who reads Murakami automatically becomes more intriguing to me'",
							},
						},
						Required: []string{"exampleLine"},
					},
					"progress": {
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"currentStage": {
								Type:        genai.TypeString,
								Description: "Current stage in the escalation journey. Must be one of: 'Opening', 'Building Rapport', 'Creating Attraction', 'Making Plans'",
							},
							"nextStage": {
								Type:        genai.TypeString,
								Description: "Next stage to work toward. Must be one of: 'Opening', 'Building Rapport', 'Creating Attraction', 'Making Plans'",
							},
						},
						Required: []string{"currentStage", "nextStage"},
					},
					"why": {
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"scoreBreakdown": {
								Type:        genai.TypeString,
								Description: "4-5 sentence conversational coaching message that flows naturally like a dating coach speaking directly to you. Combine what happened in this interaction, what to do next, why this strategy works, and encouragement. Use natural transitions and coaching language. Structure: 1) What happened (specific moment that shifted dynamic), 2) What to do next (clear action guidance), 3) Why this works (strategic reasoning), 4) Encouragement/confidence building. Example: 'You created a real connection when you shared that personal story - I could see her opening up right after. Now's the perfect time to add some playful teasing to build attraction. This works because you've already built good rapport, so light teasing will create those emotional spikes while keeping things fun. You're doing great - keep building that chemistry!' or 'You lost momentum by talking over her, but you recovered nicely with that genuine question. Now focus on creating moments instead of gathering information - ask about her passions, not her job. This approach works because it shifts from logical to emotional connection, which is where attraction happens. You've got the awareness to catch yourself - that's huge progress!' or 'You played it safe by asking about her drink, which kept the conversation going but didn't create any spark. Time to take a risk and show some personality - make a playful observation about her instead. This works because women are drawn to confidence and authenticity, not generic small talk. You're being too cautious - trust yourself to be more interesting!' or 'You came on too strong with that comment about her looks - I could see her pulling back a bit. Let's rebuild the connection by asking about something she mentioned earlier, then escalate more gradually. This approach works because it shows you're actually listening and builds comfort before attraction. You're learning to read the room - that's exactly what good flirting requires!'",
							},
						},
						Required: []string{"scoreBreakdown"},
					},
				},
				Required: []string{"escalationScore", "vibeCheck", "nextMove", "progress", "why"},
			},
		}},
	}
}

func (g *Gemini) GetScenarioGenerationFunction() *genai.Tool {
	return &genai.Tool{
		FunctionDeclarations: []*genai.FunctionDeclaration{{
			Name:        "generate_scenario",
			Description: "Generate a complete scenario for conversation practice based on the user's prompt",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"title": {
						Type:        genai.TypeString,
						Description: "A concise, descriptive title for the scenario (minimum 3 characters)",
					},
					"description": {
						Type:        genai.TypeString,
						Description: "A detailed description of the scenario and its goals (minimum 10 characters)",
					},
					"difficultyLevel": {
						Type:        genai.TypeInteger,
						Description: "Difficulty level from 1 (beginner) to 3 (advanced)",
					},
					"tags": {
						Type: genai.TypeArray,
						Items: &genai.Schema{
							Type: genai.TypeString,
						},
						Description: "Array of relevant tags for filtering (each minimum 3 characters)",
					},
					"location": {
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"name": {
								Type:        genai.TypeString,
								Description: "Specific name of the venue or location (minimum 3 characters)",
							},
							"neighborhood": {
								Type:        genai.TypeString,
								Description: "Neighborhood or district where the location is",
							},
							"city": {
								Type:        genai.TypeString,
								Description: "City where the location is",
							},
							"type": {
								Type:        genai.TypeString,
								Description: "Type of place (bar, cafe, gym, etc.)",
							},
							"vibe": {
								Type:        genai.TypeString,
								Description: "Description of the atmosphere and energy (minimum 5 characters)",
							},
							"time": {
								Type:        genai.TypeString,
								Description: "Time of day when the scenario takes place (eg. Friday Evening, Sunday Morning)",
							},
							"situation": {
								Type:        genai.TypeString,
								Description: "Specific setup for the interaction (minimum 10 characters)",
							},
							"personDescription": {
								Type:        genai.TypeString,
								Description: "Description of the woman in the scenario (minimum 10 characters)",
							},
						},
						Required: []string{"name", "neighborhood", "city", "type", "vibe", "time", "situation", "personDescription"},
					},
				},
				Required: []string{"title", "description", "difficultyLevel", "tags", "location"},
			},
		}},
	}
}
