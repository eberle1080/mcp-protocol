// Package tasks holds the types of the Tasks extension
// (io.modelcontextprotocol/tasks, MCP 2026-07-28) and the API a tool uses to
// answer a tools/call with a task instead of a result.
//
// The client opts in on each request by declaring the extension in its
// capabilities. The server decides per request whether to create a task. The
// client then polls tasks/get (or listens for notifications/tasks), answers
// the task's input requests with tasks/update, and may cancel it with
// tasks/cancel.
//
// A tool starts a task with Start. The runtime that serves the request (the
// mcp server, with tasks enabled) supplies the Runner; without one, or when
// the client did not declare the extension, Start returns ErrUnavailable and
// the tool does the work itself or refuses with MissingCapabilityError.
package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/eberle1080/jsonrpc"
	"github.com/eberle1080/mcp-protocol/schema"
)

const (
	// ExtensionID identifies the extension in capabilities.extensions.
	ExtensionID = "io.modelcontextprotocol/tasks"

	MethodGet    = "tasks/get"
	MethodUpdate = "tasks/update"
	MethodCancel = "tasks/cancel"
	// MethodNotification carries a task's state to a subscriptions/listen stream.
	MethodNotification = "notifications/tasks"

	// ResultTypeTask marks a CreateTaskResult.
	ResultTypeTask = "task"

	// ErrorCodeMissingCapability is MCP's Missing Required Client Capability.
	ErrorCodeMissingCapability = -32021
)

// Status is a task's state.
type Status string

const (
	Working       Status = "working"
	InputRequired Status = "input_required"
	Completed     Status = "completed"
	Failed        Status = "failed"
	Cancelled     Status = "cancelled"
)

// Terminal reports whether the task has finished.
func (s Status) Terminal() bool {
	return s == Completed || s == Failed || s == Cancelled
}

// Task is a task's state, with the fields of its status: inputRequests while
// input_required, result when completed, error when failed (the extension's
// DetailedTask).
type Task struct {
	TaskID        string `json:"taskId"`
	Status        Status `json:"status"`
	StatusMessage string `json:"statusMessage,omitempty"`
	// CreatedAt and LastUpdatedAt are ISO 8601 timestamps.
	CreatedAt     string `json:"createdAt"`
	LastUpdatedAt string `json:"lastUpdatedAt"`
	// TTLMs is how long after creation the server may discard the task; nil
	// (sent as null) means no limit.
	TTLMs *int64 `json:"ttlMs"`
	// PollIntervalMs is how often the server would like to be polled.
	PollIntervalMs *int64 `json:"pollIntervalMs,omitempty"`
	// InputRequests are the outstanding server-to-client requests, keyed by
	// identifiers unique over the task's life. Each is {"method", "params"}.
	InputRequests map[string]InputRequest `json:"inputRequests,omitempty"`
	// Result is the final result of the original request (a CallToolResult
	// for tools/call), including one with isError: true.
	Result json.RawMessage `json:"result,omitempty"`
	// Error is the JSON-RPC error the request ended with.
	Error *jsonrpc.Error `json:"error,omitempty"`
}

// InputRequest is a server-to-client request carried in a task, such as an
// elicitation/create or sampling/createMessage.
type InputRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// CreateTaskResult answers a request with a task (resultType "task").
type CreateTaskResult struct {
	Meta       *schema.ResultMetaObject `json:"_meta,omitempty"`
	ResultType string                   `json:"resultType"`
	Task
}

// GetTaskRequestParams are the parameters of tasks/get.
type GetTaskRequestParams struct {
	TaskID string                 `json:"taskId"`
	Meta   map[string]interface{} `json:"_meta,omitempty"`
}

// GetTaskResult is a task's current state (resultType "complete").
type GetTaskResult struct {
	Meta       *schema.ResultMetaObject `json:"_meta,omitempty"`
	ResultType string                   `json:"resultType"`
	Task
}

// UpdateTaskRequestParams answer a task's input requests. Each key must be
// one the task listed in inputRequests; the value is that request's result.
type UpdateTaskRequestParams struct {
	TaskID         string                     `json:"taskId"`
	InputResponses map[string]json.RawMessage `json:"inputResponses"`
	Meta           map[string]interface{}     `json:"_meta,omitempty"`
}

// CancelTaskRequestParams are the parameters of tasks/cancel.
type CancelTaskRequestParams struct {
	TaskID string                 `json:"taskId"`
	Meta   map[string]interface{} `json:"_meta,omitempty"`
}

// AckResult acknowledges tasks/update and tasks/cancel.
type AckResult struct {
	Meta       *schema.ResultMetaObject `json:"_meta,omitempty"`
	ResultType string                   `json:"resultType"`
}

// ClientSupports reports whether client capabilities declare the extension.
func ClientSupports(capabilities *schema.ClientCapabilities) bool {
	if capabilities == nil {
		return false
	}
	_, ok := capabilities.Extensions[ExtensionID]
	return ok
}

// MissingCapabilityError is the error for a request that needs the extension
// from a client that did not declare it.
func MissingCapabilityError() *jsonrpc.Error {
	data, _ := json.Marshal(map[string]interface{}{
		"requiredCapabilities": map[string]interface{}{
			"extensions": map[string]interface{}{ExtensionID: map[string]interface{}{}},
		},
	})
	return &jsonrpc.Error{Code: ErrorCodeMissingCapability, Message: "Missing required client capability", Data: data}
}

// ErrUnavailable means the current request cannot be answered with a task:
// the client did not declare the extension, the protocol version predates
// it, or the server does not run tasks.
var ErrUnavailable = errors.New("tasks: this request cannot be answered with a task")

// Work is the body of a task. It runs after the client has been answered with
// the task, on a context that keeps the request's values (such as its
// credentials) and is cancelled when the client cancels the task or the task
// expires. What it returns becomes the task's result: a CallToolResult
// (isError: true included) completes the task, a JSON-RPC error fails it.
type Work func(ctx context.Context, task Control) (*schema.CallToolResult, *jsonrpc.Error)

// Control is how running work reports on its task and asks the client for
// input. In 2026-07-28 a server cannot send requests to the client; a task's
// input requests are how it asks.
type Control interface {
	// ID is the task's identifier.
	ID() string
	// Progress sets the task's status message.
	Progress(message string)
	// Elicit asks the client to elicit input from its user and waits for the
	// answer (delivered by tasks/update).
	Elicit(ctx context.Context, params *schema.ElicitRequestParams) (*schema.ElicitResult, error)
	// CreateMessage asks the client to sample its model and waits for the answer.
	CreateMessage(ctx context.Context, params *schema.CreateMessageRequestParams) (*schema.CreateMessageResult, error)
	// Input sends any server-to-client request through the task and returns
	// the client's raw answer.
	Input(ctx context.Context, method string, params interface{}) (json.RawMessage, error)
}

// StartOptions tune one task.
type StartOptions struct {
	// TTL is how long after creation the server keeps the task; zero uses
	// the server's default.
	TTL time.Duration
	// PollInterval is how often the client should poll; zero uses the
	// server's default.
	PollInterval time.Duration
	// StatusMessage is the task's first status message.
	StatusMessage string
}

// Option sets a StartOptions field.
type Option func(*StartOptions)

func WithTTL(ttl time.Duration) Option { return func(o *StartOptions) { o.TTL = ttl } }

func WithPollInterval(interval time.Duration) Option {
	return func(o *StartOptions) { o.PollInterval = interval }
}

func WithStatusMessage(message string) Option {
	return func(o *StartOptions) { o.StatusMessage = message }
}

// Runner starts tasks for one request. A runtime puts one in the context of a
// request that may be answered with a task.
type Runner interface {
	Start(ctx context.Context, work Work, options StartOptions) (Task, error)
}

type runnerKey struct{}

// WithRunner returns a context whose requests may be answered with tasks
// started by runner.
func WithRunner(ctx context.Context, runner Runner) context.Context {
	return context.WithValue(ctx, runnerKey{}, runner)
}

// Start answers the current request with a task that runs work. After it
// succeeds, the tool returns (nil, nil): the client is sent the task, not
// what the tool returns. It returns ErrUnavailable when the request cannot be
// answered with a task; the tool then does the work itself, or refuses with
// MissingCapabilityError if it cannot.
func Start(ctx context.Context, work Work, options ...Option) (Task, error) {
	runner, ok := ctx.Value(runnerKey{}).(Runner)
	if !ok || runner == nil {
		return Task{}, ErrUnavailable
	}
	if work == nil {
		return Task{}, errors.New("tasks: work is required")
	}
	var o StartOptions
	for _, option := range options {
		option(&o)
	}
	return runner.Start(ctx, work, o)
}

// Available reports whether the current request may be answered with a task.
func Available(ctx context.Context) bool {
	runner, ok := ctx.Value(runnerKey{}).(Runner)
	return ok && runner != nil
}
