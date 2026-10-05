// Package parse converts Terraform values into the Go and SDK types a request
// needs, reporting a field-level diagnostic instead of an error so callers can
// append it directly.
//
// Null and unknown handling belongs to the caller; these functions assume a
// known value.
package parse

// reporting a field-level diagnostic instead of an error so callers can append
// it directly. Null and unknown handling belongs to the caller; these functions
// assume a known value.

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/viettelcloud-oss/sdks/go/core"
)

func UUIDString(v types.String, field string) (core.UUID, diag.Diagnostics) {
	var diags diag.Diagnostics
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		diags.AddError("Missing UUID value", fmt.Sprintf("%s must be provided.", field))
		return core.NilUUID, diags
	}
	parsed, err := core.ParseUUID(v.ValueString())
	if err != nil {
		diags.AddError("Invalid UUID", fmt.Sprintf("%s must be a valid UUID: %s", field, err))
		return core.NilUUID, diags
	}
	return parsed, diags
}

func OptionalUUIDString(v types.String, field string) (*core.UUID, diag.Diagnostics) {
	var diags diag.Diagnostics
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return nil, diags
	}
	parsed, err := core.ParseUUID(v.ValueString())
	if err != nil {
		diags.AddError("Invalid UUID", fmt.Sprintf("%s must be a valid UUID: %s", field, err))
		return nil, diags
	}
	return &parsed, diags
}
