# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

This is `github.com/eberle1080/mcp-protocol`, a fork of `github.com/viant/mcp-protocol`. It's a Go implementation of the Model Context Protocol (MCP) — a standardized JSON-RPC 2.0-based protocol for AI model communication. This repository contains the **shared protocol definitions and schemas**. The original is used by the actual implementation at `github.com/viant/mcp`.

## Building and Testing

### Running Tests
```bash
# Run all tests
go test ./...

# Run tests in a specific package
go test ./schema

# Run a specific test
go test ./schema -run TestStructToProperties_EmbeddedAnonymous

# Run tests with verbose output
go test -v ./...
```

### Building
```bash
# Build all packages
go build ./...

# Verify module dependencies
go mod tidy
go mod verify
```

### Code Generation
The protocol schemas are generated from JSON schema definitions:
```bash
# Regenerate schema types (uses go:generate directive in schema/generate.go)
go generate ./schema
```

## Architecture

### Core Design Pattern: Handler Interface with TypedRequest

**Critical:** All MCP operation methods in the `server.Operations` interface accept `*jsonrpc.TypedRequest[T]` parameters, NOT raw request types. For example:

```go
// ✅ CORRECT - from server/operation.go
ListResources(ctx context.Context, request *jsonrpc.TypedRequest[*schema.ListResourcesRequest]) (*schema.ListResourcesResult, *jsonrpc.Error)

// ❌ WRONG - common mistake
ListResources(ctx context.Context, request *schema.ListResourcesRequest) (*schema.ListResourcesResult, *jsonrpc.Error)
```

When implementing custom handlers that embed `DefaultHandler`, you MUST match this signature exactly. To access the actual request, use `request.Request` from the TypedRequest.

The only exception is `Initialize()`, which takes raw parameters: `Initialize(ctx context.Context, init *schema.InitializeRequestParams, result *schema.InitializeResult)`.

### Module Structure

- **`schema/`**: Core protocol types generated from MCP JSON schema definitions
  - Contains JSON-RPC request, result, and notification types
  - `schema/const.go`: Method name constants (`MethodResourcesList`, `MethodToolsCall`, etc.)
  - `schema/2025-06-18/`: Version-specific schema types
  - `schema/draft/`: Draft protocol features

- **`server/`**: Server-side interfaces and default implementations
  - `Handler` interface: Full MCP server implementation contract
  - `Operations` interface: All JSON-RPC methods a handler may implement
  - `DefaultHandler`: Base implementation with registries for tools/resources/prompts
  - `Registry`: Storage for registered tools, resources, prompts, and resource templates
  - Helper functions: `RegisterTool[I, O]()`, `RegisterResource()`, etc.

- **`client/`**: Client-side operations interface
  - `Operations` interface for MCP clients
  - Client capabilities: roots, sampling (createMessage), elicit

- **`authorization/`**: Authentication and fine-grained authorization
  - OAuth2 support (official MCP spec)
  - Experimental fine-grained resource/tool authorization
  - Token management and policy definitions

- **`logger/`**: Logging interface for JSON-RPC notifications

- **`oauth2/meta/`**: OAuth2 metadata definitions
  - Authorization server metadata
  - JWK (JSON Web Key) handling
  - Resource metadata

- **`extension/`**: Optional helpers NOT part of official MCP spec
  - `continuation`: Pagination/truncation hints for tool responses

- **`syncmap/`**: Thread-safe map implementations used by registries

### Creating Custom Handlers

There are two approaches to creating MCP server implementations:

#### 1. Using DefaultHandler with Registration (Recommended for Simple Cases)
```go
newHandler := serverproto.WithDefaultHandler(context.Background(), func(h *serverproto.DefaultHandler) {
    // Register resources
    h.RegisterResource(schema.Resource{Name: "hello", Uri: "/hello"}, handlerFunc)

    // Register typed tools with automatic schema generation
    serverproto.RegisterTool[*InputType](h, "toolName", "description", handlerFunc)
})
```

#### 2. Embedding DefaultHandler (For Custom Behavior)
```go
type MyHandler struct {
    *server.DefaultHandler
}

// Override specific methods - MUST use TypedRequest parameter
func (h *MyHandler) ListResources(ctx context.Context, jReq *jsonrpc.TypedRequest[*schema.ListResourcesRequest]) (*schema.ListResourcesResult, *jsonrpc.Error) {
    request := jReq.Request  // Access actual request from TypedRequest
    // Custom implementation
}

// Declare which methods are implemented
func (h *MyHandler) Implements(method string) bool {
    switch method {
    case schema.MethodResourcesList:
        return true
    }
    return h.DefaultHandler.Implements(method)
}
```

### Tool Registration with Automatic Schema Generation

The `RegisterTool[I, O]()` generic function automatically generates JSON schemas from struct types using reflection:

- **Struct tags control schema generation:**
  - `json:"fieldName"`: Field name (required)
  - `json:",omitempty"`: Marks field as optional
  - `description:"text"`: Field description
  - `choice:"value"`: Enum values (multiple tags create array)
  - `default:"value"`: Default value
  - `format:"uri"`: JSON schema format hint
  - `required:"true|false"`: Explicitly mark required/optional
  - `optional`: Alternative to `required:"false"`
  - `internal:"true"`: Skip field in schema
  - `json:",inline"`: Flatten embedded struct fields

- **Required field logic:**
  - Non-pointer fields without `omitempty` are required by default
  - Pointer fields or fields with `omitempty` are optional
  - Embedded structs: value embedding propagates required, pointer embedding doesn't
  - Explicit `required` tag overrides defaults

Example:
```go
type ToolInput struct {
    Name   string `json:"name" description:"User name"`                    // required
    Email  string `json:"email,omitempty" format:"email"`                  // optional
    State  string `json:"state" choice:"open" choice:"closed" choice:"all"` // enum
    Domain string `json:"domain" default:"github.com"`                     // has default
}
```

### JSON-RPC Error Handling

Return `*jsonrpc.Error` for failures:
- `jsonrpc.NewMethodNotFound()`: Method/resource/tool not found
- `jsonrpc.NewInvalidRequest()`: Validation failures
- `jsonrpc.NewError(code, message, data)`: Custom errors
- Standard codes: `jsonrpc.InvalidParams`, `jsonrpc.InternalError`

### Protocol Version Compatibility

Check client protocol version in handlers:
```go
if !schema.IsProtocolNewer(d.ClientInitialize.ProtocolVersion, "2025-03-26") {
    // Handle older clients - e.g., remove outputSchema which was added after 2025-03-26
}
```

### Registry Pattern

`DefaultHandler` embeds a `Registry` with thread-safe maps:
- `ToolRegistry`: Maps tool name → ToolEntry (metadata + handler)
- `ResourceRegistry`: Maps URI → ResourceEntry (metadata + handler)
- `ResourceTemplateRegistry`: URI templates
- `Prompts`: Maps prompt name → PromptEntry (metadata + handler)
- `Methods`: Tracks which JSON-RPC methods are implemented

Handlers use these registries to dynamically respond to `list` operations and dispatch `call`/`read` operations.

### Server Capabilities

Set `ServerCapabilities` on DefaultHandler to advertise features:
```go
handler.ServerCapabilities = &schema.ServerCapabilities{
    Resources: &schema.ServerCapabilitiesResources{Subscribe: true},
    Tools:     &schema.ServerCapabilitiesTools{},
    Prompts:   &schema.ServerCapabilitiesPrompts{},
}
```

The `Initialize` method automatically sets capabilities based on registered items.

## Common Patterns

### Accessing Request from TypedRequest
```go
func (h *Handler) ReadResource(ctx context.Context, jReq *jsonrpc.TypedRequest[*schema.ReadResourceRequest]) (*schema.ReadResourceResult, *jsonrpc.Error) {
    request := jReq.Request  // Get actual *schema.ReadResourceRequest
    uri := request.Params.Uri
    // ...
}
```

### Resource Subscriptions
```go
// DefaultHandler maintains subscription state
handler.Subscription.Put(uri, true)   // Subscribe
handler.Subscription.Delete(uri)      // Unsubscribe
```

### Sending Notifications
```go
// Use the Notifier from Handler
handler.Notifier.Notify(ctx, &jsonrpc.Notification{
    Method: schema.MethodNotificationResourceUpdated,
    Params: params,
})
```

### Client Capabilities Check
```go
func (h *DefaultHandler) Initialize(ctx context.Context, init *schema.InitializeRequestParams, result *schema.InitializeResult) {
    h.ClientInitialize = init
    h.Client.Init(ctx, &init.Capabilities)
    // Check capabilities
    if init.Capabilities.Sampling != nil {
        // Client supports sampling
    }
}
```
