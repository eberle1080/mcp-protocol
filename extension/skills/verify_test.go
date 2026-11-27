package skills

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/eberle1080/mcp-protocol/schema"
	"github.com/stretchr/testify/require"
)

// The supporting files and digests from SEP-2640's example message flow.
const (
	specInvoice        = "# Invoice\n\nCustomer:\nAmount:\n"
	specInvoiceDigest  = "sha256:61f4ea6d2c75fde1b4977219e7e3107d491c3c26aefb6686e84d6281c088d9ee"
	specPurchase       = "# Purchase order\n\nSupplier:\nItems:\n"
	specPurchaseDigest = "sha256:f2ff774b1737ff3dec81c47946f9976f18a1a9f69dda0a81f22eabd95173c158"
	specSkillMD        = "---\nname: pdf-processing\ndescription: Extract, fill, and assemble PDF documents\n---\n\n# PDF processing\n\nChoose the matching template from `templates/`.\n"
	specSkillMDDigest  = "sha256:99b737495721155ece826d57521e2d66141ebdc1344a400487481ea2642ab19e"
)

func specSkill(t *testing.T) *Static {
	t.Helper()
	compiled, err := (Compiler{Source: fstest.MapFS{
		"SKILL.md":                    {Data: []byte(specSkillMD)},
		"templates/invoice.md":        {Data: []byte(specInvoice)},
		"templates/purchase-order.md": {Data: []byte(specPurchase)},
	}}).Compile(context.Background(), "skill://pdf-processing/SKILL.md")
	require.NoError(t, err)
	return compiled
}

func readBytes(t *testing.T, compiled *Static, uri string) []byte {
	t.Helper()
	result, rpcErr := compiled.ReadResource(context.Background(), &schema.ReadResourceRequest{Params: schema.ReadResourceRequestParams{Uri: uri}})
	require.Nil(t, rpcErr)
	data, err := ContentBytes(uri, result)
	require.NoError(t, err)
	return data
}

func TestCompilerMatchesTheSpecDigests(t *testing.T) {
	entry := specSkill(t).Metadata()
	want := map[string]schema.SkillResource{
		"skill://pdf-processing/SKILL.md":                    {Uri: "skill://pdf-processing/SKILL.md", Digest: specSkillMDDigest, Size: 151},
		"skill://pdf-processing/templates/invoice.md":        {Uri: "skill://pdf-processing/templates/invoice.md", Digest: specInvoiceDigest, Size: 29},
		"skill://pdf-processing/templates/purchase-order.md": {Uri: "skill://pdf-processing/templates/purchase-order.md", Digest: specPurchaseDigest, Size: 35},
	}
	require.Len(t, entry.Resources.Files, len(want))
	for _, file := range entry.Resources.Files {
		require.Equal(t, want[file.Uri], file)
	}
}

func TestVerifyFileAcceptsTheBytesTheEntryDescribes(t *testing.T) {
	compiled := specSkill(t)
	entry := compiled.Metadata()
	for _, file := range entry.Resources.Files {
		require.NoError(t, VerifyFile(file, readBytes(t, compiled, file.Uri)), file.Uri)
	}
	require.NoError(t, VerifyFrontmatter(entry, readBytes(t, compiled, entry.Uri)))
}

func TestVerifyFileRefusesChangedBytes(t *testing.T) {
	file := schema.SkillResource{Uri: "skill://pdf-processing/templates/invoice.md", Digest: specInvoiceDigest, Size: 29}

	err := VerifyFile(file, []byte(specInvoice+"extra"))
	require.True(t, IsVerificationError(err))
	require.Contains(t, err.Error(), "read 34 bytes, the entry says 29")

	sameLength := []byte(specInvoice)
	sameLength[2] = 'X'
	err = VerifyFile(file, sameLength)
	require.True(t, IsVerificationError(err))
	require.Contains(t, err.Error(), "digest")
}

func TestVerifyFrontmatterComparesFieldByField(t *testing.T) {
	skillMD := []byte("---\nname: demo\ndescription: A demo.\nversion: 2\nlicense: MIT\n---\nBody\n")
	parsed, err := Frontmatter(skillMD)
	require.NoError(t, err)

	// The entry a client decoded from the wire, and one built in Go with
	// plain numbers, both match the file.
	raw, err := json.Marshal(schema.Skill{Uri: "skill://demo/SKILL.md", Frontmatter: parsed, Resources: schema.SkillResources{Dynamic: true}})
	require.NoError(t, err)
	var wire schema.Skill
	require.NoError(t, json.Unmarshal(raw, &wire))
	require.NoError(t, VerifyFrontmatter(wire, skillMD))
	built := schema.Skill{Uri: "skill://demo/SKILL.md", Frontmatter: map[string]interface{}{"name": "demo", "description": "A demo.", "version": 2, "license": "MIT"}}
	require.NoError(t, VerifyFrontmatter(built, skillMD))

	for name, change := range map[string]func(map[string]interface{}){
		"description": func(f map[string]interface{}) { f["description"] = "Something else." },
		"license":     func(f map[string]interface{}) { delete(f, "license") },
		"version":     func(f map[string]interface{}) { f["version"] = 3 },
		"extra":       func(f map[string]interface{}) { f["extra"] = true },
	} {
		forged := schema.Skill{Uri: built.Uri, Frontmatter: map[string]interface{}{}}
		for key, value := range built.Frontmatter {
			forged.Frontmatter[key] = value
		}
		change(forged.Frontmatter)
		err := VerifyFrontmatter(forged, skillMD)
		require.True(t, IsVerificationError(err), name)
		require.Contains(t, err.Error(), name)
	}

	err = VerifyFrontmatter(built, []byte("no frontmatter"))
	require.True(t, IsVerificationError(err))
}

func TestContentBytes(t *testing.T) {
	uri := "skill://demo/data.bin"
	blob := schema.ReadResourceResultContentsElem{Uri: uri, Blob: base64.StdEncoding.EncodeToString([]byte{0, 1, 255})}
	data, err := ContentBytes(uri, &schema.ReadResourceResult{Contents: []schema.ReadResourceResultContentsElem{blob}})
	require.NoError(t, err)
	require.Equal(t, []byte{0, 1, 255}, data)

	text := schema.ReadResourceResultContentsElem{Uri: uri, Text: "héllo"}
	data, err = ContentBytes(uri, &schema.ReadResourceResult{Contents: []schema.ReadResourceResultContentsElem{text}})
	require.NoError(t, err)
	require.Equal(t, []byte("héllo"), data)

	for name, result := range map[string]*schema.ReadResourceResult{
		"nil":        nil,
		"empty":      {},
		"other URI":  {Contents: []schema.ReadResourceResultContentsElem{{Uri: "skill://demo/other.bin", Text: "x"}}},
		"two items":  {Contents: []schema.ReadResourceResultContentsElem{text, text}},
		"bad base64": {Contents: []schema.ReadResourceResultContentsElem{{Uri: uri, Blob: "!!"}}},
	} {
		_, err := ContentBytes(uri, result)
		require.True(t, IsVerificationError(err), name)
	}
}

func TestResolveAgainstTheSkillRoot(t *testing.T) {
	entry := schema.Skill{Uri: "skill://acme/billing/refunds/SKILL.md", Frontmatter: map[string]interface{}{"name": "refunds", "description": "Refunds."}}
	for ref, want := range map[string]string{
		"references/GUIDE.md":         "skill://acme/billing/refunds/references/GUIDE.md",
		"./references/GUIDE.md":       "skill://acme/billing/refunds/references/GUIDE.md",
		"a/../references/GUIDE.md":    "skill://acme/billing/refunds/references/GUIDE.md",
		"references/GUIDE.md#setup":   "skill://acme/billing/refunds/references/GUIDE.md",
		"my%20notes.md":               "skill://acme/billing/refunds/my%20notes.md",
		"templates":                   "skill://acme/billing/refunds/templates",
		".":                           "skill://acme/billing/refunds",
		"skill://elsewhere/SKILL.md":  "skill://elsewhere/SKILL.md",
		"https://example.com/x.md":    "https://example.com/x.md",
		"examples/email.md":           "skill://acme/billing/refunds/examples/email.md",
		"examples/../examples/x/y.md": "skill://acme/billing/refunds/examples/x/y.md",
	} {
		got, err := Resolve(entry, ref)
		require.NoError(t, err, ref)
		require.Equal(t, want, got, ref)
	}
	for _, ref := range []string{"", "../other/SKILL.md", "a/../../x", "/etc/passwd", "//host/x", "x?y=1", `a\b`} {
		_, err := Resolve(entry, ref)
		require.Error(t, err, ref)
	}

	hostOnly := schema.Skill{Uri: "skill://git-workflow/SKILL.md", Frontmatter: map[string]interface{}{"name": "git-workflow", "description": "Git."}}
	got, err := Resolve(hostOnly, "references/x.md")
	require.NoError(t, err)
	require.Equal(t, "skill://git-workflow/references/x.md", got)
	root, err := RootOf(hostOnly)
	require.NoError(t, err)
	require.True(t, Within(root, got))
	require.True(t, Within(root, root))
	require.False(t, Within(root, "skill://git-workflow-extra/SKILL.md"))
}

func TestCompilerCarriesMeta(t *testing.T) {
	meta := map[string]interface{}{"com.example/owner": "team-a", "com.example/rank": 2}
	compiled, err := (Compiler{Source: fstest.MapFS{"SKILL.md": {Data: []byte("---\nname: demo\ndescription: A demo.\n---\n")}}, Meta: meta}).Compile(context.Background(), "skill://demo/SKILL.md")
	require.NoError(t, err)
	meta["com.example/owner"] = "changed after compile"

	entry := compiled.Metadata()
	require.Equal(t, "team-a", entry.Meta["com.example/owner"])
	require.Equal(t, json.Number("2"), entry.Meta["com.example/rank"])
	entry.Meta["com.example/owner"] = "changed copy"
	require.Equal(t, "team-a", compiled.Metadata().Meta["com.example/owner"])

	raw, err := json.Marshal(entry)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"_meta":{"com.example/owner":"changed copy","com.example/rank":2}`)

	_, err = (Compiler{Source: fstest.MapFS{"SKILL.md": {Data: []byte("---\nname: demo\ndescription: A demo.\n---\n")}}, Meta: map[string]interface{}{"bad": make(chan int)}}).Compile(context.Background(), "skill://demo/SKILL.md")
	require.Error(t, err)

	plain, err := (Compiler{Source: fstest.MapFS{"SKILL.md": {Data: []byte("---\nname: demo\ndescription: A demo.\n---\n")}}}).Compile(context.Background(), "skill://demo/SKILL.md")
	require.NoError(t, err)
	raw, err = json.Marshal(plain.Metadata())
	require.NoError(t, err)
	require.NotContains(t, string(raw), "_meta")
}

func TestContentBindingFollowsTheResourcesSet(t *testing.T) {
	entry := specSkill(t).Metadata()
	binding, ok := ContentBinding(entry)
	require.True(t, ok)

	reordered := entry
	reordered.Resources.Files = append([]schema.SkillResource{}, entry.Resources.Files...)
	reordered.Resources.Files[0], reordered.Resources.Files[2] = reordered.Resources.Files[2], reordered.Resources.Files[0]
	reordered.Frontmatter = map[string]interface{}{"name": "pdf-processing", "description": "A new description changes nothing here."}
	same, ok := ContentBinding(reordered)
	require.True(t, ok)
	require.Equal(t, binding, same)

	rotated := entry
	rotated.Resources.Files = append([]schema.SkillResource{}, entry.Resources.Files...)
	rotated.Resources.Files[1].Digest = "sha256:" + strings.Repeat("0", 64)
	changed, _ := ContentBinding(rotated)
	require.NotEqual(t, binding, changed)

	removed := entry
	removed.Resources.Files = entry.Resources.Files[:2]
	changed, _ = ContentBinding(removed)
	require.NotEqual(t, binding, changed)

	_, ok = ContentBinding(schema.Skill{Uri: entry.Uri, Frontmatter: entry.Frontmatter, Resources: schema.SkillResources{Dynamic: true}})
	require.False(t, ok)
}

func TestContentItemRefusesAmbiguousItems(t *testing.T) {
	uri := "skill://demo/notes.md"
	good := base64.StdEncoding.EncodeToString([]byte("checked"))
	_, _, err := ContentItem(uri, &schema.ReadResourceResult{Contents: []schema.ReadResourceResultContentsElem{{Uri: uri, Text: "injected", Blob: good}}})
	require.True(t, IsVerificationError(err), "text and blob together: which one would the caller use?")

	item, data, err := ContentItem(uri, &schema.ReadResourceResult{Contents: []schema.ReadResourceResultContentsElem{{Uri: uri + "#x", Text: "injected"}, {Uri: uri, Blob: good}}})
	require.NoError(t, err)
	require.Equal(t, "checked", string(data))
	require.Equal(t, uri, item.Uri, "only the item for the file comes back")
}

func TestInsideCatchesRespellings(t *testing.T) {
	root := "skill://example/review"
	for _, uri := range []string{
		"skill://example/review",
		"skill://example/review/notes/new.md",
		"SKILL://example/review/notes/new.md",
		"skill://EXAMPLE/review/notes/new.md",
		"skill://example/r%65view/notes/new.md",
		"skill://example/review/../review/x.md",
		"skill://example/other/../review/x.md",
		"skill://example//review/x.md",
		"skill://example/review/x.md?y",
		"skill://example/review/x.md#y",
		"skill://example/review%2Fx.md",
		"::not a uri",
	} {
		require.True(t, Inside(root, uri), uri)
	}
	for _, uri := range []string{"skill://example/reviews/x.md", "skill://example/other/x.md", "docs://example/review/x.md", "skill://example/review/../other/x.md"} {
		require.False(t, Inside(root, uri), uri)
	}
	require.True(t, Inside("skill://git-workflow", "skill://git-workflow/"))
}

func TestCanonicalAllowsOneSpelling(t *testing.T) {
	for _, uri := range []string{"skill://git-workflow", "skill://reports/daily/SKILL.md", "skill://demo/my%20notes.md", "skill://demo/notes%23v2.md", "docs://team/demo/a:b.md"} {
		require.NoError(t, Canonical(uri), uri)
	}
	for _, uri := range []string{
		"SKILL://reports/daily/SKILL.md",
		"skill://reports/daily/../other/secret.md",
		"skill://reports/daily/./SKILL.md",
		"skill://reports/daily/%2E%2E/other/secret.md",
		"skill://reports/daily%2FSKILL.md",
		"skill://reports/daily/SKILL.md?x",
		"skill://reports/daily/SKILL.md?",
		"skill://reports/daily/SKILL.md#x",
		"skill://reports/daily/SKILL.md#",
		"skill://reports//daily/SKILL.md",
		"skill://reports/daily/",
		"skill://user@reports/daily/SKILL.md",
		"skill:reports/daily/SKILL.md",
		"skill://reports/d%61ily/SKILL.md",
	} {
		require.Error(t, Canonical(uri), uri)
	}
}

func TestVerifyFrontmatterComparesNumbersByValueAndKeysByPresence(t *testing.T) {
	skillMD := []byte("---\nname: demo\ndescription: d\nversion: 1.0\n---\nbody\n")
	for _, version := range []string{"1.0", "1", "1e0", "10e-1"} {
		var entry schema.Skill
		require.NoError(t, json.Unmarshal([]byte(`{"uri":"skill://demo/SKILL.md","frontmatter":{"name":"demo","description":"d","version":`+version+`},"resources":"dynamic"}`), &entry))
		require.NoError(t, VerifyFrontmatter(entry, skillMD), version)
	}
	var entry schema.Skill
	require.NoError(t, json.Unmarshal([]byte(`{"uri":"skill://demo/SKILL.md","frontmatter":{"name":"demo","description":"d","version":1.5},"resources":"dynamic"}`), &entry))
	require.Error(t, VerifyFrontmatter(entry, skillMD))

	withNull := []byte("---\nname: demo\ndescription: d\nextra:\nnested: {a: null}\n---\nbody\n")
	require.NoError(t, json.Unmarshal([]byte(`{"uri":"skill://demo/SKILL.md","frontmatter":{"name":"demo","description":"d","nested":{"a":null}},"resources":"dynamic"}`), &entry))
	err := VerifyFrontmatter(entry, withNull)
	require.ErrorContains(t, err, "extra", "a key present with null is not a missing key")
	require.NoError(t, json.Unmarshal([]byte(`{"uri":"skill://demo/SKILL.md","frontmatter":{"name":"demo","description":"d","extra":null,"nested":{}},"resources":"dynamic"}`), &entry))
	require.ErrorContains(t, VerifyFrontmatter(entry, withNull), "nested")
	require.NoError(t, json.Unmarshal([]byte(`{"uri":"skill://demo/SKILL.md","frontmatter":{"name":"demo","description":"d","extra":null,"nested":{"a":null}},"resources":"dynamic"}`), &entry))
	require.NoError(t, VerifyFrontmatter(entry, withNull))
}

func TestCompiledSkillsCanBeLoaded(t *testing.T) {
	for _, name := range []string{"100%.md", `a\b.md`} {
		_, err := (Compiler{Source: fstest.MapFS{
			"SKILL.md": {Data: []byte("---\nname: demo\ndescription: d\n---\n")},
			name:       {Data: []byte("x")},
		}}).Compile(context.Background(), "skill://acme/demo/SKILL.md")
		require.Error(t, err, name)
	}
	names := []string{"my notes.md", "a#b.md", "café.md", "a:b.md", "a+b.md", "a[1].md", "dir with space/x.md"}
	files := fstest.MapFS{"SKILL.md": {Data: []byte("---\nname: demo\ndescription: d\n---\n")}}
	for _, name := range names {
		files[name] = &fstest.MapFile{Data: []byte(name)}
	}
	compiled, err := (Compiler{Source: files}).Compile(context.Background(), "skill://acme/demo/SKILL.md")
	require.NoError(t, err)
	entry := compiled.Metadata()
	require.NoError(t, Validate(entry))
	for _, file := range entry.Resources.Files {
		require.NoError(t, Canonical(file.Uri), file.Uri)
	}
	// Written as a link would write them, each name resolves to its file.
	for ref, name := range map[string]string{"my%20notes.md": "my notes.md", "a%23b.md": "a#b.md", "caf%C3%A9.md": "café.md", "./a:b.md": "a:b.md", "a+b.md": "a+b.md", "a[1].md": "a[1].md", "dir%20with%20space/x.md": "dir with space/x.md", "my notes.md": "my notes.md"} {
		uri, err := Resolve(entry, ref)
		require.NoError(t, err, ref)
		_, listed := File(entry, uri)
		require.True(t, listed, "%s resolved to %s", ref, uri)
		data, err := ContentBytes(uri, mustRead(t, compiled, uri))
		require.NoError(t, err)
		require.Equal(t, name, string(data))
	}
}

func mustRead(t *testing.T, compiled *Static, uri string) *schema.ReadResourceResult {
	t.Helper()
	result, rpcErr := compiled.ReadResource(context.Background(), &schema.ReadResourceRequest{Params: schema.ReadResourceRequestParams{Uri: uri}})
	require.Nil(t, rpcErr)
	return result
}
