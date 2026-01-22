package client

import (
	"context"
	"github.com/eberle1080/jsonrpc"
)

// Handler extends Operations with support for JSON-RPC notifications.
type Handler interface {
	Operations
	OnNotification(ctx context.Context, notification *jsonrpc.Notification)
}
