package network

import (
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"

	networksdk "github.com/viettelcloud-oss/sdks/go/network"
)

type VPCModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	CIDR        types.String `tfsdk:"cidr"`
	Region      types.String `tfsdk:"region"`
	DisplayName types.String `tfsdk:"display_name"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

// The resource and data source schemas expose the same attributes with
// different configurability, so both use the shared model. Give one its own
// struct as soon as its schema carries an attribute the other does not.
type (
	VPCResourceModel   = VPCModel
	VPCDataSourceModel = VPCModel
)

func populateVPCModel(vpc *networksdk.VPCSchema, m *VPCModel) {
	if vpc == nil {
		return
	}

	m.ID = types.StringValue(vpc.Id.String())
	m.Name = types.StringValue(vpc.Name)
	m.DisplayName = types.StringValue(vpc.DisplayName)
	m.CIDR = types.StringValue(vpc.Cidr)
	m.Region = types.StringValue(vpc.Region.Name)
	m.CreatedAt = types.StringValue(vpc.CreatedAt.Format(time.RFC3339))
	m.UpdatedAt = types.StringValue(vpc.UpdatedAt.Format(time.RFC3339))

	m.Description = types.StringPointerValue(vpc.Description)
}

func populateVPCResourceState(vpc *networksdk.VPCSchema, state *VPCResourceModel) {
	if vpc == nil {
		return
	}

	configuredName := state.Name
	configuredRegion := state.Region
	populateVPCModel(vpc, state)
	// Keep the configured representation when the backend only trims outer
	// whitespace, otherwise Terraform reports an inconsistent result.
	if !configuredName.IsNull() && !configuredName.IsUnknown() && strings.TrimSpace(configuredName.ValueString()) == vpc.Name {
		state.Name = configuredName
	}
	if !configuredRegion.IsNull() && !configuredRegion.IsUnknown() && strings.TrimSpace(configuredRegion.ValueString()) == vpc.Region.Name {
		state.Region = configuredRegion
	}
}

func populateVPCDataSourceState(vpc *networksdk.VPCSchema, state *VPCDataSourceModel) {
	populateVPCModel(vpc, state)
}
