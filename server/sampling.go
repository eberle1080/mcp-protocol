package server

import (
	"errors"

	"github.com/eberle1080/mcp-protocol/schema"
)

var (
	// ErrSamplingNotSupported is returned when the client doesn't support sampling
	ErrSamplingNotSupported = errors.New("client does not support sampling")
	// ErrElicitationNotSupported is returned when the client doesn't support elicitation
	ErrElicitationNotSupported = errors.New("client does not support elicitation")
)

// GetSamplingCapability returns the client's sampling capability, or nil if the client didn't
// declare one. Per the spec, declaring the capability object means sampling is supported;
// its optional "tools" member means tool-enabled sampling is supported.
func (d *DefaultHandler) GetSamplingCapability() *schema.SamplingCapability {
	if d.ClientInitialize == nil || d.ClientInitialize.Capabilities.Sampling == nil {
		return nil
	}

	return &schema.SamplingCapability{
		Enabled:       true,
		SupportsTools: d.ClientInitialize.Capabilities.Sampling.Tools != nil,
	}
}

// GetElicitationCapability returns the client's elicitation capability, or nil if the client
// didn't declare one. Per the spec, declaring the capability object means elicitation is
// supported; its optional "form" and "url" members name the supported modes, and an empty
// object means form mode only (for compatibility with clients predating modes).
func (d *DefaultHandler) GetElicitationCapability() *schema.ElicitationCapability {
	if d.ClientInitialize == nil || d.ClientInitialize.Capabilities.Elicitation == nil {
		return nil
	}

	elicitation := d.ClientInitialize.Capabilities.Elicitation
	capability := &schema.ElicitationCapability{Enabled: true}

	if elicitation.Form != nil || elicitation.Url == nil {
		capability.SupportedModes = append(capability.SupportedModes, "form")
	}

	if elicitation.Url != nil {
		capability.SupportedModes = append(capability.SupportedModes, "url")
	}

	return capability
}

// CanSample checks if the client supports sampling
func (d *DefaultHandler) CanSample() bool {
	cap := d.GetSamplingCapability()
	return cap != nil && cap.Enabled
}

// CanElicit checks if the client supports elicitation
func (d *DefaultHandler) CanElicit() bool {
	cap := d.GetElicitationCapability()
	return cap != nil && cap.Enabled
}

// SupportsElicitationMode checks if the client supports the specified mode
func (d *DefaultHandler) SupportsElicitationMode(mode string) bool {
	cap := d.GetElicitationCapability()
	if cap == nil {
		return false
	}
	for _, m := range cap.SupportedModes {
		if m == mode {
			return true
		}
	}
	return false
}
