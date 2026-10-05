package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	blockstoragesdk "github.com/viettelcloud-oss/sdks/go/blockstorage"
	"github.com/viettelcloud-oss/sdks/go/core"
	networksdk "github.com/viettelcloud-oss/sdks/go/network"
	projectsdk "github.com/viettelcloud-oss/sdks/go/project"
	serversdk "github.com/viettelcloud-oss/sdks/go/server"

	"github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/parse"
	blockstoragelookup "github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/provider/blockstorage/lookup"
	networklookup "github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/provider/network/lookup"
	projectlookup "github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/provider/project/lookup"
	"github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/provider/providerdata"
	serverlookup "github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/provider/server/lookup"
	"github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/wait"
)

const (
	serverOperationTimeout = 30 * time.Minute
	serverPollInterval     = 10 * time.Second
	// transientBackendErrorCode identifies a backend refusal that may succeed
	// when retried after the upstream service recovers.
	transientBackendErrorCode = "provider_error"
)

// elasticIPAttachTimeout bounds retries after a server-side attach refusal.
// It is shorter than serverOperationTimeout because an attach still refused
// minutes later is a standing problem rather than an operation in flight.
const elasticIPAttachTimeout = 2 * time.Minute

// privateIPDeleteInterval and privateIPDeleteTimeout bound private IP cleanup.
// This network operation uses a shorter interval and timeout than server
// lifecycle operations.
const (
	privateIPDeleteInterval = 2 * time.Second
	privateIPDeleteTimeout  = 5 * time.Minute
)

// volumeDetachInterval and volumeDetachTimeout bound the wait for a deleted
// server's volumes to leave in-use. The backend detaches them in the background
// after the server is gone, so this covers a detach, not a server operation.
const (
	volumeDetachInterval = 5 * time.Second
	volumeDetachTimeout  = 5 * time.Minute
)

var (
	_ resource.Resource                   = &ServerResource{}
	_ resource.ResourceWithImportState    = &ServerResource{}
	_ resource.ResourceWithValidateConfig = &ServerResource{}
	_ resource.ResourceWithModifyPlan     = &ServerResource{}
)

type ServerResource struct {
	client             *serversdk.Client
	blockStorageClient *blockstoragesdk.Client
	networkClient      *networksdk.Client
	projectClient      *projectsdk.Client
	projectID          core.UUID
}

// serverUpdateRequest carries every mutation an update applies, already built
// and validated, so the mutating sequence performs no further plan decoding.
type serverUpdateRequest struct {
	patchServer       bool
	patchBody         serversdk.ServerPartialUpdateSchema
	rebuild           bool
	rebuildBody       serversdk.ServerRebuildSchema
	rebuiltImageID    core.UUID
	resize            bool
	resizeBody        serversdk.ServerResizeSchema
	updateBandwidth   bool
	bandwidth         int
	replaceGroups     bool
	securityGroupBody serversdk.ServerUpdateSecurityGroupSchema
	attachments       attachmentUpdatePlan
	changePowerState  bool
	desiredPowerState types.String
	priorPowerState   types.String
}

type privateIPUpdatePlan struct {
	expectedCount int
	desiredIDs    map[core.UUID]struct{}
	removedIDs    map[core.UUID]struct{}
	deletedIDs    []core.UUID
	existingIDs   []core.UUID
	subnetIDs     []core.UUID
}

type elasticIPUpdatePlan struct {
	expectedCount int
	desiredIDs    map[core.UUID]struct{}
	removedIDs    map[core.UUID]struct{}
	deletedIDs    []core.UUID
	existingIDs   []core.UUID
	newConfigs    []ElasticIPInputModel
	regionID      core.UUID
}

type bootVolumeUpdatePlan struct {
	resizeID   core.UUID
	resizeSize int
}

// dataVolumeUpdatePlan carries only resizes because the backend has no attach
// or detach volume operation yet. Add desired and removed sets here when it
// does, the way privateIPUpdatePlan already reconciles private_ips.
// resizeIDs and resizeSizes are index-aligned and always appended together.
type dataVolumeUpdatePlan struct {
	resizeIDs   []core.UUID
	resizeSizes []int
}

type attachmentUpdatePlan struct {
	private    *privateIPUpdatePlan
	elastic    *elasticIPUpdatePlan
	boot       *bootVolumeUpdatePlan
	dataVolume *dataVolumeUpdatePlan
}

// privateIPStatePlanModifier and elasticIPStatePlanModifier carry the computed
// half of an unchanged attachment forward from state. Both lists mix
// practitioner configuration with values only the backend can supply. A plan
// must therefore keep the computed values that an unchanged attachment already
// has, and leave the rest unknown. Otherwise Terraform either reports drift on
// every plan or promises values the apply cannot honor.
type (
	privateIPStatePlanModifier struct{}
	elasticIPStatePlanModifier struct{}
)

// zoneResolveFunc and subnetResolveFunc resolve the human-readable references
// that a create request carries to SDK IDs. Injection lets request builders run
// without a live client, as with the lookup packages' resolve functions.
type (
	zoneResolveFunc   func(context.Context, string) (projectsdk.ProjectZoneSchema, error)
	subnetResolveFunc func(context.Context, string) (networksdk.SubnetSchema, error)
)

func NewServerResource() resource.Resource {
	return &ServerResource{}
}

func (r *ServerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server"
}

func (r *ServerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage a Viettel Cloud server.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Description: "Server ID (UUID).",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Server name.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Optional server description. Omitting it preserves the backend value; set an empty string to clear it.",
			},
			"boot": schema.SingleNestedAttribute{
				Required:    true,
				Description: "Boot configuration. Supported boot types are image, custom_image, local_disk, and volume.",
				Attributes: map[string]schema.Attribute{
					"boot_type": schema.StringAttribute{
						Required: true, Description: "Boot source type.",
						PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
					},
					"image": schema.StringAttribute{
						Optional:    true,
						Description: "Exact image name. Required for image and local_disk boot types.",
					},
					"custom_image_id": schema.StringAttribute{Optional: true, Description: "Custom image ID (UUID)."},
					"volume_id": schema.StringAttribute{
						Optional: true, Description: "Existing boot volume ID (UUID).",
						PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
					},
					"volume_type": schema.StringAttribute{
						Optional:      true,
						Description:   "Exact volume type name. Required for image and custom_image boot types.",
						PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
					},
					"volume_size": schema.Int64Attribute{
						Optional: true, Description: "Server-created boot volume size in GB. Updates can only increase it.",
					},
					"iops": schema.Int64Attribute{
						Optional: true, Computed: true,
						Description: "Requested boot volume IOPS. The backend allocates a value when it is omitted.",
						// The backend allocates IOPS from the volume type when the
						// attribute is omitted, so an omitted value must keep the
						// allocated state instead of planning a replacement.
						PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown(), int64planmodifier.RequiresReplaceIfConfigured()},
					},
					"delete_on_termination": schema.BoolAttribute{
						Optional: true,
						Computed: true,
						Description: "Delete the boot volume after deleting the server. Defaults to true for boot types " +
							"image and custom_image, and to false for boot type volume.",
						PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
					},
				},
			},
			"flavor": schema.SingleNestedAttribute{
				Required:    true,
				Description: "Predefined or custom server flavor.",
				Attributes: map[string]schema.Attribute{
					"kind": schema.StringAttribute{Required: true, Description: "Flavor kind: predefined or custom."},
					"name": schema.StringAttribute{Optional: true, Description: "Exact predefined flavor name."},
					"family": schema.StringAttribute{
						Optional:    true,
						Description: "Custom flavor family: basic, premium, enterprise, gpu, or spot.",
					},
					"vcpus": schema.Int64Attribute{Optional: true, Description: "Custom flavor virtual CPU count."},
					"ram":   schema.Int64Attribute{Optional: true, Description: "Custom flavor RAM in GiB."},
				},
			},
			"zone": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Exact project zone name.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"key_pair_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Key pair ID (UUID) associated with the server.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"key_pair_name": schema.StringAttribute{
				Computed:    true,
				Description: "Key pair name associated with the server.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"placement_group_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Placement group ID (UUID) associated with the server.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"placement_group_name": schema.StringAttribute{
				Computed:    true,
				Description: "Placement group name associated with the server.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"data_volumes": schema.ListNestedAttribute{
				Optional:    true,
				Description: "Data volumes created and attached with the server.",
				// Attribute modifiers handle changes within each volume.
				// The list modifier handles count changes because an added or
				// removed element may leave the remaining attributes unchanged.
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplaceIf(
						dataVolumeCountChanged,
						"Replaces the server when the number of data volumes changes.",
						"Replaces the server when the number of data volumes changes.",
					),
				},
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					// The ID identifies which backend volume an element owns, so
					// a resize extends the volume the practitioner configured
					// rather than whichever attachment the server happens to
					// report at that position.
					"id": schema.StringAttribute{
						Computed: true, Description: "Attached data volume ID (UUID).",
						PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
					},
					"volume_type": schema.StringAttribute{
						Required: true, Description: "Exact volume type name.",
						PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
					},
					"volume_size": schema.Int64Attribute{
						Required: true, Description: "Server-created data volume size in GB. Updates can only increase it.",
					},
					"iops": schema.Int64Attribute{
						Optional: true, Computed: true,
						Description: "Requested volume IOPS. The backend allocates a value when it is omitted.",
						// Same contract as boot.iops: the backend allocates
						// IOPS from the volume type when the attribute is
						// omitted, so an omitted value keeps the allocated
						// state instead of planning a replacement.
						PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown(), int64planmodifier.RequiresReplaceIfConfigured()},
					},
					"delete_on_termination": schema.BoolAttribute{
						Optional: true,
						Computed: true,
						Description: "Delete this data volume after deleting the server. Defaults to true. Removing it " +
							"from configuration preserves the last applied value instead of restoring the default.",
						PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
					},
				}},
			},
			"private_ips": schema.ListNestedAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Private IP attachments. kind subnet creates an IP; kind ip attaches an existing IP.",
				PlanModifiers: []planmodifier.List{
					preservePrivateIPState(),
				},
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"kind": schema.StringAttribute{
						Required:    true,
						Description: "Attachment kind: subnet or ip.",
					},
					"id": schema.StringAttribute{
						Optional:    true,
						Computed:    true,
						Description: "Private IP ID. Required when kind is ip; computed when kind is subnet.",
					},
					"subnet_id": schema.StringAttribute{
						Optional:    true,
						Description: "Subnet ID. Configure exactly one of subnet_id or subnet_cidr when kind is subnet.",
					},
					"subnet_cidr": schema.StringAttribute{
						Optional:    true,
						Description: "Exact subnet CIDR. Configure exactly one of subnet_id or subnet_cidr when kind is subnet.",
					},
					"delete_on_termination": schema.BoolAttribute{
						Optional: true,
						Computed: true,
						Description: "Delete this private IP after deleting the server or removing the attachment from configuration. " +
							"Defaults to true for kind subnet and false for kind ip.",
					},
					"ip_address":  schema.StringAttribute{Computed: true},
					"mac_address": schema.StringAttribute{Computed: true},
				}},
			},
			"elastic_ips": schema.ListNestedAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Elastic IP attachments. kind existing attaches an IP; kind new creates an IP.",
				PlanModifiers: []planmodifier.List{
					preserveElasticIPState(),
				},
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"kind": schema.StringAttribute{
						Required:    true,
						Description: "Attachment kind: existing or new.",
					},
					"id": schema.StringAttribute{
						Optional:    true,
						Computed:    true,
						Description: "Elastic IP ID. Required when kind is existing; computed when kind is new.",
					},
					"enable_ipv4": schema.BoolAttribute{
						Optional:    true,
						Description: "Enable IPv4 when kind is new. Defaults to true.",
					},
					"enable_ipv6": schema.BoolAttribute{
						Optional:    true,
						Description: "Enable IPv6 when kind is new. Defaults to false.",
					},
					"delete_on_termination": schema.BoolAttribute{
						Optional: true,
						Computed: true,
						Description: "Delete this elastic IP after deleting the server or removing the attachment from configuration. " +
							"Defaults to true for kind new and false for kind existing.",
					},
					"ip_address":   schema.StringAttribute{Computed: true},
					"ipv6_address": schema.StringAttribute{Computed: true},
					"status":       schema.StringAttribute{Computed: true},
				}},
			},
			"user_data": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Sensitive:   true,
				Description: "Cloud-init user data.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"bandwidth": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				Description: "Server bandwidth in Mbps. The backend enforces the supported range and rejects a " +
					"value below its minimum, so omit it to accept the backend default.",
			},
			"security_group_ids": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Description: "Security group IDs attached to the server. Configure an empty set to detach all security groups.",
			},
			"quantity": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(1),
				Description: "Number of servers to create. A Terraform server resource manages exactly one server, so only 1 is supported.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"power_state": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Desired power state. Supported values are running and shutdown.",
			},
			"status":          schema.StringAttribute{Computed: true, Description: "Current lifecycle status."},
			"server_type":     schema.StringAttribute{Computed: true, Description: "Backend server type."},
			"data_volume_ids": schema.ListAttribute{Computed: true, ElementType: types.StringType, Description: "Attached data volume IDs."},
			"created_at":      schema.StringAttribute{Computed: true, Description: "Creation timestamp (RFC3339)."},
		},
	}
}

func (r *ServerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(*providerdata.Configured)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("Expected *providerdata.Configured, got %T", req.ProviderData))
		return
	}
	r.client = data.Server
	r.blockStorageClient = data.BlockStorage
	r.networkClient = data.Network
	r.projectClient = data.Project
	r.projectID = data.ProjectID
}

// ValidateConfig reports the structural mistakes a practitioner can make in a
// server configuration while Terraform is still planning. Create and Update
// repeat these checks because a value can be unknown until apply, but reporting
// them here avoids creating or mutating a server that cannot succeed.
func (r *ServerResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config ServerResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateServerQuantity(config.Quantity, &resp.Diagnostics)
	validateServerPowerState(config.PowerState, &resp.Diagnostics)
	validateServerFlavor(ctx, config.Flavor, &resp.Diagnostics)
	validateServerBoot(ctx, config.Boot, &resp.Diagnostics)
	validateServerPrivateIPs(ctx, config.PrivateIPs, &resp.Diagnostics)
	validateServerElasticIPs(ctx, config.ElasticIPs, &resp.Diagnostics)
	validateServerDataVolumes(ctx, config.DataVolumes, &resp.Diagnostics)
	validateServerUUID(config.KeyPairID, path.Root("key_pair_id"), "key_pair_id", &resp.Diagnostics)
	validateServerUUID(config.PlacementGroupID, path.Root("placement_group_id"), "placement_group_id", &resp.Diagnostics)
}

// ModifyPlan removes computed-only updates after the attribute modifiers have
// restored unchanged attachment and volume state. Import can initially propose
// different computed values, causing Framework to mark all runtime outputs
// unknown before those modifiers run. Preserve them only when restoring the
// unconfigured computed outputs makes the entire plan identical to refreshed
// state. Any configured change or unknown input keeps the normal update plan.
func (r *ServerResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() || !req.Config.Raw.IsFullyKnown() || len(resp.RequiresReplace) > 0 {
		return
	}
	candidate := req.Plan
	for name, attribute := range req.Plan.Schema.GetAttributes() {
		if !attribute.IsComputed() {
			continue
		}
		p := path.Root(name)
		var configured, planned, prior attr.Value
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, p, &configured)...)
		resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, p, &planned)...)
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, p, &prior)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if configured.IsNull() && planned.IsUnknown() {
			resp.Diagnostics.Append(candidate.SetAttribute(ctx, p, prior)...)
		}
	}
	if !resp.Diagnostics.HasError() && candidate.Raw.Equal(req.State.Raw) {
		resp.Plan = candidate
	}
}

func (r *ServerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ServerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	desiredPowerState := plan.PowerState
	resp.Diagnostics.Append(validateDesiredPowerState(desiredPowerState)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Keep the planned attachment lists before populateServerPendingState
	// rewrites them. It turns a list that the practitioner left unset into a known
	// empty one, which would otherwise read as "the plan fixed zero
	// attachments".
	plannedPrivateIPs, plannedElasticIPs := plan.PrivateIPs, plan.ElasticIPs

	body, diags := r.buildCreateBody(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := r.client.CreateServer(ctx, serversdk.CreateServerParams{ProjectID: r.projectID}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating server", err.Error())
		return
	}
	if created == nil || len(created.ServerIds) != 1 {
		resp.Diagnostics.AddError("Error creating server", "The API must return exactly one server ID.")
		return
	}

	serverID := created.ServerIds[0]
	resp.Diagnostics.Append(populateServerPendingState(ctx, &plan, serverID)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.waitUntilReady(ctx, serverID); err != nil {
		resp.Diagnostics.AddError(
			"Error waiting for server to be ready",
			fmt.Sprintf("%s\n\nServer ID %s was preserved in Terraform state to avoid creating an untracked duplicate.", err, serverID),
		)
		return
	}
	if isDesiredPowerState(desiredPowerState, serversdk.ServerPowerStateShutdown) {
		if err := r.requestServerOperation(ctx, serverID, func(ctx context.Context) error {
			return r.client.StopServer(ctx, serverID, serversdk.StopServerParams{ProjectID: r.projectID})
		}); err != nil {
			resp.Diagnostics.AddError("Error stopping server after creation", err.Error())
			return
		}
		if _, err := r.waitUntilShutdown(ctx, serverID); err != nil {
			resp.Diagnostics.AddError("Error waiting for server shutdown after creation", err.Error())
			return
		}
	}
	server, err := r.waitUntilAttachmentsSettled(ctx, serverID, plannedPrivateIPs, plannedElasticIPs)
	if err != nil {
		resp.Diagnostics.AddError("Error reading server after creation", err.Error())
		return
	}
	if server == nil {
		resp.Diagnostics.AddError("Error reading server after creation", emptyServerResponse(serverID))
		return
	}
	resp.Diagnostics.Append(r.refreshServerResourceState(ctx, server, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ServerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ServerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	serverID, diags := parse.UUIDString(state.ID, "id")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	server, err := r.get(ctx, serverID)
	if errors.Is(err, serversdk.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if isTransientEnumError(err) {
		// The backend reports a status this provider cannot map, so there is
		// nothing to refresh into. Keeping the last known state lets the
		// practitioner still plan a replacement or destroy the server instead
		// of being blocked by every refresh.
		resp.Diagnostics.AddWarning(
			"Unable to refresh server state",
			fmt.Sprintf(
				"The API reported a status this provider does not support, so Terraform kept the last known state for server %s: %s",
				serverID, err,
			),
		)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading server", err.Error())
		return
	}
	// A deleted server can still be readable for a while, so treat that status
	// like a missing server instead of refreshing unusable state.
	if server == nil || server.Status == serversdk.ServerStatusDeleted {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(r.refreshServerResourceState(ctx, server, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ServerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ServerResourceModel
	var state ServerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	serverID, diags := parse.UUIDString(state.ID, "id")
	resp.Diagnostics.Append(diags...)
	changePowerState := powerStateChanged(plan, state)
	if changePowerState {
		resp.Diagnostics.Append(validateDesiredPowerState(plan.PowerState)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	body, changed, updateDiags := buildServerUpdateBody(plan, state)
	resp.Diagnostics.Append(updateDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	bootChanged, bootDiags := bootSourceChanged(ctx, plan, state)
	resp.Diagnostics.Append(bootDiags...)
	var rebuildBody serversdk.ServerRebuildSchema
	var expectedImageID core.UUID
	if bootChanged && !resp.Diagnostics.HasError() {
		imageFinder := serverlookup.NewImageFinder(r.client, r.projectID)
		rebuildBody, expectedImageID, bootDiags = buildServerRebuildBody(ctx, plan.Boot, r.projectID, imageFinder.Resolve)
		resp.Diagnostics.Append(bootDiags...)
	}
	needsResize, flavorDiags := flavorChanged(ctx, plan, state)
	resp.Diagnostics.Append(flavorDiags...)
	var resizeBody serversdk.ServerResizeSchema
	if needsResize && !resp.Diagnostics.HasError() {
		resizeBody, flavorDiags = r.buildServerResizeBody(ctx, serverID, plan.Flavor)
		resp.Diagnostics.Append(flavorDiags...)
	}
	var securityGroupBody serversdk.ServerUpdateSecurityGroupSchema
	if securityGroupsChanged(plan, state) && !resp.Diagnostics.HasError() {
		securityGroupBody, diags = buildSecurityGroupUpdateBody(ctx, plan.SecurityGroupIDs)
		resp.Diagnostics.Append(diags...)
	}
	attachmentPlan, attachmentDiags := r.buildAttachmentUpdatePlan(ctx, serverID, plan, state)
	resp.Diagnostics.Append(attachmentDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Every request below mutates the server, so the refresh must run even when
	// one step fails. Otherwise Terraform keeps prior state for the changes that
	// already succeeded and reports drift that no longer exists.
	resp.Diagnostics.Append(r.applyServerUpdate(ctx, serverID, serverUpdateRequest{
		patchServer:       changed,
		patchBody:         body,
		rebuild:           bootChanged,
		rebuildBody:       rebuildBody,
		rebuiltImageID:    expectedImageID,
		resize:            needsResize,
		resizeBody:        resizeBody,
		updateBandwidth:   bandwidthChanged(plan, state),
		bandwidth:         int(plan.Bandwidth.ValueInt64()),
		replaceGroups:     securityGroupsChanged(plan, state),
		securityGroupBody: securityGroupBody,
		attachments:       attachmentPlan,
		changePowerState:  changePowerState,
		desiredPowerState: plan.PowerState,
		priorPowerState:   state.PowerState,
	})...)

	// An attach or detach can lag the update response, so the refresh has to
	// run against a server whose attachments already match the plan. A failed
	// step above may never reach that count. Waiting it out would replace the
	// real failure with a timeout, so refresh against the current response and
	// let the diagnostics already collected stand.
	var server *serversdk.ServerDetailSchema
	var err error
	if resp.Diagnostics.HasError() {
		server, err = r.get(ctx, serverID)
	} else {
		server, err = r.waitUntilAttachmentsSettled(ctx, serverID, plan.PrivateIPs, plan.ElasticIPs)
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading server after update", err.Error())
		return
	}
	if server == nil {
		resp.Diagnostics.AddError("Error reading server after update", emptyServerResponse(serverID))
		return
	}
	// Update never changes the number of server-owned data volumes. Carry their
	// stable order into refresh because computed plan values are otherwise
	// unknown during any update and the server API can reorder attachments.
	plan.DataVolumeIDs = state.DataVolumeIDs
	resp.Diagnostics.Append(r.refreshServerResourceState(ctx, server, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ServerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ServerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	serverID, diags := parse.UUIDString(state.ID, "id")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The volume attachments are only readable while the server exists, so the
	// cascade targets are resolved before deletion and deleted afterwards.
	var volumeIDs []core.UUID
	server, err := r.get(ctx, serverID)
	switch {
	case errors.Is(err, serversdk.ErrNotFound):
	case isTransientEnumError(err):
		// A server in a status this provider cannot map, such as one that failed
		// to build, must still be destroyable. Skip the state refresh and the
		// shutdown and delete it directly.
		resp.Diagnostics.Append(r.deleteServer(ctx, serverID, nil)...)
	case err != nil:
		resp.Diagnostics.AddError("Error reading server before deletion", err.Error())
	case server != nil:
		resp.Diagnostics.Append(populateServerResourceState(ctx, server, &state)...)
		targets, targetDiags := serverVolumeCascadeTargets(ctx, state, server)
		resp.Diagnostics.Append(targetDiags...)
		if !resp.Diagnostics.HasError() {
			volumeIDs = targets
			// Report a shutdown failure only when deletion also fails, because
			// then it explains why deletion was rejected.
			resp.Diagnostics.Append(r.deleteServer(ctx, serverID, r.stopBeforeDeletion(ctx, serverID, server))...)
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}
	r.deleteTerminatedAttachments(ctx, state, &resp.Diagnostics)
	r.deleteTerminatedVolumes(ctx, volumeIDs, &resp.Diagnostics)
}

func (r *ServerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// --- Server lifecycle: fetch, waiters and deletion ---

func (r *ServerResource) get(ctx context.Context, id core.UUID) (*serversdk.ServerDetailSchema, error) {
	return r.client.GetServer(ctx, id, serversdk.GetServerParams{ProjectID: r.projectID})
}

func (r *ServerResource) waitUntilReady(ctx context.Context, id core.UUID) error {
	return r.waitForServer(ctx, id, "ready", func(server *serversdk.ServerDetailSchema) (bool, error) {
		if server.PowerState == serversdk.ServerPowerStateRunning && !serverIsBusy(server) {
			return true, nil
		}
		if server.Status == serversdk.ServerStatusDeleted {
			return false, fmt.Errorf("server %s entered terminal status %s", id, server.Status)
		}
		// A server can report a stopped status while it is still building, so a
		// non-running status is terminal only once provisioning has finished.
		if !serverIsBusy(server) && (server.Status == serversdk.ServerStatusShutoff ||
			server.Status == serversdk.ServerStatusStopped || server.Status == serversdk.ServerStatusSuspended) {
			return false, fmt.Errorf("server %s entered terminal status %s", id, server.Status)
		}
		return false, nil
	})
}

func (r *ServerResource) waitUntilShutdown(ctx context.Context, id core.UUID) (*serversdk.ServerDetailSchema, error) {
	server, err := r.pollServer(ctx, id, "shutdown", func(server *serversdk.ServerDetailSchema) (bool, error) {
		return serverIsShutdown(server) && !serverIsBusy(server), nil
	})
	if errors.Is(err, serversdk.ErrNotFound) {
		return nil, nil
	}
	return server, err
}

func (r *ServerResource) waitUntilNotPending(ctx context.Context, id core.UUID) (*serversdk.ServerDetailSchema, error) {
	return r.pollServer(ctx, id, "power state to leave pending", func(server *serversdk.ServerDetailSchema) (bool, error) {
		return server.PowerState != serversdk.ServerPowerStatePending, nil
	})
}

func (r *ServerResource) waitUntilBandwidth(ctx context.Context, id core.UUID, expected int) error {
	return r.waitForServer(ctx, id, "bandwidth update", func(server *serversdk.ServerDetailSchema) (bool, error) {
		return server.Bandwidth != nil && *server.Bandwidth == expected && !serverIsBusy(server), nil
	})
}

// waitUntilAttachmentsSettled waits for a non-busy server with the planned
// attachment counts and allocated addresses. Detail responses can lag updates;
// refreshing too early can produce a list length that violates the plan.
// Null or unknown lists impose no count or address requirement. This waiter
// does not compare attachment IDs.
func (r *ServerResource) waitUntilAttachmentsSettled(
	ctx context.Context,
	id core.UUID,
	privateIPs types.List,
	elasticIPs types.List,
) (*serversdk.ServerDetailSchema, error) {
	privateIPsFixed := !privateIPs.IsNull() && !privateIPs.IsUnknown()
	elasticIPsFixed := !elasticIPs.IsNull() && !elasticIPs.IsUnknown()
	wantPrivateIPs, wantElasticIPs := len(privateIPs.Elements()), len(elasticIPs.Elements())

	gotPrivateIPs, gotElasticIPs := 0, 0
	addressesPending := false
	result, err := r.pollServer(ctx, id, "attachments to settle", func(server *serversdk.ServerDetailSchema) (bool, error) {
		gotPrivateIPs, gotElasticIPs = len(server.PrivateIps), len(server.ElasticIps)
		privateIPsReady := gotPrivateIPs == wantPrivateIPs && privateIPAddressesAssigned(server.PrivateIps)
		elasticIPsReady := gotElasticIPs == wantElasticIPs && elasticIPAddressesAssigned(server.ElasticIps)
		addressesPending = (privateIPsFixed && !privateIPAddressesAssigned(server.PrivateIps)) ||
			(elasticIPsFixed && !elasticIPAddressesAssigned(server.ElasticIps))
		if serverIsBusy(server) ||
			(privateIPsFixed && !privateIPsReady) ||
			(elasticIPsFixed && !elasticIPsReady) {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		if addressesPending {
			return nil, fmt.Errorf(
				"%w (the API reported %d private IPs and %d elastic IPs, and at least one of them still has no address)",
				err, gotPrivateIPs, gotElasticIPs,
			)
		}
		return nil, fmt.Errorf(
			"%w (the API reported %d private IPs and %d elastic IPs)",
			err, gotPrivateIPs, gotElasticIPs,
		)
	}
	return result, nil
}

// waitForServer polls the server until settled reports the operation finished.
// The poll function classifies its own outcomes for the shared waiter: a
// missing server is fatal; other read failures are reported as not-ready.
// A single slow response, or a status this provider
// cannot map, must not abandon an operation that is still running. The reason
// the server was not ready surfaces in the timeout error.
func (r *ServerResource) waitForServer(
	ctx context.Context,
	id core.UUID,
	operation string,
	settled func(*serversdk.ServerDetailSchema) (bool, error),
) error {
	_, err := r.pollServer(ctx, id, operation, settled)
	return err
}

// pollServer returns the response from the successful poll so callers can use
// the settled values without another read.
func (r *ServerResource) pollServer(
	ctx context.Context,
	id core.UUID,
	operation string,
	settled func(*serversdk.ServerDetailSchema) (bool, error),
) (*serversdk.ServerDetailSchema, error) {
	w := wait.New[*serversdk.ServerDetailSchema](serverPollInterval, serverOperationTimeout)
	server, err := w.WaitFor(ctx, func(ctx context.Context) (*serversdk.ServerDetailSchema, error) {
		server, err := r.get(ctx, id)
		switch {
		case errors.Is(err, serversdk.ErrNotFound):
			return nil, err
		case err != nil:
			return nil, fmt.Errorf("poll server %s: %s: %w", id, err, wait.ErrNotReady)
		case server == nil:
			return nil, fmt.Errorf("the API returned an empty response for server %s: %w", id, wait.ErrNotReady)
		}
		complete, completeErr := settled(server)
		if completeErr != nil {
			return nil, completeErr
		}
		if !complete {
			return nil, fmt.Errorf("server %s has not finished %s yet: %w", id, operation, wait.ErrNotReady)
		}
		return server, nil
	})
	if err != nil {
		return nil, fmt.Errorf("waiting for server %s %s: %w", id, operation, err)
	}
	return server, nil
}

// waitUntilDeleted treats a missing server or one with deleted status as gone.
// A read that fails for any other reason is
// reported as not-ready so a server that is still being torn down is not
// abandoned mid-deletion.
func (r *ServerResource) waitUntilDeleted(ctx context.Context, id core.UUID) error {
	w := wait.New[struct{}](serverPollInterval, serverOperationTimeout)
	_, err := w.WaitFor(ctx, func(ctx context.Context) (struct{}, error) {
		server, err := r.get(ctx, id)
		switch {
		case errors.Is(err, serversdk.ErrNotFound):
			return struct{}{}, nil
		case err != nil:
			return struct{}{}, fmt.Errorf("poll server %s: %s: %w", id, err, wait.ErrNotReady)
		case server != nil && server.Status == serversdk.ServerStatusDeleted:
			return struct{}{}, nil
		}
		return struct{}{}, fmt.Errorf("server %s is not deleted yet: %w", id, wait.ErrNotReady)
	})
	if err != nil {
		return fmt.Errorf("waiting for server %s deletion: %w", id, err)
	}
	return nil
}

func (r *ServerResource) buildCreateBody(ctx context.Context, plan ServerResourceModel) (serversdk.ServerCreateSchema, diag.Diagnostics) {
	zoneFinder := projectlookup.NewZoneFinder(r.projectClient, r.projectID)
	flavorFinder := serverlookup.NewFlavorFinder(r.client, r.projectID)
	imageFinder := serverlookup.NewImageFinder(r.client, r.projectID)
	volumeTypeFinder := blockstoragelookup.NewVolumeTypeFinder(r.blockStorageClient)
	subnetFinder := networklookup.NewSubnetFinder(r.networkClient, r.projectID)
	return buildServerCreateBody(
		ctx,
		plan,
		func(ctx context.Context, name string) (projectsdk.ProjectZoneSchema, error) {
			return zoneFinder.Resolve(ctx, projectlookup.ZoneFilter{Name: &name})
		},
		flavorFinder.Resolve,
		imageFinder.Resolve,
		volumeTypeFinder.ResolveForCreate,
		func(ctx context.Context, cidr string) (networksdk.SubnetSchema, error) {
			return subnetFinder.Resolve(ctx, networklookup.SubnetFilter{CIDR: &cidr})
		},
	)
}

func (r *ServerResource) deleteTerminatedAttachments(ctx context.Context, state ServerResourceModel, diags *diag.Diagnostics) {
	privateIPIDs, elasticIPIDs, valueDiags := serverCascadeTargets(ctx, state)
	diags.Append(valueDiags...)
	if diags.HasError() {
		return
	}
	diags.Append(r.deletePrivateIPs(ctx, privateIPIDs)...)
	for _, id := range elasticIPIDs {
		if err := r.networkClient.DeleteElasticIp(ctx, id, networksdk.DeleteElasticIpParams{ProjectID: r.projectID}); err != nil && !errors.Is(err, networksdk.ErrNotFound) {
			diags.AddError("Error deleting server elastic IP", fmt.Sprintf("Unable to delete elastic IP %s: %s", id, err))
		}
	}
}

// deleteTerminatedVolumes deletes the volumes a destroyed server left behind,
// which the backend keeps. Each target is polled first because the attachment
// is released in the background and an in-use volume cannot be deleted.
func (r *ServerResource) deleteTerminatedVolumes(ctx context.Context, ids []core.UUID, diags *diag.Diagnostics) {
	for _, id := range ids {
		if err := r.waitUntilVolumeDetached(ctx, id); err != nil {
			diags.AddError("Error waiting for server volume to detach", err.Error())
			continue
		}
		err := r.blockStorageClient.DeleteVolume(ctx, id, blockstoragesdk.DeleteVolumeParams{ProjectID: r.projectID})
		if err != nil && !errors.Is(err, blockstoragesdk.ErrNotFound) {
			diags.AddError("Error deleting server volume", fmt.Sprintf("Unable to delete volume %s: %s", id, err))
		}
	}
}

// waitUntilVolumeDetached waits for a volume to become deletable: until the
// backend finishes detaching it, a delete is rejected with "Volume with status
// in-use is not allowed to delete". A read that fails is reported as not-ready
// so a detach still in progress is not abandoned; an abandoned detach ends the
// wait, because the volume will not become available on its own.
func (r *ServerResource) waitUntilVolumeDetached(ctx context.Context, id core.UUID) error {
	w := wait.New[struct{}](volumeDetachInterval, volumeDetachTimeout)
	_, err := w.WaitFor(ctx, func(ctx context.Context) (struct{}, error) {
		volume, err := r.blockStorageClient.GetVolume(ctx, id, blockstoragesdk.GetVolumeParams{ProjectID: r.projectID})
		switch {
		case errors.Is(err, blockstoragesdk.ErrNotFound):
			return struct{}{}, nil
		case err != nil:
			return struct{}{}, fmt.Errorf("poll volume %s: %s: %w", id, err, wait.ErrNotReady)
		case volume == nil:
			return struct{}{}, fmt.Errorf("the API returned an empty response for volume %s: %w", id, wait.ErrNotReady)
		case volumeDeletable(volume.Status):
			return struct{}{}, nil
		case volumeDetachFailed(volume.Status):
			return struct{}{}, fmt.Errorf("volume %s stopped detaching and settled in status %q", id, volume.Status)
		}
		return struct{}{}, fmt.Errorf("volume %s is still attached (status=%s): %w", id, volume.Status, wait.ErrNotReady)
	})
	if err != nil {
		return fmt.Errorf("waiting for volume %s to detach: %w", id, err)
	}
	return nil
}

// volumeDeletable reports the statuses that end the detach wait. Available is
// the status the API accepts a deletion in; deleting means the backend already
// removed the volume, leaving the delete call a tolerated no-op.
func volumeDeletable(status blockstoragesdk.VolumeStatus) bool {
	return status == blockstoragesdk.VolumeStatusAvailable || status == blockstoragesdk.VolumeStatusDeleting
}

// volumeDetachFailed reports a detach the backend abandoned. Polling it to the
// deadline would only delay the destroy, and the status names the reason.
func volumeDetachFailed(status blockstoragesdk.VolumeStatus) bool {
	return status == blockstoragesdk.VolumeStatusDetachFailed || status == blockstoragesdk.VolumeStatusFailed
}

func (r *ServerResource) deleteServer(ctx context.Context, id core.UUID, stopErr error) diag.Diagnostics {
	var diags diag.Diagnostics
	if err := r.requestDeletion(ctx, id); err != nil {
		diags.AddError("Error deleting server", deletionErrorDetail(err, stopErr))
		return diags
	}
	if err := r.waitUntilDeleted(ctx, id); err != nil {
		diags.AddError("Error waiting for server deletion", deletionErrorDetail(err, stopErr))
	}
	return diags
}

// requestDeletion retries a failed delete when the error reports an operation
// in progress or the server detail shows a busy state. This lets deletion wait
// for provisioning to finish. Other failures return immediately.
func (r *ServerResource) requestDeletion(ctx context.Context, id core.UUID) error {
	w := wait.New[struct{}](serverPollInterval, serverOperationTimeout)
	_, err := w.WaitFor(ctx, func(ctx context.Context) (struct{}, error) {
		err := r.client.DeleteServer(ctx, id, serversdk.DeleteServerParams{ProjectID: r.projectID})
		if err == nil || errors.Is(err, serversdk.ErrNotFound) {
			return struct{}{}, nil
		}
		if !serverOperationInProgress(err) && !r.serverIsSettling(ctx, id) {
			return struct{}{}, err
		}
		return struct{}{}, fmt.Errorf("server %s has not accepted deletion yet: %s: %w", id, err, wait.ErrNotReady)
	})
	return err
}

// requestServerOperation waits for the backend's operation lock to be released.
// The detail response can show the target power state while the previous
// operation still holds the lock. Only an explicit busy rejection is replayed;
// transport failures and other conflicts retain the SDK's retry semantics.
func (r *ServerResource) requestServerOperation(ctx context.Context, id core.UUID, request func(context.Context) error) error {
	w := wait.New[struct{}](serverPollInterval, serverOperationTimeout)
	_, err := w.WaitFor(ctx, func(ctx context.Context) (struct{}, error) {
		err := request(ctx)
		if !serverOperationInProgress(err) {
			return struct{}{}, err
		}
		return struct{}{}, fmt.Errorf("server %s is still completing another operation: %s: %w", id, err, wait.ErrNotReady)
	})
	return err
}

// serverIsSettling reports whether a rejected request is worth retrying. An
// unreadable server is not treated as settling, so the rejection surfaces with
// the reason the backend gave instead of after the whole deadline.
func (r *ServerResource) serverIsSettling(ctx context.Context, id core.UUID) bool {
	server, err := r.get(ctx, id)
	if err != nil || server == nil {
		return false
	}
	return serverIsBusy(server)
}

func (r *ServerResource) stopBeforeDeletion(ctx context.Context, id core.UUID, server *serversdk.ServerDetailSchema) error {
	if serverIsShutdown(server) {
		return nil
	}
	if err := r.requestServerOperation(ctx, id, func(ctx context.Context) error {
		return r.client.StopServer(ctx, id, serversdk.StopServerParams{ProjectID: r.projectID})
	}); err != nil {
		if errors.Is(err, serversdk.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("stop server %s: %w", id, err)
	}
	if _, err := r.waitUntilShutdown(ctx, id); err != nil {
		return fmt.Errorf("wait for server %s shutdown: %w", id, err)
	}
	return nil
}

// --- Response-to-state refresh ---

func (r *ServerResource) refreshServerResourceState(
	ctx context.Context,
	server *serversdk.ServerDetailSchema,
	state *ServerResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(r.initializeImportedFlavorState(ctx, server, state)...)
	if diags.HasError() {
		return diags
	}
	diags.Append(r.initializeImportedDataVolumeState(ctx, server, state)...)
	if diags.HasError() {
		return diags
	}
	diags.Append(populateServerResourceState(ctx, server, state)...)
	if diags.HasError() || server == nil {
		return diags
	}
	diags.Append(r.refreshServerBootState(ctx, server, state)...)
	if diags.HasError() {
		return diags
	}
	diags.Append(r.refreshServerDataVolumeState(ctx, server, state)...)
	return diags
}

// refreshServerDataVolumeState reads each managed data volume by its stable ID
// and resolves the default for an unconfigured delete_on_termination. The
// server API can return attachments in a different order between reads, so
// positional configuration must follow data_volume_ids instead.
func (r *ServerResource) refreshServerDataVolumeState(
	ctx context.Context,
	server *serversdk.ServerDetailSchema,
	state *ServerResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics
	if state.DataVolumes.IsNull() || state.DataVolumes.IsUnknown() {
		return diags
	}
	var volumes []DataVolumeInputModel
	diags.Append(state.DataVolumes.ElementsAs(ctx, &volumes, false)...)
	if diags.HasError() {
		return diags
	}
	// An element whose ID is still unknown was just created, so it takes the
	// attachment reported at its own position. Writing every reported value
	// back below turns a position the server did not honor into an
	// inconsistent-result error instead of a silently mismatched volume.
	attached := dataVolumeIDs(server)
	if len(attached) != len(volumes) {
		diags.AddError(
			"Unable to correlate server data volumes",
			fmt.Sprintf("The server state contains %d configured data volumes and %d attached data volumes.", len(volumes), len(attached)),
		)
		return diags
	}
	for i := range volumes {
		if volumes[i].ID.IsNull() || volumes[i].ID.IsUnknown() {
			volumes[i].ID = types.StringValue(attached[i])
		}
		volumes[i].DeleteOnTermination = knownBoolOrDefault(volumes[i].DeleteOnTermination, true)
		id, idDiags := parse.UUIDString(volumes[i].ID, fmt.Sprintf("data_volumes[%d].id", i))
		diags.Append(idDiags...)
		if idDiags.HasError() {
			return diags
		}
		volume, err := r.blockStorageClient.GetVolume(
			ctx,
			id,
			blockstoragesdk.GetVolumeParams{ProjectID: r.projectID},
		)
		if err != nil {
			diags.AddError("Error reading server data volume", fmt.Sprintf("Unable to read data volume %s: %s", id, err))
			return diags
		}
		if volume == nil {
			diags.AddError("Error reading server data volume", fmt.Sprintf("The API returned an empty response for data volume %s.", id))
			return diags
		}
		if volume.CreateFrom.VolumeType != nil {
			volumes[i].VolumeType = preserveEquivalentString(volumes[i].VolumeType, volume.CreateFrom.VolumeType.Name)
		}
		volumes[i].VolumeSize = types.Int64Value(int64(volume.Size))
		if volumes[i].IOPS.IsUnknown() {
			volumes[i].IOPS = volumeIOPSValue(volume)
		}
	}
	state.DataVolumes, diags = types.ListValueFrom(
		ctx,
		types.ObjectType{AttrTypes: dataVolumeResourceAttributeTypes()},
		volumes,
	)
	return diags
}

func (r *ServerResource) initializeImportedDataVolumeState(
	ctx context.Context,
	server *serversdk.ServerDetailSchema,
	state *ServerResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics
	if server == nil || !state.Boot.IsNull() && !state.Boot.IsUnknown() ||
		!state.DataVolumes.IsNull() && !state.DataVolumes.IsUnknown() {
		return diags
	}

	volumes := make([]DataVolumeInputModel, 0)
	for _, attachment := range server.Volumes {
		if attachment.MountAs != "data" {
			continue
		}
		volume, err := r.blockStorageClient.GetVolume(
			ctx,
			attachment.Volume.Id,
			blockstoragesdk.GetVolumeParams{ProjectID: r.projectID},
		)
		if err != nil {
			diags.AddError("Error reading imported server data volume", fmt.Sprintf("Unable to read data volume %s: %s", attachment.Volume.Id, err))
			return diags
		}
		if volume == nil {
			diags.AddError("Error reading imported server data volume", fmt.Sprintf("The API returned an empty response for data volume %s.", attachment.Volume.Id))
			return diags
		}
		if volume.CreateFrom.VolumeType == nil || volume.CreateFrom.VolumeType.Name == "" {
			diags.AddError(
				"Unable to determine imported server data volume type",
				fmt.Sprintf("Data volume %s does not identify its volume type, so Terraform cannot reconstruct data_volumes.", attachment.Volume.Id),
			)
			return diags
		}
		volumes = append(volumes, DataVolumeInputModel{
			VolumeType:          types.StringValue(volume.CreateFrom.VolumeType.Name),
			VolumeSize:          types.Int64Value(int64(volume.Size)),
			IOPS:                volumeIOPSValue(volume),
			DeleteOnTermination: types.BoolValue(true),
		})
	}
	if len(volumes) == 0 {
		state.DataVolumes = types.ListNull(types.ObjectType{AttrTypes: dataVolumeResourceAttributeTypes()})
		return diags
	}
	state.DataVolumes, diags = types.ListValueFrom(
		ctx,
		types.ObjectType{AttrTypes: dataVolumeResourceAttributeTypes()},
		volumes,
	)
	return diags
}

func (r *ServerResource) initializeImportedFlavorState(
	ctx context.Context,
	server *serversdk.ServerDetailSchema,
	state *ServerResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics
	if server == nil || !state.Flavor.IsNull() && !state.Flavor.IsUnknown() {
		return diags
	}
	flavorID := server.Flavor.Id
	candidates, err := serverlookup.NewFlavorFinder(r.client, r.projectID).Find(ctx, serverlookup.FlavorFilter{ID: &flavorID})
	if err != nil {
		diags.AddError("Error identifying imported server flavor", err.Error())
		return diags
	}
	if len(candidates) > 1 {
		diags.AddError("Ambiguous imported server flavor", fmt.Sprintf("Flavor ID %s matched %d catalog entries.", flavorID, len(candidates)))
		return diags
	}
	values := map[string]attr.Value{
		"kind": types.StringValue("custom"), "name": types.StringNull(), "family": types.StringNull(),
		"vcpus": types.Int64Value(int64(server.Flavor.Vcpus)), "ram": types.Int64Value(int64(server.Flavor.Ram)),
	}
	if len(candidates) == 1 {
		values["kind"] = types.StringValue("predefined")
		values["name"] = types.StringValue(candidates[0].Name)
		values["vcpus"] = types.Int64Null()
		values["ram"] = types.Int64Null()
	}
	state.Flavor, diags = types.ObjectValue(flavorAttributeTypes(), values)
	return diags
}

func (r *ServerResource) refreshServerBootState(
	ctx context.Context,
	server *serversdk.ServerDetailSchema,
	state *ServerResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics
	current, hasCurrent := decodeCurrentBoot(ctx, state.Boot, &diags)
	if diags.HasError() {
		return diags
	}

	rootVolumeID, hasRootVolume := serverRootVolumeID(server)
	var rootVolume *blockstoragesdk.VolumeDetailSchema
	// An explicitly configured existing-volume boot is already unambiguous from
	// the server attachment ID and does not depend on volume-origin metadata.
	if hasRootVolume && normalizedString(current.BootType) != "volume" {
		volume, err := r.blockStorageClient.GetVolume(
			ctx,
			rootVolumeID,
			blockstoragesdk.GetVolumeParams{ProjectID: r.projectID},
		)
		if err != nil {
			diags.AddError("Error reading server boot volume", fmt.Sprintf("Unable to read root volume %s: %s", rootVolumeID, err))
			return diags
		}
		if volume == nil {
			diags.AddError("Error reading server boot volume", fmt.Sprintf("The API returned an empty response for root volume %s.", rootVolumeID))
			return diags
		}
		rootVolume = volume
	}

	inferredBootType := inferServerBootType(server, rootVolume)
	bootType := selectServerBootType(current.BootType, inferredBootType, hasRootVolume)
	if bootType == "" {
		diags.AddError("Unable to determine server boot configuration", "The server response contains neither a root volume nor an image.")
		return diags
	}
	if !hasCurrent {
		current = BootInputModel{}
	}
	state.Boot = serverBootStateObject(server, rootVolume, rootVolumeID, bootType, current, &diags)
	return diags
}

// buildBootVolumeResizePlan resolves a changed boot.volume_size against the
// server's root volume, which only the server response identifies. It returns
// nil when the size did not change.
func (r *ServerResource) buildBootVolumeResizePlan(
	ctx context.Context,
	serverID core.UUID,
	plan ServerResourceModel,
	state ServerResourceModel,
) (*bootVolumeUpdatePlan, diag.Diagnostics) {
	var diags diag.Diagnostics
	plannedBoot, hasPlannedBoot := decodeCurrentBoot(ctx, plan.Boot, &diags)
	priorBoot, hasPriorBoot := decodeCurrentBoot(ctx, state.Boot, &diags)
	if diags.HasError() {
		return nil, diags
	}
	if !hasPlannedBoot || !hasPriorBoot || plannedBoot.VolumeSize.Equal(priorBoot.VolumeSize) {
		return nil, diags
	}
	target, ok := increasedVolumeSize(plannedBoot.VolumeSize, priorBoot.VolumeSize, "boot.volume_size", &diags)
	if !ok {
		return nil, diags
	}
	server, err := r.get(ctx, serverID)
	if err != nil {
		diags.AddError("Error reading server before boot volume resize", err.Error())
		return nil, diags
	}
	if server == nil {
		diags.AddError("Error reading server before boot volume resize", emptyServerResponse(serverID))
		return nil, diags
	}
	id, found := serverRootVolumeID(server)
	if !found {
		diags.AddError("Unable to resize boot volume", "The server response does not identify a root volume.")
		return nil, diags
	}
	if !r.resolveVolumeResize(ctx, id, bootVolumeLabel(id), target, &diags) {
		return nil, diags
	}
	return &bootVolumeUpdatePlan{resizeID: id, resizeSize: target}, diags
}

// buildDataVolumeUpdatePlan resolves every changed data_volumes size against
// the volume that element already owns, taken from prior state because the
// planned ID is unknown until the apply finishes. It returns nil when no size
// changed.
func (r *ServerResource) buildDataVolumeUpdatePlan(
	ctx context.Context,
	plan ServerResourceModel,
	state ServerResourceModel,
) (*dataVolumeUpdatePlan, diag.Diagnostics) {
	var diags diag.Diagnostics
	plannedData, plannedDataOK := decodeConfigList[DataVolumeInputModel](ctx, plan.DataVolumes, &diags)
	priorData, priorDataOK := decodeConfigList[DataVolumeInputModel](ctx, state.DataVolumes, &diags)
	if diags.HasError() {
		return nil, diags
	}
	if plannedDataOK != priorDataOK || len(plannedData) != len(priorData) {
		diags.AddError("Unable to resize data volumes", "The planned and prior data volume counts do not match.")
		return nil, diags
	}
	if !plannedDataOK {
		return nil, diags
	}

	result := dataVolumeUpdatePlan{
		resizeIDs:   make([]core.UUID, 0, len(plannedData)),
		resizeSizes: make([]int, 0, len(plannedData)),
	}
	for i := range plannedData {
		if plannedData[i].VolumeSize.Equal(priorData[i].VolumeSize) {
			continue
		}
		label := fmt.Sprintf("data_volumes[%d].volume_size", i)
		target, ok := increasedVolumeSize(plannedData[i].VolumeSize, priorData[i].VolumeSize, label, &diags)
		if !ok {
			continue
		}
		id, idDiags := parse.UUIDString(priorData[i].ID, fmt.Sprintf("data_volumes[%d].id", i))
		diags.Append(idDiags...)
		if idDiags.HasError() {
			continue
		}
		if r.resolveVolumeResize(ctx, id, dataVolumeLabel(id), target, &diags) {
			result.resizeIDs = append(result.resizeIDs, id)
			result.resizeSizes = append(result.resizeSizes, target)
		}
	}
	if diags.HasError() {
		return nil, diags
	}
	if len(result.resizeIDs) == 0 {
		return nil, diags
	}
	return &result, diags
}

// resolveVolumeResize checks target against the volume's current backend size,
// rejecting a shrink before anything is mutated. It reports false when the
// backend already reports target, so an extend is never sent with nothing to do.
func (r *ServerResource) resolveVolumeResize(
	ctx context.Context,
	id core.UUID,
	label string,
	target int,
	diags *diag.Diagnostics,
) bool {
	volume, err := r.blockStorageClient.GetVolume(
		ctx,
		id,
		blockstoragesdk.GetVolumeParams{ProjectID: r.projectID},
	)
	if err != nil {
		diags.AddError("Error reading server volume before resize", fmt.Sprintf("Unable to read %s: %s", label, err))
		return false
	}
	if volume == nil {
		diags.AddError("Error reading server volume before resize", fmt.Sprintf("The API returned an empty response for %s.", label))
		return false
	}
	if target < volume.Size {
		diags.AddError("Invalid server volume size update", fmt.Sprintf("%s can only increase from its current backend size of %d GiB; planned value is %d GiB.", label, volume.Size, target))
		return false
	}
	return target > volume.Size
}

// resizeVolume extends one server-owned volume and waits for its reported size.
func (r *ServerResource) resizeVolume(ctx context.Context, id core.UUID, size int, label string) diag.Diagnostics {
	var diags diag.Diagnostics
	body := blockstoragesdk.VolumeExtendSchema{Size: size}
	if err := r.blockStorageClient.ExtendVolume(
		ctx,
		id,
		blockstoragesdk.ExtendVolumeParams{ProjectID: r.projectID},
		body,
	); err != nil {
		diags.AddError("Error resizing server volume", fmt.Sprintf("Unable to resize %s: %s", label, err))
		return diags
	}
	if err := r.waitUntilVolumeResized(ctx, id, size); err != nil {
		diags.AddError("Error waiting for server volume resize", fmt.Sprintf("%s: %s", label, err))
	}
	return diags
}

// waitUntilVolumeResized waits until the volume reports the new size. A ready
// status is not proof on its own: the extend call only returns "accepted",
// and the volume keeps reporting the status it already had until the resize
// actually starts. A waiter that stops there returns before the new size
// exists, and the caller stores the old one. The reported size is the
// observable that settles. A backend that overshoots the requested size ends
// the wait too, rather than polling to the timeout; the honest value is then
// stored and Terraform reports the mismatch.
func (r *ServerResource) waitUntilVolumeResized(ctx context.Context, id core.UUID, size int) error {
	w := wait.New[*blockstoragesdk.VolumeDetailSchema](serverPollInterval, serverOperationTimeout)
	_, err := w.WaitFor(ctx, func(ctx context.Context) (*blockstoragesdk.VolumeDetailSchema, error) {
		volume, err := r.blockStorageClient.GetVolume(
			ctx,
			id,
			blockstoragesdk.GetVolumeParams{ProjectID: r.projectID},
		)
		if err != nil {
			return nil, fmt.Errorf("poll volume %s: %w", id, err)
		}
		if volume == nil {
			return nil, fmt.Errorf("volume %s returned an empty response", id)
		}
		if serverVolumeStatusReady(volume.Status) && volume.Size >= size {
			return volume, nil
		}
		if serverVolumeStatusFailed(volume.Status) {
			return nil, fmt.Errorf("volume %s entered terminal status %q while resizing to %d GiB", id, volume.Status, size)
		}
		return nil, fmt.Errorf("status=%s, size=%d GiB: %w", volume.Status, volume.Size, wait.ErrNotReady)
	})
	if err != nil {
		return fmt.Errorf("waiting for volume %s to reach %d GiB: %w", id, size, err)
	}
	return nil
}

// --- Update sequencing and per-attribute mutations ---

// applyServerUpdate coordinates all server mutations using a 3-phase pipeline
// to minimize downtime and ensure at most one stop/start power cycle:
// Live updates -> Offline updates -> Power state restoration
func (r *ServerResource) applyServerUpdate(
	ctx context.Context,
	serverID core.UUID,
	req serverUpdateRequest,
) diag.Diagnostics {
	var diags diag.Diagnostics

	wasRunning := powerStateIsRunning(req.priorPowerState)
	wasShutdown := powerStateIsShutdown(req.priorPowerState)
	explicitShutdown := req.changePowerState && isDesiredPowerState(req.desiredPowerState, serversdk.ServerPowerStateShutdown)

	// Rebuild and resize require the server to be in a known operational state.
	// Transient or frozen states (paused, suspended, pending, rescuing) must be
	// resolved by the practitioner before compute or OS disk modifications.
	if !wasRunning && !wasShutdown {
		if req.rebuild {
			diags.AddError(
				"Cannot rebuild server in current power state",
				fmt.Sprintf("Server is currently in %q power state. Rebuild requires the server to be either %q or %q.", req.priorPowerState.ValueString(), serversdk.ServerPowerStateRunning, serversdk.ServerPowerStateShutdown),
			)
			return diags
		}
		if req.resize {
			diags.AddError(
				"Cannot resize server in current power state",
				fmt.Sprintf("Server is currently in %q power state. Resize requires the server to be either %q or %q.", req.priorPowerState.ValueString(), serversdk.ServerPowerStateRunning, serversdk.ServerPowerStateShutdown),
			)
			return diags
		}
	}

	// --- Phase 1: Live updates on active server ---
	// Metadata, network configurations, and non-disruptive capacity changes are
	// applied first while the server remains in its prior power state.
	if req.patchServer {
		if _, err := r.client.PartialUpdateServer(
			ctx,
			serverID,
			serversdk.PartialUpdateServerParams{ProjectID: r.projectID},
			req.patchBody,
		); err != nil {
			diags.AddError("Error updating server", err.Error())
			return diags
		}
	}
	if req.updateBandwidth {
		diags.Append(r.updateBandwidth(ctx, serverID, req.bandwidth)...)
		if diags.HasError() {
			return diags
		}
	}
	if req.replaceGroups {
		diags.Append(r.updateSecurityGroups(ctx, serverID, req.securityGroupBody)...)
		if diags.HasError() {
			return diags
		}
	}
	if boot := req.attachments.boot; boot != nil {
		diags.Append(r.resizeVolume(ctx, boot.resizeID, boot.resizeSize, bootVolumeLabel(boot.resizeID))...)
		if diags.HasError() {
			return diags
		}
	}
	if data := req.attachments.dataVolume; data != nil {
		for i, id := range data.resizeIDs {
			diags.Append(r.resizeVolume(ctx, id, data.resizeSizes[i], dataVolumeLabel(id))...)
			if diags.HasError() {
				return diags
			}
		}
	}
	if req.attachments.private != nil {
		diags.Append(r.applyPrivateIPUpdate(ctx, serverID, *req.attachments.private)...)
		if diags.HasError() {
			return diags
		}
	}
	if req.attachments.elastic != nil {
		diags.Append(r.applyElasticIPUpdate(ctx, serverID, *req.attachments.elastic)...)
		if diags.HasError() {
			return diags
		}
	}

	// Live resize can update capacity without stopping the server. A successful
	// resize is preserved even if subsequent offline steps fail.
	needsOfflineResize := false
	if req.resize {
		if wasRunning && !explicitShutdown {
			needsShutdown, resizeDiags := r.tryLiveResize(ctx, serverID, req.resizeBody)
			if resizeDiags.HasError() {
				return resizeDiags
			}
			needsOfflineResize = needsShutdown
		} else {
			needsOfflineResize = true
		}
	}

	// --- Phase 2: Offline updates within a single stop/start power cycle ---
	// Operations that cannot run live (rebuild, offline resize fallback, or
	// explicit shutdown) are grouped into at most one shutdown window.
	stoppedForUpdate := false
	needsStop := wasRunning && (explicitShutdown || req.rebuild || needsOfflineResize)
	if needsStop {
		if err := r.requestServerOperation(ctx, serverID, func(ctx context.Context) error {
			return r.client.StopServer(ctx, serverID, serversdk.StopServerParams{ProjectID: r.projectID})
		}); err != nil {
			diags.AddError("Error stopping server", err.Error())
			return diags
		}
		if _, err := r.waitUntilShutdown(ctx, serverID); err != nil {
			diags.AddError("Error waiting for server shutdown", err.Error())
			return diags
		}
		stoppedForUpdate = !explicitShutdown
	}

	if needsOfflineResize {
		diags.Append(r.resizeOfflineServer(ctx, serverID, req.resizeBody)...)
		if diags.HasError() {
			if stoppedForUpdate {
				r.startAfterOfflineUpdate(ctx, serverID, &diags)
			}
			return diags
		}
	}

	if req.rebuild {
		diags.Append(r.rebuildServer(ctx, serverID, req.rebuildBody, req.rebuiltImageID)...)
		if diags.HasError() {
			if stoppedForUpdate {
				r.startAfterOfflineUpdate(ctx, serverID, &diags)
			}
			return diags
		}
	}

	// --- Phase 3: Power state restoration and reconciliation ---
	if stoppedForUpdate {
		r.startAfterOfflineUpdate(ctx, serverID, &diags)
		if diags.HasError() {
			return diags
		}
	}

	if !wasRunning && req.changePowerState && isDesiredPowerState(req.desiredPowerState, serversdk.ServerPowerStateRunning) {
		if req.rebuild || needsOfflineResize {
			r.startAfterOfflineUpdate(ctx, serverID, &diags)
		} else {
			diags.Append(r.updatePowerState(ctx, serverID, req.desiredPowerState)...)
		}
	}

	return diags
}

func (r *ServerResource) startAfterOfflineUpdate(ctx context.Context, serverID core.UUID, diags *diag.Diagnostics) {
	// Resize and rebuild responses can expose their target values before the
	// server leaves its pending power state. Wait for that operation transition,
	// then verify the expected shutdown state before starting a new operation.
	server, err := r.waitUntilNotPending(ctx, serverID)
	if err != nil {
		diags.AddError("Error waiting to start server after offline update", err.Error())
		return
	}
	if !serverIsShutdown(server) {
		diags.AddError(
			"Error waiting to start server after offline update",
			fmt.Sprintf("Server %s left pending in power state %q; expected shutdown before start.", serverID, server.PowerState),
		)
		return
	}
	if err := r.requestServerOperation(ctx, serverID, func(ctx context.Context) error {
		return r.client.StartServer(ctx, serverID, serversdk.StartServerParams{ProjectID: r.projectID})
	}); err != nil {
		diags.AddError("Error starting server after offline update", err.Error())
		return
	}
	if err := r.waitUntilRunning(ctx, serverID); err != nil {
		diags.AddError("Error waiting for server start after offline update", err.Error())
	}
}

func (r *ServerResource) updateBandwidth(ctx context.Context, serverID core.UUID, bandwidth int) diag.Diagnostics {
	var diags diag.Diagnostics
	if err := r.client.UpdateServerBandwidth(
		ctx,
		serverID,
		serversdk.UpdateServerBandwidthParams{ProjectID: r.projectID},
		serversdk.ServerUpdateBandwidthSchema{Bandwidth: &bandwidth},
	); err != nil {
		diags.AddError("Error updating server bandwidth", err.Error())
		return diags
	}
	if err := r.waitUntilBandwidth(ctx, serverID, bandwidth); err != nil {
		diags.AddError("Error waiting for server bandwidth update", err.Error())
	}
	return diags
}

func (r *ServerResource) buildServerResizeBody(
	ctx context.Context,
	serverID core.UUID,
	flavorValue types.Object,
) (serversdk.ServerResizeSchema, diag.Diagnostics) {
	var diags diag.Diagnostics
	server, err := r.get(ctx, serverID)
	if err != nil {
		diags.AddError("Error reading server before resize", err.Error())
		return serversdk.ServerResizeSchema{}, diags
	}
	if server == nil {
		diags.AddError("Error reading server before resize", emptyServerResponse(serverID))
		return serversdk.ServerResizeSchema{}, diags
	}
	flavorFinder := serverlookup.NewFlavorFinder(r.client, r.projectID)
	flavor := buildFlavor(ctx, flavorValue, &server.Zone.Id, flavorFinder.Resolve, &diags)
	if diags.HasError() {
		return serversdk.ServerResizeSchema{}, diags
	}
	return serversdk.ServerResizeSchema{Flavor: flavor}, diags
}

// tryLiveResize attempts to resize the server while it is running. It sets
// live_resize=true and returns needsShutdown=true if the backend explicitly
// rejects live resize and demands a stopped server.
func (r *ServerResource) tryLiveResize(
	ctx context.Context,
	serverID core.UUID,
	body serversdk.ServerResizeSchema,
) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics
	liveResize := true
	liveBody := body
	liveBody.LiveResize = &liveResize
	err := r.requestServerOperation(ctx, serverID, func(ctx context.Context) error {
		return r.client.ResizeServer(ctx, serverID, serversdk.ResizeServerParams{ProjectID: r.projectID}, liveBody)
	})
	if resizeRequiresShutdown(err) {
		return true, nil
	}
	if err != nil {
		diags.AddError("Error resizing server", err.Error())
		return false, diags
	}
	if err := r.waitUntilFlavor(ctx, serverID, body.Flavor); err != nil {
		diags.AddError("Error waiting for server resize", err.Error())
		return false, diags
	}
	return false, diags
}

// resizeOfflineServer issues the resize request against a server that is
// already shut down, using live_resize=false. The caller manages power state.
func (r *ServerResource) resizeOfflineServer(
	ctx context.Context,
	serverID core.UUID,
	body serversdk.ServerResizeSchema,
) diag.Diagnostics {
	var diags diag.Diagnostics
	offlineBody := body
	liveResize := false
	offlineBody.LiveResize = &liveResize
	err := r.requestServerOperation(ctx, serverID, func(ctx context.Context) error {
		return r.client.ResizeServer(ctx, serverID, serversdk.ResizeServerParams{ProjectID: r.projectID}, offlineBody)
	})
	if err != nil {
		diags.AddError("Error resizing stopped server", err.Error())
		return diags
	}
	if err := r.waitUntilFlavor(ctx, serverID, body.Flavor); err != nil {
		diags.AddError("Error waiting for server resize", err.Error())
	}
	return diags
}

func (r *ServerResource) waitUntilFlavor(ctx context.Context, serverID core.UUID, expected serversdk.ServerFlavor) error {
	return r.waitForServer(ctx, serverID, "resize", func(server *serversdk.ServerDetailSchema) (bool, error) {
		matches, err := serverFlavorMatches(server.Flavor, expected)
		return matches && !serverIsBusy(server), err
	})
}

func (r *ServerResource) rebuildServer(
	ctx context.Context,
	serverID core.UUID,
	body serversdk.ServerRebuildSchema,
	expectedImageID core.UUID,
) diag.Diagnostics {
	var diags diag.Diagnostics
	err := r.requestServerOperation(ctx, serverID, func(ctx context.Context) error {
		return r.client.RebuildServer(ctx, serverID, serversdk.RebuildServerParams{ProjectID: r.projectID}, body)
	})
	if err != nil {
		diags.AddError("Error rebuilding server", err.Error())
		return diags
	}
	if err := r.waitUntilImage(ctx, serverID, expectedImageID); err != nil {
		diags.AddError("Error waiting for server rebuild", err.Error())
	}
	return diags
}

func (r *ServerResource) waitUntilImage(ctx context.Context, serverID, expectedImageID core.UUID) error {
	return r.waitForServer(ctx, serverID, "rebuild", func(server *serversdk.ServerDetailSchema) (bool, error) {
		return server.Image != nil && server.Image.Id == expectedImageID && !serverIsBusy(server), nil
	})
}

func (r *ServerResource) updateSecurityGroups(
	ctx context.Context,
	serverID core.UUID,
	body serversdk.ServerUpdateSecurityGroupSchema,
) diag.Diagnostics {
	var diags diag.Diagnostics
	if err := r.client.UpdateServerSecurityGroup(
		ctx,
		serverID,
		serversdk.UpdateServerSecurityGroupParams{ProjectID: r.projectID},
		body,
	); err != nil {
		diags.AddError("Error updating server security groups", err.Error())
		return diags
	}
	expected := make(map[core.UUID]struct{}, len(body.SecurityGroups))
	for _, securityGroup := range body.SecurityGroups {
		expected[securityGroup.Id] = struct{}{}
	}
	if err := r.waitUntilSecurityGroups(ctx, serverID, expected); err != nil {
		diags.AddError("Error waiting for server security group update", err.Error())
	}
	return diags
}

func (r *ServerResource) waitUntilSecurityGroups(ctx context.Context, serverID core.UUID, expected map[core.UUID]struct{}) error {
	return r.waitForServer(ctx, serverID, "security group update", func(server *serversdk.ServerDetailSchema) (bool, error) {
		if len(server.SecurityGroups) != len(expected) {
			return false, nil
		}
		for _, securityGroup := range server.SecurityGroups {
			if _, ok := expected[securityGroup.Id]; !ok {
				return false, nil
			}
		}
		return !serverIsBusy(server), nil
	})
}

func (r *ServerResource) updatePowerState(ctx context.Context, serverID core.UUID, desired types.String) diag.Diagnostics {
	diags := validateDesiredPowerState(desired)
	if diags.HasError() || desired.IsNull() || desired.IsUnknown() {
		return diags
	}
	switch strings.TrimSpace(desired.ValueString()) {
	case string(serversdk.ServerPowerStateRunning):
		if err := r.requestServerOperation(ctx, serverID, func(ctx context.Context) error {
			return r.client.StartServer(ctx, serverID, serversdk.StartServerParams{ProjectID: r.projectID})
		}); err != nil {
			diags.AddError("Error starting server", err.Error())
			return diags
		}
		if err := r.waitUntilRunning(ctx, serverID); err != nil {
			diags.AddError("Error waiting for server start", err.Error())
		}
	case string(serversdk.ServerPowerStateShutdown):
		if err := r.requestServerOperation(ctx, serverID, func(ctx context.Context) error {
			return r.client.StopServer(ctx, serverID, serversdk.StopServerParams{ProjectID: r.projectID})
		}); err != nil {
			diags.AddError("Error stopping server", err.Error())
			return diags
		}
		if _, err := r.waitUntilShutdown(ctx, serverID); err != nil {
			diags.AddError("Error waiting for server stop", err.Error())
		}
	}
	return diags
}

func (r *ServerResource) waitUntilRunning(ctx context.Context, serverID core.UUID) error {
	return r.waitForServer(ctx, serverID, "start", func(server *serversdk.ServerDetailSchema) (bool, error) {
		if server.PowerState == serversdk.ServerPowerStateRunning && !serverIsBusy(server) {
			return true, nil
		}
		if server.Status == serversdk.ServerStatusDeleted {
			return false, fmt.Errorf("server %s was deleted while waiting to start", serverID)
		}
		return false, nil
	})
}

// --- Private-IP and elastic-IP attachment reconciliation ---

func (r *ServerResource) buildAttachmentUpdatePlan(
	ctx context.Context,
	serverID core.UUID,
	plan ServerResourceModel,
	state ServerResourceModel,
) (attachmentUpdatePlan, diag.Diagnostics) {
	var result attachmentUpdatePlan
	var diags diag.Diagnostics
	if privateIPsChanged(plan, state) {
		privatePlan, privateDiags := r.buildPrivateIPUpdatePlan(ctx, plan.PrivateIPs, state.PrivateIPs)
		diags.Append(privateDiags...)
		if diags.HasError() {
			return result, diags
		}
		result.private = &privatePlan
	}
	if elasticIPsChanged(plan, state) {
		elasticPlan, elasticDiags := r.buildElasticIPUpdatePlan(ctx, serverID, plan.ElasticIPs, state.ElasticIPs)
		diags.Append(elasticDiags...)
		if diags.HasError() {
			return result, diags
		}
		result.elastic = &elasticPlan
	}
	bootPlan, bootDiags := r.buildBootVolumeResizePlan(ctx, serverID, plan, state)
	diags.Append(bootDiags...)
	if diags.HasError() {
		return result, diags
	}
	result.boot = bootPlan
	dataVolumePlan, dataVolumeDiags := r.buildDataVolumeUpdatePlan(ctx, plan, state)
	diags.Append(dataVolumeDiags...)
	if diags.HasError() {
		return result, diags
	}
	result.dataVolume = dataVolumePlan
	return result, diags
}

func (r *ServerResource) updatePrivateIPs(
	ctx context.Context,
	serverID core.UUID,
	plannedValue types.List,
	priorValue types.List,
) diag.Diagnostics {
	plan, diags := r.buildPrivateIPUpdatePlan(ctx, plannedValue, priorValue)
	if diags.HasError() {
		return diags
	}
	return r.applyPrivateIPUpdate(ctx, serverID, plan)
}

func (r *ServerResource) buildPrivateIPUpdatePlan(
	ctx context.Context,
	plannedValue types.List,
	priorValue types.List,
) (privateIPUpdatePlan, diag.Diagnostics) {
	planned, prior, diags := decodePrivateIPChanges(ctx, plannedValue, priorValue)
	plan := privateIPUpdatePlan{
		expectedCount: len(planned),
		desiredIDs:    make(map[core.UUID]struct{}, len(planned)),
		removedIDs:    make(map[core.UUID]struct{}, len(prior)),
		existingIDs:   make([]core.UUID, 0, len(planned)),
		subnetIDs:     make([]core.UUID, 0, len(planned)),
	}
	if diags.HasError() {
		return plan, diags
	}
	matches, usedPrior := matchPrivateIPConfigs(planned, prior)
	subnetFinder := networklookup.NewSubnetFinder(r.networkClient, r.projectID)
	for i := range planned {
		if matches[i] >= 0 {
			id, idDiags := parse.UUIDString(planned[i].ID, fmt.Sprintf("private_ips[%d].id", i))
			diags.Append(idDiags...)
			if !idDiags.HasError() {
				plan.desiredIDs[id] = struct{}{}
			}
			continue
		}
		switch normalizedString(planned[i].Kind) {
		case "ip":
			id, idDiags := parse.UUIDString(planned[i].ID, fmt.Sprintf("private_ips[%d].id", i))
			diags.Append(idDiags...)
			if !idDiags.HasError() {
				plan.desiredIDs[id] = struct{}{}
				plan.existingIDs = append(plan.existingIDs, id)
			}
		case "subnet":
			if !planned[i].ID.IsNull() && !planned[i].ID.IsUnknown() {
				diags.AddError("Invalid private IP update", fmt.Sprintf("private_ips[%d].id must be omitted when changing a subnet attachment.", i))
				continue
			}
			subnetID, subnetDiags := resolvePrivateIPSubnet(ctx, planned[i], i, subnetFinder.Resolve)
			diags.Append(subnetDiags...)
			if !subnetDiags.HasError() {
				plan.subnetIDs = append(plan.subnetIDs, subnetID)
			}
		default:
			diags.AddError("Invalid private IP kind", fmt.Sprintf("private_ips[%d].kind must be subnet or ip.", i))
		}
	}
	for i, priorIP := range prior {
		if usedPrior[i] || priorIP.ID.IsNull() || priorIP.ID.IsUnknown() {
			continue
		}
		id, idDiags := parse.UUIDString(priorIP.ID, fmt.Sprintf("prior private_ips[%d].id", i))
		diags.Append(idDiags...)
		if idDiags.HasError() {
			continue
		}
		plan.removedIDs[id] = struct{}{}
		// Delete provider-created subnet IPs only when delete_on_termination
		// requests cleanup. Otherwise detaching deliberately preserves the IP.
		if normalizedString(priorIP.Kind) == "subnet" && boolValueOrDefault(priorIP.DeleteOnTermination, false) {
			plan.deletedIDs = append(plan.deletedIDs, id)
		}
	}
	return plan, diags
}

func (r *ServerResource) applyPrivateIPUpdate(
	ctx context.Context,
	serverID core.UUID,
	plan privateIPUpdatePlan,
) diag.Diagnostics {
	var diags diag.Diagnostics
	for id := range plan.removedIDs {
		if err := r.client.DetachPrivateIp(
			ctx,
			serverID,
			serversdk.DetachPrivateIpParams{ProjectID: r.projectID},
			serversdk.ServerDetachPrivateIPSchema{PrivateIpId: id},
		); err != nil {
			diags.AddError("Error detaching server private IP", fmt.Sprintf("Unable to detach private IP %s: %s", id, err))
			return diags
		}
	}

	for _, id := range plan.existingIDs {
		if err := r.client.AttachPrivateIp(
			ctx,
			serverID,
			serversdk.AttachPrivateIpParams{ProjectID: r.projectID},
			serversdk.ServerAttachPrivateIPSchema{PrivateIpId: id},
		); err != nil {
			diags.AddError("Error attaching server private IP", fmt.Sprintf("Unable to attach private IP %s: %s", id, err))
			return diags
		}
	}
	for _, subnetID := range plan.subnetIDs {
		id, attachDiags := r.attachNewPrivateIP(ctx, serverID, subnetID)
		diags.Append(attachDiags...)
		if diags.HasError() {
			return diags
		}
		plan.desiredIDs[id] = struct{}{}
	}
	if err := r.waitUntilPrivateIPs(ctx, serverID, plan.expectedCount, plan.desiredIDs, plan.removedIDs); err != nil {
		diags.AddError("Error waiting for server private IP update", err.Error())
		return diags
	}
	diags.Append(r.deletePrivateIPs(ctx, plan.deletedIDs)...)
	return diags
}

// attachNewPrivateIP allocates a private IP in the subnet and attaches it.
// Allocating explicitly means a failed attach cannot lose the ID of a private
// IP created inside AttachServerSubnet. The attachment keeps kind=subnet and
// its configured deletion policy in Terraform state.
func (r *ServerResource) attachNewPrivateIP(ctx context.Context, serverID, subnetID core.UUID) (core.UUID, diag.Diagnostics) {
	var diags diag.Diagnostics
	created, err := r.networkClient.CreatePrivateIp(ctx, networksdk.CreatePrivateIpParams{ProjectID: r.projectID},
		networksdk.PrivateIPCreateSchema{SubnetId: subnetID})
	if err != nil {
		diags.AddError("Error allocating server private IP", fmt.Sprintf("Unable to allocate a private IP in subnet %s: %s", subnetID, err))
		return core.NilUUID, diags
	}
	if created == nil {
		diags.AddError("Error allocating server private IP", "The API returned an empty private IP response.")
		return core.NilUUID, diags
	}
	err = r.requestServerOperation(ctx, serverID, func(ctx context.Context) error {
		return r.client.AttachPrivateIp(ctx, serverID, serversdk.AttachPrivateIpParams{ProjectID: r.projectID},
			serversdk.ServerAttachPrivateIPSchema{PrivateIpId: created.Id})
	})
	if err != nil {
		diags.AddError("Error attaching server private IP", fmt.Sprintf("Unable to attach allocated private IP %s in subnet %s: %s", created.Id, subnetID, err))
		diags.Append(r.deletePrivateIPs(ctx, []core.UUID{created.Id})...)
		return created.Id, diags
	}
	return created.Id, diags
}

// deletePrivateIPs deletes the private IPs that the provider created and waits
// until the backend has really removed each one. DeletePrivateIp only accepts
// the request. Returning while an address still exists lets Terraform go on to
// destroy the subnet that holds it. The backend refuses to delete a subnet that
// still has private IPs in it.
func (r *ServerResource) deletePrivateIPs(ctx context.Context, ids []core.UUID) diag.Diagnostics {
	var diags diag.Diagnostics
	for _, id := range ids {
		if err := r.networkClient.DeletePrivateIp(
			ctx,
			id,
			networksdk.DeletePrivateIpParams{ProjectID: r.projectID},
		); err != nil && !errors.Is(err, networksdk.ErrNotFound) {
			diags.AddError("Error deleting server private IP", fmt.Sprintf("Unable to delete private IP %s: %s", id, err))
			continue
		}
		if err := r.waitUntilPrivateIPDeleted(ctx, id); err != nil {
			diags.AddError("Error waiting for server private IP deletion", fmt.Sprintf("Unable to confirm private IP %s was deleted: %s", id, err))
		}
	}
	return diags
}

func (r *ServerResource) waitUntilPrivateIPDeleted(ctx context.Context, id core.UUID) error {
	w := wait.New[struct{}](privateIPDeleteInterval, privateIPDeleteTimeout)
	_, err := w.WaitFor(ctx, func(ctx context.Context) (struct{}, error) {
		privateIP, err := r.networkClient.GetPrivateIp(ctx, id, networksdk.GetPrivateIpParams{ProjectID: r.projectID})
		if errors.Is(err, networksdk.ErrNotFound) || err == nil && privateIP == nil {
			return struct{}{}, nil
		}
		if err != nil {
			return struct{}{}, err
		}
		return struct{}{}, fmt.Errorf("private IP %s still exists: %w", id, wait.ErrNotReady)
	})
	return err
}

func (r *ServerResource) buildElasticIPUpdatePlan(
	ctx context.Context,
	serverID core.UUID,
	plannedValue types.List,
	priorValue types.List,
) (elasticIPUpdatePlan, diag.Diagnostics) {
	planned, prior, diags := decodeElasticIPChanges(ctx, plannedValue, priorValue)
	plan := elasticIPUpdatePlan{
		expectedCount: len(planned),
		desiredIDs:    make(map[core.UUID]struct{}, len(planned)),
		removedIDs:    make(map[core.UUID]struct{}, len(prior)),
		existingIDs:   make([]core.UUID, 0, len(planned)),
		newConfigs:    make([]ElasticIPInputModel, 0, len(planned)),
	}
	if diags.HasError() {
		return plan, diags
	}
	matches, usedPrior := matchElasticIPConfigs(planned, prior)
	for i := range planned {
		if matches[i] >= 0 {
			id, idDiags := parse.UUIDString(planned[i].ID, fmt.Sprintf("elastic_ips[%d].id", i))
			diags.Append(idDiags...)
			if !idDiags.HasError() {
				plan.desiredIDs[id] = struct{}{}
			}
			continue
		}
		switch normalizedString(planned[i].Kind) {
		case "existing":
			id, idDiags := parse.UUIDString(planned[i].ID, fmt.Sprintf("elastic_ips[%d].id", i))
			diags.Append(idDiags...)
			if !idDiags.HasError() {
				plan.desiredIDs[id] = struct{}{}
				plan.existingIDs = append(plan.existingIDs, id)
			}
		case "new":
			if !planned[i].ID.IsNull() && !planned[i].ID.IsUnknown() {
				diags.AddError("Invalid elastic IP update", fmt.Sprintf("elastic_ips[%d].id must be omitted when changing a new elastic IP attachment.", i))
				continue
			}
			if planned[i].EnableIPv4.IsUnknown() || planned[i].EnableIPv6.IsUnknown() {
				diags.AddError("Unknown new elastic IP address family", fmt.Sprintf("elastic_ips[%d].enable_ipv4 and enable_ipv6 must be known when configured.", i))
				continue
			}
			if !boolValueOrDefault(planned[i].EnableIPv4, true) && !boolValueOrDefault(planned[i].EnableIPv6, false) {
				diags.AddError("Invalid new elastic IP", fmt.Sprintf("elastic_ips[%d] must enable IPv4, IPv6, or both.", i))
				continue
			}
			plan.newConfigs = append(plan.newConfigs, planned[i])
		default:
			diags.AddError("Invalid elastic IP kind", fmt.Sprintf("elastic_ips[%d].kind must be existing or new.", i))
		}
	}
	for i, priorIP := range prior {
		if usedPrior[i] || priorIP.ID.IsNull() || priorIP.ID.IsUnknown() {
			continue
		}
		id, idDiags := parse.UUIDString(priorIP.ID, fmt.Sprintf("prior elastic_ips[%d].id", i))
		diags.Append(idDiags...)
		if idDiags.HasError() {
			continue
		}
		plan.removedIDs[id] = struct{}{}
		// Delete provider-created elastic IPs only when delete_on_termination
		// requests cleanup. Otherwise detaching deliberately preserves the IP.
		if normalizedString(priorIP.Kind) == "new" && boolValueOrDefault(priorIP.DeleteOnTermination, false) {
			plan.deletedIDs = append(plan.deletedIDs, id)
		}
	}
	if len(plan.newConfigs) > 0 {
		resolvedRegionID, regionDiags := r.serverRegionID(ctx, serverID)
		diags.Append(regionDiags...)
		plan.regionID = resolvedRegionID
	}
	return plan, diags
}

func (r *ServerResource) applyElasticIPUpdate(
	ctx context.Context,
	serverID core.UUID,
	plan elasticIPUpdatePlan,
) diag.Diagnostics {
	var diags diag.Diagnostics
	for id := range plan.removedIDs {
		if err := r.client.DetachServerEip(
			ctx,
			serverID,
			serversdk.DetachServerEipParams{ProjectID: r.projectID},
			serversdk.ServerElasticIPAttachSchema{EipId: id},
		); err != nil {
			diags.AddError("Error detaching server elastic IP", fmt.Sprintf("Unable to detach elastic IP %s: %s", id, err))
			return diags
		}
	}

	for _, id := range plan.existingIDs {
		if err := r.attachElasticIP(ctx, serverID, id); err != nil {
			diags.AddError("Error attaching server elastic IP", fmt.Sprintf("Unable to attach elastic IP %s: %s", id, err))
			return diags
		}
	}
	for _, plannedIP := range plan.newConfigs {
		enableIPv4 := boolValueOrDefault(plannedIP.EnableIPv4, true)
		enableIPv6 := boolValueOrDefault(plannedIP.EnableIPv6, false)
		created, err := r.networkClient.CreateElasticIp(
			ctx,
			networksdk.CreateElasticIpParams{ProjectID: r.projectID},
			networksdk.ElasticIPCreateSchema{
				RegionId:   plan.regionID,
				EnableIpv4: &enableIPv4,
				EnableIpv6: &enableIPv6,
			},
		)
		if err != nil {
			diags.AddError("Error creating server elastic IP", err.Error())
			return diags
		}
		if created == nil {
			diags.AddError("Error creating server elastic IP", "The API returned an empty elastic IP response.")
			return diags
		}
		if err := r.attachCreatedElasticIP(ctx, serverID, created.Id, enableIPv4, enableIPv6); err != nil {
			cleanupErr := r.networkClient.DeleteElasticIp(ctx, created.Id, networksdk.DeleteElasticIpParams{ProjectID: r.projectID})
			if cleanupErr != nil && !errors.Is(cleanupErr, networksdk.ErrNotFound) {
				diags.AddError(
					"Error attaching server elastic IP",
					fmt.Sprintf("Unable to attach elastic IP %s: %s. Cleanup also failed: %s", created.Id, err, cleanupErr),
				)
			} else {
				diags.AddError("Error attaching server elastic IP", fmt.Sprintf("Unable to attach elastic IP %s: %s", created.Id, err))
			}
			return diags
		}
		plan.desiredIDs[created.Id] = struct{}{}
	}
	if err := r.waitUntilElasticIPs(ctx, serverID, plan.expectedCount, plan.desiredIDs, plan.removedIDs); err != nil {
		diags.AddError("Error waiting for server elastic IP update", err.Error())
		return diags
	}
	for _, id := range plan.deletedIDs {
		if err := r.networkClient.DeleteElasticIp(
			ctx,
			id,
			networksdk.DeleteElasticIpParams{ProjectID: r.projectID},
		); err != nil && !errors.Is(err, networksdk.ErrNotFound) {
			diags.AddError("Error deleting detached server elastic IP", fmt.Sprintf("Unable to delete elastic IP %s: %s", id, err))
		}
	}
	return diags
}

// attachCreatedElasticIP attaches an elastic IP that this provider just
// created. The network API returns the elastic IP as soon as it is recorded,
// but its address is allocated upstream a moment later. Attaching one that
// early is what the backend rejects with a transient provider error. Waiting
// for the address first keeps a request that only needed a moment from failing
// the apply, and the standalone elastic IP resource waits for the same signal.
func (r *ServerResource) attachCreatedElasticIP(
	ctx context.Context,
	serverID, elasticIPID core.UUID,
	enableIPv4, enableIPv6 bool,
) error {
	if err := r.waitUntilElasticIPAllocated(ctx, elasticIPID, enableIPv4, enableIPv6); err != nil {
		return err
	}
	return r.attachElasticIP(ctx, serverID, elasticIPID)
}

// waitUntilElasticIPAllocated waits until the elastic IP carries an address for
// every family it was created with.
func (r *ServerResource) waitUntilElasticIPAllocated(
	ctx context.Context,
	elasticIPID core.UUID,
	enableIPv4, enableIPv6 bool,
) error {
	w := wait.New[struct{}](serverPollInterval, serverOperationTimeout)
	_, err := w.WaitFor(ctx, func(ctx context.Context) (struct{}, error) {
		elasticIP, err := r.networkClient.GetElasticIp(ctx, elasticIPID, networksdk.GetElasticIpParams{ProjectID: r.projectID})
		switch {
		case errors.Is(err, networksdk.ErrNotFound):
			// A create the backend has not finished recording reads as missing.
			return struct{}{}, fmt.Errorf("not found yet: %w", wait.ErrNotReady)
		case err != nil:
			return struct{}{}, err
		case elasticIP == nil:
			return struct{}{}, fmt.Errorf("the API returned an empty response: %w", wait.ErrNotReady)
		case enableIPv4 && (elasticIP.IpAddress == nil || *elasticIP.IpAddress == ""):
			return struct{}{}, fmt.Errorf("no IPv4 assigned yet: %w", wait.ErrNotReady)
		case enableIPv6 && (elasticIP.Ipv6Address == nil || *elasticIP.Ipv6Address == ""):
			return struct{}{}, fmt.Errorf("no IPv6 assigned yet: %w", wait.ErrNotReady)
		}
		return struct{}{}, nil
	})
	if err != nil {
		return fmt.Errorf("waiting for elastic IP %s to be allocated: %w", elasticIPID, err)
	}
	return nil
}

// attachElasticIP replays an attach that the backend could not handle. The SDK
// transport leaves a POST alone because replaying one is not generally safe.
// This request only associates one elastic IP with one server, so a
// server-side refusal is worth repeating instead of failing the whole apply. A
// refusal that hid a successful attach shows up as the elastic IP already
// being on the server, which counts as done. A refusal for any other reason is
// returned as-is so the practitioner sees why the request was rejected.
func (r *ServerResource) attachElasticIP(ctx context.Context, serverID, elasticIPID core.UUID) error {
	w := wait.New[struct{}](serverPollInterval, elasticIPAttachTimeout)
	_, err := w.WaitFor(ctx, func(ctx context.Context) (struct{}, error) {
		attachErr := r.client.AttachServerEip(
			ctx,
			serverID,
			serversdk.AttachServerEipParams{ProjectID: r.projectID},
			serversdk.ServerElasticIPAttachSchema{EipId: elasticIPID},
		)
		if attachErr == nil || !isTransientBackendError(attachErr) {
			return struct{}{}, attachErr
		}
		if attached, checkErr := r.serverHasElasticIP(ctx, serverID, elasticIPID); checkErr == nil && attached {
			return struct{}{}, nil
		}
		return struct{}{}, fmt.Errorf("the backend could not handle the attach: %s: %w", attachErr, wait.ErrNotReady)
	})
	return err
}

func (r *ServerResource) serverHasElasticIP(ctx context.Context, serverID, elasticIPID core.UUID) (bool, error) {
	server, err := r.get(ctx, serverID)
	if err != nil || server == nil {
		return false, err
	}
	for _, elasticIP := range server.ElasticIps {
		if elasticIP.Id == elasticIPID {
			return true, nil
		}
	}
	return false, nil
}

func (r *ServerResource) serverRegionID(ctx context.Context, serverID core.UUID) (core.UUID, diag.Diagnostics) {
	var diags diag.Diagnostics
	server, err := r.get(ctx, serverID)
	if err != nil {
		diags.AddError("Error reading server zone", err.Error())
		return core.UUID{}, diags
	}
	zoneName := server.Zone.Name
	zone, err := projectlookup.NewZoneFinder(r.projectClient, r.projectID).Resolve(ctx, projectlookup.ZoneFilter{Name: &zoneName})
	if err != nil {
		diags.AddError("Unable to resolve server zone", err.Error())
		return core.UUID{}, diags
	}
	return zone.Region.Id, diags
}

func (r *ServerResource) waitUntilPrivateIPs(
	ctx context.Context,
	serverID core.UUID,
	expectedCount int,
	expectedIDs map[core.UUID]struct{},
	removedIDs map[core.UUID]struct{},
) error {
	return r.waitForServer(ctx, serverID, "private IP update", func(server *serversdk.ServerDetailSchema) (bool, error) {
		if len(server.PrivateIps) != expectedCount {
			return false, nil
		}
		actual := make(map[core.UUID]struct{}, len(server.PrivateIps))
		for _, privateIP := range server.PrivateIps {
			actual[privateIP.Id] = struct{}{}
		}
		// The backend lists private IPs before assigning their addresses.
		// Check every entry, including subnet IPs whose IDs were unknown in
		// the plan, so downstream resources receive allocated addresses.
		return privateIPAddressesAssigned(server.PrivateIps) &&
			containsAttachmentIDs(actual, expectedIDs) &&
			excludesAttachmentIDs(actual, removedIDs) &&
			!serverIsBusy(server), nil
	})
}

func (r *ServerResource) waitUntilElasticIPs(
	ctx context.Context,
	serverID core.UUID,
	expectedCount int,
	expectedIDs map[core.UUID]struct{},
	removedIDs map[core.UUID]struct{},
) error {
	return r.waitForServer(ctx, serverID, "elastic IP update", func(server *serversdk.ServerDetailSchema) (bool, error) {
		if len(server.ElasticIps) != expectedCount {
			return false, nil
		}
		actual := make(map[core.UUID]struct{}, len(server.ElasticIps))
		for _, elasticIP := range server.ElasticIps {
			actual[elasticIP.Id] = struct{}{}
		}
		// A freshly attached elastic IP can appear before address allocation.
		// Wait for at least one IPv4 or IPv6 address; IPv6-only IPs may keep
		// ip_address null.
		return elasticIPAddressesAssigned(server.ElasticIps) &&
			containsAttachmentIDs(actual, expectedIDs) &&
			excludesAttachmentIDs(actual, removedIDs) &&
			!serverIsBusy(server), nil
	})
}

// --- Attachment list plan modifiers ---

func (privateIPStatePlanModifier) Description(context.Context) string {
	return "Preserves computed private IP values only while the attachment configuration is unchanged."
}

func (privateIPStatePlanModifier) MarkdownDescription(context.Context) string {
	return "Preserves computed private IP values only while the attachment configuration is unchanged."
}

func (privateIPStatePlanModifier) PlanModifyList(ctx context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	if preserveUnconfiguredAttachmentList(req, resp) || req.PlanValue.IsNull() || req.PlanValue.IsUnknown() ||
		req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}

	var planned []PrivateIPInputModel
	var configured []PrivateIPInputModel
	var prior []PrivateIPInputModel
	resp.Diagnostics.Append(req.PlanValue.ElementsAs(ctx, &planned, false)...)
	resp.Diagnostics.Append(req.ConfigValue.ElementsAs(ctx, &configured, false)...)
	resp.Diagnostics.Append(req.StateValue.ElementsAs(ctx, &prior, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	preservePrivateIPComputedValues(planned, configured, prior)
	value, diags := types.ListValueFrom(ctx, req.PlanValue.ElementType(ctx), planned)
	resp.Diagnostics.Append(diags...)
	if !diags.HasError() {
		resp.PlanValue = value
	}
}

func (elasticIPStatePlanModifier) Description(context.Context) string {
	return "Preserves computed elastic IP values only while the attachment configuration is unchanged."
}

func (elasticIPStatePlanModifier) MarkdownDescription(context.Context) string {
	return "Preserves computed elastic IP values only while the attachment configuration is unchanged."
}

func (elasticIPStatePlanModifier) PlanModifyList(ctx context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	if preserveUnconfiguredAttachmentList(req, resp) || req.PlanValue.IsNull() || req.PlanValue.IsUnknown() ||
		req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}

	var planned []ElasticIPInputModel
	var configured []ElasticIPInputModel
	var prior []ElasticIPInputModel
	resp.Diagnostics.Append(req.PlanValue.ElementsAs(ctx, &planned, false)...)
	resp.Diagnostics.Append(req.ConfigValue.ElementsAs(ctx, &configured, false)...)
	resp.Diagnostics.Append(req.StateValue.ElementsAs(ctx, &prior, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	preserveElasticIPComputedValues(planned, configured, prior)
	value, diags := types.ListValueFrom(ctx, req.PlanValue.ElementType(ctx), planned)
	resp.Diagnostics.Append(diags...)
	if !diags.HasError() {
		resp.PlanValue = value
	}
}

// --- Server request and diff helpers ---

func emptyServerResponse(id core.UUID) string {
	return fmt.Sprintf("The API returned an empty response for server %s.", id)
}

func deletionErrorDetail(err, stopErr error) string {
	if stopErr == nil {
		return err.Error()
	}
	return fmt.Sprintf("%s\n\nStopping the server before deletion also failed: %s", err, stopErr)
}

func serverIsShutdown(server *serversdk.ServerDetailSchema) bool {
	return server.PowerState == serversdk.ServerPowerStateShutdown ||
		server.Status == serversdk.ServerStatusShutoff || server.Status == serversdk.ServerStatusStopped
}

func isTransientEnumError(err error) bool {
	var apiErr *serversdk.APIError
	return errors.As(err, &apiErr) && apiErr.Code() == "enum"
}

// isTransientBackendError reports whether the backend refused the request for
// a reason that is worth repeating: a server-side failure, rate limiting, or
// provider_error from a busy upstream. That code is matched independently
// because the backend does not always pair it with a 5xx status. Gating the
// replay on the status class alone would let the very refusal this exists for
// fail the apply on the first attempt.
func isTransientBackendError(err error) bool {
	if errors.Is(err, serversdk.ErrInternal) || errors.Is(err, serversdk.ErrRateLimited) {
		return true
	}
	var apiErr *serversdk.APIError
	return errors.As(err, &apiErr) && apiErr.Code() == transientBackendErrorCode
}

func buildServerUpdateBody(plan, state ServerResourceModel) (serversdk.ServerPartialUpdateSchema, bool, diag.Diagnostics) {
	var body serversdk.ServerPartialUpdateSchema
	var diags diag.Diagnostics
	if plan.Name.IsNull() || plan.Name.IsUnknown() || strings.TrimSpace(plan.Name.ValueString()) == "" {
		diags.AddError("Missing server name", "name must be configured and known.")
		return body, false, diags
	}
	name := strings.TrimSpace(plan.Name.ValueString())
	if state.Name.IsNull() || state.Name.IsUnknown() || strings.TrimSpace(state.Name.ValueString()) != name {
		body.Name = &name
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() &&
		(state.Description.IsNull() || state.Description.IsUnknown() || !plan.Description.Equal(state.Description)) {
		description := plan.Description.ValueString()
		body.Description = &description
	}
	return body, body.Name != nil || body.Description != nil, diags
}

func bandwidthChanged(plan, state ServerResourceModel) bool {
	return !plan.Bandwidth.IsNull() && !plan.Bandwidth.IsUnknown() &&
		(state.Bandwidth.IsNull() || state.Bandwidth.IsUnknown() || !plan.Bandwidth.Equal(state.Bandwidth))
}

// dataVolumeCountChanged reports whether the plan attaches a different number
// of data volumes than the server was created with. The Framework only calls
// it once the plan and state lists already differ. A difference confined to
// one element — a changed volume_type or volume_size, or the computed iops
// going unknown — is left to the modifiers on those attributes.
func dataVolumeCountChanged(
	_ context.Context,
	req planmodifier.ListRequest,
	resp *listplanmodifier.RequiresReplaceIfFuncResponse,
) {
	if req.PlanValue.IsUnknown() {
		return
	}
	if req.StateValue.IsNull() || req.PlanValue.IsNull() {
		resp.RequiresReplace = !req.StateValue.Equal(req.PlanValue)
		return
	}
	resp.RequiresReplace = len(req.StateValue.Elements()) != len(req.PlanValue.Elements())
}

// bootVolumeLabel and dataVolumeLabel name a volume in resize diagnostics.
// The ID is what identifies it: an update plan keeps only the volumes whose
// size changed, so a position in that plan would not match the data_volumes
// index a practitioner reads in their configuration.
func bootVolumeLabel(id core.UUID) string {
	return fmt.Sprintf("boot volume %s", id)
}

func dataVolumeLabel(id core.UUID) string {
	return fmt.Sprintf("data volume %s", id)
}

// increasedVolumeSize rejects unknown, removed, or shrinking size changes.
func increasedVolumeSize(planned, prior types.Int64, name string, diags *diag.Diagnostics) (int, bool) {
	if planned.IsNull() || planned.IsUnknown() || prior.IsNull() || prior.IsUnknown() {
		diags.AddError("Invalid server volume size update", fmt.Sprintf("%s must be known in both the plan and prior state.", name))
		return 0, false
	}
	if planned.ValueInt64() <= prior.ValueInt64() {
		diags.AddError("Invalid server volume size update", fmt.Sprintf("%s can only increase from %d GiB; planned value is %d GiB.", name, prior.ValueInt64(), planned.ValueInt64()))
		return 0, false
	}
	return int(planned.ValueInt64()), true
}

// serverVolumeStatusReady identifies statuses that can finish an extend.
func serverVolumeStatusReady(status blockstoragesdk.VolumeStatus) bool {
	return status == blockstoragesdk.VolumeStatusAvailable || status == blockstoragesdk.VolumeStatusInUse
}

// serverVolumeStatusFailed identifies terminal block-storage failures.
func serverVolumeStatusFailed(status blockstoragesdk.VolumeStatus) bool {
	switch status {
	case blockstoragesdk.VolumeStatusAttachFailed,
		blockstoragesdk.VolumeStatusBackupFailed,
		blockstoragesdk.VolumeStatusDetachFailed,
		blockstoragesdk.VolumeStatusExtendFailed,
		blockstoragesdk.VolumeStatusFailed,
		blockstoragesdk.VolumeStatusRestoreFailed:
		return true
	default:
		return false
	}
}

func serverCascadeTargets(ctx context.Context, state ServerResourceModel) ([]core.UUID, []core.UUID, diag.Diagnostics) {
	var diags diag.Diagnostics
	privateIPIDs := make([]core.UUID, 0)
	elasticIPIDs := make([]core.UUID, 0)
	if !state.PrivateIPs.IsNull() && !state.PrivateIPs.IsUnknown() {
		var values []PrivateIPInputModel
		diags.Append(state.PrivateIPs.ElementsAs(ctx, &values, false)...)
		for i, value := range values {
			if value.DeleteOnTermination.IsNull() || value.DeleteOnTermination.IsUnknown() || !value.DeleteOnTermination.ValueBool() {
				continue
			}
			if value.ID.IsNull() || value.ID.IsUnknown() || strings.TrimSpace(value.ID.ValueString()) == "" {
				continue
			}
			id, valueDiags := parse.UUIDString(value.ID, fmt.Sprintf("private_ips[%d].id", i))
			diags.Append(valueDiags...)
			if !valueDiags.HasError() {
				privateIPIDs = append(privateIPIDs, id)
			}
		}
	}
	if !state.ElasticIPs.IsNull() && !state.ElasticIPs.IsUnknown() {
		var values []ElasticIPInputModel
		diags.Append(state.ElasticIPs.ElementsAs(ctx, &values, false)...)
		for i, value := range values {
			if value.DeleteOnTermination.IsNull() || value.DeleteOnTermination.IsUnknown() || !value.DeleteOnTermination.ValueBool() {
				continue
			}
			if value.ID.IsNull() || value.ID.IsUnknown() || strings.TrimSpace(value.ID.ValueString()) == "" {
				continue
			}
			id, valueDiags := parse.UUIDString(value.ID, fmt.Sprintf("elastic_ips[%d].id", i))
			diags.Append(valueDiags...)
			if !valueDiags.HasError() {
				elasticIPIDs = append(elasticIPIDs, id)
			}
		}
	}
	return privateIPIDs, elasticIPIDs, diags
}

// serverVolumeCascadeTargets lists the volumes to delete once the server is
// gone. Data volumes carry their own stable ID in state, so a configured
// volume is matched against the reported attachments by that ID rather than
// by position, which the server does not guarantee to preserve. The
// auto-created boot volume has no such ID, so it is resolved from the
// reported attachments directly. Either kind survives when the server does
// not report it, having been detached outside Terraform.
func serverVolumeCascadeTargets(
	ctx context.Context,
	state ServerResourceModel,
	server *serversdk.ServerDetailSchema,
) ([]core.UUID, diag.Diagnostics) {
	var diags diag.Diagnostics
	ids := make([]core.UUID, 0)
	if boot, ok := decodeConfigObject[BootInputModel](ctx, state.Boot, &diags); ok {
		rootVolumeID, hasRootVolume := serverRootVolumeID(server)
		fallback := bootVolumeDeletedByDefault(normalizedString(boot.BootType))
		if hasRootVolume && boolValueOrDefault(boot.DeleteOnTermination, fallback) {
			ids = append(ids, rootVolumeID)
		}
	}
	if volumes, ok := decodeConfigList[DataVolumeInputModel](ctx, state.DataVolumes, &diags); ok {
		attached := make(map[string]struct{}, len(server.Volumes))
		for _, id := range dataVolumeIDs(server) {
			attached[id] = struct{}{}
		}
		for i, volume := range volumes {
			if !boolValueOrDefault(volume.DeleteOnTermination, true) {
				continue
			}
			if volume.ID.IsNull() || volume.ID.IsUnknown() || strings.TrimSpace(volume.ID.ValueString()) == "" {
				continue
			}
			id, idDiags := parse.UUIDString(volume.ID, fmt.Sprintf("data_volumes[%d].id", i))
			diags.Append(idDiags...)
			if idDiags.HasError() {
				continue
			}
			if _, ok := attached[id.String()]; ok {
				ids = append(ids, id)
			}
		}
	}
	return ids, diags
}

// --- Update decision helpers ---

func resizeRequiresShutdown(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *serversdk.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, detail := range apiErr.Errors {
		message := strings.ToLower(strings.TrimSpace(detail.Message))
		if detail.Code == "invalid_input" && strings.Contains(message, "cannot resize server with power state running") {
			return true
		}
	}
	return false
}

func serverFlavorMatches(actual serversdk.NestedFlavorSchema, expected serversdk.ServerFlavor) (bool, error) {
	discriminator, err := expected.Discriminator()
	if err != nil {
		return false, fmt.Errorf("read resize flavor discriminator: %w", err)
	}
	switch discriminator {
	case "predefined":
		predefined, err := expected.AsPredefinedServerFlavor()
		if err != nil {
			return false, fmt.Errorf("decode predefined resize flavor: %w", err)
		}
		return actual.Id == predefined.Id, nil
	case "custom":
		custom, err := expected.AsCustomServerFlavor()
		if err != nil {
			return false, fmt.Errorf("decode custom resize flavor: %w", err)
		}
		return actual.Vcpus == custom.Vcpus && actual.Ram == custom.Ram, nil
	default:
		return false, fmt.Errorf("unsupported resize flavor discriminator %q", discriminator)
	}
}

func buildServerRebuildBody(
	ctx context.Context,
	bootValue types.Object,
	projectID core.UUID,
	resolveImage serverlookup.ImageResolveFunc,
) (serversdk.ServerRebuildSchema, core.UUID, diag.Diagnostics) {
	body := serversdk.ServerRebuildSchema{ProjectId: projectID}
	var diags diag.Diagnostics
	if bootValue.IsNull() || bootValue.IsUnknown() {
		diags.AddError("Missing boot configuration", "boot must be configured and known.")
		return body, core.UUID{}, diags
	}
	var boot BootInputModel
	diags.Append(bootValue.As(ctx, &boot, basetypes.ObjectAsOptions{})...)
	if diags.HasError() || boot.BootType.IsNull() || boot.BootType.IsUnknown() {
		return body, core.UUID{}, diags
	}
	switch strings.TrimSpace(boot.BootType.ValueString()) {
	case "image", "local_disk":
		image, imageDiags := resolveConfiguredName(
			ctx,
			boot.Image,
			"boot.image",
			"image",
			func(ctx context.Context, name string) (serversdk.ImageSchema, error) {
				return resolveImage(ctx, serverlookup.ImageFilter{Name: &name})
			},
		)
		diags.Append(imageDiags...)
		if !imageDiags.HasError() {
			body.ImageId = &image.Id
			return body, image.Id, diags
		}
	case "custom_image":
		imageID, imageDiags := parse.UUIDString(boot.CustomImageID, "boot.custom_image_id")
		diags.Append(imageDiags...)
		if !imageDiags.HasError() {
			body.CustomImageId = &imageID
			return body, imageID, diags
		}
	default:
		diags.AddError("Unsupported server rebuild", "The configured boot type cannot be rebuilt in place.")
	}
	return body, core.UUID{}, diags
}

func buildSecurityGroupUpdateBody(ctx context.Context, value types.Set) (serversdk.ServerUpdateSecurityGroupSchema, diag.Diagnostics) {
	body := serversdk.ServerUpdateSecurityGroupSchema{SecurityGroups: make([]serversdk.ServerSecurityGroup, 0, len(value.Elements()))}
	var diags diag.Diagnostics
	if value.IsNull() || value.IsUnknown() {
		return body, diags
	}
	var configuredIDs []types.String
	diags.Append(value.ElementsAs(ctx, &configuredIDs, false)...)
	for i, configuredID := range configuredIDs {
		id, idDiags := parse.UUIDString(configuredID, fmt.Sprintf("security_group_ids[%d]", i))
		diags.Append(idDiags...)
		if !idDiags.HasError() {
			body.SecurityGroups = append(body.SecurityGroups, serversdk.ServerSecurityGroup{Id: id})
		}
	}
	return body, diags
}

func flavorChanged(ctx context.Context, plan, state ServerResourceModel) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics
	if plan.Flavor.IsNull() || plan.Flavor.IsUnknown() {
		return false, diags
	}
	if state.Flavor.IsNull() || state.Flavor.IsUnknown() {
		return true, diags
	}
	var planned FlavorInputModel
	var prior FlavorInputModel
	diags.Append(plan.Flavor.As(ctx, &planned, basetypes.ObjectAsOptions{})...)
	diags.Append(state.Flavor.As(ctx, &prior, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return false, diags
	}
	if normalizedString(planned.Kind) != normalizedString(prior.Kind) {
		return true, diags
	}
	switch normalizedString(planned.Kind) {
	case "predefined":
		return normalizedString(planned.Name) != normalizedString(prior.Name), diags
	case "custom":
		familyChanged := normalizedString(prior.Family) != "" && normalizedString(planned.Family) != normalizedString(prior.Family)
		return familyChanged || !planned.VCPUs.Equal(prior.VCPUs) || !planned.RAM.Equal(prior.RAM), diags
	default:
		return !plan.Flavor.Equal(state.Flavor), diags
	}
}

func bootSourceChanged(ctx context.Context, plan, state ServerResourceModel) (bool, diag.Diagnostics) {
	var diags diag.Diagnostics
	if plan.Boot.IsNull() || plan.Boot.IsUnknown() || state.Boot.IsNull() || state.Boot.IsUnknown() {
		return false, diags
	}
	var plannedBoot BootInputModel
	var priorBoot BootInputModel
	diags.Append(plan.Boot.As(ctx, &plannedBoot, basetypes.ObjectAsOptions{})...)
	diags.Append(state.Boot.As(ctx, &priorBoot, basetypes.ObjectAsOptions{})...)
	if diags.HasError() || plannedBoot.BootType.IsNull() || plannedBoot.BootType.IsUnknown() ||
		priorBoot.BootType.IsNull() || priorBoot.BootType.IsUnknown() ||
		strings.TrimSpace(plannedBoot.BootType.ValueString()) != strings.TrimSpace(priorBoot.BootType.ValueString()) {
		return false, diags
	}
	switch strings.TrimSpace(plannedBoot.BootType.ValueString()) {
	case "image", "local_disk":
		return normalizedString(plannedBoot.Image) != normalizedString(priorBoot.Image), diags
	case "custom_image":
		return normalizedString(plannedBoot.CustomImageID) != normalizedString(priorBoot.CustomImageID), diags
	default:
		return false, diags
	}
}

func securityGroupsChanged(plan, state ServerResourceModel) bool {
	return !plan.SecurityGroupIDs.IsNull() && !plan.SecurityGroupIDs.IsUnknown() &&
		!plan.SecurityGroupIDs.Equal(state.SecurityGroupIDs)
}

func validateDesiredPowerState(value types.String) diag.Diagnostics {
	var diags diag.Diagnostics
	if value.IsNull() || value.IsUnknown() {
		return diags
	}
	desired := strings.TrimSpace(value.ValueString())
	if desired != string(serversdk.ServerPowerStateRunning) && desired != string(serversdk.ServerPowerStateShutdown) {
		diags.AddError("Invalid server power state", `power_state must be "running" or "shutdown".`)
	}
	return diags
}

func isDesiredPowerState(value types.String, expected serversdk.ServerPowerState) bool {
	return !value.IsNull() && !value.IsUnknown() && strings.TrimSpace(value.ValueString()) == string(expected)
}

func powerStateIsRunning(value types.String) bool {
	if value.IsNull() || value.IsUnknown() {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(value.ValueString()), string(serversdk.ServerPowerStateRunning))
}

func powerStateIsShutdown(value types.String) bool {
	if value.IsNull() || value.IsUnknown() {
		return false
	}
	s := strings.ToLower(strings.TrimSpace(value.ValueString()))
	return s == string(serversdk.ServerPowerStateShutdown) || s == "shutoff" || s == "stopped"
}

func powerStateChanged(plan, state ServerResourceModel) bool {
	return !plan.PowerState.IsNull() && !plan.PowerState.IsUnknown() &&
		normalizedString(plan.PowerState) != normalizedString(state.PowerState)
}

func serverOperationInProgress(err error) bool {
	var apiErr *serversdk.APIError
	if !errors.Is(err, serversdk.ErrConflict) || !errors.As(err, &apiErr) {
		return false
	}
	for _, detail := range apiErr.Errors {
		if detail.Code == "conflict" && strings.Contains(strings.ToLower(detail.Message), "another operation running on this server") {
			return true
		}
	}
	return false
}

func serverIsBusy(server *serversdk.ServerDetailSchema) bool {
	return server.Status == serversdk.ServerStatusBuilding || server.PowerState == serversdk.ServerPowerStatePending
}

// --- Attachment matching and settling helpers ---

func resolvePrivateIPSubnet(
	ctx context.Context,
	config PrivateIPInputModel,
	index int,
	resolve func(context.Context, networklookup.SubnetFilter) (networksdk.SubnetSchema, error),
) (core.UUID, diag.Diagnostics) {
	var diags diag.Diagnostics
	hasID := normalizedString(config.SubnetID) != ""
	hasCIDR := normalizedString(config.SubnetCIDR) != ""
	if hasID == hasCIDR {
		diags.AddError("Invalid subnet reference", fmt.Sprintf("private_ips[%d] must configure exactly one of subnet_id or subnet_cidr.", index))
		return core.UUID{}, diags
	}
	if hasID {
		id, idDiags := parse.UUIDString(config.SubnetID, fmt.Sprintf("private_ips[%d].subnet_id", index))
		diags.Append(idDiags...)
		return id, diags
	}
	subnet, subnetDiags := resolveConfiguredName(
		ctx,
		config.SubnetCIDR,
		fmt.Sprintf("private_ips[%d].subnet_cidr", index),
		"subnet",
		func(ctx context.Context, cidr string) (networksdk.SubnetSchema, error) {
			return resolve(ctx, networklookup.SubnetFilter{CIDR: &cidr})
		},
	)
	diags.Append(subnetDiags...)
	return subnet.Id, diags
}

func decodePrivateIPChanges(ctx context.Context, plannedValue, priorValue types.List) ([]PrivateIPInputModel, []PrivateIPInputModel, diag.Diagnostics) {
	var planned []PrivateIPInputModel
	var prior []PrivateIPInputModel
	var diags diag.Diagnostics
	diags.Append(plannedValue.ElementsAs(ctx, &planned, false)...)
	if !priorValue.IsNull() && !priorValue.IsUnknown() {
		diags.Append(priorValue.ElementsAs(ctx, &prior, false)...)
	}
	return planned, prior, diags
}

func decodeElasticIPChanges(ctx context.Context, plannedValue, priorValue types.List) ([]ElasticIPInputModel, []ElasticIPInputModel, diag.Diagnostics) {
	var planned []ElasticIPInputModel
	var prior []ElasticIPInputModel
	var diags diag.Diagnostics
	diags.Append(plannedValue.ElementsAs(ctx, &planned, false)...)
	if !priorValue.IsNull() && !priorValue.IsUnknown() {
		diags.Append(priorValue.ElementsAs(ctx, &prior, false)...)
	}
	return planned, prior, diags
}

func matchPrivateIPConfigs(planned, prior []PrivateIPInputModel) ([]int, []bool) {
	matches := make([]int, len(planned))
	usedPrior := make([]bool, len(prior))
	for i := range matches {
		matches[i] = -1
	}
	for plannedIndex := range planned {
		plannedIP := &planned[plannedIndex]
		for j := range prior {
			if !usedPrior[j] && privateIPPlanIdentityEqual(*plannedIP, prior[j]) {
				plannedIP.ID = prior[j].ID
				matches[plannedIndex] = j
				usedPrior[j] = true
				break
			}
		}
	}
	return matches, usedPrior
}

func matchElasticIPConfigs(planned, prior []ElasticIPInputModel) ([]int, []bool) {
	matches := make([]int, len(planned))
	usedPrior := make([]bool, len(prior))
	for i := range matches {
		matches[i] = -1
	}
	for plannedIndex := range planned {
		plannedIP := &planned[plannedIndex]
		for j := range prior {
			if !usedPrior[j] && elasticIPPlanIdentityEqual(*plannedIP, prior[j]) {
				plannedIP.ID = prior[j].ID
				matches[plannedIndex] = j
				usedPrior[j] = true
				break
			}
		}
	}
	return matches, usedPrior
}

func normalizedString(value types.String) string {
	if value.IsNull() || value.IsUnknown() {
		return ""
	}
	return strings.TrimSpace(value.ValueString())
}

func boolValueOrDefault(value types.Bool, fallback bool) bool {
	if value.IsNull() || value.IsUnknown() {
		return fallback
	}
	return value.ValueBool()
}

// privateIPAddressesAssigned reports whether every attachment already carries
// its address. The backend lists an attachment before its address is allocated,
// and refreshing that response into state records a null ip_address for an
// attachment that does have one moments later.
func privateIPAddressesAssigned(privateIPs []serversdk.NestedPrivateIPSchema) bool {
	for _, privateIP := range privateIPs {
		if privateIP.IpAddress == nil {
			return false
		}
	}
	return true
}

// elasticIPAddressesAssigned is the counterpart to privateIPAddressesAssigned.
// Either address family satisfies it because an elastic IP can be IPv6 only.
func elasticIPAddressesAssigned(elasticIPs []serversdk.NestedElasticIPSchema) bool {
	for _, elasticIP := range elasticIPs {
		if elasticIP.IpAddress == nil && elasticIP.Ipv6Address == nil {
			return false
		}
	}
	return true
}

func containsAttachmentIDs(actual, expected map[core.UUID]struct{}) bool {
	for id := range expected {
		if _, ok := actual[id]; !ok {
			return false
		}
	}
	return true
}

func excludesAttachmentIDs(actual, excluded map[core.UUID]struct{}) bool {
	for id := range excluded {
		if _, ok := actual[id]; ok {
			return false
		}
	}
	return true
}

func privateIPsChanged(plan, state ServerResourceModel) bool {
	return !plan.PrivateIPs.IsNull() && !plan.PrivateIPs.IsUnknown() && !plan.PrivateIPs.Equal(state.PrivateIPs)
}

func elasticIPsChanged(plan, state ServerResourceModel) bool {
	return !plan.ElasticIPs.IsNull() && !plan.ElasticIPs.IsUnknown() && !plan.ElasticIPs.Equal(state.ElasticIPs)
}

// --- Attachment plan-modifier helpers ---

func preservePrivateIPState() planmodifier.List {
	return privateIPStatePlanModifier{}
}

func preserveElasticIPState() planmodifier.List {
	return elasticIPStatePlanModifier{}
}

func preserveUnconfiguredAttachmentList(req planmodifier.ListRequest, resp *planmodifier.ListResponse) bool {
	if !req.PlanValue.IsUnknown() {
		return false
	}
	if req.ConfigValue.IsUnknown() {
		return true
	}
	if req.ConfigValue.IsNull() && !req.StateValue.IsNull() && !req.StateValue.IsUnknown() {
		resp.PlanValue = req.StateValue
	}
	return true
}

func preservePrivateIPComputedValues(planned, configured, prior []PrivateIPInputModel) {
	used := make([]bool, len(prior))
	for i := range planned {
		matched := false
		for j := range prior {
			if used[j] || i >= len(configured) || !privateIPPlanIdentityEqual(configured[i], prior[j]) {
				continue
			}
			planned[i].ID = prior[j].ID
			planned[i].IPAddress = prior[j].IPAddress
			planned[i].MACAddress = prior[j].MACAddress
			used[j] = true
			matched = true
			break
		}
		if matched {
			continue
		}
		if i >= len(configured) || configured[i].ID.IsNull() || configured[i].ID.IsUnknown() {
			planned[i].ID = types.StringUnknown()
		}
		if i >= len(configured) || configured[i].DeleteOnTermination.IsNull() || configured[i].DeleteOnTermination.IsUnknown() {
			planned[i].DeleteOnTermination = types.BoolUnknown()
		}
		planned[i].IPAddress = types.StringUnknown()
		planned[i].MACAddress = types.StringUnknown()
	}
}

func preserveElasticIPComputedValues(planned, configured, prior []ElasticIPInputModel) {
	used := make([]bool, len(prior))
	matchedCount := 0
	for i := range planned {
		matched := false
		for j := range prior {
			if used[j] || i >= len(configured) || !elasticIPPlanIdentityEqual(configured[i], prior[j]) {
				continue
			}
			planned[i].ID = prior[j].ID
			planned[i].IPAddress = prior[j].IPAddress
			planned[i].IPv6Address = prior[j].IPv6Address
			planned[i].Status = prior[j].Status
			used[j] = true
			matched = true
			matchedCount++
			break
		}
		if matched {
			continue
		}
		if i >= len(configured) || configured[i].ID.IsNull() || configured[i].ID.IsUnknown() {
			planned[i].ID = types.StringUnknown()
		}
		if i >= len(configured) || configured[i].DeleteOnTermination.IsNull() || configured[i].DeleteOnTermination.IsUnknown() {
			planned[i].DeleteOnTermination = types.BoolUnknown()
		}
		planned[i].IPAddress = types.StringUnknown()
		planned[i].IPv6Address = types.StringUnknown()
		planned[i].Status = types.StringUnknown()
	}
	if matchedCount != len(planned) || len(planned) != len(prior) {
		for i := range planned {
			planned[i].Status = types.StringUnknown()
		}
	}
}

func privateIPPlanIdentityEqual(left, right PrivateIPInputModel) bool {
	if normalizedString(left.Kind) != normalizedString(right.Kind) {
		return false
	}
	if normalizedString(left.Kind) == "subnet" {
		return normalizedString(left.SubnetID) == normalizedString(right.SubnetID) &&
			normalizedString(left.SubnetCIDR) == normalizedString(right.SubnetCIDR)
	}
	return normalizedString(left.ID) != "" && normalizedString(left.ID) == normalizedString(right.ID)
}

func elasticIPPlanIdentityEqual(left, right ElasticIPInputModel) bool {
	if normalizedString(left.Kind) != normalizedString(right.Kind) {
		return false
	}
	if normalizedString(left.Kind) == "new" {
		if left.EnableIPv4.IsUnknown() || left.EnableIPv6.IsUnknown() {
			return false
		}
		return boolValueOrDefault(left.EnableIPv4, true) == boolValueOrDefault(right.EnableIPv4, true) &&
			boolValueOrDefault(left.EnableIPv6, false) == boolValueOrDefault(right.EnableIPv6, false)
	}
	return normalizedString(left.ID) != "" && normalizedString(left.ID) == normalizedString(right.ID)
}

// --- Create request construction ---

func buildServerCreateBody(
	ctx context.Context,
	plan ServerResourceModel,
	resolveZone zoneResolveFunc,
	resolveFlavor serverlookup.FlavorResolveFunc,
	resolveImage serverlookup.ImageResolveFunc,
	resolveVolumeType blockstoragelookup.VolumeTypeResolveFunc,
	resolveSubnet subnetResolveFunc,
) (serversdk.ServerCreateSchema, diag.Diagnostics) {
	var body serversdk.ServerCreateSchema
	var diags diag.Diagnostics
	if plan.Name.IsNull() || plan.Name.IsUnknown() || strings.TrimSpace(plan.Name.ValueString()) == "" {
		diags.AddError("Missing server name", "name must be configured and known.")
	} else {
		body.Name = strings.TrimSpace(plan.Name.ValueString())
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		description := plan.Description.ValueString()
		body.Description = &description
	}
	if !plan.UserData.IsNull() && !plan.UserData.IsUnknown() {
		userData := plan.UserData.ValueString()
		body.UserData = &userData
	}
	if !plan.Bandwidth.IsNull() && !plan.Bandwidth.IsUnknown() {
		bandwidth := int(plan.Bandwidth.ValueInt64())
		body.Bandwidth = &bandwidth
	}
	quantity := 1
	if !plan.Quantity.IsNull() && !plan.Quantity.IsUnknown() {
		quantity = int(plan.Quantity.ValueInt64())
	}
	if quantity != 1 {
		diags.AddError("Unsupported server quantity", "quantity must be 1 because each Terraform resource manages one server.")
	}
	body.Quantity = &quantity

	if !plan.Zone.IsNull() && !plan.Zone.IsUnknown() {
		zone, zoneDiags := resolveConfiguredName(ctx, plan.Zone, "zone", "zone", resolveZone)
		diags.Append(zoneDiags...)
		if !zoneDiags.HasError() {
			body.ZoneId = &zone.Id
		}
	}
	if !plan.KeyPairID.IsNull() && !plan.KeyPairID.IsUnknown() {
		keyPairID, idDiags := parse.UUIDString(plan.KeyPairID, "key_pair_id")
		diags.Append(idDiags...)
		if !idDiags.HasError() {
			body.KeyPairId = &keyPairID
		}
	}
	if !plan.PlacementGroupID.IsNull() && !plan.PlacementGroupID.IsUnknown() {
		placementGroupID, idDiags := parse.UUIDString(plan.PlacementGroupID, "placement_group_id")
		diags.Append(idDiags...)
		if !idDiags.HasError() {
			body.PlacementGroupId = &placementGroupID
		}
	}
	body.Flavor = buildFlavor(ctx, plan.Flavor, body.ZoneId, resolveFlavor, &diags)

	var boot BootInputModel
	if plan.Boot.IsNull() || plan.Boot.IsUnknown() {
		diags.AddError("Missing boot configuration", "boot must be configured and known.")
	} else {
		bootDiags := plan.Boot.As(ctx, &boot, basetypes.ObjectAsOptions{})
		diags.Append(bootDiags...)
		if !bootDiags.HasError() {
			bootBody, valueDiags := buildBoot(ctx, boot, body.ZoneId, resolveImage, resolveVolumeType)
			diags.Append(valueDiags...)
			body.Boot = bootBody
		}
	}

	buildPrivateIPs(ctx, plan.PrivateIPs, &body, resolveSubnet, &diags)
	buildDataVolumes(ctx, plan.DataVolumes, &body, body.ZoneId, resolveVolumeType, &diags)
	buildElasticIPs(ctx, plan.ElasticIPs, &body, &diags)
	buildSecurityGroups(ctx, plan.SecurityGroupIDs, &body, &diags)
	return body, diags
}

func buildFlavor(
	ctx context.Context,
	value types.Object,
	zoneID *core.UUID,
	resolve serverlookup.FlavorResolveFunc,
	diags *diag.Diagnostics,
) serversdk.ServerFlavor {
	var result serversdk.ServerFlavor
	if value.IsNull() || value.IsUnknown() {
		diags.AddError("Missing flavor", "flavor must be configured and known.")
		return result
	}
	var flavor FlavorInputModel
	valueDiags := value.As(ctx, &flavor, basetypes.ObjectAsOptions{})
	diags.Append(valueDiags...)
	if valueDiags.HasError() || flavor.Kind.IsNull() || flavor.Kind.IsUnknown() {
		if flavor.Kind.IsNull() || flavor.Kind.IsUnknown() {
			diags.AddError("Missing flavor kind", "flavor.kind must be configured and known.")
		}
		return result
	}

	switch strings.TrimSpace(flavor.Kind.ValueString()) {
	case "predefined":
		resolved, resolveDiags := resolveConfiguredName(
			ctx,
			flavor.Name,
			"flavor.name",
			"predefined flavor",
			func(ctx context.Context, name string) (serversdk.FlavorSchema, error) {
				return resolve(ctx, serverlookup.FlavorFilter{Name: &name, ZoneID: zoneID})
			},
		)
		diags.Append(resolveDiags...)
		if !resolveDiags.HasError() {
			appendUnionDiagnostic(diags, result.FromPredefinedServerFlavor(serversdk.PredefinedServerFlavor{Id: resolved.Id}), "flavor")
		}
	case "custom":
		if flavor.Family.IsNull() || flavor.Family.IsUnknown() || flavor.VCPUs.IsNull() || flavor.VCPUs.IsUnknown() ||
			flavor.RAM.IsNull() || flavor.RAM.IsUnknown() {
			diags.AddError("Incomplete custom flavor", "flavor.family, flavor.vcpus, and flavor.ram must be configured and known.")
			return result
		}
		if flavor.VCPUs.ValueInt64() < 1 || flavor.RAM.ValueInt64() < 1 {
			diags.AddError("Invalid custom flavor", "flavor.vcpus and flavor.ram must be greater than zero.")
			return result
		}
		appendUnionDiagnostic(diags, result.FromCustomServerFlavor(serversdk.CustomServerFlavor{
			Family: serversdk.FlavorFamily(strings.TrimSpace(flavor.Family.ValueString())),
			Vcpus:  int(flavor.VCPUs.ValueInt64()),
			Ram:    int(flavor.RAM.ValueInt64()),
		}), "flavor")
	default:
		diags.AddError("Invalid flavor kind", `flavor.kind must be "predefined" or "custom".`)
	}
	return result
}

func buildBoot(
	ctx context.Context,
	boot BootInputModel,
	zoneID *core.UUID,
	resolveImage serverlookup.ImageResolveFunc,
	resolveVolumeType blockstoragelookup.VolumeTypeResolveFunc,
) (serversdk.ServerBootOptions, diag.Diagnostics) {
	var result serversdk.ServerBootOptions
	var diags diag.Diagnostics
	if boot.BootType.IsNull() || boot.BootType.IsUnknown() {
		diags.AddError("Missing boot type", "boot.boot_type must be configured and known.")
		return result, diags
	}

	switch strings.TrimSpace(boot.BootType.ValueString()) {
	case "image":
		volumeType, ok := resolveBootVolumeType(ctx, boot, zoneID, resolveVolumeType, &diags)
		if !ok {
			return result, diags
		}
		image, imageDiags := resolveConfiguredName(
			ctx,
			boot.Image,
			"boot.image",
			"image",
			func(ctx context.Context, name string) (serversdk.ImageSchema, error) {
				return resolveImage(ctx, serverlookup.ImageFilter{Name: &name})
			},
		)
		diags.Append(imageDiags...)
		if imageDiags.HasError() {
			return result, diags
		}
		value := serversdk.ServerBootFromImage{
			ImageId: image.Id, VolumeTypeId: volumeType.Id, VolumeSize: int(boot.VolumeSize.ValueInt64()),
		}
		if !boot.IOPS.IsNull() && !boot.IOPS.IsUnknown() {
			iops := int(boot.IOPS.ValueInt64())
			value.Iops = &iops
		}
		appendUnionDiagnostic(&diags, result.FromServerBootFromImage(value), "boot")
	case "custom_image":
		customImageID, idDiags := parse.UUIDString(boot.CustomImageID, "boot.custom_image_id")
		diags.Append(idDiags...)
		volumeType, ok := resolveBootVolumeType(ctx, boot, zoneID, resolveVolumeType, &diags)
		if idDiags.HasError() || !ok {
			return result, diags
		}
		value := serversdk.ServerBootFromCustomImage{
			CustomImageId: customImageID, VolumeTypeId: volumeType.Id, VolumeSize: int(boot.VolumeSize.ValueInt64()),
		}
		if !boot.IOPS.IsNull() && !boot.IOPS.IsUnknown() {
			iops := int(boot.IOPS.ValueInt64())
			value.Iops = &iops
		}
		appendUnionDiagnostic(&diags, result.FromServerBootFromCustomImage(value), "boot")
	case "local_disk":
		image, imageDiags := resolveConfiguredName(
			ctx,
			boot.Image,
			"boot.image",
			"image",
			func(ctx context.Context, name string) (serversdk.ImageSchema, error) {
				return resolveImage(ctx, serverlookup.ImageFilter{Name: &name})
			},
		)
		diags.Append(imageDiags...)
		if !imageDiags.HasError() {
			appendUnionDiagnostic(&diags, result.FromServerBootFromLocalDisk(serversdk.ServerBootFromLocalDisk{ImageId: image.Id}), "boot")
		}
	case "volume":
		volumeID, idDiags := parse.UUIDString(boot.VolumeID, "boot.volume_id")
		diags.Append(idDiags...)
		if !idDiags.HasError() {
			appendUnionDiagnostic(&diags, result.FromServerBootFromVolume(serversdk.ServerBootFromVolume{VolumeId: volumeID}), "boot")
		}
	default:
		diags.AddError("Invalid boot type", "boot.boot_type must be image, custom_image, local_disk, or volume.")
	}
	return result, diags
}

func resolveBootVolumeType(
	ctx context.Context,
	boot BootInputModel,
	zoneID *core.UUID,
	resolve blockstoragelookup.VolumeTypeResolveFunc,
	diags *diag.Diagnostics,
) (blockstoragesdk.VolumeTypeSchema, bool) {
	if boot.VolumeSize.IsNull() || boot.VolumeSize.IsUnknown() || boot.VolumeSize.ValueInt64() < 1 {
		diags.AddError("Invalid boot volume size", "boot.volume_size must be known and greater than zero.")
		return blockstoragesdk.VolumeTypeSchema{}, false
	}
	volumeType, valueDiags := resolveConfiguredName(
		ctx,
		boot.VolumeType,
		"boot.volume_type",
		"volume type",
		func(ctx context.Context, name string) (blockstoragesdk.VolumeTypeSchema, error) {
			return resolve(ctx, blockstoragelookup.VolumeTypeResolveRequest{
				Name: name, ZoneID: zoneID, RequestedSize: int(boot.VolumeSize.ValueInt64()),
			})
		},
	)
	diags.Append(valueDiags...)
	return volumeType, !valueDiags.HasError()
}

func buildPrivateIPs(
	ctx context.Context,
	value types.List,
	body *serversdk.ServerCreateSchema,
	resolveSubnet subnetResolveFunc,
	diags *diag.Diagnostics,
) {
	if value.IsNull() || value.IsUnknown() {
		return
	}
	var configs []PrivateIPInputModel
	valueDiags := value.ElementsAs(ctx, &configs, false)
	diags.Append(valueDiags...)
	if valueDiags.HasError() || len(configs) == 0 {
		return
	}
	items := make([]serversdk.ServerPrivateIP, 0, len(configs))
	for i, config := range configs {
		if config.Kind.IsNull() || config.Kind.IsUnknown() {
			diags.AddError("Missing private IP kind", fmt.Sprintf("private_ips[%d].kind must be configured and known.", i))
			continue
		}
		var item serversdk.ServerPrivateIP
		switch strings.TrimSpace(config.Kind.ValueString()) {
		case "subnet":
			if normalizedString(config.ID) != "" {
				diags.AddError("Invalid subnet private IP", fmt.Sprintf("private_ips[%d].id must not be configured when kind is subnet.", i))
				continue
			}
			hasID := normalizedString(config.SubnetID) != ""
			hasCIDR := normalizedString(config.SubnetCIDR) != ""
			if hasID == hasCIDR {
				diags.AddError("Invalid subnet reference", fmt.Sprintf("private_ips[%d] must configure exactly one of subnet_id or subnet_cidr.", i))
				continue
			}
			var subnetID core.UUID
			if hasID {
				parsed, idDiags := parse.UUIDString(config.SubnetID, fmt.Sprintf("private_ips[%d].subnet_id", i))
				diags.Append(idDiags...)
				if idDiags.HasError() {
					continue
				}
				subnetID = parsed
			} else {
				subnet, subnetDiags := resolveConfiguredName(
					ctx,
					config.SubnetCIDR,
					fmt.Sprintf("private_ips[%d].subnet_cidr", i),
					"subnet",
					resolveSubnet,
				)
				diags.Append(subnetDiags...)
				if subnetDiags.HasError() {
					continue
				}
				subnetID = subnet.Id
			}
			if err := item.FromNewServerPrivateIP(serversdk.NewServerPrivateIP{SubnetId: subnetID}); err != nil {
				appendUnionDiagnostic(diags, err, fmt.Sprintf("private_ips[%d]", i))
				continue
			}
		case "ip":
			if normalizedString(config.SubnetID) != "" || normalizedString(config.SubnetCIDR) != "" {
				diags.AddError("Invalid private IP reference", fmt.Sprintf("private_ips[%d] must not configure subnet_id or subnet_cidr when kind is ip.", i))
				continue
			}
			id, idDiags := parse.UUIDString(config.ID, fmt.Sprintf("private_ips[%d].id", i))
			diags.Append(idDiags...)
			if idDiags.HasError() {
				continue
			}
			if err := item.FromExistingServerPrivateIP(serversdk.ExistingServerPrivateIP{Id: id}); err != nil {
				appendUnionDiagnostic(diags, err, fmt.Sprintf("private_ips[%d]", i))
				continue
			}
		default:
			diags.AddError("Invalid private IP kind", fmt.Sprintf("private_ips[%d].kind must be subnet or ip.", i))
			continue
		}
		items = append(items, item)
	}
	body.PrivateIps = &items
}

func buildDataVolumes(
	ctx context.Context,
	value types.List,
	body *serversdk.ServerCreateSchema,
	zoneID *core.UUID,
	resolve blockstoragelookup.VolumeTypeResolveFunc,
	diags *diag.Diagnostics,
) {
	if value.IsNull() || value.IsUnknown() {
		return
	}
	var configs []DataVolumeInputModel
	valueDiags := value.ElementsAs(ctx, &configs, false)
	diags.Append(valueDiags...)
	if valueDiags.HasError() || len(configs) == 0 {
		return
	}
	items := make([]serversdk.ServerDataVolume, 0, len(configs))
	for i, config := range configs {
		if config.VolumeSize.IsNull() || config.VolumeSize.IsUnknown() || config.VolumeSize.ValueInt64() < 1 {
			diags.AddError("Invalid data volume size", fmt.Sprintf("data_volumes[%d].volume_size must be known and greater than zero.", i))
			continue
		}
		volumeType, typeDiags := resolveConfiguredName(
			ctx,
			config.VolumeType,
			fmt.Sprintf("data_volumes[%d].volume_type", i),
			"volume type",
			func(ctx context.Context, name string) (blockstoragesdk.VolumeTypeSchema, error) {
				return resolve(ctx, blockstoragelookup.VolumeTypeResolveRequest{
					Name: name, ZoneID: zoneID, RequestedSize: int(config.VolumeSize.ValueInt64()),
				})
			},
		)
		diags.Append(typeDiags...)
		if typeDiags.HasError() {
			continue
		}
		item := serversdk.ServerDataVolume{VolumeTypeId: volumeType.Id, VolumeSize: int(config.VolumeSize.ValueInt64())}
		if !config.IOPS.IsNull() && !config.IOPS.IsUnknown() {
			iops := int(config.IOPS.ValueInt64())
			item.Iops = &iops
		}
		items = append(items, item)
	}
	body.DataVolumes = &items
}

func buildElasticIPs(ctx context.Context, value types.List, body *serversdk.ServerCreateSchema, diags *diag.Diagnostics) {
	if value.IsNull() || value.IsUnknown() {
		return
	}
	var configs []ElasticIPInputModel
	valueDiags := value.ElementsAs(ctx, &configs, false)
	diags.Append(valueDiags...)
	if valueDiags.HasError() || len(configs) == 0 {
		return
	}
	items := make([]serversdk.ServerElasticIP, 0, len(configs))
	for i, config := range configs {
		if config.Kind.IsNull() || config.Kind.IsUnknown() {
			diags.AddError("Missing elastic IP kind", fmt.Sprintf("elastic_ips[%d].kind must be configured and known.", i))
			continue
		}
		var item serversdk.ServerElasticIP
		switch strings.TrimSpace(config.Kind.ValueString()) {
		case "existing":
			id, idDiags := parse.UUIDString(config.ID, fmt.Sprintf("elastic_ips[%d].id", i))
			diags.Append(idDiags...)
			if idDiags.HasError() {
				continue
			}
			if err := item.FromExistingServerElasticIP(serversdk.ExistingServerElasticIP{Id: id}); err != nil {
				appendUnionDiagnostic(diags, err, fmt.Sprintf("elastic_ips[%d]", i))
				continue
			}
		case "new":
			if normalizedString(config.ID) != "" {
				diags.AddError("Invalid new elastic IP", fmt.Sprintf("elastic_ips[%d].id must not be configured when kind is new.", i))
				continue
			}
			if config.EnableIPv4.IsUnknown() || config.EnableIPv6.IsUnknown() {
				diags.AddError("Unknown new elastic IP address family", fmt.Sprintf("elastic_ips[%d].enable_ipv4 and enable_ipv6 must be known when configured.", i))
				continue
			}
			enableIPv4 := config.EnableIPv4.IsNull() || config.EnableIPv4.IsUnknown() || config.EnableIPv4.ValueBool()
			enableIPv6 := !config.EnableIPv6.IsNull() && !config.EnableIPv6.IsUnknown() && config.EnableIPv6.ValueBool()
			if err := item.FromNewServerElasticIP(serversdk.NewServerElasticIP{EnableIpv4: enableIPv4, EnableIpv6: enableIPv6}); err != nil {
				appendUnionDiagnostic(diags, err, fmt.Sprintf("elastic_ips[%d]", i))
				continue
			}
		default:
			diags.AddError("Invalid elastic IP kind", fmt.Sprintf("elastic_ips[%d].kind must be existing or new.", i))
			continue
		}
		items = append(items, item)
	}
	body.ElasticIps = &items
}

func buildSecurityGroups(ctx context.Context, value types.Set, body *serversdk.ServerCreateSchema, diags *diag.Diagnostics) {
	if value.IsNull() || value.IsUnknown() {
		return
	}
	var configuredIDs []types.String
	diags.Append(value.ElementsAs(ctx, &configuredIDs, false)...)
	if diags.HasError() {
		return
	}
	securityGroups := make([]serversdk.ServerSecurityGroup, 0, len(configuredIDs))
	for i, configuredID := range configuredIDs {
		id, idDiags := parse.UUIDString(configuredID, fmt.Sprintf("security_group_ids[%d]", i))
		diags.Append(idDiags...)
		if !idDiags.HasError() {
			securityGroups = append(securityGroups, serversdk.ServerSecurityGroup{Id: id})
		}
	}
	body.SecurityGroups = &securityGroups
}

func resolveConfiguredName[T any](
	ctx context.Context,
	value types.String,
	attributePath string,
	resourceName string,
	resolve func(context.Context, string) (T, error),
) (T, diag.Diagnostics) {
	var zero T
	var diags diag.Diagnostics
	name := normalizedString(value)
	if name == "" {
		diags.AddError("Missing "+resourceName, attributePath+" must be configured and known.")
		return zero, diags
	}
	resolved, err := resolve(ctx, name)
	if err != nil {
		diags.AddError("Unable to resolve "+resourceName, fmt.Sprintf("%s: %s", attributePath, err))
		return zero, diags
	}
	return resolved, diags
}

func appendUnionDiagnostic(diags *diag.Diagnostics, err error, field string) {
	if err != nil {
		diags.AddError("Invalid "+field+" configuration", err.Error())
	}
}

// --- Configuration validation helpers ---

func validateServerQuantity(value types.Int64, diags *diag.Diagnostics) {
	if !value.IsNull() && !value.IsUnknown() && value.ValueInt64() != 1 {
		diags.AddAttributeError(
			path.Root("quantity"),
			"Unsupported server quantity",
			"quantity must be 1 because each Terraform resource manages one server. Use count or for_each to manage several servers.",
		)
	}
}

func validateServerPowerState(value types.String, diags *diag.Diagnostics) {
	if value.IsNull() || value.IsUnknown() {
		return
	}
	desired := normalizedString(value)
	if desired != string(serversdk.ServerPowerStateRunning) && desired != string(serversdk.ServerPowerStateShutdown) {
		diags.AddAttributeError(
			path.Root("power_state"),
			"Invalid server power state",
			`power_state must be "running" or "shutdown".`,
		)
	}
}

func validateServerFlavor(ctx context.Context, value types.Object, diags *diag.Diagnostics) {
	flavor, ok := decodeConfigObject[FlavorInputModel](ctx, value, diags)
	if !ok || flavor.Kind.IsNull() || flavor.Kind.IsUnknown() {
		return
	}
	root := path.Root("flavor")
	switch normalizedString(flavor.Kind) {
	case "predefined":
		requireConfigured(flavor.Name, root.AtName("name"), "flavor.name", `flavor.kind is "predefined"`, diags)
		rejectConfigured(flavor.Family, root.AtName("family"), "flavor.family", `flavor.kind is "predefined"`, diags)
		rejectConfiguredInt64(flavor.VCPUs, root.AtName("vcpus"), "flavor.vcpus", `flavor.kind is "predefined"`, diags)
		rejectConfiguredInt64(flavor.RAM, root.AtName("ram"), "flavor.ram", `flavor.kind is "predefined"`, diags)
	case "custom":
		requireConfigured(flavor.Family, root.AtName("family"), "flavor.family", `flavor.kind is "custom"`, diags)
		requireConfiguredInt64(flavor.VCPUs, root.AtName("vcpus"), "flavor.vcpus", `flavor.kind is "custom"`, diags)
		requireConfiguredInt64(flavor.RAM, root.AtName("ram"), "flavor.ram", `flavor.kind is "custom"`, diags)
		rejectConfigured(flavor.Name, root.AtName("name"), "flavor.name", `flavor.kind is "custom"`, diags)
		requirePositiveInt64(flavor.VCPUs, root.AtName("vcpus"), "flavor.vcpus", diags)
		requirePositiveInt64(flavor.RAM, root.AtName("ram"), "flavor.ram", diags)
		if !flavor.Family.IsNull() && !flavor.Family.IsUnknown() &&
			!serversdk.FlavorFamily(normalizedString(flavor.Family)).Valid() {
			diags.AddAttributeError(
				root.AtName("family"),
				"Invalid custom flavor family",
				"flavor.family must be basic, premium, enterprise, gpu, or spot.",
			)
		}
	default:
		diags.AddAttributeError(
			root.AtName("kind"),
			"Invalid flavor kind",
			`flavor.kind must be "predefined" or "custom".`,
		)
	}
}

func validateServerBoot(ctx context.Context, value types.Object, diags *diag.Diagnostics) {
	boot, ok := decodeConfigObject[BootInputModel](ctx, value, diags)
	if !ok || boot.BootType.IsNull() || boot.BootType.IsUnknown() {
		return
	}
	root := path.Root("boot")
	bootType := normalizedString(boot.BootType)
	because := fmt.Sprintf("boot.boot_type is %q", bootType)
	switch bootType {
	case "image", "custom_image":
		if bootType == "image" {
			requireConfigured(boot.Image, root.AtName("image"), "boot.image", because, diags)
			rejectConfigured(boot.CustomImageID, root.AtName("custom_image_id"), "boot.custom_image_id", because, diags)
		} else {
			requireConfigured(boot.CustomImageID, root.AtName("custom_image_id"), "boot.custom_image_id", because, diags)
			rejectConfigured(boot.Image, root.AtName("image"), "boot.image", because, diags)
		}
		requireConfigured(boot.VolumeType, root.AtName("volume_type"), "boot.volume_type", because, diags)
		requireConfiguredInt64(boot.VolumeSize, root.AtName("volume_size"), "boot.volume_size", because, diags)
		requirePositiveInt64(boot.VolumeSize, root.AtName("volume_size"), "boot.volume_size", diags)
		requirePositiveInt64(boot.IOPS, root.AtName("iops"), "boot.iops", diags)
		rejectConfigured(boot.VolumeID, root.AtName("volume_id"), "boot.volume_id", because, diags)
	case "local_disk":
		requireConfigured(boot.Image, root.AtName("image"), "boot.image", because, diags)
		rejectConfigured(boot.CustomImageID, root.AtName("custom_image_id"), "boot.custom_image_id", because, diags)
		rejectConfigured(boot.VolumeID, root.AtName("volume_id"), "boot.volume_id", because, diags)
		rejectConfigured(boot.VolumeType, root.AtName("volume_type"), "boot.volume_type", because, diags)
		rejectConfiguredInt64(boot.VolumeSize, root.AtName("volume_size"), "boot.volume_size", because, diags)
		rejectConfiguredInt64(boot.IOPS, root.AtName("iops"), "boot.iops", because, diags)
		rejectConfiguredBool(
			boot.DeleteOnTermination,
			root.AtName("delete_on_termination"),
			"boot.delete_on_termination",
			because,
			diags,
		)
	case "volume":
		requireConfigured(boot.VolumeID, root.AtName("volume_id"), "boot.volume_id", because, diags)
		rejectConfigured(boot.Image, root.AtName("image"), "boot.image", because, diags)
		rejectConfigured(boot.CustomImageID, root.AtName("custom_image_id"), "boot.custom_image_id", because, diags)
		rejectConfigured(boot.VolumeType, root.AtName("volume_type"), "boot.volume_type", because, diags)
		rejectConfiguredInt64(boot.VolumeSize, root.AtName("volume_size"), "boot.volume_size", because, diags)
		rejectConfiguredInt64(boot.IOPS, root.AtName("iops"), "boot.iops", because, diags)
	default:
		diags.AddAttributeError(
			root.AtName("boot_type"),
			"Invalid boot type",
			"boot.boot_type must be image, custom_image, local_disk, or volume.",
		)
	}
}

func validateServerPrivateIPs(ctx context.Context, value types.List, diags *diag.Diagnostics) {
	configs, ok := decodeConfigList[PrivateIPInputModel](ctx, value, diags)
	if !ok {
		return
	}
	seen := make(map[string]int, len(configs))
	for i, config := range configs {
		element := path.Root("private_ips").AtListIndex(i)
		if config.Kind.IsNull() || config.Kind.IsUnknown() {
			continue
		}
		switch normalizedString(config.Kind) {
		case "subnet":
			rejectConfigured(config.ID, element.AtName("id"), fmt.Sprintf("private_ips[%d].id", i), `kind is "subnet"`, diags)
			if config.SubnetID.IsNull() == config.SubnetCIDR.IsNull() {
				diags.AddAttributeError(
					element,
					"Invalid subnet reference",
					fmt.Sprintf("private_ips[%d] must configure exactly one of subnet_id or subnet_cidr.", i),
				)
			}
		case "ip":
			requireConfigured(config.ID, element.AtName("id"), fmt.Sprintf("private_ips[%d].id", i), `kind is "ip"`, diags)
			rejectConfigured(config.SubnetID, element.AtName("subnet_id"), fmt.Sprintf("private_ips[%d].subnet_id", i), `kind is "ip"`, diags)
			rejectConfigured(config.SubnetCIDR, element.AtName("subnet_cidr"), fmt.Sprintf("private_ips[%d].subnet_cidr", i), `kind is "ip"`, diags)
			rejectDuplicateID(config.ID, element.AtName("id"), "private_ips", i, seen, diags)
		default:
			diags.AddAttributeError(
				element.AtName("kind"),
				"Invalid private IP kind",
				fmt.Sprintf("private_ips[%d].kind must be subnet or ip.", i),
			)
		}
	}
}

func validateServerElasticIPs(ctx context.Context, value types.List, diags *diag.Diagnostics) {
	configs, ok := decodeConfigList[ElasticIPInputModel](ctx, value, diags)
	if !ok {
		return
	}
	seen := make(map[string]int, len(configs))
	for i, config := range configs {
		element := path.Root("elastic_ips").AtListIndex(i)
		if config.Kind.IsNull() || config.Kind.IsUnknown() {
			continue
		}
		switch normalizedString(config.Kind) {
		case "existing":
			requireConfigured(config.ID, element.AtName("id"), fmt.Sprintf("elastic_ips[%d].id", i), `kind is "existing"`, diags)
			rejectConfiguredBool(config.EnableIPv4, element.AtName("enable_ipv4"), fmt.Sprintf("elastic_ips[%d].enable_ipv4", i), `kind is "existing"`, diags)
			rejectConfiguredBool(config.EnableIPv6, element.AtName("enable_ipv6"), fmt.Sprintf("elastic_ips[%d].enable_ipv6", i), `kind is "existing"`, diags)
			rejectDuplicateID(config.ID, element.AtName("id"), "elastic_ips", i, seen, diags)
		case "new":
			rejectConfigured(config.ID, element.AtName("id"), fmt.Sprintf("elastic_ips[%d].id", i), `kind is "new"`, diags)
			if !boolValueOrDefault(config.EnableIPv4, true) && !boolValueOrDefault(config.EnableIPv6, false) &&
				!config.EnableIPv4.IsUnknown() && !config.EnableIPv6.IsUnknown() {
				diags.AddAttributeError(
					element,
					"Invalid new elastic IP",
					fmt.Sprintf("elastic_ips[%d] must enable IPv4, IPv6, or both.", i),
				)
			}
		default:
			diags.AddAttributeError(
				element.AtName("kind"),
				"Invalid elastic IP kind",
				fmt.Sprintf("elastic_ips[%d].kind must be existing or new.", i),
			)
		}
	}
}

func validateServerDataVolumes(ctx context.Context, value types.List, diags *diag.Diagnostics) {
	configs, ok := decodeConfigList[DataVolumeInputModel](ctx, value, diags)
	if !ok {
		return
	}
	for i, config := range configs {
		element := path.Root("data_volumes").AtListIndex(i)
		requirePositiveInt64(config.VolumeSize, element.AtName("volume_size"), fmt.Sprintf("data_volumes[%d].volume_size", i), diags)
		requirePositiveInt64(config.IOPS, element.AtName("iops"), fmt.Sprintf("data_volumes[%d].iops", i), diags)
	}
}

func decodeConfigObject[T any](ctx context.Context, value types.Object, diags *diag.Diagnostics) (T, bool) {
	var decoded T
	if value.IsNull() || value.IsUnknown() {
		return decoded, false
	}
	valueDiags := value.As(ctx, &decoded, basetypes.ObjectAsOptions{})
	diags.Append(valueDiags...)
	return decoded, !valueDiags.HasError()
}

func decodeConfigList[T any](ctx context.Context, value types.List, diags *diag.Diagnostics) ([]T, bool) {
	if value.IsNull() || value.IsUnknown() {
		return nil, false
	}
	var decoded []T
	valueDiags := value.ElementsAs(ctx, &decoded, false)
	diags.Append(valueDiags...)
	return decoded, !valueDiags.HasError()
}

// requireConfigured treats an unknown value as configured because Terraform
// resolves it before apply.
func requireConfigured(value types.String, attribute path.Path, name, because string, diags *diag.Diagnostics) {
	if value.IsNull() || (!value.IsUnknown() && strings.TrimSpace(value.ValueString()) == "") {
		diags.AddAttributeError(
			attribute,
			"Missing "+name,
			fmt.Sprintf("%s must be configured with a non-empty value because %s.", name, because),
		)
	}
}

func rejectConfigured(value types.String, attribute path.Path, name, because string, diags *diag.Diagnostics) {
	if !value.IsNull() {
		diags.AddAttributeError(
			attribute,
			"Unsupported "+name,
			fmt.Sprintf("%s must not be configured because %s.", name, because),
		)
	}
}

func requireConfiguredInt64(value types.Int64, attribute path.Path, name, because string, diags *diag.Diagnostics) {
	if value.IsNull() {
		diags.AddAttributeError(
			attribute,
			"Missing "+name,
			fmt.Sprintf("%s must be configured because %s.", name, because),
		)
	}
}

func rejectConfiguredInt64(value types.Int64, attribute path.Path, name, because string, diags *diag.Diagnostics) {
	if !value.IsNull() {
		diags.AddAttributeError(
			attribute,
			"Unsupported "+name,
			fmt.Sprintf("%s must not be configured because %s.", name, because),
		)
	}
}

func rejectConfiguredBool(value types.Bool, attribute path.Path, name, because string, diags *diag.Diagnostics) {
	if !value.IsNull() {
		diags.AddAttributeError(
			attribute,
			"Unsupported "+name,
			fmt.Sprintf("%s must not be configured because %s.", name, because),
		)
	}
}

func requirePositiveInt64(value types.Int64, attribute path.Path, name string, diags *diag.Diagnostics) {
	if !value.IsNull() && !value.IsUnknown() && value.ValueInt64() < 1 {
		diags.AddAttributeError(
			attribute,
			"Invalid "+name,
			fmt.Sprintf("%s must be greater than zero, got %d.", name, value.ValueInt64()),
		)
	}
}

func rejectDuplicateID(value types.String, attribute path.Path, list string, index int, seen map[string]int, diags *diag.Diagnostics) {
	if value.IsNull() || value.IsUnknown() {
		return
	}
	id := normalizedString(value)
	if first, ok := seen[id]; ok {
		diags.AddAttributeError(
			attribute,
			"Duplicate "+list+" entry",
			fmt.Sprintf("%s[%d] and %s[%d] reference the same ID %s.", list, first, list, index, id),
		)
		return
	}
	seen[id] = index
}

func validateServerUUID(value types.String, attribute path.Path, name string, diags *diag.Diagnostics) {
	if value.IsNull() || value.IsUnknown() {
		return
	}
	if _, idDiags := parse.UUIDString(value, name); idDiags.HasError() {
		diags.AddAttributeError(
			attribute,
			"Invalid "+name,
			fmt.Sprintf("%s must be a valid UUID.", name),
		)
	}
}
