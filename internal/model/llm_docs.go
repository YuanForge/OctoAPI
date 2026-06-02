package model

// OpenAIModelInfo is the OpenAI-compatible model metadata returned by
// GET /v1/models.
type OpenAIModelInfo struct {
	ID      string `json:"id" example:"gpt-4o-mini"`
	Object  string `json:"object" example:"model"`
	Created int64  `json:"created" example:"1710000000"`
	OwnedBy string `json:"owned_by" example:"fanapi"`
}

// OpenAIModelListResponse documents GET /v1/models.
type OpenAIModelListResponse struct {
	Object string            `json:"object" example:"list"`
	Data   []OpenAIModelInfo `json:"data"`
}

// APIErrorResponse is the common error shape returned by API endpoints.
type APIErrorResponse struct {
	Error string `json:"error" example:"请求体 JSON 格式错误"`
}

// OpenAIChatCompletionRequest documents the OpenAI-compatible
// /v1/chat/completions request body. The model field is the routing_model shown
// in FanAPI, not necessarily the upstream provider's real model name.
type OpenAIChatCompletionRequest struct {
	Model               string              `json:"model" binding:"required" example:"gpt-4o-mini"`
	Messages            []OpenAIChatMessage `json:"messages" binding:"required"`
	Stream              bool                `json:"stream" example:"false"`
	Temperature         *float64            `json:"temperature,omitempty" example:"0.7"`
	TopP                *float64            `json:"top_p,omitempty" example:"1"`
	MaxTokens           int                 `json:"max_tokens,omitempty" example:"1024"`
	MaxCompletionTokens int                 `json:"max_completion_tokens,omitempty" example:"1024"`
	ResponseModalities  []string            `json:"response_modalities,omitempty" example:"TEXT,IMAGE"`
	Tools               []OpenAIChatTool    `json:"tools,omitempty"`
	ToolChoice          interface{}         `json:"tool_choice,omitempty" swaggertype:"object"`
}

// OpenAIChatMessage documents a Chat Completions message. For multimodal
// requests, content may also be an OpenAI content-parts array.
type OpenAIChatMessage struct {
	Role       string                 `json:"role" binding:"required" example:"user"`
	Content    interface{}            `json:"content,omitempty" swaggertype:"string" example:"你好，介绍一下 FanAPI"`
	ToolCallID string                 `json:"tool_call_id,omitempty" example:"call_weather"`
	ToolCalls  []OpenAIChatToolCall   `json:"tool_calls,omitempty"`
	Name       string                 `json:"name,omitempty" example:"weather"`
	Extra      map[string]interface{} `json:"-" swaggerignore:"true"`
}

type OpenAIChatTool struct {
	Type     string             `json:"type" example:"function"`
	Function OpenAIFunctionSpec `json:"function"`
}

type OpenAIFunctionSpec struct {
	Name        string                 `json:"name" example:"get_weather"`
	Description string                 `json:"description,omitempty" example:"查询指定城市天气"`
	Parameters  map[string]interface{} `json:"parameters,omitempty" swaggertype:"object"`
	Strict      bool                   `json:"strict,omitempty" example:"false"`
}

type OpenAIChatToolCall struct {
	ID       string             `json:"id" example:"call_weather"`
	Type     string             `json:"type" example:"function"`
	Function OpenAIFunctionCall `json:"function"`
}

type OpenAIFunctionCall struct {
	Name      string `json:"name" example:"get_weather"`
	Arguments string `json:"arguments" example:"{\"city\":\"Shanghai\"}"`
}

type OpenAIChatCompletionResponse struct {
	ID      string             `json:"id" example:"chatcmpl_abc123"`
	Object  string             `json:"object" example:"chat.completion"`
	Created int64              `json:"created" example:"1710000000"`
	Model   string             `json:"model" example:"gpt-4o-mini"`
	Choices []OpenAIChatChoice `json:"choices"`
	Usage   OpenAIUsage        `json:"usage"`
}

type OpenAIChatChoice struct {
	Index        int               `json:"index" example:"0"`
	Message      OpenAIChatMessage `json:"message"`
	FinishReason string            `json:"finish_reason" example:"stop"`
}

type OpenAIUsage struct {
	PromptTokens     int64 `json:"prompt_tokens" example:"12"`
	CompletionTokens int64 `json:"completion_tokens" example:"24"`
	TotalTokens      int64 `json:"total_tokens" example:"36"`
}

type ClaudeMessagesRequest struct {
	Model       string          `json:"model" binding:"required" example:"claude-3-5-sonnet"`
	System      string          `json:"system,omitempty" example:"You are a helpful assistant."`
	Messages    []ClaudeMessage `json:"messages" binding:"required"`
	MaxTokens   int             `json:"max_tokens" binding:"required" example:"1024"`
	Stream      bool            `json:"stream" example:"false"`
	Temperature *float64        `json:"temperature,omitempty" example:"0.7"`
	TopP        *float64        `json:"top_p,omitempty" example:"1"`
	Tools       []ClaudeTool    `json:"tools,omitempty"`
	ToolChoice  interface{}     `json:"tool_choice,omitempty" swaggertype:"object"`
}

type ClaudeMessage struct {
	Role    string               `json:"role" binding:"required" example:"user"`
	Content []ClaudeContentBlock `json:"content" binding:"required"`
}

type ClaudeContentBlock struct {
	Type      string                 `json:"type" example:"text"`
	Text      string                 `json:"text,omitempty" example:"你好，介绍一下 FanAPI"`
	Source    *ClaudeImageSource     `json:"source,omitempty"`
	ID        string                 `json:"id,omitempty" example:"toolu_abc123"`
	Name      string                 `json:"name,omitempty" example:"get_weather"`
	Input     map[string]interface{} `json:"input,omitempty" swaggertype:"object"`
	ToolUseID string                 `json:"tool_use_id,omitempty" example:"toolu_abc123"`
	Content   string                 `json:"content,omitempty" example:"天气晴朗"`
}

type ClaudeImageSource struct {
	Type      string `json:"type" example:"base64"`
	MediaType string `json:"media_type,omitempty" example:"image/png"`
	Data      string `json:"data,omitempty" example:"iVBORw0KGgo..."`
	URL       string `json:"url,omitempty" example:"https://example.com/image.png"`
}

type ClaudeTool struct {
	Name        string                 `json:"name" example:"get_weather"`
	Description string                 `json:"description,omitempty" example:"查询指定城市天气"`
	InputSchema map[string]interface{} `json:"input_schema" swaggertype:"object"`
}

type ClaudeMessagesResponse struct {
	ID           string               `json:"id" example:"msg_abc123"`
	Type         string               `json:"type" example:"message"`
	Role         string               `json:"role" example:"assistant"`
	Model        string               `json:"model" example:"claude-3-5-sonnet"`
	Content      []ClaudeContentBlock `json:"content"`
	StopReason   string               `json:"stop_reason" example:"end_turn"`
	StopSequence *string              `json:"stop_sequence"`
	Usage        ClaudeUsage          `json:"usage"`
}

type ClaudeUsage struct {
	InputTokens  int64 `json:"input_tokens" example:"12"`
	OutputTokens int64 `json:"output_tokens" example:"24"`
}

type GeminiGenerateContentRequest struct {
	Model             string                  `json:"model,omitempty" example:"gemini-2.5-flash"`
	Contents          []GeminiContent         `json:"contents" binding:"required"`
	SystemInstruction *GeminiContent          `json:"systemInstruction,omitempty"`
	GenerationConfig  *GeminiGenerationConfig `json:"generationConfig,omitempty"`
	Tools             []GeminiTool            `json:"tools,omitempty"`
	Stream            bool                    `json:"stream,omitempty" example:"false"`
}

type GeminiContent struct {
	Role  string       `json:"role,omitempty" example:"user"`
	Parts []GeminiPart `json:"parts" binding:"required"`
}

type GeminiPart struct {
	Text             string                  `json:"text,omitempty" example:"你好，介绍一下 FanAPI"`
	InlineData       *GeminiBlob             `json:"inlineData,omitempty"`
	FileData         *GeminiFileData         `json:"fileData,omitempty"`
	FunctionCall     *GeminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *GeminiFunctionResponse `json:"functionResponse,omitempty"`
}

type GeminiBlob struct {
	MimeType string `json:"mimeType" example:"image/png"`
	Data     string `json:"data" example:"iVBORw0KGgo..."`
}

type GeminiFileData struct {
	MimeType string `json:"mimeType,omitempty" example:"image/jpeg"`
	FileURI  string `json:"fileUri" example:"https://example.com/image.jpg"`
}

type GeminiFunctionCall struct {
	Name string                 `json:"name" example:"get_weather"`
	Args map[string]interface{} `json:"args,omitempty" swaggertype:"object"`
}

type GeminiFunctionResponse struct {
	Name     string                 `json:"name" example:"get_weather"`
	Response map[string]interface{} `json:"response" swaggertype:"object"`
}

type GeminiGenerationConfig struct {
	Temperature        *float64 `json:"temperature,omitempty" example:"0.7"`
	TopP               *float64 `json:"topP,omitempty" example:"1"`
	MaxOutputTokens    int      `json:"maxOutputTokens,omitempty" example:"1024"`
	ResponseModalities []string `json:"responseModalities,omitempty" example:"TEXT,IMAGE"`
}

type GeminiTool struct {
	FunctionDeclarations []GeminiFunctionDeclaration `json:"functionDeclarations,omitempty"`
}

type GeminiFunctionDeclaration struct {
	Name        string                 `json:"name" example:"get_weather"`
	Description string                 `json:"description,omitempty" example:"查询指定城市天气"`
	Parameters  map[string]interface{} `json:"parameters,omitempty" swaggertype:"object"`
}

type GeminiGenerateContentResponse struct {
	Candidates     []GeminiCandidate      `json:"candidates"`
	UsageMetadata  GeminiUsageMetadata    `json:"usageMetadata"`
	PromptFeedback map[string]interface{} `json:"promptFeedback,omitempty" swaggertype:"object"`
}

type GeminiCandidate struct {
	Content      GeminiContent `json:"content"`
	FinishReason string        `json:"finishReason" example:"STOP"`
	Index        int           `json:"index" example:"0"`
}

type GeminiUsageMetadata struct {
	PromptTokenCount     int64 `json:"promptTokenCount" example:"12"`
	CandidatesTokenCount int64 `json:"candidatesTokenCount" example:"24"`
	TotalTokenCount      int64 `json:"totalTokenCount" example:"36"`
}

type ResponsesRequest struct {
	Model           string               `json:"model" binding:"required" example:"gpt-4o-mini"`
	Input           []ResponsesInputItem `json:"input" binding:"required"`
	Instructions    string               `json:"instructions,omitempty" example:"You are a helpful assistant."`
	Stream          bool                 `json:"stream" example:"false"`
	MaxOutputTokens int                  `json:"max_output_tokens,omitempty" example:"1024"`
	Temperature     *float64             `json:"temperature,omitempty" example:"0.7"`
	TopP            *float64             `json:"top_p,omitempty" example:"1"`
	Tools           []ResponsesTool      `json:"tools,omitempty"`
	ToolChoice      interface{}          `json:"tool_choice,omitempty" swaggertype:"object"`
}

type ResponsesInputItem struct {
	Type      string                 `json:"type,omitempty" example:"message"`
	Role      string                 `json:"role,omitempty" example:"user"`
	Content   []ResponsesContentPart `json:"content,omitempty"`
	CallID    string                 `json:"call_id,omitempty" example:"call_weather"`
	Output    interface{}            `json:"output,omitempty" swaggertype:"object"`
	Name      string                 `json:"name,omitempty" example:"get_weather"`
	Arguments string                 `json:"arguments,omitempty" example:"{\"city\":\"Shanghai\"}"`
}

type ResponsesContentPart struct {
	Type     string `json:"type" example:"input_text"`
	Text     string `json:"text,omitempty" example:"你好，介绍一下 FanAPI"`
	ImageURL string `json:"image_url,omitempty" example:"https://example.com/image.png"`
}

type ResponsesTool struct {
	Type        string                 `json:"type" example:"function"`
	Name        string                 `json:"name" example:"get_weather"`
	Description string                 `json:"description,omitempty" example:"查询指定城市天气"`
	Parameters  map[string]interface{} `json:"parameters,omitempty" swaggertype:"object"`
	Strict      bool                   `json:"strict,omitempty" example:"false"`
}

type ResponsesResponse struct {
	ID        string                `json:"id" example:"resp_abc123"`
	Object    string                `json:"object" example:"response"`
	CreatedAt int64                 `json:"created_at" example:"1710000000"`
	Model     string                `json:"model" example:"gpt-4o-mini"`
	Status    string                `json:"status" example:"completed"`
	Output    []ResponsesOutputItem `json:"output"`
	Usage     ResponsesUsage        `json:"usage"`
}

type ResponsesOutputItem struct {
	Type      string                 `json:"type" example:"message"`
	ID        string                 `json:"id,omitempty" example:"msg_abc123"`
	Status    string                 `json:"status,omitempty" example:"completed"`
	Role      string                 `json:"role,omitempty" example:"assistant"`
	Content   []ResponsesContentPart `json:"content,omitempty"`
	CallID    string                 `json:"call_id,omitempty" example:"call_weather"`
	Name      string                 `json:"name,omitempty" example:"get_weather"`
	Arguments string                 `json:"arguments,omitempty" example:"{\"city\":\"Shanghai\"}"`
}

type ResponsesUsage struct {
	InputTokens  int64 `json:"input_tokens" example:"12"`
	OutputTokens int64 `json:"output_tokens" example:"24"`
}
