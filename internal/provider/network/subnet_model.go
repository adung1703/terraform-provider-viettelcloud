package network

import (
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/viettelcloud-oss/sdks/go/core"
	networksdk "github.com/viettelcloud-oss/sdks/go/network"
)

type SubnetModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	CIDR        types.String `tfsdk:"cidr"`
	VPCID       types.String `tfsdk:"vpc_id"`
	VPCName     types.String `tfsdk:"vpc_name"`
	Region      types.String `tfsdk:"region"`
	DisplayName types.String `tfsdk:"display_name"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

type SubnetResourceModel struct {
	SubnetModel
}

type SubnetDataSourceModel struct {
	SubnetModel
}

func populateSubnetModel(subnet *networksdk.SubnetSchema, m *SubnetModel) {
	if subnet == nil {
		return
	}

	m.ID = types.StringValue(subnet.Id.String())
	m.Name = types.StringValue(subnet.Name)
	m.CIDR = types.StringValue(subnet.Cidr)
	m.VPCID = types.StringValue(subnet.Vpc.Id.String())
	m.VPCName = types.StringValue(subnet.Vpc.Name)
	m.Region = types.StringValue(subnet.Region.Name)
	m.DisplayName = types.StringValue(subnet.DisplayName)
	m.CreatedAt = types.StringValue(subnet.CreatedAt.Format(time.RFC3339))
	m.UpdatedAt = types.StringValue(subnet.UpdatedAt.Format(time.RFC3339))

	m.Description = types.StringPointerValue(subnet.Description)
}

func populateSubnetResourceState(subnet *networksdk.SubnetSchema, state *SubnetResourceModel) {
	if subnet == nil {
		return
	}

	originalName := state.Name
	originalVPCID := state.VPCID
	populateSubnetModel(subnet, &state.SubnetModel)

	if !originalName.IsNull() && !originalName.IsUnknown() && strings.TrimSpace(originalName.ValueString()) == subnet.Name {
		state.Name = originalName
	}
	// Keep the original representation only when its parsed UUID still
	// identifies the VPC returned by the backend.
	if !originalVPCID.IsNull() && !originalVPCID.IsUnknown() {
		parsedVPCID, err := core.ParseUUID(originalVPCID.ValueString())
		if err == nil && parsedVPCID == subnet.Vpc.Id {
			state.VPCID = originalVPCID
		}
	}
}

func populateSubnetDataSourceState(subnet *networksdk.SubnetSchema, state *SubnetDataSourceModel) {
	if subnet == nil {
		return
	}

	populateSubnetModel(subnet, &state.SubnetModel)
}
