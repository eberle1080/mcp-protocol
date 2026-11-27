package server

import (
	"context"
	"testing"
	"testing/fstest"
	"time"

	"github.com/eberle1080/jsonrpc"
	skillformat "github.com/eberle1080/mcp-protocol/extension/skills"
	"github.com/eberle1080/mcp-protocol/schema"
	"github.com/stretchr/testify/require"
)

func readDirectory(d *DefaultHandler, uri string, cursor *string) (*schema.ReadResourceDirectoryResult, *jsonrpc.Error) {
	return d.ReadResourceDirectory(context.Background(), &jsonrpc.TypedRequest[*schema.ReadResourceDirectoryRequest]{
		Request: &schema.ReadResourceDirectoryRequest{Method: schema.MethodResourcesDirectoryRead, Params: schema.ReadResourceDirectoryRequestParams{Uri: uri, Cursor: cursor}},
	})
}

func childSummary(resources []schema.Resource) map[string]string {
	result := map[string]string{}
	for _, resource := range resources {
		kind := ""
		if resource.MimeType != nil {
			kind = *resource.MimeType
		}
		result[resource.Uri] = resource.Name + " " + kind
	}
	return result
}

func pdfSkillHandler(t *testing.T) *DefaultHandler {
	t.Helper()
	compiled, err := (skillformat.Compiler{Source: fstest.MapFS{
		"SKILL.md":                    {Data: []byte("---\nname: pdf-processing\ndescription: Extract, fill, and assemble PDF documents\n---\nPick a template from `templates/`.\n")},
		"templates/invoice.md":        {Data: []byte("# Invoice\n")},
		"templates/purchase-order.md": {Data: []byte("# Purchase order\n")},
		"templates/regional/eu.md":    {Data: []byte("# EU\n")},
		"scripts/fill.py":             {Data: []byte("print('fill')\n")},
	}}).Compile(context.Background(), "skill://pdf-processing/SKILL.md")
	require.NoError(t, err)
	d := NewDefaultHandler(nil, nil, nil)
	require.NoError(t, d.RegisterStaticSkill(compiled))
	return d
}

func TestDirectoryReadListsDirectChildren(t *testing.T) {
	d := pdfSkillHandler(t)

	root, rpcErr := readDirectory(d, "skill://pdf-processing", nil)
	require.Nil(t, rpcErr)
	require.Equal(t, schema.ResultTypeComplete, root.ResultType)
	require.Equal(t, map[string]string{
		"skill://pdf-processing/SKILL.md":  "pdf-processing text/markdown",
		"skill://pdf-processing/scripts":   "scripts inode/directory",
		"skill://pdf-processing/templates": "templates inode/directory",
	}, childSummary(root.Resources))
	require.Equal(t, "skill://pdf-processing/SKILL.md", root.Resources[0].Uri, "sorted by URI")

	templates, rpcErr := readDirectory(d, "skill://pdf-processing/templates", nil)
	require.Nil(t, rpcErr)
	require.Equal(t, map[string]string{
		"skill://pdf-processing/templates/invoice.md":        "skill://pdf-processing/templates/invoice.md text/markdown",
		"skill://pdf-processing/templates/purchase-order.md": "skill://pdf-processing/templates/purchase-order.md text/markdown",
		"skill://pdf-processing/templates/regional":          "regional inode/directory",
	}, childSummary(templates.Resources))
	for _, child := range templates.Resources {
		if *child.MimeType != schema.DirectoryMimeType {
			require.NotNil(t, child.Size, "files keep their ordinary metadata")
		}
	}

	regional, rpcErr := readDirectory(d, "skill://pdf-processing/templates/regional", nil)
	require.Nil(t, rpcErr)
	require.Len(t, regional.Resources, 1)

	// The listing is a copy; changing it does not change the registry.
	*templates.Resources[0].MimeType = "changed"
	again, _ := readDirectory(d, "skill://pdf-processing/templates", nil)
	require.Equal(t, "text/markdown", *again.Resources[0].MimeType)
}

func TestDirectoryReadRefusesWhatIsNotADirectory(t *testing.T) {
	d := pdfSkillHandler(t)
	cursor := "anything"
	for name, call := range map[string]func() (*schema.ReadResourceDirectoryResult, *jsonrpc.Error){
		"file": func() (*schema.ReadResourceDirectoryResult, *jsonrpc.Error) {
			return readDirectory(d, "skill://pdf-processing/SKILL.md", nil)
		},
		"missing": func() (*schema.ReadResourceDirectoryResult, *jsonrpc.Error) {
			return readDirectory(d, "skill://pdf-processing/nothing", nil)
		},
		"outside skills": func() (*schema.ReadResourceDirectoryResult, *jsonrpc.Error) {
			return readDirectory(d, "skill://other", nil)
		},
		"trailing slash": func() (*schema.ReadResourceDirectoryResult, *jsonrpc.Error) {
			return readDirectory(d, "skill://pdf-processing/templates/", nil)
		},
		"empty URI": func() (*schema.ReadResourceDirectoryResult, *jsonrpc.Error) { return readDirectory(d, "", nil) },
		"cursor": func() (*schema.ReadResourceDirectoryResult, *jsonrpc.Error) {
			return readDirectory(d, "skill://pdf-processing", &cursor)
		},
		"nil request": func() (*schema.ReadResourceDirectoryResult, *jsonrpc.Error) {
			return d.ReadResourceDirectory(context.Background(), nil)
		},
	} {
		result, rpcErr := call()
		require.Nil(t, result, name)
		require.NotNil(t, rpcErr, name)
		require.EqualValues(t, jsonrpc.InvalidParams, rpcErr.Code, name)
	}
	_, rpcErr := readDirectory(d, "skill://pdf-processing/SKILL.md", nil)
	require.Equal(t, "skill://pdf-processing/SKILL.md is not a directory resource", rpcErr.Message)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, rpcErr = d.ReadResourceDirectory(ctx, &jsonrpc.TypedRequest[*schema.ReadResourceDirectoryRequest]{Request: &schema.ReadResourceDirectoryRequest{Params: schema.ReadResourceDirectoryRequestParams{Uri: "skill://pdf-processing"}}})
	require.EqualValues(t, jsonrpc.InternalError, rpcErr.Code)
}

func TestDirectoryReadIsDeclaredWithSkills(t *testing.T) {
	none := NewDefaultHandler(nil, nil, nil)
	require.False(t, none.Implements(schema.MethodResourcesDirectoryRead))
	discovered := &schema.DiscoverResult{}
	none.Discover(context.Background(), discovered)
	require.NotContains(t, discovered.Capabilities.Extensions, schema.SkillsExtension)

	d := pdfSkillHandler(t)
	require.True(t, d.Implements(schema.MethodResourcesDirectoryRead))
	discovered = &schema.DiscoverResult{}
	d.Discover(context.Background(), discovered)
	require.Equal(t, map[string]interface{}{schema.SkillsDirectoryRead: true}, discovered.Capabilities.Extensions[schema.SkillsExtension])
	require.NotNil(t, discovered.Capabilities.Resources)

	off := false
	d.SkillsDirectoryRead = &off
	require.False(t, d.Implements(schema.MethodResourcesDirectoryRead))
	discovered = &schema.DiscoverResult{}
	d.Discover(context.Background(), discovered)
	require.Equal(t, map[string]interface{}{}, discovered.Capabilities.Extensions[schema.SkillsExtension])
	require.True(t, d.Implements(schema.MethodSkillsGet))
}

func TestDirectoryReadCarriesTheSkillsCacheHints(t *testing.T) {
	d := pdfSkillHandler(t)
	result, rpcErr := readDirectory(d, "skill://pdf-processing", nil)
	require.Nil(t, rpcErr)
	require.Nil(t, result.TtlMs)
	require.Empty(t, result.CacheScope)

	d.SkillsCacheTTL = time.Hour
	d.SkillsCacheScope = schema.CacheableResultCacheScopePublic
	result, rpcErr = readDirectory(d, "skill://pdf-processing", nil)
	require.Nil(t, rpcErr)
	require.Equal(t, 3600000, *result.TtlMs)
	require.Equal(t, schema.CacheableResultCacheScopePublic, result.CacheScope)
}

func TestDirectoryReadListsADynamicSkillsRegisteredFiles(t *testing.T) {
	d := NewDefaultHandler(nil, nil, nil)
	reader := func(context.Context, *schema.ReadResourceRequest) (*schema.ReadResourceResult, *jsonrpc.Error) {
		return &schema.ReadResourceResult{}, nil
	}
	for _, uri := range []string{"skill://reports/daily/SKILL.md", "skill://reports/daily/sections/today.md", "skill://reports/other.md"} {
		require.NoError(t, d.RegisterResource(schema.Resource{Uri: uri, Name: uri}, reader))
	}
	require.NoError(t, d.RegisterSkill(schema.Skill{Uri: "skill://reports/daily/SKILL.md", Frontmatter: map[string]interface{}{"name": "daily", "description": "Today's report."}, Resources: schema.SkillResources{Dynamic: true}}))

	// Off by default: only the server's author knows that every file of a
	// generated skill is a registered resource.
	require.False(t, d.Implements(schema.MethodResourcesDirectoryRead))
	discovered := &schema.DiscoverResult{}
	d.Discover(context.Background(), discovered)
	require.Equal(t, map[string]interface{}{}, discovered.Capabilities.Extensions[schema.SkillsExtension])

	on := true
	d.SkillsDirectoryRead = &on
	require.True(t, d.Implements(schema.MethodResourcesDirectoryRead))

	root, rpcErr := readDirectory(d, "skill://reports/daily", nil)
	require.Nil(t, rpcErr)
	require.Equal(t, map[string]string{
		"skill://reports/daily/SKILL.md": "skill://reports/daily/SKILL.md ",
		"skill://reports/daily/sections": "sections inode/directory",
	}, childSummary(root.Resources))

	// skill://reports holds a skill but is not inside one.
	_, rpcErr = readDirectory(d, "skill://reports", nil)
	require.NotNil(t, rpcErr)
}

func TestADynamicSkillTurnsTheDefaultOff(t *testing.T) {
	d := pdfSkillHandler(t)
	require.True(t, d.Implements(schema.MethodResourcesDirectoryRead))
	require.NoError(t, d.RegisterResource(schema.Resource{Uri: "skill://live/SKILL.md", Name: "live"}, func(context.Context, *schema.ReadResourceRequest) (*schema.ReadResourceResult, *jsonrpc.Error) {
		return &schema.ReadResourceResult{}, nil
	}))
	require.NoError(t, d.RegisterSkill(schema.Skill{Uri: "skill://live/SKILL.md", Frontmatter: map[string]interface{}{"name": "live", "description": "Live."}, Resources: schema.SkillResources{Dynamic: true}}))
	require.False(t, d.Implements(schema.MethodResourcesDirectoryRead))
}

func TestSkillEntryMetaReachesTheWire(t *testing.T) {
	compiled, err := (skillformat.Compiler{
		Source: fstest.MapFS{"SKILL.md": {Data: []byte("---\nname: demo\ndescription: A demo.\n---\n")}},
		Meta:   map[string]interface{}{"com.example/owner": "team-a"},
	}).Compile(context.Background(), "skill://demo/SKILL.md")
	require.NoError(t, err)
	d := NewDefaultHandler(nil, nil, nil)
	require.NoError(t, d.RegisterStaticSkill(compiled))

	listed, rpcErr := d.ListSkills(context.Background(), nil)
	require.Nil(t, rpcErr)
	require.Equal(t, "team-a", listed.Skills[0].Meta["com.example/owner"])
	got, rpcErr := d.GetSkill(context.Background(), &jsonrpc.TypedRequest[*schema.GetSkillRequest]{Request: &schema.GetSkillRequest{Params: schema.GetSkillRequestParams{Uri: "skill://demo/SKILL.md"}}})
	require.Nil(t, rpcErr)
	require.Equal(t, "team-a", got.Skill.Meta["com.example/owner"])

	dynamic := NewDefaultHandler(nil, nil, nil)
	require.NoError(t, dynamic.RegisterResource(schema.Resource{Uri: "skill://live/SKILL.md", Name: "live"}, func(context.Context, *schema.ReadResourceRequest) (*schema.ReadResourceResult, *jsonrpc.Error) {
		return &schema.ReadResourceResult{}, nil
	}))
	entry := schema.Skill{Uri: "skill://live/SKILL.md", Frontmatter: map[string]interface{}{"name": "live", "description": "Live."}, Resources: schema.SkillResources{Dynamic: true}, Meta: map[string]interface{}{"com.example/source": "feed"}}
	require.NoError(t, dynamic.RegisterSkill(entry))
	entry.Meta["com.example/source"] = "changed by caller"
	got, rpcErr = dynamic.GetSkill(context.Background(), &jsonrpc.TypedRequest[*schema.GetSkillRequest]{Request: &schema.GetSkillRequest{Params: schema.GetSkillRequestParams{Uri: "skill://live/SKILL.md"}}})
	require.Nil(t, rpcErr)
	require.Equal(t, "feed", got.Skill.Meta["com.example/source"])
}
