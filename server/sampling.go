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

// GetSamplingCapability returns the client's sampling capability if available
func (d *DefaultHandler) GetSamplingCapability() *schema.SamplingCapability {
	if d.ClientInitialize == nil || d.ClientInitialize.Capabilities.Sampling == nil {
		return nil
	}

	// Parse the map into a strongly-typed capability
	cap := &schema.SamplingCapability{}
	if enabled, ok := d.ClientInitialize.Capabilities.Sampling["enabled"].(bool); ok {
		cap.Enabled = enabled
	}
	if supportsTools, ok := d.ClientInitialize.Capabilities.Sampling["supportsTools"].(bool); ok {
		cap.SupportsTools = supportsTools
	}

	return cap
}

// GetElicitationCapability returns the client's elicitation capability if available
func (d *DefaultHandler) GetElicitationCapability() *schema.ElicitationCapability {
	if d.ClientInitialize == nil {
		d.Logger.Debug("GetElicitationCapability: ClientInitialize is nil")
		return nil
	}

	if d.ClientInitialize.Capabilities.Elicitation == nil {
		d.Logger.Debug("GetElicitationCapability: Elicitation map is nil")
		return nil
	}

	d.Logger.Debug("GetElicitationCapability: parsing capabilities", "raw_map", d.ClientInitialize.Capabilities.Elicitation)

	// Parse the map into a strongly-typed capability
	cap := &schema.ElicitationCapability{}
	if enabled, ok := d.ClientInitialize.Capabilities.Elicitation["enabled"].(bool); ok {
		cap.Enabled = enabled
		d.Logger.Debug("GetElicitationCapability: found enabled field", "value", enabled)
	} else {
		d.Logger.Debug("GetElicitationCapability: enabled field not found or wrong type",
			"enabled_raw", d.ClientInitialize.Capabilities.Elicitation["enabled"])
	}

	if modes, ok := d.ClientInitialize.Capabilities.Elicitation["supportedModes"].([]interface{}); ok {
		for _, mode := range modes {
			if modeStr, ok := mode.(string); ok {
				cap.SupportedModes = append(cap.SupportedModes, modeStr)
			}
		}
		d.Logger.Debug("GetElicitationCapability: found supportedModes", "modes", cap.SupportedModes)
	} else {
		d.Logger.Debug("GetElicitationCapability: supportedModes not found or wrong type",
			"supportedModes_raw", d.ClientInitialize.Capabilities.Elicitation["supportedModes"])
	}

	d.Logger.Debug("GetElicitationCapability: returning capability", "enabled", cap.Enabled, "modes", cap.SupportedModes)
	return cap
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
