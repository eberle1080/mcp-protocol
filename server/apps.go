package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/eberle1080/jsonrpc"
	"github.com/eberle1080/mcp-protocol/extension/apps"
	"github.com/eberle1080/mcp-protocol/schema"
)

// HTMLSource produces an MCP App's HTML document on each read.
type HTMLSource func(ctx context.Context) (string, error)

// StaticHTML serves the same document on every read.
func StaticHTML(document string) HTMLSource {
	return func(context.Context) (string, error) { return document, nil }
}

// RegisterAppResource registers an MCP App's view (SEP-1865): a ui://
// resource whose content is an HTML document served as
// text/html;profile=mcp-app. The metadata (CSP origins, permissions, domain,
// border preference) goes on the resources/list entry, where a host can review
// it before reading, and on every content item, where hosts must look for it.
// Registering a view declares the extension.
func (r *Registry) RegisterAppResource(resource schema.Resource, meta apps.ResourceMeta, html HTMLSource) error {
	if err := apps.ValidateResourceURI(resource.Uri); err != nil {
		return err
	}
	// Keep a copy: a caller changing its value later must not change, or
	// race with, what is served.
	meta, err := cloneResourceMeta(meta)
	if err != nil {
		return err
	}
	if err := meta.Validate(); err != nil {
		return err
	}
	if html == nil {
		return fmt.Errorf("app resource %s has no HTML source", resource.Uri)
	}
	mimeType := apps.MimeType
	resource.MimeType = &mimeType
	listing, err := apps.WithResourceMeta(resource.Meta, meta)
	if err != nil {
		return err
	}
	resource.Meta = listing
	uri := resource.Uri
	handler := func(ctx context.Context, _ *schema.ReadResourceRequest) (*schema.ReadResourceResult, *jsonrpc.Error) {
		document, err := html(ctx)
		if err != nil {
			return nil, jsonrpc.NewInternalError(fmt.Sprintf("app resource %s: %v", uri, err), nil)
		}
		// A fresh map per read: callers may change what they receive.
		contentMeta, _ := apps.WithResourceMeta(nil, meta)
		contentType := apps.MimeType
		return &schema.ReadResourceResult{Contents: []schema.ReadResourceResultContentsElem{{
			Uri: uri, MimeType: &contentType, Text: document, Meta: contentMeta,
		}}}, nil
	}
	if err := r.RegisterResource(resource, handler); err != nil {
		return err
	}
	r.appResources.Put(uri, true)
	return nil
}

func cloneResourceMeta(meta apps.ResourceMeta) (apps.ResourceMeta, error) {
	raw, err := json.Marshal(meta)
	if err != nil {
		return apps.ResourceMeta{}, err
	}
	var copy apps.ResourceMeta
	err = json.Unmarshal(raw, &copy)
	return copy, err
}

// ImplementsApps reports whether any app resource is registered.
func (r *Registry) ImplementsApps() bool {
	return r != nil && r.appResources != nil && r.appResources.Size() > 0
}

// LinkToolToApp links a registered tool to a registered app resource: the
// tool's tools/list entry carries meta as _meta.ui (and the view's URI under
// the deprecated "ui/resourceUri" key, as the official SDK writes it). With
// Visibility [app], the tool is for the view alone: hosts keep it from the
// model, and this handler hides it from, and refuses it to, clients that do
// not declare MCP Apps. A tool must still return meaningful content for hosts
// that show no view.
func (r *Registry) LinkToolToApp(name string, meta apps.ToolMeta) error {
	entry, ok := r.ToolRegistry.Get(name)
	if !ok {
		return fmt.Errorf("tool %q is not registered", name)
	}
	if meta.ResourceURI != "" {
		if _, ok := r.appResources.Get(meta.ResourceURI); !ok {
			return fmt.Errorf("tool %q links to %s, which is not a registered app resource", name, meta.ResourceURI)
		}
	}
	metadata, err := apps.SetToolMeta(entry.Metadata, meta)
	if err != nil {
		return fmt.Errorf("tool %q: %w", name, err)
	}
	linked := *entry
	linked.Metadata = metadata
	if inner := entry.MetadataFor; inner != nil {
		linked.MetadataFor = func(ctx context.Context) (schema.Tool, error) {
			tool, err := inner(ctx)
			if err != nil {
				return tool, err
			}
			return apps.SetToolMeta(tool, meta)
		}
	}
	r.ToolRegistry.Put(name, &linked)
	return nil
}

// clientSupportsApps reports whether the client behind a request declared MCP
// Apps: per request in 2026-07-28, at initialize before that. An older
// request's own _meta never counts, initialize or not.
func (d *DefaultHandler) clientSupportsApps(meta schema.RequestMetaObject) bool {
	if meta.IoModelcontextprotocolProtocolVersion == schema.LatestProtocolVersion {
		return apps.ClientSupports(&meta.IoModelcontextprotocolClientCapabilities)
	}
	if d.ClientInitialize != nil {
		return apps.ClientSupports(&d.ClientInitialize.Capabilities)
	}
	return false
}

// withoutAppOnlyTools drops the tools only a view may use. A client that
// cannot render views has no view to use them, and would otherwise show them
// to the model.
func withoutAppOnlyTools(tools []schema.Tool) []schema.Tool {
	kept := tools[:0:0]
	for _, tool := range tools {
		if apps.VisibleToModel(tool) {
			kept = append(kept, tool)
		}
	}
	return kept
}
