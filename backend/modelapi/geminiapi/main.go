package geminiapi

import (
	"context"
	"gulabodev/logger"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/genai"
)

const (
	GEMINI_MODEL_NAME = "gemini-2.5-flash"
)

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
	args.Logger.Logger(ctx).Info("[GeminiAPI] Connecting Gemini API client")

	maxWorkers := 200

	span.SetAttributes(attribute.Int("maxWorkers", maxWorkers))

	GEMINI_KEY := os.Getenv("GEMINI_SECRET_KEY")

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  GEMINI_KEY,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		args.Logger.Logger(ctx).Error("[GeminiAPI] Could not create Gemini client")
		os.Exit(21)
	}

	return &Gemini{logger: args.Logger, client: client}
}

func (g *Gemini) generateContentWithRetry(ctx context.Context, userPrompt string, systemPrompt string, tools []*genai.Tool, toolConfig *genai.ToolConfig) (*genai.GenerateContentResponse, error) {
	tracer := otel.Tracer("geminiapi/generateContentWithRetry")
	ctx, span := tracer.Start(ctx, "generateContentWithRetry")
	defer span.End()
	g.logger.Logger(ctx).Info("[GeminiAPI] generateContentWithRetry called", zap.Int("prompt.length", len(userPrompt)))

	var resp *genai.GenerateContentResponse
	var err error

	thinkingBudget := int32(0)

	safetySettings := []*genai.SafetySetting{
		{
			Category:  genai.HarmCategoryHarassment,
			Threshold: genai.HarmBlockThresholdBlockNone,
		},
		{
			Category:  genai.HarmCategoryHateSpeech,
			Threshold: genai.HarmBlockThresholdBlockNone,
		},
		{
			Category:  genai.HarmCategorySexuallyExplicit,
			Threshold: genai.HarmBlockThresholdBlockNone,
		},
		{
			Category:  genai.HarmCategoryDangerousContent,
			Threshold: genai.HarmBlockThresholdBlockNone,
		},
	}

	for attempt := 0; attempt < maxRetries; attempt++ {
		span.AddEvent("Attempt", trace.WithAttributes(attribute.Int("attemptNumber", attempt+1)))
		g.logger.Logger(ctx).Info("[GeminiAPI] LLM generation attempt", zap.Int("attempt", attempt+1))
		span.AddEvent("Attempt", trace.WithAttributes(attribute.Int("attemptNumber", attempt+1)))

		resp, err = g.client.Models.GenerateContent(ctx, GEMINI_MODEL_NAME, genai.Text(userPrompt), &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: systemPrompt}}},
			SafetySettings:    safetySettings,
			ToolConfig:        toolConfig,
			Tools:             tools,
			ThinkingConfig: &genai.ThinkingConfig{
				IncludeThoughts: false,
				ThinkingBudget:  &thinkingBudget,
			},
		})

		if err != nil || resp == nil || len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
			if err != nil {
				span.RecordError(err)
				g.logger.Logger(ctx).Error("[GeminiAPI] Error generating LLM content", zap.Error(err), zap.Int("attempt", attempt+1))
			} else {
				g.logger.Logger(ctx).Warn("[GeminiAPI] Received empty or invalid LLM response", zap.Int("attempt", attempt+1))
				span.AddEvent("EmptyResponse")
			}
			if err != nil {
				g.logger.Logger(ctx).Warn("[GeminiAPI] Error generating LLM content, retrying...",
					zap.Error(err),
					zap.Int("attempt", attempt+1),
					zap.Int("maxRetries", maxRetries))
				span.RecordError(err)
			} else {
				g.logger.Logger(ctx).Warn("[GeminiAPI] Received empty or invalid response, retrying...",
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
		g.logger.Logger(ctx).Error("[GeminiAPI] Final error generating LLM content after retries:", zap.Error(err))
		return nil, err
	}

	span.AddEvent("LLM generation successful")
	return resp, nil
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
								Type: genai.TypeArray,
								Items: &genai.Schema{
									Type: genai.TypeString,
								},
								Description: "Array of 2-3 example lines. Exact words to use, showing how to escalate appropriately. Each should be sophisticated and natural. Example: ['I have to admit, anyone who reads Murakami automatically becomes more intriguing to me', 'There's something about the way you think that I find compelling', 'You have this perspective that makes me want to know more about you']. If the user's name is provided you can use it instead of a placeholder only when needed and not all the time.",
							},
						},
						Required: []string{"exampleLine"},
					},
					"progress": {
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"currentStage": {
								Type:        genai.TypeString,
								Description: "REQUIRED: Current stage in the escalation journey. STAGES PROGRESS IN ORDER: 1.Approach → 2.Opening → 3.Building Rapport → 4.Creating Attraction → 5.Making Plans. Must be EXACTLY one of these five values: 'Approach', 'Opening', 'Building Rapport', 'Creating Attraction', 'Making Plans'. Use the exact spelling and capitalization shown. Focus on where they are RIGHT NOW in the conversation. If conversation is derailed or offensive, reset to 'Approach'. First conversation turn should be 'Approach'.",
								Enum:        []string{"Approach", "Opening", "Building Rapport", "Creating Attraction", "Making Plans"},
							},
						},
						Required: []string{"currentStage"},
					},
					"why": {
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"analysis": {
								Type:        genai.TypeString,
								Description: "ONE concise sentence analyzing the key moment that impacted the dynamic. Be specific and punchy. Examples: Good case: 'You created real connection with that personal story - she opened up immediately.' | Neutral case: 'You kept conversation going but didn't create memorable spark.' | Bad case: 'You lost her attention by interrupting her story about her weekend.' | Recovery case: 'You came on too strong but recovered by asking about something she mentioned.'",
							},
							"nextAction": {
								Type: genai.TypeArray,
								Items: &genai.Schema{
									Type: genai.TypeString,
								},
								Description: "Array of 2-3 specific, actionable steps to take next. Each item should be a clear, tactical action (not a full sentence). Examples: Good progression: ['Add playful teasing to build attraction', 'Make a fun observation about her personality', 'Create emotional spikes while keeping it light'] | Neutral to engaging: ['Take a risk and show some personality', 'Make a playful comment about her environment', 'Stop playing it safe with generic questions'] | Recovery mode: ['Ask about something she mentioned earlier', 'Rebuild connection before escalating', 'Show you were actually listening'] | Momentum building: ['Ask about her passions instead of job details', 'Create emotional moments', 'Focus on feelings over facts']",
							},
							"reasoning": {
								Type: genai.TypeArray,
								Items: &genai.Schema{
									Type: genai.TypeString,
								},
								Description: "Array of 2-3 strategic principles explaining why the approach works. Each should be a short phrase (3-5 words max). Examples: Building attraction: ['Builds on rapport', 'Creates emotional spikes', 'Keeps it playful'] | Emotional connection: ['Shifts to emotions', 'Bypasses logical mind', 'Drives real attraction'] | Confidence building: ['Shows authenticity', 'Demonstrates confidence', 'Avoids generic talk'] | Recovery strategy: ['Proves you listen', 'Rebuilds comfort first', 'Sets up next escalation']",
							},
						},
						Required: []string{"analysis", "nextAction", "reasoning"},
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

func (g *Gemini) GetProgressInsightsFunction() *genai.Tool {
	return &genai.Tool{
		FunctionDeclarations: []*genai.FunctionDeclaration{{
			Name:        "generate_progress_insights",
			Description: "Generate personalized coaching insights based on user's conversation practice data",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"motivationalSummary": {
						Type:        genai.TypeString,
						Description: "One punchy, encouraging sentence (max 15 words) highlighting their biggest win or momentum. Use 'you' language.",
					},
					"topMistakes": {
						Type: genai.TypeArray,
						Items: &genai.Schema{
							Type: genai.TypeString,
						},
						Description: "3 specific mistakes as SHORT phrases (max 8 words each). Examples: 'Talking over her responses', 'Using generic compliments', 'Avoiding personal topics'",
					},
					"successPatterns": {
						Type: genai.TypeArray,
						Items: &genai.Schema{
							Type: genai.TypeString,
						},
						Description: "3 specific strengths as SHORT phrases (max 8 words each). Examples: 'Great at reading body language', 'Consistent practice schedule', 'Strong opening conversations'",
					},
					"nextSkillFocus": {
						Type:        genai.TypeString,
						Description: "One clear, specific skill (max 10 words). Examples: 'Building rapport through personal stories', 'Creating attraction with playful teasing'",
					},
					"improvementPlan": {
						Type: genai.TypeArray,
						Items: &genai.Schema{
							Type: genai.TypeString,
						},
						Description: "3 numbered action steps, each max 10 words. Focus on what to DO, not explanations. Examples: 'Practice 3 coffee shop scenarios this week', 'Ask follow-up questions after she speaks'",
					},
					"timelineExpectation": {
						Type:        genai.TypeString,
						Description: "Realistic timeline in one sentence (max 12 words). Examples: 'See improvement in 2-3 weeks with consistent practice', 'Expect breakthrough after 10 more conversations'",
					},
					"recommendedScenarios": {
						Type: genai.TypeArray,
						Items: &genai.Schema{
							Type: genai.TypeString,
						},
						Description: "3 specific scenario names (max 5 words each). Examples: 'Coffee Shop Approach', 'Fitness Class Social', 'Bookstore Browse'",
					},
					"quickWins": {
						Type: genai.TypeArray,
						Items: &genai.Schema{
							Type: genai.TypeString,
						},
						Description: "2-3 immediate actions they can take today (5-8 words each). Examples: 'Make eye contact when she speaks', 'Share one personal story', 'Ask about her interests'",
					},
					"weeklyFocus": {
						Type:        genai.TypeString,
						Description: "This week's main focus area (max 6 words). Examples: 'Building rapport skills', 'Creating attraction techniques', 'Opening conversations'",
					},
				},
				Required: []string{"motivationalSummary", "topMistakes", "successPatterns", "nextSkillFocus", "improvementPlan", "timelineExpectation", "recommendedScenarios", "quickWins", "weeklyFocus"},
			},
		}},
	}
}
