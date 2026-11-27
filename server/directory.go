package server

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/eberle1080/jsonrpc"
	skillformat "github.com/eberle1080/mcp-protocol/extension/skills"
	"github.com/eberle1080/mcp-protocol/schema"
)

// ResourceDirectories is optional. A handler that implements it serves
// SEP-2640's resources/directory/read.
type ResourceDirectories interface {
	ReadResourceDirectory(context.Context, *jsonrpc.TypedRequest[*schema.ReadResourceDirectoryRequest]) (*schema.ReadResourceDirectoryResult, *jsonrpc.Error)
}

// ReadResourceDirectory lists the direct children of a directory inside a
// registered skill: the skill root (skill://pdf-processing) or any directory
// below it (skill://pdf-processing/templates). The listing comes from the
// resources registered for the skills, so it covers a static skill completely.
// A dynamic skill's files are listed only if they are registered resources,
// so a handler with dynamic skills has directory reads off unless it sets
// SkillsDirectoryRead to true (all its files are registered) or overrides
// this method.
func (d *DefaultHandler) ReadResourceDirectory(ctx context.Context, request *jsonrpc.TypedRequest[*schema.ReadResourceDirectoryRequest]) (*schema.ReadResourceDirectoryResult, *jsonrpc.Error) {
	if err := ctx.Err(); err != nil {
		return nil, jsonrpc.NewInternalError(err.Error(), nil)
	}
	if request == nil || request.Request == nil || request.Request.Params.Uri == "" {
		return nil, jsonrpc.NewInvalidParamsError("directory URI is required", nil)
	}
	if request.Request.Params.Cursor != nil {
		// Every listing fits in one page, so this server never issues a cursor.
		return nil, jsonrpc.NewInvalidParamsError("invalid directory cursor", nil)
	}
	children, rpcErr := d.readSkillDirectory(request.Request.Params.Uri)
	if rpcErr != nil {
		return nil, rpcErr
	}
	result := &schema.ReadResourceDirectoryResult{ResultType: schema.ResultTypeComplete, Resources: children}
	result.TtlMs, result.CacheScope = d.skillsCacheHints()
	return result, nil
}

// directoryReadEnabled reports whether resources/directory/read is on. Unset,
// it is on only while every registered skill is compiled: the listing of a
// generated skill is complete only if all its files are registered resources,
// which only the server's author can promise, by setting it to true.
func (d *DefaultHandler) directoryReadEnabled() bool {
	if d.SkillsDirectoryRead != nil {
		return *d.SkillsDirectoryRead
	}
	for _, entry := range d.skills.Values() {
		if entry.Resources.Dynamic {
			return false
		}
	}
	return true
}

// readSkillDirectory derives a directory's children from the registered
// resources under it. A directory exists while it holds at least one file, so
// the listing is never empty.
func (r *Registry) readSkillDirectory(uri string) ([]schema.Resource, *jsonrpc.Error) {
	inSkill := false
	for _, entry := range r.skills.Values() {
		if root, err := skillformat.RootOf(entry); err == nil && skillformat.Within(root, uri) {
			inSkill = true
			break
		}
	}
	if !inSkill || strings.HasSuffix(uri, "/") {
		return nil, jsonrpc.NewInvalidParamsError(fmt.Sprintf("no directory resource at %s", uri), nil)
	}
	prefix := uri + "/"
	children := map[string]schema.Resource{}
	isFile := false
	for _, resource := range r.ListRegisteredResources() {
		if resource.Uri == uri {
			isFile = true
			continue
		}
		rest, ok := strings.CutPrefix(resource.Uri, prefix)
		if !ok || rest == "" {
			continue
		}
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			dir := prefix + rest[:slash]
			if _, seen := children[dir]; !seen {
				children[dir] = directoryResource(dir, rest[:slash])
			}
			continue
		}
		copy, err := cloneStaticResource(resource)
		if err != nil {
			return nil, jsonrpc.NewInternalError(err.Error(), nil)
		}
		children[resource.Uri] = copy
	}
	if isFile {
		return nil, jsonrpc.NewInvalidParamsError(fmt.Sprintf("%s is not a directory resource", uri), nil)
	}
	if len(children) == 0 {
		return nil, jsonrpc.NewInvalidParamsError(fmt.Sprintf("no directory resource at %s", uri), nil)
	}
	result := make([]schema.Resource, 0, len(children))
	for _, child := range children {
		result = append(result, child)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Uri < result[j].Uri })
	return result, nil
}

func directoryResource(uri, segment string) schema.Resource {
	name := segment
	if decoded, err := url.PathUnescape(segment); err == nil {
		name = decoded
	}
	mimeType := schema.DirectoryMimeType
	return schema.Resource{Uri: uri, Name: name, MimeType: &mimeType}
}
