package apps

import (
	"encoding/json"
	"testing"

	"github.com/eberle1080/mcp-protocol/schema"
	"github.com/stretchr/testify/require"
)

func TestToolMetaVisibility(t *testing.T) {
	require.True(t, ToolMeta{}.Allows(VisibilityModel))
	require.True(t, ToolMeta{}.Allows(VisibilityApp))
	appOnly := ToolMeta{Visibility: []Visibility{VisibilityApp}}
	require.False(t, appOnly.Allows(VisibilityModel))
	require.True(t, appOnly.Allows(VisibilityApp))

	require.NoError(t, ToolMeta{ResourceURI: "ui://weather/dashboard", Visibility: []Visibility{VisibilityModel, VisibilityApp}}.Validate())
	require.Error(t, ToolMeta{Visibility: []Visibility{"user"}}.Validate())
	require.Error(t, ToolMeta{ResourceURI: "https://example.com/view.html"}.Validate())
	require.Error(t, ToolMeta{ResourceURI: "ui:///no-host"}.Validate())
}

func TestSetToolMetaWritesBothKeysAndReadsEither(t *testing.T) {
	tool := schema.Tool{Name: "get_weather", Meta: map[string]interface{}{"com.example/owner": "weather-team"}}
	linked, err := SetToolMeta(tool, ToolMeta{ResourceURI: "ui://weather/dashboard", Visibility: []Visibility{VisibilityModel, VisibilityApp}})
	require.NoError(t, err)
	require.Nil(t, tool.Meta["ui"], "the original is not changed")
	require.Equal(t, "weather-team", linked.Meta["com.example/owner"])
	require.Equal(t, "ui://weather/dashboard", linked.Meta[LegacyResourceURIKey])

	raw, err := json.Marshal(linked)
	require.NoError(t, err)
	require.JSONEq(t, `{"resourceUri":"ui://weather/dashboard","visibility":["model","app"]}`, string(mustJSON(t, linked.Meta["ui"])))
	var decoded schema.Tool
	require.NoError(t, json.Unmarshal(raw, &decoded))
	meta, ok := ToolMetaOf(decoded)
	require.True(t, ok)
	require.Equal(t, "ui://weather/dashboard", meta.ResourceURI)
	require.True(t, VisibleToModel(decoded))
	require.True(t, CallableByApp(decoded))

	legacy := schema.Tool{Name: "old", Meta: map[string]interface{}{LegacyResourceURIKey: "ui://old/view"}}
	meta, ok = ToolMetaOf(legacy)
	require.True(t, ok)
	require.Equal(t, "ui://old/view", meta.ResourceURI)

	_, ok = ToolMetaOf(schema.Tool{Name: "plain"})
	require.False(t, ok)
	require.True(t, VisibleToModel(schema.Tool{Name: "plain"}))

	appOnly, err := SetToolMeta(schema.Tool{Name: "refresh"}, ToolMeta{ResourceURI: "ui://weather/dashboard", Visibility: []Visibility{VisibilityApp}})
	require.NoError(t, err)
	require.False(t, VisibleToModel(appOnly))
	require.True(t, CallableByApp(appOnly))

	modelOnly, err := SetToolMeta(schema.Tool{Name: "explain"}, ToolMeta{Visibility: []Visibility{VisibilityModel}})
	require.NoError(t, err)
	require.NotContains(t, modelOnly.Meta, LegacyResourceURIKey)
	require.False(t, CallableByApp(modelOnly))

	_, err = SetToolMeta(tool, ToolMeta{ResourceURI: "file:///etc/passwd"})
	require.Error(t, err)
}

func mustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return raw
}

func TestValidSourceRefusesPolicyInjection(t *testing.T) {
	for _, ok := range []string{
		"https://api.openweathermap.org",
		"wss://realtime.service.com",
		"https://*.cloudflare.com",
		"http://localhost:8080",
		"https://cdn.example.com/assets/",
		"https://example.com:*",
	} {
		require.True(t, ValidSource(ok), ok)
	}
	for _, bad := range []string{
		"https://a.example.com; script-src *",
		"https://a.example.com 'unsafe-eval'",
		"*",
		"'self'",
		"data:",
		"javascript:alert(1)",
		"https://",
		"https://a.example.com,https://b.example.com",
		"ftp://files.example.com",
		"https://a.example.com/\"",
		"example.com",
		"",
	} {
		require.False(t, ValidSource(bad), bad)
	}
	require.Error(t, ResourceMeta{CSP: &CSP{ConnectDomains: []string{"https://a.example.com; default-src *"}}}.Validate())
	require.NoError(t, ResourceMeta{CSP: &CSP{ConnectDomains: []string{"https://a.example.com"}}}.Validate())
}

func TestContentSecurityPolicy(t *testing.T) {
	policy, rejected := ResourceMeta{}.ContentSecurityPolicy()
	require.Empty(t, rejected)
	require.Equal(t, "default-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; media-src 'self' data:; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'self'", policy)

	meta := ResourceMeta{CSP: &CSP{
		ConnectDomains:  []string{"https://api.openweathermap.org"},
		ResourceDomains: []string{"https://cdn.jsdelivr.net", "https://evil.example; script-src *"},
	}}
	policy, rejected = meta.ContentSecurityPolicy()
	require.Equal(t, []string{"https://evil.example; script-src *"}, rejected, "each bad origin is reported once")
	require.Equal(t, "default-src 'none'; "+
		"script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; "+
		"style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; "+
		"connect-src 'self' https://api.openweathermap.org; "+
		"img-src 'self' data: https://cdn.jsdelivr.net; "+
		"font-src 'self' https://cdn.jsdelivr.net; "+
		"media-src 'self' data: https://cdn.jsdelivr.net; "+
		"frame-src 'none'; object-src 'none'; base-uri 'self'", policy)

	policy, _ = ResourceMeta{CSP: &CSP{FrameDomains: []string{"https://www.youtube.com"}, BaseURIDomains: []string{"https://cdn.example.com"}}}.ContentSecurityPolicy()
	require.Contains(t, policy, "frame-src https://www.youtube.com;")
	require.Contains(t, policy, "base-uri https://cdn.example.com")
	require.Contains(t, policy, "connect-src 'self';")
}

func TestAllowAttribute(t *testing.T) {
	require.Equal(t, "", ResourceMeta{}.AllowAttribute())
	require.Equal(t, "camera; clipboard-write", ResourceMeta{Permissions: &Permissions{Camera: &Permission{}, ClipboardWrite: &Permission{}}}.AllowAttribute())

	var decoded ResourceMeta
	require.NoError(t, json.Unmarshal([]byte(`{"permissions":{"microphone":{},"geolocation":{}},"prefersBorder":false}`), &decoded))
	require.Equal(t, "microphone; geolocation", decoded.AllowAttribute())
	require.False(t, *decoded.PrefersBorder)
}

func TestResourceMetaRoundTrip(t *testing.T) {
	border := true
	meta := ResourceMeta{CSP: &CSP{ConnectDomains: []string{"https://api.example.com"}}, Domain: "a904794854a047f6.claudemcpcontent.com", PrefersBorder: &border}
	withMeta, err := WithResourceMeta(map[string]interface{}{"other": 1}, meta)
	require.NoError(t, err)
	require.Equal(t, 1, withMeta["other"])
	read, found, err := ResourceMetaOf(withMeta)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, meta, read)

	cleared, err := WithResourceMeta(withMeta, ResourceMeta{})
	require.NoError(t, err)
	require.NotContains(t, cleared, MetaKey)
	empty, err := WithResourceMeta(nil, ResourceMeta{})
	require.NoError(t, err)
	require.Nil(t, empty)

	_, found, err = ResourceMetaOf(nil)
	require.NoError(t, err)
	require.False(t, found)
	_, _, err = ResourceMetaOf(map[string]interface{}{"ui": "not an object"})
	require.Error(t, err)
}

func TestClientSupports(t *testing.T) {
	require.False(t, ClientSupports(nil))
	require.False(t, ClientSupports(&schema.ClientCapabilities{}))

	declared := &schema.ClientCapabilities{Extensions: map[string]map[string]interface{}{ExtensionID: ClientSettings()}}
	require.True(t, ClientSupports(declared))
	var decoded schema.ClientCapabilities
	require.NoError(t, json.Unmarshal(mustJSON(t, declared), &decoded))
	require.True(t, ClientSupports(&decoded))

	other := &schema.ClientCapabilities{Extensions: map[string]map[string]interface{}{ExtensionID: {"mimeTypes": []interface{}{"text/uri-list"}}}}
	require.False(t, ClientSupports(other))
	typed := &schema.ClientCapabilities{Extensions: map[string]map[string]interface{}{ExtensionID: {"mimeTypes": []string{MimeType}}}}
	require.True(t, ClientSupports(typed))
}

func TestMetadataIsReadAsTheSpecSpellsIt(t *testing.T) {
	// Go's JSON decoding would match these keys case-insensitively; a host must
	// not act on origins other hosts and auditors would not see.
	meta, found, err := ResourceMetaOf(map[string]interface{}{"ui": map[string]interface{}{"CSP": map[string]interface{}{"connectDomains": []interface{}{"https://evil.example"}}}})
	require.NoError(t, err)
	require.True(t, found)
	require.Nil(t, meta.CSP)
	policy, _ := meta.ContentSecurityPolicy()
	require.Contains(t, policy, "connect-src 'none'")

	meta, _, err = ResourceMetaOf(map[string]interface{}{"ui": map[string]interface{}{"csp": map[string]interface{}{"ConnectDomains": []interface{}{"https://evil.example"}}}})
	require.NoError(t, err)
	policy, _ = meta.ContentSecurityPolicy()
	require.NotContains(t, policy, "evil")

	for name, ui := range map[string]interface{}{
		"csp not an object":      map[string]interface{}{"csp": "https://a.example"},
		"domains not strings":    map[string]interface{}{"csp": map[string]interface{}{"connectDomains": []interface{}{1}}},
		"border not a boolean":   map[string]interface{}{"prefersBorder": "yes"},
		"domain not a string":    map[string]interface{}{"domain": 7},
		"permissions not object": map[string]interface{}{"permissions": []interface{}{"camera"}},
	} {
		_, _, err := ResourceMetaOf(map[string]interface{}{"ui": ui})
		require.Error(t, err, name)
	}

	meta, _, err = ResourceMetaOf(map[string]interface{}{"ui": map[string]interface{}{"permissions": map[string]interface{}{"camera": true, "microphone": false, "geolocation": nil, "clipboardWrite": map[string]interface{}{}}}})
	require.NoError(t, err)
	require.Equal(t, "camera; clipboard-write", meta.AllowAttribute())

	// A Go caller may store the typed value directly.
	border := true
	meta, found, err = ResourceMetaOf(map[string]interface{}{"ui": ResourceMeta{PrefersBorder: &border}})
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, *meta.PrefersBorder)
}

func TestMalformedToolVisibilityFailsClosed(t *testing.T) {
	for name, ui := range map[string]interface{}{
		"a string":        map[string]interface{}{"visibility": "app"},
		"a non-string":    map[string]interface{}{"visibility": []interface{}{"app", 1}},
		"not an object":   "ui://weather/dashboard",
		"uri not string":  map[string]interface{}{"resourceUri": 5},
		"explicitly none": map[string]interface{}{"visibility": []interface{}{}},
	} {
		tool := schema.Tool{Name: "t", Meta: map[string]interface{}{"ui": ui}}
		require.False(t, VisibleToModel(tool), name)
		require.False(t, CallableByApp(tool), name)
	}
	_, _, err := ParseToolMeta(schema.Tool{Name: "t", Meta: map[string]interface{}{"ui": map[string]interface{}{"visibility": "app"}}})
	require.Error(t, err)

	// Keys are exact here too.
	tool := schema.Tool{Name: "t", Meta: map[string]interface{}{"ui": map[string]interface{}{"Visibility": []interface{}{"app"}}}}
	require.True(t, VisibleToModel(tool), "an unknown key is ignored, so the default applies")

	typed := schema.Tool{Name: "t", Meta: map[string]interface{}{"ui": ToolMeta{Visibility: []Visibility{VisibilityApp}}}}
	require.False(t, VisibleToModel(typed))
	require.True(t, CallableByApp(typed))

	_, err = SetToolMeta(schema.Tool{Name: "t"}, ToolMeta{Visibility: []Visibility{}})
	require.Error(t, err, "an empty visibility would hide the tool from everyone")
}
