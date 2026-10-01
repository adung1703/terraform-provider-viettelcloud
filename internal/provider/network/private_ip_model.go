package network

import (
	"time"

	"github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/compare"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	networksdk "github.com/viettelcloud-oss/sdks/go/network"
)

var privateIPAttachedVIPAttributeTypes = map[string]attr.Type{
	"id":         types.StringType,
	"ip_address": types.StringType,
}

type PrivateIPModel struct {
	ID            types.String `tfsdk:"id"`
	Description   types.String `tfsdk:"description"`
	IPAddress     types.String `tfsdk:"ip_address"`
	MACAddress    types.String `tfsdk:"mac_address"`
	SubnetID      types.String `tfsdk:"subnet_id"`
	SubnetName    types.String `tfsdk:"subnet_name"`
	SubnetCIDR    types.String `tfsdk:"subnet_cidr"`
	VPCID         types.String `tfsdk:"vpc_id"`
	VPCName       types.String `tfsdk:"vpc_name"`
	Region        types.String `tfsdk:"region"`
	ServerID      types.String `tfsdk:"server_id"`
	ServerName    types.String `tfsdk:"server_name"`
	DeviceOwner   types.String `tfsdk:"device_owner"`
	DisplayName   types.String `tfsdk:"display_name"`
	PortSecurity  types.Bool   `tfsdk:"port_security"`
	AllowedCIDRs  types.Set    `tfsdk:"allowed_cidrs"`
	AllowedVIPIDs types.Set    `tfsdk:"allowed_vip_ids"`
	AttachedVIPs  types.Set    `tfsdk:"attached_vips"`
	CreatedAt     types.String `tfsdk:"created_at"`
	UpdatedAt     types.String `tfsdk:"updated_at"`
}

type PrivateIPResourceModel struct {
	PrivateIPModel
}

// VPCCIDR is lookup-only: NestedVPCSchema carries no CIDR, so the backend can
// never populate it and the configured value is passed through untouched.
type PrivateIPDataSourceModel struct {
	PrivateIPModel
	VPCCIDR types.String `tfsdk:"vpc_cidr"`
}

func populatePrivateIPModel(privateIP *networksdk.PrivateIPSchema, m *PrivateIPModel) {
	if privateIP == nil {
		return
	}

	m.ID = types.StringValue(privateIP.Id.String())
	m.SubnetID = types.StringValue(privateIP.Subnet.Id.String())
	m.SubnetName = types.StringValue(privateIP.Subnet.Name)
	m.SubnetCIDR = types.StringValue(privateIP.Subnet.Cidr)
	m.VPCID = types.StringValue(privateIP.Vpc.Id.String())
	m.VPCName = types.StringValue(privateIP.Vpc.Name)
	m.Region = types.StringValue(privateIP.Region.Name)
	m.DeviceOwner = types.StringValue(string(privateIP.DeviceOwner))
	m.DisplayName = types.StringValue(privateIP.DisplayName)
	m.PortSecurity = types.BoolValue(privateIP.PortSecurity)
	m.CreatedAt = types.StringValue(privateIP.CreatedAt.Format(time.RFC3339))
	m.UpdatedAt = types.StringValue(privateIP.UpdatedAt.Format(time.RFC3339))

	m.Description = types.StringPointerValue(privateIP.Description)
	m.IPAddress = types.StringPointerValue(privateIP.IpAddress)
	m.MACAddress = types.StringPointerValue(privateIP.MacAddress)

	if privateIP.Server != nil {
		m.ServerID = types.StringValue(privateIP.Server.Id.String())
		m.ServerName = types.StringValue(privateIP.Server.Name)
	} else {
		m.ServerID = types.StringNull()
		m.ServerName = types.StringNull()
	}

	if privateIP.AllowedCidrs != nil {
		values := make([]attr.Value, 0, len(*privateIP.AllowedCidrs))
		for _, cidr := range *privateIP.AllowedCidrs {
			values = append(values, types.StringValue(cidr))
		}
		m.AllowedCIDRs = types.SetValueMust(types.StringType, values)
	} else {
		m.AllowedCIDRs = types.SetNull(types.StringType)
	}

	attachedVIPType := types.ObjectType{AttrTypes: privateIPAttachedVIPAttributeTypes}
	if privateIP.AttachedVips != nil {
		values := make([]attr.Value, 0, len(*privateIP.AttachedVips))
		idValues := make([]attr.Value, 0, len(*privateIP.AttachedVips))
		for _, vip := range *privateIP.AttachedVips {
			idValues = append(idValues, types.StringValue(vip.Id.String()))
			values = append(values, types.ObjectValueMust(privateIPAttachedVIPAttributeTypes, map[string]attr.Value{
				"id":         types.StringValue(vip.Id.String()),
				"ip_address": types.StringPointerValue(vip.IpAddress),
			}))
		}
		m.AllowedVIPIDs = types.SetValueMust(types.StringType, idValues)
		m.AttachedVIPs = types.SetValueMust(attachedVIPType, values)
	} else {
		m.AllowedVIPIDs = types.SetNull(types.StringType)
		m.AttachedVIPs = types.SetNull(attachedVIPType)
	}
}

func populatePrivateIPResourceState(privateIP *networksdk.PrivateIPSchema, state *PrivateIPResourceModel) {
	if privateIP == nil {
		return
	}

	originalSubnetID := state.SubnetID
	originalIPAddress := state.IPAddress
	originalMACAddress := state.MACAddress
	originalAllowedVIPIDs := state.AllowedVIPIDs
	populatePrivateIPModel(privateIP, &state.PrivateIPModel)

	if compare.SameUUID(originalSubnetID, privateIP.Subnet.Id) {
		state.SubnetID = originalSubnetID
	}
	if privateIP.IpAddress != nil && compare.SameIPAddress(originalIPAddress, *privateIP.IpAddress) {
		state.IPAddress = originalIPAddress
	}
	if privateIP.MacAddress != nil && compare.SameMACAddress(originalMACAddress, *privateIP.MacAddress) {
		state.MACAddress = originalMACAddress
	}
	if compare.SameUUIDSet(originalAllowedVIPIDs, state.AllowedVIPIDs) {
		state.AllowedVIPIDs = originalAllowedVIPIDs
	}
}

func populatePrivateIPDataSourceState(privateIP *networksdk.PrivateIPSchema, state *PrivateIPDataSourceModel) {
	if privateIP == nil {
		return
	}

	populatePrivateIPModel(privateIP, &state.PrivateIPModel)
}
