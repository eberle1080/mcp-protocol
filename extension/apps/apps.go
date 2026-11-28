// Package apps holds the types and pure helpers of the MCP Apps extension
// (io.modelcontextprotocol/ui, SEP-1865): interactive HTML views that a server
// declares as ui:// resources and links to its tools, and that a host renders
// in a sandboxed iframe.
//
// It covers what passes over MCP: the client's declaration, a UI resource's
// metadata (CSP domains, permissions, dedicated domain, border preference), a
// tool's link to its view and who may call it, and the Content Security
// Policy and iframe allow attribute a host builds from that metadata. The
// ui/* messages between a view and its host never reach the server and are
// not modelled here.
package apps

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/eberle1080/mcp-protocol/schema"
)

const (
	// ExtensionID identifies the extension in capabilities.extensions.
	ExtensionID = "io.modelcontextprotocol/ui"
	// MimeType is the content type of an MCP App's HTML.
	MimeType = "text/html;profile=mcp-app"
	// Scheme is the URI scheme of UI resources.
	Scheme = "ui"
	// MetaKey is the _meta key that holds UI metadata on tools, resources and
	// resource contents.
	MetaKey = "ui"
	// LegacyResourceURIKey is the deprecated flat _meta key for a tool's view.
	// Writers set it beside _meta.ui.resourceUri; readers accept either.
	LegacyResourceURIKey = "ui/resourceUri"
)

// Visibility says who may use a tool.
type Visibility string

const (
	// VisibilityModel: the tool is in the agent's tool list and the model may call it.
	VisibilityModel Visibility = "model"
	// VisibilityApp: a view from the same server may call the tool.
	VisibilityApp Visibility = "app"
)

// ToolMeta is a tool's _meta.ui.
type ToolMeta struct {
	// ResourceURI is the ui:// resource that renders the tool's results.
	ResourceURI string `json:"resourceUri,omitempty"`
	// Visibility defaults to model and app when empty.
	Visibility []Visibility `json:"visibility,omitempty"`
}

// Allows reports whether who may use the tool. A nil Visibility (the field
// omitted) allows both; an explicitly empty one, as a host checking
// visibility.includes(...) reads it, allows neither.
func (m ToolMeta) Allows(who Visibility) bool {
	if m.Visibility == nil {
		return true
	}
	for _, v := range m.Visibility {
		if v == who {
			return true
		}
	}
	return false
}

// Validate checks the metadata a server is about to publish.
func (m ToolMeta) Validate() error {
	if m.ResourceURI != "" {
		if err := ValidateResourceURI(m.ResourceURI); err != nil {
			return err
		}
	}
	if m.Visibility != nil && len(m.Visibility) == 0 {
		return fmt.Errorf("empty tool visibility allows no one; leave it unset for model and app")
	}
	for _, v := range m.Visibility {
		if v != VisibilityModel && v != VisibilityApp {
			return fmt.Errorf("unknown tool visibility %q", v)
		}
	}
	return nil
}

// CSP lists the origins a view needs beyond its own document. Each list left
// empty means none.
type CSP struct {
	// ConnectDomains: fetch, XHR and WebSocket (connect-src).
	ConnectDomains []string `json:"connectDomains,omitempty"`
	// ResourceDomains: scripts, styles, images, fonts and media.
	ResourceDomains []string `json:"resourceDomains,omitempty"`
	// FrameDomains: nested iframes (frame-src).
	FrameDomains []string `json:"frameDomains,omitempty"`
	// BaseURIDomains: allowed <base> URIs (base-uri).
	BaseURIDomains []string `json:"baseUriDomains,omitempty"`
}

// Permission is an empty object that requests a browser capability.
type Permission struct{}

// Permissions are the browser capabilities a view asks for. A host may grant
// them through the iframe's allow attribute; a view must not assume it did.
type Permissions struct {
	Camera         *Permission `json:"camera,omitempty"`
	Microphone     *Permission `json:"microphone,omitempty"`
	Geolocation    *Permission `json:"geolocation,omitempty"`
	ClipboardWrite *Permission `json:"clipboardWrite,omitempty"`
}

// ResourceMeta is a UI resource's _meta.ui.
type ResourceMeta struct {
	CSP         *CSP         `json:"csp,omitempty"`
	Permissions *Permissions `json:"permissions,omitempty"`
	// Domain asks for a dedicated sandbox origin. Its format is host-specific.
	Domain string `json:"domain,omitempty"`
	// PrefersBorder: true asks for a border and background, false for none,
	// unset leaves it to the host.
	PrefersBorder *bool `json:"prefersBorder,omitempty"`
}

// IsZero reports whether the metadata says nothing.
func (m ResourceMeta) IsZero() bool {
	return m.CSP == nil && m.Permissions == nil && m.Domain == "" && m.PrefersBorder == nil
}

// Validate checks the metadata a server is about to publish: every CSP entry
// must be an origin a host can put into a policy unchanged.
func (m ResourceMeta) Validate() error {
	if m.CSP == nil {
		return nil
	}
	for _, list := range [][]string{m.CSP.ConnectDomains, m.CSP.ResourceDomains, m.CSP.FrameDomains, m.CSP.BaseURIDomains} {
		for _, domain := range list {
			if !ValidSource(domain) {
				return fmt.Errorf("invalid CSP origin %q", domain)
			}
		}
	}
	return nil
}

// ValidateResourceURI checks that uri is a ui:// resource URI.
func ValidateResourceURI(uri string) error {
	parsed, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("invalid UI resource URI %q: %w", uri, err)
	}
	if parsed.Scheme != Scheme || parsed.Host == "" {
		return fmt.Errorf("UI resource URI %q must start with ui://", uri)
	}
	return nil
}

// source matches a CSP host-source with an explicit scheme: an origin, an
// optional "*." wildcard on the host, an optional port and an optional path.
// It admits nothing that could end a directive or add a keyword: no spaces,
// semicolons, commas or quotes.
var source = regexp.MustCompile(`^(https?|wss?)://(\*\.)?[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*(:([0-9]{1,5}|\*))?(/[A-Za-z0-9._~!$&()*+=:@%/-]*)?$`)

// ValidSource reports whether domain is an origin a host may copy into a
// Content Security Policy, such as "https://api.example.com",
// "wss://realtime.example.com" or "https://*.example.com". A server's CSP
// metadata is untrusted input: an entry like "https://a.example; script-src *"
// would rewrite the policy if copied as it is.
func ValidSource(domain string) bool {
	return source.MatchString(domain)
}

// ToolMetaOf reads a tool's UI metadata from _meta.ui, falling back to the
// deprecated flat "ui/resourceUri" key. It reports false when the tool has
// neither. Keys are matched exactly as SEP-1865 spells them. Metadata that
// does not parse fails closed: the tool is returned as visible to no one.
func ToolMetaOf(tool schema.Tool) (ToolMeta, bool) {
	meta, found, err := ParseToolMeta(tool)
	if err != nil {
		closed := ToolMeta{Visibility: []Visibility{}}
		if legacy, ok := tool.Meta[LegacyResourceURIKey].(string); ok {
			closed.ResourceURI = legacy
		}
		return closed, true
	}
	return meta, found
}

// ParseToolMeta is ToolMetaOf with the parse error, for callers that want to
// report malformed metadata.
func ParseToolMeta(tool schema.Tool) (ToolMeta, bool, error) {
	var meta ToolMeta
	found := false
	if raw, ok := tool.Meta[MetaKey]; ok && raw != nil {
		object, err := asObject(raw)
		if err != nil {
			return ToolMeta{}, true, fmt.Errorf("_meta.ui: %w", err)
		}
		if meta.ResourceURI, err = optionalString(object, "resourceUri"); err != nil {
			return ToolMeta{}, true, fmt.Errorf("_meta.ui: %w", err)
		}
		if value, present := object["visibility"]; present && value != nil {
			list, err := stringList(value)
			if err != nil {
				return ToolMeta{}, true, fmt.Errorf("_meta.ui.visibility: %w", err)
			}
			meta.Visibility = make([]Visibility, 0, len(list))
			for _, v := range list {
				meta.Visibility = append(meta.Visibility, Visibility(v))
			}
		}
		found = true
	}
	if meta.ResourceURI == "" {
		if legacy, ok := tool.Meta[LegacyResourceURIKey].(string); ok && legacy != "" {
			meta.ResourceURI = legacy
			found = true
		}
	}
	return meta, found, nil
}

// SetToolMeta returns a copy of tool carrying meta in _meta.ui and, as the
// official SDK does for hosts that read only the old form, the resource URI
// in the deprecated "ui/resourceUri" key. Other _meta keys are kept.
func SetToolMeta(tool schema.Tool, meta ToolMeta) (schema.Tool, error) {
	if err := meta.Validate(); err != nil {
		return tool, err
	}
	value, err := toMap(meta)
	if err != nil {
		return tool, err
	}
	merged := make(map[string]interface{}, len(tool.Meta)+2)
	for key, v := range tool.Meta {
		merged[key] = v
	}
	merged[MetaKey] = value
	delete(merged, LegacyResourceURIKey)
	if meta.ResourceURI != "" {
		merged[LegacyResourceURIKey] = meta.ResourceURI
	}
	tool.Meta = merged
	return tool, nil
}

// VisibleToModel reports whether a host may show the tool to the model.
func VisibleToModel(tool schema.Tool) bool {
	meta, _ := ToolMetaOf(tool)
	return meta.Allows(VisibilityModel)
}

// CallableByApp reports whether a view from the tool's own server may call it.
func CallableByApp(tool schema.Tool) bool {
	meta, _ := ToolMetaOf(tool)
	return meta.Allows(VisibilityApp)
}

// ResourceMetaOf reads UI metadata from a resource's or a content item's
// _meta. It reports false when there is none. Keys are matched exactly as
// SEP-1865 spells them: a host must not act on origins that other hosts, and
// anyone auditing the metadata, would not see.
func ResourceMetaOf(meta map[string]interface{}) (ResourceMeta, bool, error) {
	raw, ok := meta[MetaKey]
	if !ok || raw == nil {
		return ResourceMeta{}, false, nil
	}
	object, err := asObject(raw)
	if err != nil {
		return ResourceMeta{}, false, fmt.Errorf("invalid _meta.ui: %w", err)
	}
	var result ResourceMeta
	if value, present := object["csp"]; present && value != nil {
		cspObject, err := asObject(value)
		if err != nil {
			return ResourceMeta{}, false, fmt.Errorf("invalid _meta.ui.csp: %w", err)
		}
		csp := &CSP{}
		for key, target := range map[string]*[]string{
			"connectDomains": &csp.ConnectDomains, "resourceDomains": &csp.ResourceDomains,
			"frameDomains": &csp.FrameDomains, "baseUriDomains": &csp.BaseURIDomains,
		} {
			if value, present := cspObject[key]; present && value != nil {
				if *target, err = stringList(value); err != nil {
					return ResourceMeta{}, false, fmt.Errorf("invalid _meta.ui.csp.%s: %w", key, err)
				}
			}
		}
		result.CSP = csp
	}
	if value, present := object["permissions"]; present && value != nil {
		permissionsObject, err := asObject(value)
		if err != nil {
			return ResourceMeta{}, false, fmt.Errorf("invalid _meta.ui.permissions: %w", err)
		}
		permissions := &Permissions{}
		for key, target := range map[string]**Permission{
			"camera": &permissions.Camera, "microphone": &permissions.Microphone,
			"geolocation": &permissions.Geolocation, "clipboardWrite": &permissions.ClipboardWrite,
		} {
			// The spec's value is {}; anything but absent, null or false asks.
			if value, present := permissionsObject[key]; present && value != nil && value != false {
				*target = &Permission{}
			}
		}
		result.Permissions = permissions
	}
	if result.Domain, err = optionalString(object, "domain"); err != nil {
		return ResourceMeta{}, false, fmt.Errorf("invalid _meta.ui: %w", err)
	}
	if value, present := object["prefersBorder"]; present && value != nil {
		border, ok := value.(bool)
		if !ok {
			return ResourceMeta{}, false, fmt.Errorf("invalid _meta.ui.prefersBorder: not a boolean")
		}
		result.PrefersBorder = &border
	}
	return result, true, nil
}

// asObject gives metadata its decoded JSON form (a struct a Go caller stored
// becomes the map a client would receive) and requires an object.
func asObject(value interface{}) (map[string]interface{}, error) {
	if object, ok := value.(map[string]interface{}); ok {
		return object, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var object map[string]interface{}
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, fmt.Errorf("not an object")
	}
	return object, nil
}

func optionalString(object map[string]interface{}, key string) (string, error) {
	value, present := object[key]
	if !present || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s is not a string", key)
	}
	return text, nil
}

func stringList(value interface{}) ([]string, error) {
	switch list := value.(type) {
	case []string:
		return append([]string{}, list...), nil
	case []interface{}:
		result := make([]string, 0, len(list))
		for _, item := range list {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("not a list of strings")
			}
			result = append(result, text)
		}
		return result, nil
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		var items []string
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, fmt.Errorf("not a list of strings")
		}
		return items, nil
	}
}

// WithResourceMeta returns a copy of a _meta object carrying meta as its ui
// key. A zero meta removes the key.
func WithResourceMeta(existing map[string]interface{}, meta ResourceMeta) (map[string]interface{}, error) {
	result := make(map[string]interface{}, len(existing)+1)
	for key, v := range existing {
		result[key] = v
	}
	delete(result, MetaKey)
	if meta.IsZero() {
		if len(result) == 0 {
			return nil, nil
		}
		return result, nil
	}
	value, err := toMap(meta)
	if err != nil {
		return nil, err
	}
	result[MetaKey] = value
	return result, nil
}

// ClientSettings is the extension settings a client declares.
func ClientSettings() map[string]interface{} {
	return map[string]interface{}{"mimeTypes": []interface{}{MimeType}}
}

// ClientSupports reports whether client capabilities declare MCP Apps with
// the HTML content type.
func ClientSupports(capabilities *schema.ClientCapabilities) bool {
	if capabilities == nil {
		return false
	}
	settings, ok := capabilities.Extensions[ExtensionID]
	if !ok {
		return false
	}
	types, _ := settings["mimeTypes"].([]interface{})
	for _, t := range types {
		if t == MimeType {
			return true
		}
	}
	if typed, ok := settings["mimeTypes"].([]string); ok {
		for _, t := range typed {
			if t == MimeType {
				return true
			}
		}
	}
	return false
}

// toMap gives metadata the plain map form it has after a JSON round trip, so
// a registry hands out the same shape a client decodes.
func toMap(value interface{}) (map[string]interface{}, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	err = decoder.Decode(&result)
	return result, err
}

// joinSources writes validated sources after the given leading tokens,
// skipping (and reporting) any entry that is not a valid source.
func joinSources(lead []string, sources []string, rejected *[]string) string {
	parts := append([]string(nil), lead...)
	for _, s := range sources {
		if ValidSource(s) {
			parts = append(parts, s)
		} else {
			*rejected = append(*rejected, s)
		}
	}
	return strings.Join(parts, " ")
}
