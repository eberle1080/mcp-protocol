package server

import (
	"context"
	"testing"

	"github.com/eberle1080/jsonrpc"
	"github.com/eberle1080/mcp-protocol/extension/apps"
	"github.com/eberle1080/mcp-protocol/schema"
	"github.com/stretchr/testify/require"
)

const weatherView = "<!DOCTYPE html><html><body>Weather</body></html>"

func weatherHandler(t *testing.T) *DefaultHandler {
	t.Helper()
	d := NewDefaultHandler(nil, nil, nil)
	border := true
	meta := apps.ResourceMeta{CSP: &apps.CSP{ConnectDomains: []string{"https://api.openweathermap.org"}}, PrefersBorder: &border}
	require.NoError(t, d.RegisterAppResource(schema.Resource{Uri: "ui://weather/dashboard", Name: "weather_dashboard"}, meta, StaticHTML(weatherView)))
	reply := func(context.Context, *schema.CallToolRequest) (*schema.CallToolResult, *jsonrpc.Error) {
		return &schema.CallToolResult{Content: []schema.CallToolResultContentElem{schema.TextContent{Type: "text", Text: "Sunny, 72°F"}}}, nil
	}
	d.RegisterToolWithSchema("get_weather", "Current weather", schema.ToolInputSchema{Type: "object"}, nil, reply)
	d.RegisterToolWithSchema("refresh_dashboard", "Refresh the dashboard", schema.ToolInputSchema{Type: "object"}, nil, reply)
	d.RegisterToolWithSchema("plain", "No view", schema.ToolInputSchema{Type: "object"}, nil, reply)
	require.NoError(t, d.LinkToolToApp("get_weather", apps.ToolMeta{ResourceURI: "ui://weather/dashboard"}))
	require.NoError(t, d.LinkToolToApp("refresh_dashboard", apps.ToolMeta{ResourceURI: "ui://weather/dashboard", Visibility: []apps.Visibility{apps.VisibilityApp}}))
	return d
}

func TestAppResourceListingAndContent(t *testing.T) {
	d := weatherHandler(t)
	listed := d.ListRegisteredResources()
	require.Len(t, listed, 1)
	require.Equal(t, apps.MimeType, *listed[0].MimeType)
	listingMeta, found, err := apps.ResourceMetaOf(listed[0].Meta)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []string{"https://api.openweathermap.org"}, listingMeta.CSP.ConnectDomains)

	read, rpcErr := d.ReadResource(context.Background(), &jsonrpc.TypedRequest[*schema.ReadResourceRequest]{Request: &schema.ReadResourceRequest{Params: schema.ReadResourceRequestParams{Uri: "ui://weather/dashboard"}}})
	require.Nil(t, rpcErr)
	require.Len(t, read.Contents, 1)
	item := read.Contents[0]
	require.Equal(t, "ui://weather/dashboard", item.Uri)
	require.Equal(t, apps.MimeType, *item.MimeType)
	require.Equal(t, weatherView, item.Text)
	contentMeta, found, err := apps.ResourceMetaOf(item.Meta)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, *contentMeta.PrefersBorder)

	// Each read hands out its own metadata.
	item.Meta["ui"] = "changed"
	again, _ := d.ReadResource(context.Background(), &jsonrpc.TypedRequest[*schema.ReadResourceRequest]{Request: &schema.ReadResourceRequest{Params: schema.ReadResourceRequestParams{Uri: "ui://weather/dashboard"}}})
	_, found, err = apps.ResourceMetaOf(again.Contents[0].Meta)
	require.NoError(t, err)
	require.True(t, found)

	discovered := &schema.DiscoverResult{}
	d.Discover(context.Background(), discovered)
	require.Equal(t, map[string]interface{}{}, discovered.Capabilities.Extensions[apps.ExtensionID])
	require.NotNil(t, discovered.Capabilities.Resources)

	none := &schema.DiscoverResult{}
	NewDefaultHandler(nil, nil, nil).Discover(context.Background(), none)
	require.NotContains(t, none.Capabilities.Extensions, apps.ExtensionID)
}

func TestRegisterAppResourceRefusesBadInput(t *testing.T) {
	d := NewDefaultHandler(nil, nil, nil)
	require.Error(t, d.RegisterAppResource(schema.Resource{Uri: "https://example.com/view.html", Name: "x"}, apps.ResourceMeta{}, StaticHTML("x")))
	require.Error(t, d.RegisterAppResource(schema.Resource{Uri: "ui://x/view", Name: "x"}, apps.ResourceMeta{CSP: &apps.CSP{ConnectDomains: []string{"https://a.example; script-src *"}}}, StaticHTML("x")))
	require.Error(t, d.RegisterAppResource(schema.Resource{Uri: "ui://x/view", Name: "x"}, apps.ResourceMeta{}, nil))
	require.False(t, d.ImplementsApps())

	d.RegisterToolWithSchema("t", "t", schema.ToolInputSchema{Type: "object"}, nil, nil)
	require.Error(t, d.LinkToolToApp("missing", apps.ToolMeta{}))
	require.Error(t, d.LinkToolToApp("t", apps.ToolMeta{ResourceURI: "ui://not/registered"}))
	require.Error(t, d.LinkToolToApp("t", apps.ToolMeta{Visibility: []apps.Visibility{"user"}}))
}

func TestLinkToolToAppKeepsCallerSpecificMetadata(t *testing.T) {
	d := weatherHandler(t)
	d.RegisterTool(&ToolEntry{
		Metadata: schema.Tool{Name: "per_caller", InputSchema: schema.ToolInputSchema{Type: "object"}},
		MetadataFor: func(context.Context) (schema.Tool, error) {
			return schema.Tool{Name: "per_caller", Title: ptr("Caller title")}, nil
		},
	})
	require.NoError(t, d.LinkToolToApp("per_caller", apps.ToolMeta{ResourceURI: "ui://weather/dashboard"}))
	tools, err := d.ListToolsFor(context.Background())
	require.NoError(t, err)
	for _, tool := range tools {
		if tool.Name == "per_caller" {
			require.Equal(t, "Caller title", *tool.Title)
			meta, ok := apps.ToolMetaOf(tool)
			require.True(t, ok)
			require.Equal(t, "ui://weather/dashboard", meta.ResourceURI)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func toolNames(tools []schema.Tool) []string {
	var names []string
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

func appsMeta(version string, declared bool) schema.RequestMetaObject {
	meta := schema.RequestMetaObject{IoModelcontextprotocolProtocolVersion: version}
	if declared {
		meta.IoModelcontextprotocolClientCapabilities.Extensions = map[string]map[string]interface{}{apps.ExtensionID: apps.ClientSettings()}
	}
	return meta
}

func TestAppOnlyToolsExistOnlyForClientsThatRenderViews(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		prepare  func(*DefaultHandler) schema.RequestMetaObject
		withView bool
	}{
		{"2026-07-28 declaring apps", func(*DefaultHandler) schema.RequestMetaObject { return appsMeta(schema.LatestProtocolVersion, true) }, true},
		{"2026-07-28 without apps", func(*DefaultHandler) schema.RequestMetaObject { return appsMeta(schema.LatestProtocolVersion, false) }, false},
		{"2025-11-25 that declared apps at initialize", func(d *DefaultHandler) schema.RequestMetaObject {
			d.ClientInitialize = &schema.InitializeRequestParams{ProtocolVersion: "2025-11-25", Capabilities: schema.ClientCapabilities{Extensions: map[string]map[string]interface{}{apps.ExtensionID: apps.ClientSettings()}}}
			return appsMeta("2025-11-25", false)
		}, true},
		{"2025-11-25 without apps", func(d *DefaultHandler) schema.RequestMetaObject {
			d.ClientInitialize = &schema.InitializeRequestParams{ProtocolVersion: "2025-11-25"}
			return appsMeta("2025-11-25", true) // a legacy request's own _meta does not count
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := weatherHandler(t)
			meta := tc.prepare(d)
			listed, rpcErr := d.ListTools(ctx, &jsonrpc.TypedRequest[*schema.ListToolsRequest]{Request: &schema.ListToolsRequest{Params: schema.ListToolsRequestParams{Meta: meta}}})
			require.Nil(t, rpcErr)
			call := func(name string) *jsonrpc.Error {
				_, rpcErr := d.CallTool(ctx, &jsonrpc.TypedRequest[*schema.CallToolRequest]{Request: &schema.CallToolRequest{Params: schema.CallToolRequestParams{Name: name, Meta: meta}}})
				return rpcErr
			}
			require.Nil(t, call("get_weather"))
			require.Nil(t, call("plain"))
			if tc.withView {
				require.ElementsMatch(t, []string{"get_weather", "refresh_dashboard", "plain"}, toolNames(listed.Tools))
				require.Nil(t, call("refresh_dashboard"))
			} else {
				require.ElementsMatch(t, []string{"get_weather", "plain"}, toolNames(listed.Tools))
				rpcErr := call("refresh_dashboard")
				require.NotNil(t, rpcErr)
				require.EqualValues(t, jsonrpc.MethodNotFound, rpcErr.Code)
			}
		})
	}
}

func TestAppResourceMetadataIsCopiedAtRegistration(t *testing.T) {
	d := NewDefaultHandler(nil, nil, nil)
	meta := apps.ResourceMeta{CSP: &apps.CSP{ConnectDomains: []string{"https://api.example.com"}}}
	require.NoError(t, d.RegisterAppResource(schema.Resource{Uri: "ui://x/view", Name: "x"}, meta, StaticHTML("<html></html>")))
	meta.CSP.ConnectDomains = append(meta.CSP.ConnectDomains[:0], "https://evil.example; script-src *")

	read, rpcErr := d.ReadResource(context.Background(), &jsonrpc.TypedRequest[*schema.ReadResourceRequest]{Request: &schema.ReadResourceRequest{Params: schema.ReadResourceRequestParams{Uri: "ui://x/view"}}})
	require.Nil(t, rpcErr)
	served, _, err := apps.ResourceMetaOf(read.Contents[0].Meta)
	require.NoError(t, err)
	require.Equal(t, []string{"https://api.example.com"}, served.CSP.ConnectDomains)
}

func TestAppOnlyCallsAreJudgedByTheCallersMetadata(t *testing.T) {
	ctx := context.Background()
	d := weatherHandler(t)
	reply := func(context.Context, *schema.CallToolRequest) (*schema.CallToolResult, *jsonrpc.Error) {
		return &schema.CallToolResult{Content: []schema.CallToolResultContentElem{}}, nil
	}
	appOnly, err := apps.SetToolMeta(schema.Tool{Name: "per_caller"}, apps.ToolMeta{Visibility: []apps.Visibility{apps.VisibilityApp}})
	require.NoError(t, err)
	d.RegisterTool(&ToolEntry{
		Handler:     reply,
		Metadata:    schema.Tool{Name: "per_caller", InputSchema: schema.ToolInputSchema{Type: "object"}},
		MetadataFor: func(context.Context) (schema.Tool, error) { return appOnly, nil },
	})
	meta := appsMeta(schema.LatestProtocolVersion, false)
	listed, rpcErr := d.ListTools(ctx, &jsonrpc.TypedRequest[*schema.ListToolsRequest]{Request: &schema.ListToolsRequest{Params: schema.ListToolsRequestParams{Meta: meta}}})
	require.Nil(t, rpcErr)
	require.NotContains(t, toolNames(listed.Tools), "per_caller")
	_, rpcErr = d.CallTool(ctx, &jsonrpc.TypedRequest[*schema.CallToolRequest]{Request: &schema.CallToolRequest{Params: schema.CallToolRequestParams{Name: "per_caller", Meta: meta}}})
	require.NotNil(t, rpcErr)
}

func TestAnOlderRequestNeverVouchesForItself(t *testing.T) {
	ctx := context.Background()
	d := weatherHandler(t) // no initialize
	meta := appsMeta("2025-11-25", true)
	listed, rpcErr := d.ListTools(ctx, &jsonrpc.TypedRequest[*schema.ListToolsRequest]{Request: &schema.ListToolsRequest{Params: schema.ListToolsRequestParams{Meta: meta}}})
	require.Nil(t, rpcErr)
	require.NotContains(t, toolNames(listed.Tools), "refresh_dashboard")
}
