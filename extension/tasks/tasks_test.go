package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/eberle1080/jsonrpc"
	"github.com/eberle1080/mcp-protocol/schema"
	"github.com/stretchr/testify/require"
)

func TestTaskShapesMatchTheExtensionSchema(t *testing.T) {
	ttl, poll := int64(60000), int64(5000)
	created := CreateTaskResult{ResultType: ResultTypeTask, Task: Task{
		TaskID: "786512e2", Status: Working, StatusMessage: "The operation is now in progress.",
		CreatedAt: "2025-11-25T10:30:00Z", LastUpdatedAt: "2025-11-25T10:40:00Z", TTLMs: &ttl, PollIntervalMs: &poll,
	}}
	raw, err := json.Marshal(created)
	require.NoError(t, err)
	require.JSONEq(t, `{"resultType":"task","taskId":"786512e2","status":"working","statusMessage":"The operation is now in progress.","createdAt":"2025-11-25T10:30:00Z","lastUpdatedAt":"2025-11-25T10:40:00Z","ttlMs":60000,"pollIntervalMs":5000}`, string(raw))

	unlimited := GetTaskResult{ResultType: schema.ResultTypeComplete, Task: Task{TaskID: "a", Status: Failed, CreatedAt: "t", LastUpdatedAt: "t",
		Error: &jsonrpc.Error{Code: -32603, Message: "API rate limit exceeded"}}}
	raw, err = json.Marshal(unlimited)
	require.NoError(t, err)
	require.JSONEq(t, `{"resultType":"complete","taskId":"a","status":"failed","createdAt":"t","lastUpdatedAt":"t","ttlMs":null,"error":{"code":-32603,"message":"API rate limit exceeded"}}`, string(raw))

	var decoded GetTaskResult
	require.NoError(t, json.Unmarshal([]byte(`{"resultType":"complete","taskId":"b","status":"input_required","createdAt":"t","lastUpdatedAt":"t","ttlMs":3600000,"inputRequests":{"name":{"method":"elicitation/create","params":{"mode":"form","message":"Please enter your name."}}}}`), &decoded))
	require.Equal(t, InputRequired, decoded.Status)
	require.Equal(t, int64(3600000), *decoded.TTLMs)
	require.Equal(t, "elicitation/create", decoded.InputRequests["name"].Method)
	require.False(t, decoded.Status.Terminal())
	require.True(t, Completed.Terminal())
	require.True(t, Cancelled.Terminal())
}

func TestMissingCapabilityError(t *testing.T) {
	err := MissingCapabilityError()
	require.EqualValues(t, -32021, err.Code)
	require.JSONEq(t, `{"requiredCapabilities":{"extensions":{"io.modelcontextprotocol/tasks":{}}}}`, string(err.Data))
}

func TestClientSupports(t *testing.T) {
	require.False(t, ClientSupports(nil))
	require.False(t, ClientSupports(&schema.ClientCapabilities{}))
	require.True(t, ClientSupports(&schema.ClientCapabilities{Extensions: map[string]map[string]interface{}{ExtensionID: {}}}))
}

type recordingRunner struct{ options StartOptions }

func (r *recordingRunner) Start(_ context.Context, _ Work, options StartOptions) (Task, error) {
	r.options = options
	return Task{TaskID: "started"}, nil
}

func TestStartNeedsARunner(t *testing.T) {
	work := func(context.Context, Control) (*schema.CallToolResult, *jsonrpc.Error) { return nil, nil }
	_, err := Start(context.Background(), work)
	require.True(t, errors.Is(err, ErrUnavailable))
	require.False(t, Available(context.Background()))

	runner := &recordingRunner{}
	ctx := WithRunner(context.Background(), runner)
	require.True(t, Available(ctx))
	task, err := Start(ctx, work, WithStatusMessage("queued"), WithPollInterval(2e9))
	require.NoError(t, err)
	require.Equal(t, "started", task.TaskID)
	require.Equal(t, "queued", runner.options.StatusMessage)
	require.EqualValues(t, 2e9, runner.options.PollInterval)
	_, err = Start(ctx, nil)
	require.Error(t, err)
}
