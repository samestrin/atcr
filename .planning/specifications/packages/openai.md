# openai

> ⚠️ **NOT A PROJECT DEPENDENCY** — `github.com/openai/openai-go` is NOT in atcr's `go.mod`.
> atcr uses a plain `net/http` client for OpenAI-compatible APIs (see [standard-library.md](standard-library.md) — "provider client" section).
> This file is reference material only and does not reflect atcr's implementation.

**Version:** v3.39.0 (latest) / v1.12.0 (legacy)
**Registry:** [pkg.go.dev/github.com/openai/openai-go](https://pkg.go.dev/github.com/openai/openai-go)
**Official Docs:** [https://pkg.go.dev/github.com/openai/openai-go/v3](https://pkg.go.dev/github.com/openai/openai-go/v3)
**Tier:** Reference only (not used in atcr)
**Last Updated:** June 14, 2026

---

## Overview

Official OpenAI Go SDK providing comprehensive access to OpenAI APIs. The library supports the Responses API (primary), Chat Completions API, Assistants API, and extensive administrative functions. Built with type safety, automatic pagination, streaming support, and robust error handling.

**Key Features:**
- Responses API and Chat Completions API for LLM interactions
- Streaming responses with typed event handling
- Tool calling and structured outputs
- Automatic pagination via `ListAutoPaging` iterators
- File uploads with flexible reader support
- Webhook signature verification
- Middleware and custom request support
- Azure OpenAI integration
- Workload identity authentication (Kubernetes, Azure, GCP)

## Installation

```bash
go get -u github.com/openai/openai-go/v3
```

**Requirements:** Go 1.22+

**Import:**
```go
import (
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/conversations"
)
```

## Core API

### Client Initialization

```go
client := openai.NewClient(
	option.WithAPIKey("your-api-key"),
)
```

### Responses API (Primary)

The Responses API is the primary interface for LLM interactions:

```go
resp, err := client.Responses.New(ctx, responses.ResponseNewParams{
	Input: responses.ResponseNewParamsInputUnion{
		OfString: openai.String("Write a haiku about computers"),
	},
	Model: openai.ChatModelGPT5_2,
})
if err != nil {
	panic(err)
}
println(resp.OutputText())
```

**Multi-turn Conversations:**
```go
response, err := client.Responses.New(ctx, responses.ResponseNewParams{
	Model:              openai.ChatModelGPT5_2,
	PreviousResponseID: openai.String(previousResponse.ID),
	Input: responses.ResponseNewParamsInputUnion{
		OfString: openai.String("Follow-up question"),
	},
})
```

**Conversations API:**
```go
conv, err := client.Conversations.New(ctx, conversations.ConversationNewParams{})
response, err := client.Responses.New(ctx, responses.ResponseNewParams{
	Model: openai.ChatModelGPT5_2,
	Input: responses.ResponseNewParamsInputUnion{
		OfString: openai.String("Hello!"),
	},
	Conversation: responses.ResponseNewParamsConversationUnion{
		OfConversationObject: &responses.ResponseConversationParam{
			ID: conv.ID,
		},
	},
})
```

### Chat Completions API

```go
chatCompletion, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
	Messages: []openai.ChatCompletionMessageParamUnion{
		openai.DeveloperMessage("You are a helpful assistant."),
		openai.UserMessage("How do I check if a slice is empty?"),
	},
	Model: openai.ChatModelGPT5_2,
})
```

### Streaming Responses

```go
stream := client.Responses.NewStreaming(ctx, responses.ResponseNewParams{
	Model: openai.ChatModelGPT5_2,
	Input: responses.ResponseNewParamsInputUnion{
		OfString: openai.String("Write a haiku"),
	},
})

for stream.Next() {
	event := stream.Current()
	print(event.Delta)
}
if stream.Err() != nil {
	panic(stream.Err())
}
```

### Tool Calling

```go
params := responses.ResponseNewParams{
	Model: openai.ChatModelGPT5_2,
	Input: responses.ResponseNewParamsInputUnion{
		OfString: openai.String("What is the weather in NYC?"),
	},
	Tools: []responses.ToolUnionParam{{
		OfFunction: &responses.FunctionToolParam{
			Name:        "get_weather",
			Description: openai.String("Get weather at location"),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"location": map[string]string{"type": "string"},
				},
				"required": []string{"location"},
			},
		},
	}},
}

response, _ := client.Responses.New(ctx, params)
for _, item := range response.Output {
	if item.Type == "function_call" {
		toolCall := item.AsFunctionCall()
		// Process tool call
	}
}
```

## Common Patterns

### Pagination

**Auto-paging (recommended):**
```go
iter := client.FineTuning.Jobs.ListAutoPaging(ctx, openai.FineTuningJobListParams{
	Limit: openai.Int(20),
})
for iter.Next() {
	job := iter.Current()
	fmt.Printf("%+v\n", job)
}
if err := iter.Err(); err != nil {
	panic(err.Error())
}
```

**Manual pagination:**
```go
page, err := client.FineTuning.Jobs.List(ctx, openai.FineTuningJobListParams{
	Limit: openai.Int(20),
})
for page != nil {
	for _, job := range page.Data {
		fmt.Printf("%+v\n", job)
	}
	page, err = page.GetNextPage()
}
```

### Error Handling

```go
_, err := client.FineTuning.Jobs.New(ctx, openai.FineTuningJobNewParams{
	Model:        openai.FineTuningJobNewParamsModel("gpt-4o"),
	TrainingFile: "file-abc123",
})
if err != nil {
	var apierr *openai.Error
	if errors.As(err, &apierr) {
		println(string(apierr.DumpRequest(true)))
		println(string(apierr.DumpResponse(true)))
	}
	panic(err.Error())
}
```

### Timeouts

```go
ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
defer cancel()

client.Responses.New(ctx, params,
	option.WithRequestTimeout(20*time.Second),
)
```

### File Uploads

```go
file, err := os.Open("input.jsonl")
params := openai.FileNewParams{
	File:    file,
	Purpose: openai.FilePurposeFineTune,
}

// Custom filename/contentType
params := openai.FileNewParams{
	File:    openai.File(strings.NewReader(`{"hello": "foo"}`), "file.go", "application/json"),
	Purpose: openai.FilePurposeFineTune,
}
```

### Request/Response Handling

**Request fields:**
- Required fields tagged `api:"required"` always serialize
- Optional primitives use `param.Opt[T]` via constructors: `openai.String()`, `openai.Int()`
- `param.IsOmitted(any)` checks presence
- `param.Null[T]()` sends `null`

**Response objects:**
- All fields are ordinary value types
- `.Valid()` returns true if field is not `null`, present, or parseable
- `.Raw()` returns raw JSON
- `.JSON.ExtraFields` captures undocumented properties

### Webhook Verification

```go
client := openai.NewClient(
	option.WithWebhookSecret(os.Getenv("OPENAI_WEBHOOK_SECRET")),
)

webhookEvent, err := client.Webhooks.Unwrap(body, request.Header)
if err != nil {
	log.Printf("Invalid webhook signature: %v", err)
	return
}

switch event := webhookEvent.AsAny().(type) {
case webhooks.ResponseCompletedWebhookEvent:
	log.Printf("Response completed: %+v", event.Data)
case webhooks.ResponseFailedWebhookEvent:
	log.Printf("Response failed: %+v", event.Data)
}
```

### Retries

Connection errors, 408, 409, 429, and 500+ are retried 2 times by default with exponential backoff.

```go
// Disable retries
client := openai.NewClient(option.WithMaxRetries(0))

// Custom retry count
client.Responses.New(ctx, params, option.WithMaxRetries(5))
```

### Middleware

```go
func Logger(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
	start := time.Now()
	LogReq(req)
	res, err := next(req)
	end := time.Now()
	LogRes(res, err, end-start)
	return res, err
}

client := openai.NewClient(option.WithMiddleware(Logger))
```

### Request Options

```go
client := openai.NewClient(
	option.WithHeader("X-Some-Header", "custom_header_info"),
)

client.Responses.New(ctx, params,
	option.WithHeader("X-Some-Header", "some_other_value"),
	option.WithJSONSet("some.json.path", map[string]string{"my": "object"}),
)
```

### Raw Response Access

```go
var httpResp *http.Response
response, err := client.Responses.New(ctx, params,
	option.WithResponseInto(&httpResp),
)
fmt.Printf("Status Code: %d\n", httpResp.StatusCode)
fmt.Printf("Headers: %+#v\n", httpResp.Header)
```

### Custom/Undocumented Requests

```go
// Custom endpoints
client.Get(ctx, "/custom/endpoint", params, &result)
client.Post(ctx, "/custom/endpoint", body, &result)

// Custom request params
option.WithJSONSet("data.last_name", "Doe")
option.WithQuerySet("custom_param", "value")

// Access undocumented response fields
result.JSON.RawJSON()
result.JSON.ExtraFields
```

## Integration Notes

### Azure OpenAI

```go
import (
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/openai/openai-go/v3/azure"
)

client := openai.NewClient(
	azure.WithEndpoint(azureOpenAIEndpoint, azureOpenAIAPIVersion),
	azure.WithTokenCredential(tokenCredential),
	// or azure.WithAPIKey(azureOpenAIAPIKey),
)
```

### Workload Identity Authentication

**Kubernetes:**
```go
client := openai.NewClient(
	option.WithWorkloadIdentity(auth.WorkloadIdentity{
		IdentityProviderID: "idp-123",
		ServiceAccountID:   "sa-456",
		Provider:           auth.K8sServiceAccountTokenProvider(""),
	}),
)
```

**Azure Managed Identity:**
```go
Provider: auth.AzureManagedIdentityTokenProvider(nil)
```

**GCP Compute Engine:**
```go
Provider: auth.GCPIDTokenProvider(nil)
```

Custom token providers implement `auth.SubjectTokenProvider` interface. Default refresh buffer is 20 minutes.

### Structured Outputs

Use `jsonschema` tags on Go structs:

```go
type HistoricalComputer struct {
	Origin       Origin   `json:"origin" jsonschema_description:"The origin"`
	Name         string   `json:"full_name" jsonschema_description:"The name"`
	Legacy       string   `json:"legacy" jsonschema:"enum=positive,enum=neutral,enum=negative"`
	NotableFacts []string `json:"notable_facts" jsonschema_description:"Key facts"`
}
```

Use `jsonschema.Reflector` with `AllowAdditionalProperties: false` and `DoNotReference: true`.

### Helper Functions

| Function | Purpose |
|----------|---------|
| `openai.String(s)` | Get `*string` pointer |
| `openai.Int(i)` | Get `*int` pointer |
| `openai.Float(f)` | Get `*float64` pointer |
| `openai.Bool(b)` | Get `*bool` pointer |
| `openai.Time(t)` | Get `*time.Time` pointer |
| `openai.Ptr(v)` | Generic pointer helper |
| `openai.Opt(v)` | Optional value wrapper |
| `openai.File(rdr, filename, contentType)` | Create file upload from reader |

### Service Types

The SDK exposes numerous service types for different API domains:

- **Responses API:** `client.Responses`
- **Chat Completions:** `client.Chat.Completions`
- **Assistants API:** `client.Beta.Assistants`, `client.Beta.Threads`, `client.Beta.ThreadRuns`
- **Audio:** `client.Audio.Speech`, `client.Audio.Transcriptions`, `client.Audio.Translations`
- **Files:** `client.Files`
- **Fine-tuning:** `client.FineTuning.Jobs`
- **Batches:** `client.Batches`
- **Admin APIs:** `client.Admin.OrganizationProjects`, `client.Admin.OrganizationGroups`, etc.

All services follow consistent patterns:
- `Get(ctx, id, opts...)`
- `List(ctx, query, opts...)`
- `ListAutoPaging(ctx, query, opts...)`
- `New(ctx, body, opts...)`
- `Update(ctx, id, body, opts...)`
- `Delete(ctx, id, opts...)`

---
**Source:** Extracted from official sources on June 14, 2026.
