package server

import (
	"context"
	"github.com/eberle1080/jsonrpc"
	"github.com/eberle1080/jsonrpc/transport"
	"github.com/eberle1080/mcp-protocol/client"
	"github.com/eberle1080/mcp-protocol/logger"
)

// Handler represents a protocol implementer.
type Handler interface {
	Operations

	OnNotification(ctx context.Context, notification *jsonrpc.Notification)

	// Implements checks if the method is implemented.
	Implements(method string) bool
}

// NewHandler creates new handler implementer.
type NewHandler func(ctx context.Context, notifier transport.Notifier, logger logger.Logger, client client.Operations) (Handler, error)
