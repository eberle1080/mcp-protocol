package skills

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"path"
	"reflect"
	"sort"
	"strings"

	"github.com/eberle1080/mcp-protocol/schema"
)

// The functions below are the checks SEP-2640 requires of a host that reads a
// skill ("Integrity and Verification"). They are pure: they fetch nothing and
// grant nothing. A host calls them on what it has already read.

// VerificationError is a host-side verification failure: the bytes read do not
// match the entry's size or digest, SKILL.md's frontmatter differs from the
// entry's, or the read was of a file the held entry does not list. It is not a
// protocol error. The content must not be used; the recovery is to refresh the
// entry with skills/get, which revokes any approval bound to the old entry.
type VerificationError struct {
	URI    string
	Reason string
}

func (e *VerificationError) Error() string {
	return fmt.Sprintf("skill verification failed for %s: %s", e.URI, e.Reason)
}

// IsVerificationError reports whether err is, or wraps, a VerificationError.
func IsVerificationError(err error) bool {
	var target *VerificationError
	return errors.As(err, &target)
}

func failed(uri, format string, args ...interface{}) error {
	return &VerificationError{URI: uri, Reason: fmt.Sprintf(format, args...)}
}

// RootOf returns the URI of the skill's root directory: its SKILL.md URI
// without the "/SKILL.md" suffix.
func RootOf(entry schema.Skill) (string, error) {
	name, _ := entry.Frontmatter["name"].(string)
	return Root(entry.Uri, name)
}

// Within reports whether uri is the skill root or lies inside it.
func Within(root, uri string) bool {
	return root != "" && (uri == root || strings.HasPrefix(uri, root+"/"))
}

// Inside reports whether uri names the skill root or something under it,
// however it is spelled: scheme and authority compare without case, escapes
// are decoded, and dot segments are resolved. Use it, not Within, to decide
// whether a read belongs to the skill, so that a respelling such as
// SKILL://demo/notes.md or skill://demo/a/../notes.md cannot pass for a read
// outside it. A URI that does not parse counts as inside, so it is refused
// rather than read unchecked.
func Inside(root, uri string) bool {
	normalRoot, ok := normalizeURI(root)
	if !ok {
		return true
	}
	normal, ok := normalizeURI(uri)
	if !ok {
		return true
	}
	return normal == normalRoot || strings.HasPrefix(normal, normalRoot+"/")
}

func normalizeURI(uri string) (string, bool) {
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Scheme == "" {
		return "", false
	}
	host := strings.ToLower(parsed.Host)
	if parsed.Opaque != "" {
		host = strings.ToLower(parsed.Opaque)
	}
	clean := parsed.Path
	if clean != "" {
		clean = path.Clean("/" + clean)
		if clean == "/" {
			clean = ""
		}
	}
	return strings.ToLower(parsed.Scheme) + "://" + host + clean, true
}

// Canonical checks that uri has the one spelling a skill file URI may have: no
// query, fragment or user info, no dot segments or empty segments, a lower-case
// scheme, and no escapes beyond the ones url.URL itself writes. Compiler and
// Resolve produce such URIs; a URI that is not canonical may name a different
// file than it appears to.
func Canonical(uri string) error {
	parsed, err := url.Parse(uri)
	if err != nil {
		return err
	}
	switch {
	case parsed.Scheme == "" || parsed.Scheme != strings.ToLower(parsed.Scheme):
		return fmt.Errorf("%q has no lower-case scheme", uri)
	case parsed.Opaque != "" || parsed.User != nil:
		return fmt.Errorf("%q is not a hierarchical URI", uri)
	case parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(uri, "#"):
		return fmt.Errorf("%q has a query or fragment", uri)
	case parsed.RawPath != "":
		return fmt.Errorf("%q escapes characters that need no escaping", uri)
	case parsed.Path != "" && path.Clean(parsed.Path) != parsed.Path:
		return fmt.Errorf("%q has dot or empty path segments", uri)
	case parsed.String() != uri:
		return fmt.Errorf("%q is not in canonical form", uri)
	}
	return nil
}

// File returns the entry's manifest record for uri. It reports false when the
// entry lists no such file, and always for a dynamic skill.
func File(entry schema.Skill, uri string) (schema.SkillResource, bool) {
	for _, file := range entry.Resources.Files {
		if file.Uri == uri {
			return file, true
		}
	}
	return schema.SkillResource{}, false
}

// Resolve turns a reference written inside a skill into a resource URI. A
// relative path resolves against the skill root, as a filesystem path would:
// "references/GUIDE.md" in skill://acme/refunds/SKILL.md is
// skill://acme/refunds/references/GUIDE.md. A path that climbs out of the
// root is refused. An absolute URI is returned as given; whether it belongs
// to the skill is for Inside, Canonical and File to say.
//
// A relative reference is read as a URL reference, the way a Markdown link
// is: "#" starts a fragment and "?" a query (which is refused), so a file
// whose name contains "#", "?" or "%" is written with escapes
// ("notes%23v2.md"), and a name with ":" before any "/" is written "./a:b.md".
func Resolve(entry schema.Skill, ref string) (string, error) {
	root, err := RootOf(entry)
	if err != nil {
		return "", err
	}
	relative := ref
	if parsed, err := url.Parse(ref); err == nil {
		if parsed.Scheme != "" {
			return ref, nil
		}
		if parsed.Host != "" || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.ForceQuery {
			return "", fmt.Errorf("invalid skill reference %q", ref)
		}
		// A Markdown link may carry a fragment ("GUIDE.md#setup") or escapes
		// ("my%20notes.md"); the file is the decoded path.
		relative = parsed.Path
	}
	if relative == "" || strings.HasPrefix(relative, "/") || strings.Contains(relative, "\\") {
		return "", fmt.Errorf("invalid skill reference %q", ref)
	}
	clean := path.Clean(relative)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("skill reference %q leaves the skill", ref)
	}
	if clean == "." {
		return root, nil
	}
	// Build the URI the way Compiler names files, so the same path escapes
	// the same way.
	base, err := url.Parse(root)
	if err != nil {
		return "", err
	}
	resolved := *base
	resolved.Path += "/" + clean
	resolved.RawPath = ""
	return resolved.String(), nil
}

// ContentItem returns the one content item for uri in a resources/read result,
// and its raw bytes: the UTF-8 bytes of text, or the decoded bytes of a blob.
// The result must hold exactly one item for uri, and that item must carry text
// or a blob, not both, so the bytes checked are the bytes a caller would use.
// A caller that hands the read on should hand on only this item.
func ContentItem(uri string, result *schema.ReadResourceResult) (schema.ReadResourceResultContentsElem, []byte, error) {
	if result == nil {
		return schema.ReadResourceResultContentsElem{}, nil, failed(uri, "the read returned no result")
	}
	var match *schema.ReadResourceResultContentsElem
	count := 0
	for i := range result.Contents {
		if result.Contents[i].Uri == uri {
			match = &result.Contents[i]
			count++
		}
	}
	if count != 1 {
		return schema.ReadResourceResultContentsElem{}, nil, failed(uri, "the read returned %d content items for the file, want 1", count)
	}
	if match.Blob != "" && match.Text != "" {
		return schema.ReadResourceResultContentsElem{}, nil, failed(uri, "the content item carries both text and a blob")
	}
	if match.Blob != "" {
		data, err := base64.StdEncoding.DecodeString(match.Blob)
		if err != nil {
			return schema.ReadResourceResultContentsElem{}, nil, failed(uri, "the blob is not valid base64: %v", err)
		}
		return *match, data, nil
	}
	return *match, []byte(match.Text), nil
}

// ContentBytes returns the raw bytes of uri from a resources/read result; see
// ContentItem.
func ContentBytes(uri string, result *schema.ReadResourceResult) ([]byte, error) {
	_, data, err := ContentItem(uri, result)
	return data, err
}

// VerifyFile checks bytes read for a file against its manifest record: the
// length against size, then the SHA-256 digest.
func VerifyFile(file schema.SkillResource, data []byte) error {
	if int64(len(data)) != file.Size {
		return failed(file.Uri, "read %d bytes, the entry says %d", len(data), file.Size)
	}
	if digest := fmt.Sprintf("sha256:%x", sha256.Sum256(data)); digest != file.Digest {
		return failed(file.Uri, "digest %s, the entry says %s", digest, file.Digest)
	}
	return nil
}

// VerifyFrontmatter parses the frontmatter of a SKILL.md the host read and
// compares it, field by field, with the entry's frontmatter. Hosts must do this
// whether the entry lists files or is dynamic.
func VerifyFrontmatter(entry schema.Skill, skillMD []byte) error {
	parsed, err := Frontmatter(skillMD)
	if err != nil {
		return failed(entry.Uri, "SKILL.md frontmatter: %v", err)
	}
	read, err := normalizeJSON(parsed)
	if err != nil {
		return failed(entry.Uri, "SKILL.md frontmatter: %v", err)
	}
	listed, err := normalizeJSON(entry.Frontmatter)
	if err != nil {
		return failed(entry.Uri, "entry frontmatter: %v", err)
	}
	keys := map[string]bool{}
	for key := range read {
		keys[key] = true
	}
	for key := range listed {
		keys[key] = true
	}
	differ := []string{}
	for key := range keys {
		a, inRead := read[key]
		b, inListed := listed[key]
		if inRead != inListed || !sameJSON(a, b) {
			differ = append(differ, key)
		}
	}
	if len(differ) > 0 {
		sort.Strings(differ)
		return failed(entry.Uri, "SKILL.md frontmatter differs from the entry in %s", strings.Join(differ, ", "))
	}
	return nil
}

// sameJSON compares two decoded JSON values. Numbers compare by value, so 1,
// 1.0 and 1e0 are equal however the server wrote them; an object key present
// with null differs from a missing key.
func sameJSON(a, b interface{}) bool {
	switch x := a.(type) {
	case json.Number:
		y, ok := b.(json.Number)
		if !ok {
			return false
		}
		p, okA := new(big.Rat).SetString(x.String())
		q, okB := new(big.Rat).SetString(y.String())
		return okA && okB && p.Cmp(q) == 0
	case map[string]interface{}:
		y, ok := b.(map[string]interface{})
		if !ok || len(x) != len(y) {
			return false
		}
		for key, value := range x {
			other, present := y[key]
			if !present || !sameJSON(value, other) {
				return false
			}
		}
		return true
	case []interface{}:
		y, ok := b.([]interface{})
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !sameJSON(x[i], y[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
	}
}

// normalizeJSON gives two frontmatter maps the same Go representation (numbers
// as json.Number) so they compare by JSON value, not by how they were built.
func normalizeJSON(value map[string]interface{}) (map[string]interface{}, error) {
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

// ContentBinding identifies an entry's resources set: every file's URI and
// digest. A host that persists a user's approval of a skill stores it with
// this value and treats the approval as revoked when a later entry for the
// skill has a different one, whether a file was changed, added or removed
// (SEP-2640, content-bound approval). A dynamic skill cannot be bound: ok is
// false.
func ContentBinding(entry schema.Skill) (binding string, ok bool) {
	if entry.Resources.Dynamic || entry.Resources.Files == nil {
		return "", false
	}
	lines := make([]string, 0, len(entry.Resources.Files))
	for _, file := range entry.Resources.Files {
		lines = append(lines, file.Uri+"\x00"+file.Digest)
	}
	sort.Strings(lines)
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(strings.Join(lines, "\n")))), true
}
