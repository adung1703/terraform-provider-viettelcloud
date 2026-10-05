package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	blockstoragesdk "github.com/viettelcloud-oss/sdks/go/blockstorage"
	"github.com/viettelcloud-oss/sdks/go/core"
	serversdk "github.com/viettelcloud-oss/sdks/go/server"
)

type DataVolumeInputModel struct {
	ID                  types.String `tfsdk:"id"`
	VolumeType          types.String `tfsdk:"volume_type"`
	VolumeSize          types.Int64  `tfsdk:"volume_size"`
	IOPS                types.Int64  `tfsdk:"iops"`
	DeleteOnTermination types.Bool   `tfsdk:"delete_on_termination"`
}

type PrivateIPInputModel struct {
	Kind                types.String `tfsdk:"kind"`
	ID                  types.String `tfsdk:"id"`
	SubnetID            types.String `tfsdk:"subnet_id"`
	SubnetCIDR          types.String `tfsdk:"subnet_cidr"`
	DeleteOnTermination types.Bool   `tfsdk:"delete_on_termination"`
	IPAddress           types.String `tfsdk:"ip_address"`
	MACAddress          types.String `tfsdk:"mac_address"`
}

type ElasticIPInputModel struct {
	Kind                types.String `tfsdk:"kind"`
	ID                  types.String `tfsdk:"id"`
	EnableIPv4          types.Bool   `tfsdk:"enable_ipv4"`
	EnableIPv6          types.Bool   `tfsdk:"enable_ipv6"`
	DeleteOnTermination types.Bool   `tfsdk:"delete_on_termination"`
	IPAddress           types.String `tfsdk:"ip_address"`
	IPv6Address         types.String `tfsdk:"ipv6_address"`
	Status              types.String `tfsdk:"status"`
}

type BootInputModel struct {
	BootType            types.String `tfsdk:"boot_type"`
	Image               types.String `tfsdk:"image"`
	CustomImageID       types.String `tfsdk:"custom_image_id"`
	VolumeID            types.String `tfsdk:"volume_id"`
	VolumeType          types.String `tfsdk:"volume_type"`
	VolumeSize          types.Int64  `tfsdk:"volume_size"`
	IOPS                types.Int64  `tfsdk:"iops"`
	DeleteOnTermination types.Bool   `tfsdk:"delete_on_termination"`
}

type FlavorInputModel struct {
	Kind   types.String `tfsdk:"kind"`
	Name   types.String `tfsdk:"name"`
	Family types.String `tfsdk:"family"`
	VCPUs  types.Int64  `tfsdk:"vcpus"`
	RAM    types.Int64  `tfsdk:"ram"`
}

type ServerModel struct {
	ID                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	Description        types.String `tfsdk:"description"`
	Zone               types.String `tfsdk:"zone"`
	KeyPairID          types.String `tfsdk:"key_pair_id"`
	KeyPairName        types.String `tfsdk:"key_pair_name"`
	PlacementGroupID   types.String `tfsdk:"placement_group_id"`
	PlacementGroupName types.String `tfsdk:"placement_group_name"`
	UserData           types.String `tfsdk:"user_data"`
	Bandwidth          types.Int64  `tfsdk:"bandwidth"`
	PowerState         types.String `tfsdk:"power_state"`
	Status             types.String `tfsdk:"status"`
	ServerType         types.String `tfsdk:"server_type"`
	CreatedAt          types.String `tfsdk:"created_at"`
}

type ServerResourceModel struct {
	ServerModel

	Boot             types.Object `tfsdk:"boot"`
	Flavor           types.Object `tfsdk:"flavor"`
	DataVolumes      types.List   `tfsdk:"data_volumes"`
	PrivateIPs       types.List   `tfsdk:"private_ips"`
	ElasticIPs       types.List   `tfsdk:"elastic_ips"`
	SecurityGroupIDs types.Set    `tfsdk:"security_group_ids"`
	Quantity         types.Int64  `tfsdk:"quantity"`
	DataVolumeIDs    types.List   `tfsdk:"data_volume_ids"`
}

func flavorAttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"kind": types.StringType, "name": types.StringType, "family": types.StringType,
		"vcpus": types.Int64Type, "ram": types.Int64Type,
	}
}

func dataVolumeResourceAttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id": types.StringType, "volume_type": types.StringType,
		"volume_size": types.Int64Type, "iops": types.Int64Type,
		"delete_on_termination": types.BoolType,
	}
}

func privateIPResourceAttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"kind": types.StringType, "id": types.StringType, "subnet_id": types.StringType,
		"subnet_cidr": types.StringType, "delete_on_termination": types.BoolType,
		"ip_address": types.StringType, "mac_address": types.StringType,
	}
}

func elasticIPResourceAttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"kind": types.StringType, "id": types.StringType, "enable_ipv4": types.BoolType,
		"enable_ipv6": types.BoolType, "delete_on_termination": types.BoolType,
		"ip_address": types.StringType, "ipv6_address": types.StringType, "status": types.StringType,
	}
}

func bootResourceAttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"boot_type": types.StringType, "image": types.StringType, "custom_image_id": types.StringType,
		"volume_id": types.StringType, "volume_type": types.StringType, "volume_size": types.Int64Type, "iops": types.Int64Type,
		"delete_on_termination": types.BoolType,
	}
}

func populateServerResourceState(ctx context.Context, server *serversdk.ServerDetailSchema, state *ServerResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if server == nil {
		return diags
	}

	configuredName := state.Name
	configuredZone := state.Zone
	configuredUserData := state.UserData
	configuredBandwidth := state.Bandwidth
	configuredKeyPairID := state.KeyPairID
	configuredPlacementGroupID := state.PlacementGroupID
	populateServerModel(server, &state.ServerModel)
	if !configuredName.IsNull() && !configuredName.IsUnknown() && strings.TrimSpace(configuredName.ValueString()) == server.Name {
		state.Name = configuredName
	}
	if !configuredKeyPairID.IsNull() && !configuredKeyPairID.IsUnknown() && server.KeyPair != nil &&
		strings.EqualFold(strings.TrimSpace(configuredKeyPairID.ValueString()), server.KeyPair.Id.String()) {
		state.KeyPairID = configuredKeyPairID
	}
	if !configuredPlacementGroupID.IsNull() && !configuredPlacementGroupID.IsUnknown() && server.PlacementGroup != nil &&
		strings.EqualFold(strings.TrimSpace(configuredPlacementGroupID.ValueString()), server.PlacementGroup.Id.String()) {
		state.PlacementGroupID = configuredPlacementGroupID
	}
	state.Flavor = serverFlavorObject(ctx, state.Flavor, server.Flavor, &diags)
	if !configuredZone.IsNull() && !configuredZone.IsUnknown() && strings.TrimSpace(configuredZone.ValueString()) == server.Zone.Name {
		state.Zone = configuredZone
	}
	// The server response omits user data and bandwidth for some server types.
	// Replacing a configured value with the omitted one would leave state
	// inconsistent with the plan. user_data would then replace the server on
	// every plan, because it cannot be updated in place.
	state.UserData = preserveEquivalentString(configuredUserData, server.UserData)
	if server.Bandwidth == nil {
		state.Bandwidth = knownInt64OrNull(configuredBandwidth)
	}
	state.Quantity = types.Int64Value(1)

	state.PrivateIPs = privateIPResourceList(ctx, state.PrivateIPs, server.PrivateIps, false, &diags)
	state.ElasticIPs = elasticIPResourceList(ctx, state.ElasticIPs, server.ElasticIps, false, &diags)
	securityGroups, groupDiags := types.SetValueFrom(ctx, types.StringType, securityGroupIDs(server))
	diags.Append(groupDiags...)
	state.SecurityGroupIDs = securityGroups
	// The API's attachment order isn't stable across reads.
	dataVolumes, volumeDiags := types.ListValueFrom(ctx, types.StringType, dataVolumeIDs(server))
	diags.Append(volumeDiags...)
	state.DataVolumeIDs = dataVolumes
	return diags
}

func populateServerModel(server *serversdk.ServerDetailSchema, state *ServerModel) {
	state.ID = types.StringValue(server.Id.String())
	state.Name = types.StringValue(server.Name)
	state.Description = types.StringValue(server.Description)
	state.Zone = types.StringValue(server.Zone.Name)
	if server.KeyPair != nil {
		state.KeyPairID = types.StringValue(server.KeyPair.Id.String())
		state.KeyPairName = types.StringValue(server.KeyPair.Name)
	} else {
		state.KeyPairID = types.StringNull()
		state.KeyPairName = types.StringNull()
	}
	if server.PlacementGroup != nil {
		state.PlacementGroupID = types.StringValue(server.PlacementGroup.Id.String())
		state.PlacementGroupName = types.StringValue(server.PlacementGroup.Name)
	} else {
		state.PlacementGroupID = types.StringNull()
		state.PlacementGroupName = types.StringNull()
	}
	state.UserData = types.StringValue(server.UserData)
	state.PowerState = types.StringValue(string(server.PowerState))
	state.Status = types.StringValue(string(server.Status))
	state.ServerType = types.StringValue(server.ServerType)
	state.CreatedAt = types.StringValue(server.CreatedAt.Format(time.RFC3339))
	if server.Bandwidth == nil {
		state.Bandwidth = types.Int64Null()
	} else {
		state.Bandwidth = types.Int64Value(int64(*server.Bandwidth))
	}
}

func populateServerPendingState(ctx context.Context, state *ServerResourceModel, id core.UUID) diag.Diagnostics {
	var diags diag.Diagnostics
	state.ID = types.StringValue(id.String())
	state.Quantity = types.Int64Value(1)
	if state.Description.IsUnknown() {
		state.Description = types.StringNull()
	}
	if state.Zone.IsUnknown() {
		state.Zone = types.StringNull()
	}
	if state.KeyPairID.IsUnknown() {
		state.KeyPairID = types.StringNull()
	}
	if state.KeyPairName.IsUnknown() {
		state.KeyPairName = types.StringNull()
	}
	if state.PlacementGroupID.IsUnknown() {
		state.PlacementGroupID = types.StringNull()
	}
	if state.PlacementGroupName.IsUnknown() {
		state.PlacementGroupName = types.StringNull()
	}
	if state.Bandwidth.IsUnknown() {
		state.Bandwidth = types.Int64Null()
	}
	if state.UserData.IsUnknown() {
		state.UserData = types.StringNull()
	}
	state.Boot = knownBootObject(state.Boot, &diags)
	state.DataVolumes = knownDataVolumeList(ctx, state.DataVolumes, &diags)
	state.PowerState = types.StringNull()
	state.Status = types.StringNull()
	state.ServerType = types.StringNull()
	state.PrivateIPs = privateIPResourceList(ctx, state.PrivateIPs, nil, true, &diags)
	state.ElasticIPs = elasticIPResourceList(ctx, state.ElasticIPs, nil, true, &diags)
	if state.SecurityGroupIDs.IsUnknown() {
		state.SecurityGroupIDs = types.SetNull(types.StringType)
	}
	state.DataVolumeIDs = types.ListNull(types.StringType)
	state.CreatedAt = types.StringNull()
	return diags
}

// --- Boot and data-volume state ---

func serverBootStateObject(
	server *serversdk.ServerDetailSchema,
	rootVolume *blockstoragesdk.VolumeDetailSchema,
	rootVolumeID core.UUID,
	bootType string,
	current BootInputModel,
	diags *diag.Diagnostics,
) types.Object {
	values := map[string]attr.Value{
		"boot_type":       preserveEquivalentString(current.BootType, bootType),
		"image":           types.StringNull(),
		"custom_image_id": types.StringNull(),
		"volume_id":       types.StringNull(),
		"volume_type":     types.StringNull(),
		"volume_size":     types.Int64Null(),
		"iops":            types.Int64Null(),
		"delete_on_termination": knownBoolOrDefault(
			current.DeleteOnTermination,
			bootVolumeDeletedByDefault(bootType),
		),
	}

	switch bootType {
	case "image":
		imageName := ""
		if server.Image != nil {
			imageName = server.Image.Name
		} else if rootVolume != nil && rootVolume.CreateFrom.Image != nil {
			imageName = rootVolume.CreateFrom.Image.Name
		}
		if imageName == "" && (current.Image.IsNull() || current.Image.IsUnknown()) {
			diags.AddError("Unable to determine server boot image", "The server and root-volume responses do not identify the image used by this server.")
		}
		values["image"] = preserveEquivalentString(current.Image, imageName)
		populateBootVolumeState(values, rootVolume, current, diags)
	case "custom_image":
		customImageID := ""
		if server.Image != nil {
			customImageID = server.Image.Id.String()
		} else if rootVolume != nil && rootVolume.CreateFrom.CustomImage != nil {
			customImageID = rootVolume.CreateFrom.CustomImage.Id.String()
		}
		if customImageID == "" && (current.CustomImageID.IsNull() || current.CustomImageID.IsUnknown()) {
			diags.AddError("Unable to determine server custom image", "The server and root-volume responses do not identify the custom image used by this server.")
		}
		values["custom_image_id"] = preserveEquivalentString(current.CustomImageID, customImageID)
		populateBootVolumeState(values, rootVolume, current, diags)
	case "local_disk":
		if server.Image != nil {
			values["image"] = preserveEquivalentString(current.Image, server.Image.Name)
		} else {
			values["image"] = knownStringOrNull(current.Image)
		}
	case "volume":
		values["volume_id"] = preserveEquivalentString(current.VolumeID, rootVolumeID.String())
	default:
		diags.AddError("Unsupported server boot type", fmt.Sprintf("The server state contains unsupported boot type %q.", bootType))
	}

	result, valueDiags := types.ObjectValue(bootResourceAttributeTypes(), values)
	diags.Append(valueDiags...)
	return result
}

func populateBootVolumeState(
	values map[string]attr.Value,
	volume *blockstoragesdk.VolumeDetailSchema,
	current BootInputModel,
	diags *diag.Diagnostics,
) {
	if volume == nil {
		values["volume_type"] = knownStringOrNull(current.VolumeType)
		values["volume_size"] = current.VolumeSize
		values["iops"] = current.IOPS
		return
	}
	if volume.CreateFrom.VolumeType != nil {
		values["volume_type"] = preserveEquivalentString(current.VolumeType, volume.CreateFrom.VolumeType.Name)
	} else {
		if current.VolumeType.IsNull() || current.VolumeType.IsUnknown() {
			diags.AddError(
				"Unable to determine server boot volume type",
				"The root-volume response does not identify its volume type, so Terraform cannot reconstruct the server boot configuration.",
			)
		}
		values["volume_type"] = knownStringOrNull(current.VolumeType)
	}
	values["volume_size"] = types.Int64Value(int64(volume.Size))
	if volume.Iops != nil {
		values["iops"] = types.Int64Value(int64(*volume.Iops))
	} else {
		values["iops"] = knownInt64OrNull(current.IOPS)
	}
}

// knownBootObject replaces the computed boot values Terraform planned as
// unknown with null so intermediate state written before the server is ready
// never contains an unknown value.
func knownBootObject(value types.Object, diags *diag.Diagnostics) types.Object {
	if value.IsNull() || value.IsUnknown() {
		return value
	}
	attributes := value.Attributes()
	known := make(map[string]attr.Value, len(attributes))
	for name, attributeType := range bootResourceAttributeTypes() {
		attribute, ok := attributes[name]
		if !ok || attribute.IsUnknown() {
			switch attributeType {
			case types.Int64Type:
				attribute = types.Int64Null()
			case types.BoolType:
				attribute = types.BoolNull()
			default:
				attribute = types.StringNull()
			}
		}
		known[name] = attribute
	}
	result, valueDiags := types.ObjectValue(bootResourceAttributeTypes(), known)
	diags.Append(valueDiags...)
	return result
}

func knownDataVolumeList(ctx context.Context, value types.List, diags *diag.Diagnostics) types.List {
	elementType := types.ObjectType{AttrTypes: dataVolumeResourceAttributeTypes()}
	if value.IsNull() || value.IsUnknown() {
		return types.ListNull(elementType)
	}
	var volumes []DataVolumeInputModel
	diags.Append(value.ElementsAs(ctx, &volumes, false)...)
	if diags.HasError() {
		return types.ListNull(elementType)
	}
	for i := range volumes {
		volumes[i].ID = knownStringOrNull(volumes[i].ID)
		volumes[i].IOPS = knownInt64OrNull(volumes[i].IOPS)
		volumes[i].DeleteOnTermination = knownBoolOrNull(volumes[i].DeleteOnTermination)
	}
	result, valueDiags := types.ListValueFrom(ctx, elementType, volumes)
	diags.Append(valueDiags...)
	return result
}

func decodeCurrentBoot(ctx context.Context, value types.Object, diags *diag.Diagnostics) (BootInputModel, bool) {
	if value.IsNull() || value.IsUnknown() {
		return BootInputModel{}, false
	}
	var current BootInputModel
	valueDiags := value.As(ctx, &current, basetypes.ObjectAsOptions{})
	diags.Append(valueDiags...)
	return current, !valueDiags.HasError()
}

func selectServerBootType(current types.String, inferred string, hasRootVolume bool) string {
	currentType := normalizedString(current)
	if currentType == "volume" && hasRootVolume {
		// A volume can retain its image/custom-image origin after being attached as
		// an existing boot volume, so only configured state can disambiguate it.
		return currentType
	}
	if currentType == "local_disk" && !hasRootVolume {
		return currentType
	}
	return inferred
}

func inferServerBootType(server *serversdk.ServerDetailSchema, rootVolume *blockstoragesdk.VolumeDetailSchema) string {
	if rootVolume == nil {
		if server.Image != nil {
			return "local_disk"
		}
		return ""
	}
	if rootVolume.CreateFrom.CustomImage != nil {
		return "custom_image"
	}
	if rootVolume.CreateFrom.Image != nil {
		return "image"
	}
	return "volume"
}

// bootVolumeDeletedByDefault reports whether an unconfigured
// boot.delete_on_termination deletes the boot volume. The provider creates that
// volume for image and custom_image, so it owns its lifetime; boot type volume
// attaches one the practitioner owns, and local_disk has none.
func bootVolumeDeletedByDefault(bootType string) bool {
	return bootType == "image" || bootType == "custom_image"
}

func serverRootVolumeID(server *serversdk.ServerDetailSchema) (core.UUID, bool) {
	for _, volume := range server.Volumes {
		if volume.MountAs == "root" {
			return volume.Volume.Id, true
		}
	}
	return core.UUID{}, false
}

func volumeIOPSValue(volume *blockstoragesdk.VolumeDetailSchema) types.Int64 {
	if volume.Iops == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*volume.Iops))
}

// --- Attachment and flavor state ---

func privateIPResourceList(
	ctx context.Context,
	current types.List,
	values []serversdk.NestedPrivateIPSchema,
	preserveUnmatched bool,
	diags *diag.Diagnostics,
) types.List {
	var configs []PrivateIPInputModel
	if !current.IsNull() && !current.IsUnknown() {
		valueDiags := current.ElementsAs(ctx, &configs, false)
		diags.Append(valueDiags...)
		if valueDiags.HasError() {
			return types.ListNull(types.ObjectType{AttrTypes: privateIPResourceAttributeTypes()})
		}
	}
	matches, used := matchAttachmentIDs(len(configs), len(values), func(configIndex, valueIndex int) bool {
		id := configs[configIndex].ID
		return !id.IsNull() && !id.IsUnknown() && id.ValueString() == values[valueIndex].Id.String()
	}, func(configIndex int) bool {
		id := configs[configIndex].ID
		return !id.IsNull() && !id.IsUnknown()
	})

	elements := make([]attr.Value, 0, max(len(configs), len(values)))
	for i, config := range configs {
		if matches[i] < 0 && !preserveUnmatched {
			continue
		}
		var value *serversdk.NestedPrivateIPSchema
		if matches[i] >= 0 {
			value = &values[matches[i]]
		}
		elements = append(elements, privateIPResourceObject(config, value, diags))
	}
	for i := range values {
		if !used[i] {
			elements = append(elements, privateIPResourceObject(PrivateIPInputModel{}, &values[i], diags))
		}
	}
	result, valueDiags := types.ListValue(types.ObjectType{AttrTypes: privateIPResourceAttributeTypes()}, elements)
	diags.Append(valueDiags...)
	return result
}

func privateIPResourceObject(config PrivateIPInputModel, value *serversdk.NestedPrivateIPSchema, diags *diag.Diagnostics) types.Object {
	kind := knownStringOrDefault(config.Kind, "ip")
	id := knownStringOrNull(config.ID)
	ipAddress := types.StringNull()
	macAddress := types.StringNull()
	if value != nil {
		id = types.StringValue(value.Id.String())
		if value.IpAddress != nil {
			ipAddress = types.StringValue(*value.IpAddress)
		}
		if value.MacAddress != nil {
			macAddress = types.StringValue(*value.MacAddress)
		}
	}
	result, valueDiags := types.ObjectValue(privateIPResourceAttributeTypes(), map[string]attr.Value{
		"kind": kind, "id": id, "subnet_id": knownStringOrNull(config.SubnetID),
		"subnet_cidr":           knownStringOrNull(config.SubnetCIDR),
		"delete_on_termination": knownBoolOrDefault(config.DeleteOnTermination, normalizedString(kind) == "subnet"),
		"ip_address":            ipAddress, "mac_address": macAddress,
	})
	diags.Append(valueDiags...)
	return result
}

func elasticIPResourceList(
	ctx context.Context,
	current types.List,
	values []serversdk.NestedElasticIPSchema,
	preserveUnmatched bool,
	diags *diag.Diagnostics,
) types.List {
	var configs []ElasticIPInputModel
	if !current.IsNull() && !current.IsUnknown() {
		valueDiags := current.ElementsAs(ctx, &configs, false)
		diags.Append(valueDiags...)
		if valueDiags.HasError() {
			return types.ListNull(types.ObjectType{AttrTypes: elasticIPResourceAttributeTypes()})
		}
	}
	matches, used := matchAttachmentIDs(len(configs), len(values), func(configIndex, valueIndex int) bool {
		id := configs[configIndex].ID
		return !id.IsNull() && !id.IsUnknown() && id.ValueString() == values[valueIndex].Id.String()
	}, func(configIndex int) bool {
		id := configs[configIndex].ID
		return !id.IsNull() && !id.IsUnknown()
	})

	elements := make([]attr.Value, 0, max(len(configs), len(values)))
	for i, config := range configs {
		if matches[i] < 0 && !preserveUnmatched {
			continue
		}
		var value *serversdk.NestedElasticIPSchema
		if matches[i] >= 0 {
			value = &values[matches[i]]
		}
		elements = append(elements, elasticIPResourceObject(config, value, diags))
	}
	for i := range values {
		if !used[i] {
			elements = append(elements, elasticIPResourceObject(ElasticIPInputModel{}, &values[i], diags))
		}
	}
	result, valueDiags := types.ListValue(types.ObjectType{AttrTypes: elasticIPResourceAttributeTypes()}, elements)
	diags.Append(valueDiags...)
	return result
}

func elasticIPResourceObject(config ElasticIPInputModel, value *serversdk.NestedElasticIPSchema, diags *diag.Diagnostics) types.Object {
	kind := knownStringOrDefault(config.Kind, "existing")
	id := knownStringOrNull(config.ID)
	ipv4 := types.StringNull()
	ipv6 := types.StringNull()
	status := types.StringNull()
	if value != nil {
		id = types.StringValue(value.Id.String())
		if value.IpAddress != nil {
			ipv4 = types.StringValue(*value.IpAddress)
		}
		if value.Ipv6Address != nil {
			ipv6 = types.StringValue(*value.Ipv6Address)
		}
		status = types.StringValue(string(value.Status))
	}
	result, valueDiags := types.ObjectValue(elasticIPResourceAttributeTypes(), map[string]attr.Value{
		"kind": kind, "id": id, "enable_ipv4": knownBoolOrNull(config.EnableIPv4),
		"enable_ipv6":           knownBoolOrNull(config.EnableIPv6),
		"delete_on_termination": knownBoolOrDefault(config.DeleteOnTermination, normalizedString(kind) == "new"),
		"ip_address":            ipv4, "ipv6_address": ipv6, "status": status,
	})
	diags.Append(valueDiags...)
	return result
}

func serverFlavorObject(
	ctx context.Context,
	current types.Object,
	backend serversdk.NestedFlavorSchema,
	diags *diag.Diagnostics,
) types.Object {
	values := map[string]attr.Value{
		"kind": types.StringValue("predefined"), "name": types.StringValue(backend.Name),
		"family": types.StringNull(), "vcpus": types.Int64Null(), "ram": types.Int64Null(),
	}
	if !current.IsNull() && !current.IsUnknown() {
		var configured FlavorInputModel
		valueDiags := current.As(ctx, &configured, basetypes.ObjectAsOptions{})
		diags.Append(valueDiags...)
		if !valueDiags.HasError() && !configured.Kind.IsNull() && !configured.Kind.IsUnknown() {
			kind := strings.TrimSpace(configured.Kind.ValueString())
			values["kind"] = configured.Kind
			switch kind {
			case "custom":
				values["name"] = types.StringNull()
				values["family"] = knownStringOrNull(configured.Family)
				values["vcpus"] = types.Int64Value(int64(backend.Vcpus))
				values["ram"] = types.Int64Value(int64(backend.Ram))
			case "predefined":
				if !configured.Name.IsNull() && !configured.Name.IsUnknown() &&
					strings.TrimSpace(configured.Name.ValueString()) == backend.Name {
					values["name"] = configured.Name
				}
			}
		}
	}
	result, valueDiags := types.ObjectValue(flavorAttributeTypes(), values)
	diags.Append(valueDiags...)
	return result
}

func matchAttachmentIDs(
	configCount int,
	valueCount int,
	matchesID func(int, int) bool,
	hasConfiguredID func(int) bool,
) ([]int, []bool) {
	matches := make([]int, configCount)
	used := make([]bool, valueCount)
	for i := range matches {
		matches[i] = -1
	}
	for configIndex := range configCount {
		for valueIndex := range valueCount {
			if !used[valueIndex] && matchesID(configIndex, valueIndex) {
				matches[configIndex] = valueIndex
				used[valueIndex] = true
				break
			}
		}
	}
	for configIndex := range configCount {
		if matches[configIndex] >= 0 || hasConfiguredID(configIndex) {
			continue
		}
		for valueIndex := range valueCount {
			if !used[valueIndex] {
				matches[configIndex] = valueIndex
				used[valueIndex] = true
				break
			}
		}
	}
	return matches, used
}

func securityGroupIDs(server *serversdk.ServerDetailSchema) []string {
	values := make([]string, 0, len(server.SecurityGroups))
	for _, securityGroup := range server.SecurityGroups {
		values = append(values, securityGroup.Id.String())
	}
	return values
}

func dataVolumeIDs(server *serversdk.ServerDetailSchema) []string {
	values := make([]string, 0, len(server.Volumes))
	for _, volume := range server.Volumes {
		if volume.MountAs == "data" {
			values = append(values, volume.Volume.Id.String())
		}
	}
	return values
}

// --- Framework value helpers ---

func knownStringOrNull(value types.String) types.String {
	if value.IsNull() || value.IsUnknown() {
		return types.StringNull()
	}
	return value
}

func knownStringOrDefault(value types.String, fallback string) types.String {
	if value.IsNull() || value.IsUnknown() {
		return types.StringValue(fallback)
	}
	return value
}

func knownBoolOrNull(value types.Bool) types.Bool {
	if value.IsNull() || value.IsUnknown() {
		return types.BoolNull()
	}
	return value
}

func knownBoolOrDefault(value types.Bool, fallback bool) types.Bool {
	if value.IsNull() || value.IsUnknown() {
		return types.BoolValue(fallback)
	}
	return value
}

func preserveEquivalentString(current types.String, backend string) types.String {
	if backend == "" {
		return knownStringOrNull(current)
	}
	if !current.IsNull() && !current.IsUnknown() && normalizedString(current) == backend {
		return current
	}
	return types.StringValue(backend)
}

func knownInt64OrNull(value types.Int64) types.Int64 {
	if value.IsNull() || value.IsUnknown() {
		return types.Int64Null()
	}
	return value
}
