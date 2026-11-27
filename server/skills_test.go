package server

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"github.com/eberle1080/jsonrpc"
	skillformat "github.com/eberle1080/mcp-protocol/extension/skills"
	"github.com/eberle1080/mcp-protocol/schema"
	"testing"
	"time"
	"testing/fstest"
)

func TestSkillsRegistrySnapshotsAndStaleCursor(t *testing.T) {
	ctx := context.Background()
	d := NewDefaultHandler(nil, nil, nil)
	add := func(index int) {
		root := fmt.Sprintf("skill://team%d/demo", index)
		files := fstest.MapFS{"SKILL.md": {Data: []byte("---\nname: demo\ndescription: demo\n---\n")}}
		entry, err := (skillformat.Compiler{Source: files}).Compile(ctx, root+"/SKILL.md")
		require.NoError(t, err)
		require.Error(t, d.RegisterSkill(entry.Metadata()), "unproven manifest accepted")
		require.NoError(t, d.RegisterStaticSkill(entry))
		require.Error(t, d.RegisterStaticSkill(entry))
		entry.Metadata().Frontmatter["description"] = "mutated caller"
	}
	for i := 0; i < 33; i++ {
		add(i)
	}
	page, rpc := d.ListSkills(ctx, nil)
	require.Nil(t, rpc)
	require.Len(t, page.Skills, 32)
	require.Equal(t, "demo", page.Skills[0].Frontmatter["description"])
	add(33)
	_, rpc = d.ListSkills(ctx, &jsonrpc.TypedRequest[*schema.ListSkillsRequest]{Request: &schema.ListSkillsRequest{Params: schema.ListSkillsRequestParams{Cursor: page.NextCursor}}})
	require.NotNil(t, rpc)
	require.EqualValues(t, -32602, rpc.Code)
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	_, rpc = d.ListSkills(ctx, nil)
	require.NotNil(t, rpc)
}

func TestSkillsDynamicEntrypointRequiresReader(t *testing.T) {
	registry := NewRegistry()
	entry := schema.Skill{Uri: "skill://demo/SKILL.md", Frontmatter: map[string]interface{}{"name": "demo", "description": "demo"}, Resources: schema.SkillResources{Dynamic: true}}
	registry.RegisterResource(schema.Resource{Name: "demo", Uri: entry.Uri}, nil)
	require.Error(t, registry.RegisterSkill(entry))
	require.False(t, registry.ImplementsSkills())
	registry.RegisterResource(schema.Resource{Name: "demo", Uri: entry.Uri}, func(context.Context, *schema.ReadResourceRequest) (*schema.ReadResourceResult, *jsonrpc.Error) {
		return nil, nil
	})
	require.NoError(t, registry.RegisterSkill(entry))
	require.True(t, registry.ImplementsSkills())
}

func TestSkillsCarryConfiguredCacheHints(t *testing.T) {
	ctx := context.Background()
	files := fstest.MapFS{"SKILL.md": {Data: []byte("---\nname: demo\ndescription: A demo skill.\n---\nBody")}}
	compiled, err := (skillformat.Compiler{Source: files}).Compile(ctx, "skill://demo/SKILL.md")
	require.NoError(t, err)

	unset := NewDefaultHandler(nil, nil, nil)
	require.NoError(t, unset.RegisterStaticSkill(compiled))
	listed, rpcErr := unset.ListSkills(ctx, &jsonrpc.TypedRequest[*schema.ListSkillsRequest]{Request: &schema.ListSkillsRequest{}})
	require.Nil(t, rpcErr)
	require.Nil(t, listed.TtlMs, "unset hints are left to the transport's defaults")
	require.Empty(t, listed.CacheScope)

	configured := NewDefaultHandler(nil, nil, nil)
	configured.SkillsCacheTTL = time.Hour
	configured.SkillsCacheScope = schema.CacheableResultCacheScopePublic
	require.NoError(t, configured.RegisterStaticSkill(compiled))
	listed, rpcErr = configured.ListSkills(ctx, &jsonrpc.TypedRequest[*schema.ListSkillsRequest]{Request: &schema.ListSkillsRequest{}})
	require.Nil(t, rpcErr)
	require.Equal(t, 3600000, *listed.TtlMs)
	require.Equal(t, schema.CacheableResultCacheScopePublic, listed.CacheScope)
	got, rpcErr := configured.GetSkill(ctx, &jsonrpc.TypedRequest[*schema.GetSkillRequest]{Request: &schema.GetSkillRequest{Params: schema.GetSkillRequestParams{Uri: "skill://demo/SKILL.md"}}})
	require.Nil(t, rpcErr)
	require.Equal(t, 3600000, *got.TtlMs)
	require.Equal(t, schema.CacheableResultCacheScopePublic, got.CacheScope)
}

func TestStaticSkillEntrypointResourceTakesSkillName(t *testing.T) {
	files := fstest.MapFS{
		"SKILL.md":         {Data: []byte("---\nname: demo\ndescription: A demo skill.\n---\nBody")},
		"reference/one.md": {Data: []byte("# One")},
	}
	compiled, err := (skillformat.Compiler{Source: files}).Compile(context.Background(), "skill://demo/SKILL.md")
	require.NoError(t, err)
	names := map[string]string{}
	for _, resource := range compiled.Resources() {
		names[resource.Uri] = resource.Name
	}
	require.Equal(t, "demo", names["skill://demo/SKILL.md"])
	require.Equal(t, "skill://demo/reference/one.md", names["skill://demo/reference/one.md"])
}
