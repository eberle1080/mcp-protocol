package server

import (
	"context"
	"regexp"

	"github.com/eberle1080/jsonrpc"
	"github.com/eberle1080/mcp-protocol/schema"
)

// ResourceTemplateEntry holds metadata for a resource template.
type ResourceTemplateEntry struct {
	Metadata schema.ResourceTemplate
	Handler  ResourceHandlerFunc
}

// ResourceHandlerFunc defines a function to handle a resource read.
type ResourceHandlerFunc func(ctx context.Context, request *schema.ReadResourceRequest) (*schema.ReadResourceResult, *jsonrpc.Error)

// ResourceEntry holds a handler with its metadata.
type ResourceEntry struct {
	Handler  ResourceHandlerFunc
	Metadata schema.Resource
}

// RegisterResource registers a resource with metadata and handler on this handler.
func (d *Registry) RegisterResource(resource schema.Resource, handler ResourceHandlerFunc) {
	d.Methods.Put(schema.MethodResourcesList, true)
	d.Methods.Put(schema.MethodResourcesRead, true)
	d.ResourceRegistry.Put(resource.Uri, &ResourceEntry{
		Handler:  handler,
		Metadata: resource,
	})
}

// RegisterResourceTemplate registers a resource template on this handler.
func (d *Registry) RegisterResourceTemplate(template schema.ResourceTemplate, handler ResourceHandlerFunc) {
	d.Methods.Put(schema.MethodResourcesTemplatesList, true)
	d.ResourceTemplateRegistry.Put(template.UriTemplate, &ResourceTemplateEntry{
		Metadata: template,
		Handler:  handler,
	})
}

// ListRegisteredResources returns metadata for all registered resources on this handler.
func (d *Registry) ListRegisteredResources() []schema.Resource {
	var list []schema.Resource
	d.ResourceRegistry.Range(func(_ string, entry *ResourceEntry) bool {
		list = append(list, entry.Metadata)
		return true
	})
	return list
}

// ListRegisteredResourceTemplates returns metadata for all registered resource templates on this handler.
func (d *Registry) ListRegisteredResourceTemplates() []schema.ResourceTemplate {
	var list []schema.ResourceTemplate
	d.ResourceTemplateRegistry.Range(func(_ string, entry *ResourceTemplateEntry) bool {
		list = append(list, entry.Metadata)
		return true
	})
	return list
}

// matchesTemplate checks if a URI matches a URI template pattern.
// Converts RFC 6570 template syntax like "amp://providers/{provider}" to a regex
// and checks if the URI matches.
func matchesTemplate(uri, template string) bool {
	// Escape special regex characters except for {}
	pattern := regexp.QuoteMeta(template)

	// Replace {variable} with a regex pattern that matches any non-empty string
	// {variable} becomes ([^/]+) to match anything except slashes
	pattern = regexp.MustCompile(`\\\{[^}]+\\\}`).ReplaceAllString(pattern, `([^/]+)`)

	// Anchor the pattern to match the entire URI
	pattern = "^" + pattern + "$"

	matched, _ := regexp.MatchString(pattern, uri)
	return matched
}

// getResourceHandler retrieves the handler for a registered resource on this handler.
func (d *Registry) getResourceHandler(uri string) (ResourceHandlerFunc, bool) {
	// First try exact match for static resources
	if resourceEntry, ok := d.ResourceRegistry.Get(uri); ok {
		return resourceEntry.Handler, true
	}

	// Then try exact match for templates (unlikely but possible)
	if templateEntry, ok := d.ResourceTemplateRegistry.Get(uri); ok {
		return templateEntry.Handler, true
	}

	// Finally, iterate through templates and try pattern matching
	var matchedHandler ResourceHandlerFunc
	d.ResourceTemplateRegistry.Range(func(templatePattern string, entry *ResourceTemplateEntry) bool {
		if matchesTemplate(uri, templatePattern) {
			matchedHandler = entry.Handler
			return false // Stop iteration
		}
		return true // Continue iteration
	})

	if matchedHandler != nil {
		return matchedHandler, true
	}

	return nil, false
}

// RegisterResource registers a resource using a typed handler that returns a Go struct.
// The struct will be JSON-marshaled into the ReadResourceResult.Contents field.
func RegisterResource[I any](registry *Registry, resource schema.Resource, handler func(ctx context.Context, uri string) (*schema.ReadResourceResult, *jsonrpc.Error)) {
	wrapped := func(ctx context.Context, request *schema.ReadResourceRequest) (*schema.ReadResourceResult, *jsonrpc.Error) {
		output, rpcErr := handler(ctx, request.Params.Uri)
		if rpcErr != nil {
			return nil, rpcErr
		}
		return output, nil
	}
	registry.RegisterResource(resource, wrapped)
}
