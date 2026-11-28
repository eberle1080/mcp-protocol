package apps

import "strings"

// ContentSecurityPolicy builds the policy a host serves a view's document
// with, from the view's metadata, as SEP-1865 constructs it:
//
//   - with no csp metadata, the restrictive default: scripts and styles from
//     the document itself, images and media also from data: URLs, no
//     connections, no nested frames, no plugins, base URIs from the document;
//   - with csp metadata, the declared resource origins added to script, style,
//     image, font and media sources, the declared connect origins to
//     connect-src, frame origins to frame-src (else 'none') and base URI
//     origins to base-uri (else 'self').
//
// Entries that are not valid sources (see ValidSource) are left out and
// returned in rejected; a host may refuse to render a view that has any. The
// policy only ever narrows to what the metadata declares.
func (m ResourceMeta) ContentSecurityPolicy() (policy string, rejected []string) {
	if m.CSP == nil {
		return strings.Join([]string{
			"default-src 'none'",
			"script-src 'self' 'unsafe-inline'",
			"style-src 'self' 'unsafe-inline'",
			"img-src 'self' data:",
			"media-src 'self' data:",
			"connect-src 'none'",
			"frame-src 'none'",
			"object-src 'none'",
			"base-uri 'self'",
		}, "; "), nil
	}
	csp := m.CSP
	resources := joinSources(nil, csp.ResourceDomains, &rejected)
	with := func(lead string, sources string) string {
		if sources == "" {
			return lead
		}
		return lead + " " + sources
	}
	frames := joinSources(nil, csp.FrameDomains, &rejected)
	if frames == "" {
		frames = "'none'"
	}
	bases := joinSources(nil, csp.BaseURIDomains, &rejected)
	if bases == "" {
		bases = "'self'"
	}
	return strings.Join([]string{
		"default-src 'none'",
		with("script-src 'self' 'unsafe-inline'", resources),
		with("style-src 'self' 'unsafe-inline'", resources),
		with("connect-src 'self'", joinSources(nil, csp.ConnectDomains, &rejected)),
		with("img-src 'self' data:", resources),
		with("font-src 'self'", resources),
		with("media-src 'self' data:", resources),
		"frame-src " + frames,
		"object-src 'none'",
		"base-uri " + bases,
	}, "; "), rejected
}

// AllowAttribute is the iframe allow attribute for the permissions the view
// asks for, such as "camera; clipboard-write". A host grants only what it is
// willing to; this is what was asked.
func (m ResourceMeta) AllowAttribute() string {
	if m.Permissions == nil {
		return ""
	}
	var features []string
	for _, p := range []struct {
		requested *Permission
		feature   string
	}{
		{m.Permissions.Camera, "camera"},
		{m.Permissions.Microphone, "microphone"},
		{m.Permissions.Geolocation, "geolocation"},
		{m.Permissions.ClipboardWrite, "clipboard-write"},
	} {
		if p.requested != nil {
			features = append(features, p.feature)
		}
	}
	return strings.Join(features, "; ")
}
