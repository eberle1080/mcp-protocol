package myimpl

import (
	"context"
	"github.com/eberle1080/jsonrpc"
	"github.com/eberle1080/jsonrpc/transport"
	"github.com/eberle1080/mcp-protocol/client"
	"github.com/eberle1080/mcp-protocol/logger"
	"github.com/eberle1080/mcp-protocol/schema"
	"github.com/eberle1080/mcp-protocol/server"
)

// MyMCPServer is a sample MCP implementer embedding the default Base.
type MyMCPServer struct {
	*server.DefaultHandler
}

// ListResources implements the resources/list method.
func (i *MyMCPServer) ListResources(
	ctx context.Context,
	jReq *jsonrpc.TypedRequest[*schema.ListResourcesRequest],
) (*schema.ListResourcesResult, *jsonrpc.Error) {
	// TODO: return actual resources
	// req := jReq.Request  // Access actual request if needed
	return &schema.ListResourcesResult{}, nil
}

// Implements indicates which methods this implementer supports.
func (i *MyMCPServer) Implements(method string) bool {
	return method == schema.MethodResourcesList
}

// NewMCPServer returns a factory for MyMCPServer.
func NewMCPServer() server.NewHandler {
	return func(
		ctx context.Context,
		notifier transport.Notifier,
		log logger.Logger,
		client client.Operations,
	) (server.Handler, error) {
		base := server.NewDefaultHandler(notifier, log, client)
		return &MyMCPServer{DefaultHandler: base}, nil
	}
}
