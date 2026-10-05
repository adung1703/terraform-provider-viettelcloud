package blockstorage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/parse"
	"github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/wait"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	blockstoragesdk "github.com/viettelcloud-oss/sdks/go/blockstorage"
	"github.com/viettelcloud-oss/sdks/go/core"
	projectsdk "github.com/viettelcloud-oss/sdks/go/project"
	serversdk "github.com/viettelcloud-oss/sdks/go/server"

	blockstoragelookup "github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/provider/blockstorage/lookup"
	projectlookup "github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/provider/project/lookup"
	"github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/provider/providerdata"
	serverlookup "github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/provider/server/lookup"
)

var (
	_ resource.Resource                   = &VolumeResource{}
	_ resource.ResourceWithImportState    = &VolumeResource{}
	_ resource.ResourceWithValidateConfig = &VolumeResource{}
	_ resource.ResourceWithModifyPlan     = &VolumeResource{}
)

const (
	defaultVolumePollInterval = 10 * time.Second
	defaultVolumeTimeout      = 30 * time.Minute
)

type VolumeResource struct {
	client        *blockstoragesdk.Client
	server        *serversdk.Client
	projectClient *projectsdk.Client
	projectID     core.UUID
	pollInterval  time.Duration
	timeout       time.Duration
}

// volumeUpdatePlan splits an update into the partial-update body and the
// actions that have to travel separately. A resize is kept out of the body on
// purpose: the backend grows a volume through its own asynchronous extend
// action. A size sent in the patch body is accepted without ever taking
// effect, which surfaces as an inconsistent-result error once the volume is
// read back at its old size.
type volumeUpdatePlan struct {
	body       blockstoragesdk.VolumePartialUpdateSchema
	patch      bool
	extendSize *int
	// retypeName is the volume type the volume must end up on, empty when it is
	// unchanged. It travels through the retype action for the same reason a
	// resize travels through extend.
	retypeName string
}

func NewVolumeResource() resource.Resource {
	return &VolumeResource{}
}

func (r *VolumeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_volume"
}

func (r *VolumeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	requiresReplace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	useStateForUnknown := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		Description: "Manage a Viettel Cloud Block Storage volume.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Volume ID (UUID).",
				PlanModifiers: useStateForUnknown,
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the volume.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Optional volume description. Omitting it preserves the backend value; set it to an empty string to clear it.",
			},
			"size": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "Volume size in GiB. Required except for snapshot sources; it can only be increased after creation.",
			},
			"iops": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "Provisioned IOPS. Omitting it preserves the backend value.",
			},
			"status": schema.StringAttribute{Computed: true, Description: "Current volume status."},
			// bootable, encrypted, zone, zone_id, and project_id are fixed when
			// the volume is created, so keeping the prior value out of the plan
			// only adds "(known after apply)" noise. create_from.volume_type is
			// the one attribute that changes in place, and a retype could move
			// zone or encryption with it. ModifyPlan therefore returns those
			// two to unknown whenever the plan contains a retype.
			"bootable": schema.BoolAttribute{
				Computed:      true,
				Description:   "Whether the volume is bootable.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"encrypted": schema.BoolAttribute{
				Computed:      true,
				Description:   "Whether the volume is encrypted.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"zone": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Zone where the volume is created. Changing it replaces the volume. Removing it keeps the current zone. Do not set it for snapshot sources.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"zone_id": schema.StringAttribute{
				Computed:      true,
				Description:   "Zone ID (UUID) where the volume resides.",
				PlanModifiers: useStateForUnknown,
			},
			"volume_type": schema.StringAttribute{Computed: true, Description: "Effective volume type name."},
			"project_id": schema.StringAttribute{
				Computed:      true,
				Description:   "Project ID (UUID) owning the volume.",
				PlanModifiers: useStateForUnknown,
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				Description:   "Timestamp when the volume was created (RFC3339).",
				PlanModifiers: useStateForUnknown,
			},
			"updated_at": schema.StringAttribute{Computed: true, Description: "Timestamp when the volume was last updated (RFC3339)."},
			"create_from": schema.SingleNestedAttribute{
				Required:    true,
				Description: "Immutable volume source. Configure only fields used by source_type.",
				Attributes: map[string]schema.Attribute{
					"source_type": schema.StringAttribute{
						Required:      true,
						Description:   "Source type: empty, image, custom_image, snapshot, or backup.",
						PlanModifiers: requiresReplace,
					},
					"image": schema.StringAttribute{
						Optional:      true,
						Description:   "Unique exact image name; valid only for an image source.",
						PlanModifiers: requiresReplace,
					},
					"custom_image_id": schema.StringAttribute{
						Optional:      true,
						Description:   "Custom image ID (UUID); valid only for a custom_image source.",
						PlanModifiers: requiresReplace,
					},
					"snapshot_id": schema.StringAttribute{
						Optional:      true,
						Description:   "Snapshot ID (UUID); valid only for a snapshot source.",
						PlanModifiers: requiresReplace,
					},
					"backup_id": schema.StringAttribute{
						Optional:      true,
						Description:   "Backup ID (UUID); valid only for a backup source.",
						PlanModifiers: requiresReplace,
					},
					// The backend retypes a volume in place, so changing this
					// name updates the volume instead of replacing it.
					"volume_type": schema.StringAttribute{
						Optional:    true,
						Description: "Unique exact ready volume type name. Required except for a snapshot source, which takes its type from the snapshot. Changing it retypes the volume in place.",
					},
				},
			},
		},
	}
}

func (r *VolumeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(*providerdata.Configured)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("Expected *providerdata.Configured, got %T", req.ProviderData))
		return
	}
	r.client = data.BlockStorage
	r.server = data.Server
	r.projectClient = data.Project
	r.projectID = data.ProjectID
}

func (r *VolumeResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config VolumeResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !config.Name.IsNull() && !config.Name.IsUnknown() && strings.TrimSpace(config.Name.ValueString()) == "" {
		resp.Diagnostics.AddError("Missing volume name", "name must be configured, known, and non-empty.")
	}
	// Rejecting surrounding whitespace keeps zone comparable as a plain string,
	// so RequiresReplaceIfConfigured cannot replace a volume over padding.
	if !config.Zone.IsNull() && !config.Zone.IsUnknown() {
		if zone := config.Zone.ValueString(); strings.TrimSpace(zone) == "" || strings.TrimSpace(zone) != zone {
			resp.Diagnostics.AddError("Invalid volume zone", "zone must be non-empty and have no leading or trailing whitespace when configured.")
		}
	}
	resp.Diagnostics.Append(validateVolumeCreateFromConfig(ctx, config.CreateFrom, config.Size)...)
	resp.Diagnostics.Append(validateVolumeSnapshotZone(ctx, config.Zone, config.CreateFrom)...)
}

// ModifyPlan checks create-time constraints before a replacement can destroy
// the prior volume. It also withdraws computed values that can change.
//
// resp.RequiresReplace cannot be read to recognize a replacement. The framework
// hands this method a response whose path list starts empty and appends the
// attribute plan modifiers' paths only after it returns, so the list is always
// empty here. The immutable source fields are compared directly instead.
func (r *VolumeResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var plan VolumeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var config VolumeResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateCreate := func() {
		// size is Optional+Computed, so the proposed plan can carry the prior
		// state's value even when the practitioner omitted it. The backend's
		// snapshot create rule applies to configuration, not that carried value.
		resp.Diagnostics.Append(validateVolumeCreateSize(ctx, plan.CreateFrom, config.Size)...)
		resp.Diagnostics.Append(validateVolumeSnapshotZone(ctx, config.Zone, plan.CreateFrom)...)
		if resp.Diagnostics.HasError() {
			return
		}
		resp.Diagnostics.Append(r.validateVolumeCreatePlacement(ctx, plan, config.Zone)...)
	}

	if req.State.Raw.IsNull() {
		validateCreate()
		return
	}

	var state VolumeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	planned, plannedDiags := volumeCreateFromModel(ctx, plan.CreateFrom)
	resp.Diagnostics.Append(plannedDiags...)
	current, currentDiags := volumeCreateFromModel(ctx, state.CreateFrom)
	resp.Diagnostics.Append(currentDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A replacement destroys this volume and creates another one, so the in-place
	// size rules do not apply to it: the new volume is allowed to be smaller.
	// Reporting the create rules here as well fails the plan one pass before
	// Terraform plans the create half against a null prior state.
	if volumeSourceReplaced(planned, current) {
		validateCreate()
		if resp.Diagnostics.HasError() {
			return
		}
		if config.Zone.IsNull() {
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("zone"), types.StringUnknown())...)
		}
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("zone_id"), types.StringUnknown())...)
		return
	}
	// This mirrors RequiresReplaceIfConfigured on zone, because ModifyPlan cannot
	// read the replacement paths that the attribute modifiers report.
	if !config.Zone.IsNull() && !plan.Zone.Equal(state.Zone) {
		validateCreate()
		if resp.Diagnostics.HasError() {
			return
		}
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("zone_id"), types.StringUnknown())...)
		return
	}

	// A retype is the one in-place path that can move a value the schema
	// otherwise freezes with UseStateForUnknown. Leaving the prior zone or
	// encryption flag in the plan would fail the apply with an inconsistent
	// result after the volume has already been retyped. The plan therefore
	// stops claiming to know them.
	if volumeRetypePossible(planned, current) {
		// A configured zone stays in the plan because Terraform cannot turn a
		// configured value into an unknown one. Retype lookup uses its zone ID.
		if config.Zone.IsNull() {
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("zone"), types.StringUnknown())...)
		}
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("zone_id"), types.StringUnknown())...)
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("encrypted"), types.BoolUnknown())...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	if plan.Size.IsNull() || plan.Size.IsUnknown() || state.Size.IsNull() || state.Size.IsUnknown() {
		return
	}
	if plan.Size.ValueInt64() < state.Size.ValueInt64() {
		resp.Diagnostics.AddAttributeError(
			path.Root("size"),
			"Invalid volume size update",
			fmt.Sprintf("size can only increase from %d GiB; planned value is %d GiB. Replace the volume to make it smaller.", state.Size.ValueInt64(), plan.Size.ValueInt64()),
		)
	}
}

func (r *VolumeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan VolumeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var configuredZone types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("zone"), &configuredZone)...)
	if configuredZone.IsUnknown() {
		configuredZone = plan.Zone
	}
	resp.Diagnostics.Append(validateVolumeSnapshotZone(ctx, configuredZone, plan.CreateFrom)...)
	if resp.Diagnostics.HasError() {
		return
	}

	zoneID, zoneDiags := r.resolveZoneID(ctx, configuredZone)
	resp.Diagnostics.Append(zoneDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	imageFinder := serverlookup.NewImageFinder(r.server, r.projectID)
	volumeTypeFinder := blockstoragelookup.NewVolumeTypeFinder(r.client)
	body, diags := buildVolumeCreateBody(ctx, plan, zoneID, imageFinder.Resolve, volumeTypeFinder.ResolveForCreate)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	volume, err := r.client.CreateVolume(ctx, blockstoragesdk.CreateVolumeParams{ProjectID: r.projectID}, body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating volume", err.Error())
		return
	}

	populateVolumePendingState(&plan, volume.Id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.waitUntilReady(ctx, volume.Id); err != nil {
		resp.Diagnostics.AddError(
			"Error waiting for volume creation",
			fmt.Sprintf("%s\n\nVolume ID %s was preserved in state to avoid creating an untracked duplicate.", err, volume.Id),
		)
		return
	}

	volume, err = r.get(ctx, volume.Id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading volume after creation", err.Error())
		return
	}
	resp.Diagnostics.Append(populateVolumeResourceState(ctx, volume, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *VolumeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state VolumeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	volumeID, diags := parse.UUIDString(state.ID, "id")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	volume, err := r.get(ctx, volumeID)
	if err != nil {
		if errors.Is(err, blockstoragesdk.ErrNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading volume", err.Error())
		return
	}

	resp.Diagnostics.Append(populateVolumeResourceState(ctx, volume, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *VolumeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan VolumeResourceModel
	var state VolumeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	volumeID, diags := parse.UUIDString(state.ID, "id")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	update, diags := buildVolumeUpdatePlan(ctx, plan, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if update.patch {
		if _, err := r.client.UpdateVolume(ctx, volumeID, blockstoragesdk.UpdateVolumeParams{ProjectID: r.projectID}, update.body); err != nil {
			resp.Diagnostics.AddError("Error updating volume", err.Error())
			return
		}
		if err := r.waitUntilPatched(ctx, volumeID, update.body); err != nil {
			resp.Diagnostics.AddError("Error waiting for volume update", err.Error())
			return
		}
	}
	// The retype is resolved against the size the volume ends up with, so a type
	// too small for the planned growth is reported before anything is changed.
	if update.retypeName != "" {
		var configuredZone types.String
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("zone"), &configuredZone)...)
		if resp.Diagnostics.HasError() {
			return
		}
		var zoneID *core.UUID
		if !configuredZone.IsNull() {
			id, idDiags := parse.UUIDString(state.ZoneID, "zone_id")
			resp.Diagnostics.Append(idDiags...)
			if resp.Diagnostics.HasError() {
				return
			}
			zoneID = &id
		}
		resp.Diagnostics.Append(r.retype(ctx, volumeID, update.retypeName, volumeTargetSize(plan, state), zoneID)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	if update.extendSize != nil {
		body := blockstoragesdk.VolumeExtendSchema{Size: *update.extendSize}
		if err := r.client.ExtendVolume(ctx, volumeID, blockstoragesdk.ExtendVolumeParams{ProjectID: r.projectID}, body); err != nil {
			resp.Diagnostics.AddError("Error resizing volume", err.Error())
			return
		}
		if err := r.waitUntilResized(ctx, volumeID, *update.extendSize); err != nil {
			resp.Diagnostics.AddError("Error waiting for volume resize", err.Error())
			return
		}
	}

	volume, err := r.get(ctx, volumeID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading volume after update", err.Error())
		return
	}
	newState := plan
	resp.Diagnostics.Append(populateVolumeResourceState(ctx, volume, &newState)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *VolumeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state VolumeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	volumeID, diags := parse.UUIDString(state.ID, "id")
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteVolume(ctx, volumeID, blockstoragesdk.DeleteVolumeParams{ProjectID: r.projectID})
	if errors.Is(err, blockstoragesdk.ErrNotFound) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error deleting volume", err.Error())
		return
	}
	if err := r.waitUntilDeleted(ctx, volumeID); err != nil {
		resp.Diagnostics.AddError("Error waiting for volume deletion", err.Error())
	}
}

// ImportState accepts the volume ID on its own, or the composite
// "<volume_id>,<source_type>" form. The second form exists because the backend
// can report several origins for one volume, and Read only ever sees prior
// state. Without a source type carried in through the import ID, a
// practitioner has no way to say which origin the volume was created from.
func (r *VolumeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	rawID, rawSourceType, composite := strings.Cut(req.ID, ",")
	volumeID := strings.TrimSpace(rawID)
	if volumeID == "" {
		resp.Diagnostics.AddError(
			"Invalid volume import ID",
			fmt.Sprintf("Expected \"<volume_id>\" or \"<volume_id>,<source_type>\", got %q.", req.ID),
		)
		return
	}
	if _, diags := parse.UUIDString(types.StringValue(volumeID), "id"); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), volumeID)...)
	if !composite || resp.Diagnostics.HasError() {
		return
	}

	sourceType := strings.TrimSpace(rawSourceType)
	if !validVolumeSourceType(sourceType) {
		resp.Diagnostics.AddError(
			"Invalid volume import source type",
			fmt.Sprintf("%q is not a supported create_from.source_type. Supported values are: empty, image, custom_image, snapshot, backup.", sourceType),
		)
		return
	}
	attributeTypes := volumeCreateFromAttributeTypes()
	createFrom, diags := types.ObjectValue(attributeTypes, map[string]attr.Value{
		"source_type":     types.StringValue(sourceType),
		"image":           types.StringNull(),
		"custom_image_id": types.StringNull(),
		"snapshot_id":     types.StringNull(),
		"backup_id":       types.StringNull(),
		"volume_type":     types.StringNull(),
	})
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Read fills in the rest of create_from from the response; source_type only
	// has to be present so it can pick the matching origin.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("create_from"), createFrom)...)
}

func (r *VolumeResource) get(ctx context.Context, id core.UUID) (*blockstoragesdk.VolumeDetailSchema, error) {
	return r.client.GetVolume(ctx, id, blockstoragesdk.GetVolumeParams{ProjectID: r.projectID})
}

// resolveZoneID resolves an optional configured zone name to its ID, so a
// create_from.volume_type name that exists in more than one zone can be
// narrowed to the zone the volume is meant to land in. A volume with no
// configured zone resolves its type across every zone, as before.
func (r *VolumeResource) resolveZoneID(ctx context.Context, zone types.String) (*core.UUID, diag.Diagnostics) {
	var diags diag.Diagnostics
	if zone.IsNull() || zone.IsUnknown() {
		return nil, diags
	}
	name := strings.TrimSpace(zone.ValueString())
	if name == "" {
		return nil, diags
	}
	resolved, err := projectlookup.NewZoneFinder(r.projectClient, r.projectID).Resolve(ctx, projectlookup.ZoneFilter{Name: &name})
	if err != nil {
		diags.AddError("Unable to resolve zone", fmt.Sprintf("zone: %s", err))
		return nil, diags
	}
	return &resolved.Id, diags
}

// validateVolumeCreatePlacement checks a configured zone and its volume type
// before Terraform destroys a volume for replacement. Create resolves them
// again because the catalogue can change between plan and apply.
func (r *VolumeResource) validateVolumeCreatePlacement(ctx context.Context, plan VolumeResourceModel, zone types.String) diag.Diagnostics {
	var diags diag.Diagnostics
	if zone.IsNull() || zone.IsUnknown() || strings.TrimSpace(zone.ValueString()) == "" {
		return diags
	}
	source, sourceDiags := volumeCreateFromModel(ctx, plan.CreateFrom)
	diags.Append(sourceDiags...)
	if diags.HasError() || source.SourceType.IsNull() || source.SourceType.IsUnknown() ||
		strings.TrimSpace(source.SourceType.ValueString()) == volumeSourceTypeSnapshot {
		return diags
	}
	zoneID, zoneDiags := r.resolveZoneID(ctx, zone)
	diags.Append(zoneDiags...)
	if diags.HasError() {
		return diags
	}
	if source.VolumeType.IsNull() || source.VolumeType.IsUnknown() || plan.Size.IsNull() || plan.Size.IsUnknown() {
		return diags
	}
	_, err := blockstoragelookup.NewVolumeTypeFinder(r.client).ResolveForCreate(ctx, blockstoragelookup.VolumeTypeResolveRequest{
		Name: source.VolumeType.ValueString(), ZoneID: zoneID, RequestedSize: int(plan.Size.ValueInt64()),
	})
	if err != nil {
		diags.AddError("Unable to resolve volume type", fmt.Sprintf("create_from.volume_type: %s", err))
	}
	return diags
}

// retype moves the volume onto the volume type named by name. The backend
// exposes retype as its own action, so a changed create_from.volume_type does
// not have to destroy a volume that holds data. A configured zone narrows an
// ambiguous type name; an omitted zone keeps the prior lookup behavior.
func (r *VolumeResource) retype(ctx context.Context, volumeID core.UUID, name string, size int, zoneID *core.UUID) diag.Diagnostics {
	var diags diag.Diagnostics
	volumeType, err := blockstoragelookup.NewVolumeTypeFinder(r.client).ResolveForCreate(ctx, blockstoragelookup.VolumeTypeResolveRequest{
		Name:          name,
		ZoneID:        zoneID,
		RequestedSize: size,
	})
	if err != nil {
		diags.AddError("Unable to resolve volume type", fmt.Sprintf("create_from.volume_type: %s", err))
		return diags
	}
	body := blockstoragesdk.VolumeRetypeSchema{VolumeTypeId: volumeType.Id}
	if err := r.client.RetypeVolume(ctx, volumeID, blockstoragesdk.RetypeVolumeParams{ProjectID: r.projectID}, body); err != nil {
		diags.AddError("Error retyping volume", err.Error())
		return diags
	}
	if err := r.waitUntilRetyped(ctx, volumeID, volumeType.Name); err != nil {
		diags.AddError("Error waiting for volume retype", err.Error())
	}
	return diags
}

func (r *VolumeResource) waitUntilReady(ctx context.Context, id core.UUID) error {
	_, err := r.newWaiter().WaitFor(ctx, func(ctx context.Context) (*blockstoragesdk.VolumeDetailSchema, error) {
		volume, err := r.get(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("poll volume %s: %w", id, err)
		}
		if volumeStatusReady(volume.Status) {
			return volume, nil
		}
		if volumeStatusFailed(volume.Status) {
			return nil, fmt.Errorf("volume %s entered terminal status %q", id, volume.Status)
		}
		return nil, fmt.Errorf("status=%s: %w", volume.Status, wait.ErrNotReady)
	})
	return volumeWaitError(err, id, "become ready")
}

// waitUntilPatched waits until the volume reports the patched fields. A ready
// status proves nothing on its own: the volume keeps reporting the values it
// already had until the backend applies the change. A waiter that stops there
// hands the caller stale values, which Terraform rejects as an inconsistent
// result. The fields still outstanding are named in the not-ready reason,
// which the waiter carries into its timeout error, so a backend that never
// applies one is identifiable.
func (r *VolumeResource) waitUntilPatched(ctx context.Context, id core.UUID, body blockstoragesdk.VolumePartialUpdateSchema) error {
	_, err := r.newWaiter().WaitFor(ctx, func(ctx context.Context) (*blockstoragesdk.VolumeDetailSchema, error) {
		volume, err := r.get(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("poll volume %s: %w", id, err)
		}
		outstanding := unappliedVolumePatchFields(volume, body)
		if volumeStatusReady(volume.Status) && len(outstanding) == 0 {
			return volume, nil
		}
		if volumeStatusFailed(volume.Status) {
			return nil, fmt.Errorf("volume %s entered terminal status %q while updating", id, volume.Status)
		}
		if len(outstanding) == 0 {
			return nil, fmt.Errorf("status=%s: %w", volume.Status, wait.ErrNotReady)
		}
		return nil, fmt.Errorf(
			"status=%s, the volume still reports the previous %s: %w",
			volume.Status, strings.Join(outstanding, ", "), wait.ErrNotReady,
		)
	})
	return volumeWaitError(err, id, "apply the requested changes")
}

// waitUntilRetyped waits until the volume reports the new volume type. Like
// the extend action, retype only returns "accepted", so the effective volume
// type is the observable that settles rather than the status.
func (r *VolumeResource) waitUntilRetyped(ctx context.Context, id core.UUID, name string) error {
	_, err := r.newWaiter().WaitFor(ctx, func(ctx context.Context) (*blockstoragesdk.VolumeDetailSchema, error) {
		volume, err := r.get(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("poll volume %s: %w", id, err)
		}
		if volumeStatusReady(volume.Status) && volume.CreateFrom.VolumeType != nil && volume.CreateFrom.VolumeType.Name == name {
			return volume, nil
		}
		if volumeStatusFailed(volume.Status) {
			return nil, fmt.Errorf("volume %s entered terminal status %q while retyping to %q", id, volume.Status, name)
		}
		current := ""
		if volume.CreateFrom.VolumeType != nil {
			current = volume.CreateFrom.VolumeType.Name
		}
		return nil, fmt.Errorf("status=%s, volume_type=%q: %w", volume.Status, current, wait.ErrNotReady)
	})
	return volumeWaitError(err, id, fmt.Sprintf("become volume type %q", name))
}

// waitUntilResized waits until the volume reports the new size. A ready status
// is not proof on its own: the extend call only returns "accepted", and the
// volume keeps reporting the status it already had until the resize actually
// starts. A waiter that stops there returns before the new size exists, and
// the caller stores the old one. The reported size is the observable that
// settles. A backend that overshoots the requested size ends the wait too,
// rather than polling to the timeout; the honest value is then stored and
// Terraform reports the mismatch.
func (r *VolumeResource) waitUntilResized(ctx context.Context, id core.UUID, size int) error {
	_, err := r.newWaiter().WaitFor(ctx, func(ctx context.Context) (*blockstoragesdk.VolumeDetailSchema, error) {
		volume, err := r.get(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("poll volume %s: %w", id, err)
		}
		if volumeStatusReady(volume.Status) && volume.Size >= size {
			return volume, nil
		}
		if volumeStatusFailed(volume.Status) {
			return nil, fmt.Errorf("volume %s entered terminal status %q while resizing to %d GiB", id, volume.Status, size)
		}
		return nil, fmt.Errorf("status=%s, size=%d GiB: %w", volume.Status, volume.Size, wait.ErrNotReady)
	})
	return volumeWaitError(err, id, fmt.Sprintf("reach %d GiB", size))
}

func (r *VolumeResource) waitUntilDeleted(ctx context.Context, id core.UUID) error {
	// The volume may still report its previous status for a poll or two after
	// the delete call is accepted. A ready status therefore only means the
	// deletion failed once deleting has actually been observed.
	sawDeleting := false
	_, err := r.newWaiter().WaitFor(ctx, func(ctx context.Context) (*blockstoragesdk.VolumeDetailSchema, error) {
		volume, err := r.get(ctx, id)
		if errors.Is(err, blockstoragesdk.ErrNotFound) {
			// The volume is gone, which is the whole point of this wait; there
			// is no value left to hand back.
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("poll volume %s: %w", id, err)
		}
		if volume.Status == blockstoragesdk.VolumeStatusDeleting {
			sawDeleting = true
		}
		if volumeStatusFailed(volume.Status) || (sawDeleting && volumeStatusReady(volume.Status)) {
			return nil, fmt.Errorf("volume %s stopped deleting and settled in status %q", id, volume.Status)
		}
		return nil, fmt.Errorf("status=%s: %w", volume.Status, wait.ErrNotReady)
	})
	return volumeWaitError(err, id, "be deleted")
}

// newWaiter builds the waiter every volume poll runs on. Block storage actions
// run longer than the wait package assumes, so the volume defaults stand in for
// unset values rather than the package ones.
func (r *VolumeResource) newWaiter() *wait.Waiter[*blockstoragesdk.VolumeDetailSchema] {
	interval := r.pollInterval
	if interval <= 0 {
		interval = defaultVolumePollInterval
	}
	timeout := r.timeout
	if timeout <= 0 {
		timeout = defaultVolumeTimeout
	}
	return wait.New[*blockstoragesdk.VolumeDetailSchema](interval, timeout)
}

func volumeWaitError(err error, id core.UUID, action string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("waiting for volume %s to %s: %w", id, action, err)
}

func buildVolumeCreateBody(
	ctx context.Context,
	plan VolumeResourceModel,
	zoneID *core.UUID,
	resolveImage serverlookup.ImageResolveFunc,
	resolveVolumeType blockstoragelookup.VolumeTypeResolveFunc,
) (blockstoragesdk.VolumeCreateSchema, diag.Diagnostics) {
	var body blockstoragesdk.VolumeCreateSchema
	var diags diag.Diagnostics
	if plan.Name.IsNull() || plan.Name.IsUnknown() || strings.TrimSpace(plan.Name.ValueString()) == "" {
		diags.AddError("Missing volume name", "name must be configured, known, and non-empty.")
		return body, diags
	}

	createFrom, sourceDiags := buildVolumeCreateFrom(ctx, plan.CreateFrom, plan.Size, zoneID, resolveImage, resolveVolumeType)
	diags.Append(sourceDiags...)
	if diags.HasError() {
		return body, diags
	}

	body.Name = strings.TrimSpace(plan.Name.ValueString())
	body.CreateFrom = createFrom
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		value := plan.Description.ValueString()
		body.Description = &value
	}
	if !plan.IOPS.IsNull() && !plan.IOPS.IsUnknown() {
		value := int(plan.IOPS.ValueInt64())
		body.Iops = &value
	}
	return body, diags
}

func buildVolumeUpdatePlan(ctx context.Context, plan, state VolumeResourceModel) (volumeUpdatePlan, diag.Diagnostics) {
	var update volumeUpdatePlan
	var diags diag.Diagnostics

	if plan.Name.IsNull() || plan.Name.IsUnknown() || strings.TrimSpace(plan.Name.ValueString()) == "" {
		diags.AddError("Missing volume name", "name must be configured, known, and non-empty.")
		return volumeUpdatePlan{}, diags
	}
	planned, plannedDiags := volumeCreateFromModel(ctx, plan.CreateFrom)
	diags.Append(plannedDiags...)
	current, currentDiags := volumeCreateFromModel(ctx, state.CreateFrom)
	diags.Append(currentDiags...)
	if diags.HasError() {
		return volumeUpdatePlan{}, diags
	}
	update.retypeName = volumeRetypeName(planned, current)
	if !plan.Name.Equal(state.Name) {
		value := strings.TrimSpace(plan.Name.ValueString())
		update.body.Name = &value
		update.patch = true
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() && !plan.Description.Equal(state.Description) {
		value := plan.Description.ValueString()
		update.body.Description = &value
		update.patch = true
	}
	if !plan.IOPS.IsNull() && !plan.IOPS.IsUnknown() && !plan.IOPS.Equal(state.IOPS) {
		value := int(plan.IOPS.ValueInt64())
		update.body.Iops = &value
		update.patch = true
	}
	if !plan.Size.IsNull() && !plan.Size.IsUnknown() && !plan.Size.Equal(state.Size) {
		if plan.Size.ValueInt64() <= state.Size.ValueInt64() {
			diags.AddError("Invalid volume size update", fmt.Sprintf("size can only increase from %d GiB; planned value is %d GiB.", state.Size.ValueInt64(), plan.Size.ValueInt64()))
		} else {
			value := int(plan.Size.ValueInt64())
			update.extendSize = &value
		}
	}
	return update, diags
}

func unappliedVolumePatchFields(volume *blockstoragesdk.VolumeDetailSchema, body blockstoragesdk.VolumePartialUpdateSchema) []string {
	var fields []string
	if body.Name != nil && volume.Name != *body.Name {
		fields = append(fields, "name")
	}
	// Clearing a description is configured as an empty string, and the backend
	// may echo that back as an omitted field; both mean cleared.
	if body.Description != nil {
		current := ""
		if volume.Description != nil {
			current = *volume.Description
		}
		if current != *body.Description {
			fields = append(fields, "description")
		}
	}
	if body.Iops != nil && (volume.Iops == nil || *volume.Iops != *body.Iops) {
		fields = append(fields, "iops")
	}
	return fields
}

// volumeTargetSize is the size the volume ends the apply on. That is the
// planned size when the configuration owns it, and the current size when it
// does not, as with a snapshot source that never configured a size.
func volumeTargetSize(plan, state VolumeResourceModel) int {
	if !plan.Size.IsNull() && !plan.Size.IsUnknown() {
		return int(plan.Size.ValueInt64())
	}
	return int(state.Size.ValueInt64())
}

func populateVolumePendingState(state *VolumeResourceModel, id core.UUID) {
	state.ID = types.StringValue(id.String())
	if state.Description.IsUnknown() {
		state.Description = types.StringNull()
	}
	if state.Size.IsUnknown() {
		state.Size = types.Int64Null()
	}
	if state.IOPS.IsUnknown() {
		state.IOPS = types.Int64Null()
	}
	state.Status = types.StringNull()
	state.Bootable = types.BoolNull()
	state.Encrypted = types.BoolNull()
	if state.Zone.IsUnknown() {
		state.Zone = types.StringNull()
	}
	state.ZoneID = types.StringNull()
	state.VolumeType = types.StringNull()
	state.ProjectID = types.StringNull()
	state.CreatedAt = types.StringNull()
	state.UpdatedAt = types.StringNull()
}

func volumeStatusReady(status blockstoragesdk.VolumeStatus) bool {
	return status == blockstoragesdk.VolumeStatusAvailable || status == blockstoragesdk.VolumeStatusInUse
}

func volumeStatusFailed(status blockstoragesdk.VolumeStatus) bool {
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

// volumeCreateFromModel decodes the create_from object into its model. A null
// or unknown create_from yields the zero model, whose fields are all null, so
// callers can read it without repeating the presence check.
func volumeCreateFromModel(ctx context.Context, createFrom types.Object) (VolumeCreateFromModel, diag.Diagnostics) {
	var source VolumeCreateFromModel
	var diags diag.Diagnostics
	if createFrom.IsNull() || createFrom.IsUnknown() {
		return source, diags
	}
	diags.Append(createFrom.As(ctx, &source, basetypes.ObjectAsOptions{})...)
	return source, diags
}

// validateVolumeSnapshotZone rejects a zone that the snapshot creation request
// cannot honor. The SDK accepts only a snapshot ID for that source.
func validateVolumeSnapshotZone(ctx context.Context, zone types.String, createFrom types.Object) diag.Diagnostics {
	var diags diag.Diagnostics
	if zone.IsNull() || zone.IsUnknown() {
		return diags
	}
	source, sourceDiags := volumeCreateFromModel(ctx, createFrom)
	diags.Append(sourceDiags...)
	if diags.HasError() || source.SourceType.IsNull() || source.SourceType.IsUnknown() {
		return diags
	}
	if strings.TrimSpace(source.SourceType.ValueString()) == volumeSourceTypeSnapshot {
		diags.AddAttributeError(path.Root("zone"), "Invalid volume zone", "zone cannot be set when create_from.source_type is \"snapshot\"; the snapshot determines the volume's zone.")
	}
	return diags
}

func validVolumeSourceType(sourceType string) bool {
	switch sourceType {
	case volumeSourceTypeEmpty, volumeSourceTypeImage, volumeSourceTypeCustomImage, volumeSourceTypeSnapshot, volumeSourceTypeBackup:
		return true
	default:
		return false
	}
}

// volumeSourceReplaced reports whether the plan changes a create_from field
// that requires replacement. The comparison mirrors what those attribute plan
// modifiers do — a plain inequality between plan and state. The resource and
// the modifiers therefore cannot disagree about what counts as a replacement.
// create_from.volume_type is excluded on purpose: it retypes in place.
func volumeSourceReplaced(planned, current VolumeCreateFromModel) bool {
	return !planned.SourceType.Equal(current.SourceType) ||
		!planned.Image.Equal(current.Image) ||
		!planned.CustomImageID.Equal(current.CustomImageID) ||
		!planned.SnapshotID.Equal(current.SnapshotID) ||
		!planned.BackupID.Equal(current.BackupID)
}

// volumeRetypeName is the volume type the plan asks the volume to end up on, or
// "" when it is unchanged. An omitted volume type is not a request to change it,
// so only a configured name that differs from the effective one retypes the
// volume. ModifyPlan and Update share this so the plan's view of a retype cannot
// drift from the work the update performs.
func volumeRetypeName(planned, current VolumeCreateFromModel) string {
	if planned.VolumeType.IsUnknown() {
		return ""
	}
	name := strings.TrimSpace(planned.VolumeType.ValueString())
	if name == "" || name == strings.TrimSpace(current.VolumeType.ValueString()) {
		return ""
	}
	return name
}

// volumeRetypePossible reports whether the plan may retype the volume. An
// unknown volume type counts, because the plan cannot rule a retype out yet.
// The values a retype moves have to be withdrawn while the plan is still being
// shaped, not after the apply has already retyped the volume.
func volumeRetypePossible(planned, current VolumeCreateFromModel) bool {
	return planned.VolumeType.IsUnknown() || volumeRetypeName(planned, current) != ""
}

// validateVolumeCreateSize enforces the size rules that only apply when the
// volume is about to be created, including as the replacement half of a
// destroy-then-create. A snapshot decides the size of the volume it produces,
// so a configured size is refused. Sending one fails the create, and an apply
// that has already destroyed the previous volume then leaves the practitioner
// with neither the volume nor a configuration that can rebuild it.
func validateVolumeCreateSize(ctx context.Context, createFrom types.Object, size types.Int64) diag.Diagnostics {
	source, diags := volumeCreateFromModel(ctx, createFrom)
	if diags.HasError() {
		return diags
	}
	if source.SourceType.IsNull() || source.SourceType.IsUnknown() || size.IsNull() || size.IsUnknown() {
		return diags
	}
	if strings.TrimSpace(source.SourceType.ValueString()) != volumeSourceTypeSnapshot {
		return diags
	}
	diags.AddError(
		"Invalid size for snapshot source",
		"size must be omitted because the snapshot determines it. A volume created from a snapshot can be grown afterwards, "+
			"but the grown size cannot be carried into a new volume: remove size before changing create_from.snapshot_id.",
	)
	return diags
}

func validateVolumeCreateFromConfig(ctx context.Context, createFrom types.Object, size types.Int64) diag.Diagnostics {
	var diags diag.Diagnostics
	if createFrom.IsNull() {
		diags.AddError("Missing create_from", "create_from must be configured.")
		return diags
	}
	if createFrom.IsUnknown() {
		return diags
	}

	var source VolumeCreateFromModel
	diags.Append(createFrom.As(ctx, &source, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	if source.SourceType.IsNull() {
		diags.AddError("Missing create_from.source_type", "create_from.source_type must be configured.")
		return diags
	}
	if source.SourceType.IsUnknown() {
		return diags
	}

	sourceType := strings.TrimSpace(source.SourceType.ValueString())
	if !validVolumeSourceType(sourceType) {
		diags.AddError("Invalid create_from.source_type", "Supported values are: empty, image, custom_image, snapshot, backup.")
		return diags
	}

	if invalid := conflictingVolumeSourceFields(source, sourceType, false); len(invalid) > 0 {
		diags.AddError(
			"Invalid create_from fields",
			fmt.Sprintf("create_from.source_type %q cannot be used with: %s.", sourceType, strings.Join(invalid, ", ")),
		)
	}

	validateRequiredString := func(value types.String, field, summary string) {
		if value.IsUnknown() {
			return
		}
		if value.IsNull() || strings.TrimSpace(value.ValueString()) == "" {
			diags.AddError(summary, fmt.Sprintf("%s must be configured and non-empty for source_type %q.", field, sourceType))
		}
	}
	validateUUID := func(value types.String, field string) {
		if value.IsUnknown() {
			return
		}
		_, idDiags := parse.UUIDString(value, field)
		diags.Append(idDiags...)
	}

	switch sourceType {
	case volumeSourceTypeImage:
		validateRequiredString(source.Image, "create_from.image", "Missing image")
	case volumeSourceTypeCustomImage:
		validateUUID(source.CustomImageID, "create_from.custom_image_id")
	case volumeSourceTypeSnapshot:
		validateUUID(source.SnapshotID, "create_from.snapshot_id")
	case volumeSourceTypeBackup:
		validateUUID(source.BackupID, "create_from.backup_id")
	}

	// A snapshot determines the initial size, but the backend still accepts a
	// later resize. ValidateConfig cannot tell create from update, so the
	// create-only "size must be omitted" rule lives in buildVolumeCreateFrom.
	if sourceType == volumeSourceTypeSnapshot {
		return diags
	}

	validateRequiredString(source.VolumeType, "create_from.volume_type", "Missing volume type")
	if size.IsNull() {
		diags.AddError("Missing volume size", fmt.Sprintf("size is required for create_from.source_type %q.", sourceType))
	} else if !size.IsUnknown() && size.ValueInt64() < 1 {
		diags.AddError("Invalid volume size", "size must be greater than zero.")
	}
	return diags
}

func buildVolumeCreateFrom(
	ctx context.Context,
	createFrom types.Object,
	size types.Int64,
	zoneID *core.UUID,
	resolveImage serverlookup.ImageResolveFunc,
	resolveVolumeType blockstoragelookup.VolumeTypeResolveFunc,
) (blockstoragesdk.VolumeCreateSourceOptions, diag.Diagnostics) {
	var result blockstoragesdk.VolumeCreateSourceOptions
	var diags diag.Diagnostics
	if createFrom.IsNull() || createFrom.IsUnknown() {
		diags.AddError("Missing create_from", "create_from must be configured and known.")
		return result, diags
	}

	var source VolumeCreateFromModel
	diags.Append(createFrom.As(ctx, &source, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return result, diags
	}
	if source.SourceType.IsNull() || source.SourceType.IsUnknown() {
		diags.AddError("Missing create_from.source_type", "create_from.source_type must be configured and known.")
		return result, diags
	}

	sourceType := strings.TrimSpace(source.SourceType.ValueString())
	if invalid := conflictingVolumeSourceFields(source, sourceType, true); len(invalid) > 0 {
		diags.AddError(
			"Invalid create_from fields",
			fmt.Sprintf("create_from.source_type %q cannot be used with: %s.", sourceType, strings.Join(invalid, ", ")),
		)
		return result, diags
	}

	requireSize := func() (int, bool) {
		if size.IsNull() || size.IsUnknown() {
			diags.AddError("Missing volume size", fmt.Sprintf("size is required for create_from.source_type %q.", sourceType))
			return 0, false
		}
		if size.ValueInt64() < 1 {
			diags.AddError("Invalid volume size", "size must be greater than zero.")
			return 0, false
		}
		return int(size.ValueInt64()), true
	}
	resolveType := func(size int) (blockstoragesdk.VolumeTypeSchema, bool) {
		if source.VolumeType.IsNull() || source.VolumeType.IsUnknown() || strings.TrimSpace(source.VolumeType.ValueString()) == "" {
			diags.AddError("Missing volume type", fmt.Sprintf("create_from.volume_type is required for source_type %q.", sourceType))
			return blockstoragesdk.VolumeTypeSchema{}, false
		}
		volumeType, err := resolveVolumeType(ctx, blockstoragelookup.VolumeTypeResolveRequest{
			Name:          source.VolumeType.ValueString(),
			ZoneID:        zoneID,
			RequestedSize: size,
		})
		if err != nil {
			diags.AddError("Unable to resolve volume type", fmt.Sprintf("create_from.volume_type: %s", err))
			return blockstoragesdk.VolumeTypeSchema{}, false
		}
		return volumeType, true
	}

	switch sourceType {
	case volumeSourceTypeEmpty:
		size, ok := requireSize()
		if !ok {
			return result, diags
		}
		volumeType, ok := resolveType(size)
		if !ok {
			return result, diags
		}
		appendVolumeUnionError(&diags, result.FromVolumeCreateEmptySchema(blockstoragesdk.VolumeCreateEmptySchema{
			Size: size, VolumeTypeId: volumeType.Id,
		}))
	case volumeSourceTypeImage:
		size, ok := requireSize()
		if !ok {
			return result, diags
		}
		if source.Image.IsNull() || source.Image.IsUnknown() || strings.TrimSpace(source.Image.ValueString()) == "" {
			diags.AddError("Missing image", "create_from.image must be configured, known, and non-empty for an image source.")
			return result, diags
		}
		imageName := source.Image.ValueString()
		image, err := resolveImage(ctx, serverlookup.ImageFilter{Name: &imageName})
		if err != nil {
			diags.AddError("Unable to resolve image", fmt.Sprintf("create_from.image: %s", err))
			return result, diags
		}
		if image.MinVolumeSize != nil && size < *image.MinVolumeSize {
			diags.AddError("Invalid volume size", fmt.Sprintf("image %q requires at least %d GiB, requested %d GiB.", image.Name, *image.MinVolumeSize, size))
			return result, diags
		}
		volumeType, ok := resolveType(size)
		if !ok {
			return result, diags
		}
		appendVolumeUnionError(&diags, result.FromVolumeCreateFromImageSchema(blockstoragesdk.VolumeCreateFromImageSchema{
			ImageId: image.Id, Size: size, VolumeTypeId: volumeType.Id,
		}))
	case volumeSourceTypeCustomImage:
		size, ok := requireSize()
		if !ok {
			return result, diags
		}
		imageID, idDiags := parse.UUIDString(source.CustomImageID, "create_from.custom_image_id")
		diags.Append(idDiags...)
		volumeType, typeOK := resolveType(size)
		if diags.HasError() || !typeOK {
			return result, diags
		}
		appendVolumeUnionError(&diags, result.FromVolumeCreateFromCustomImageSchema(blockstoragesdk.VolumeCreateFromCustomImageSchema{
			CustomImageId: imageID, Size: size, VolumeTypeId: volumeType.Id,
		}))
	case volumeSourceTypeSnapshot:
		if !size.IsNull() && !size.IsUnknown() {
			diags.AddError("Invalid size for snapshot source", "size must be omitted because the snapshot determines it.")
			return result, diags
		}
		snapshotID, idDiags := parse.UUIDString(source.SnapshotID, "create_from.snapshot_id")
		diags.Append(idDiags...)
		if diags.HasError() {
			return result, diags
		}
		appendVolumeUnionError(&diags, result.FromVolumeCreateFromSnapshotSchema(blockstoragesdk.VolumeCreateFromSnapshotSchema{SnapshotId: snapshotID}))
	case volumeSourceTypeBackup:
		size, ok := requireSize()
		if !ok {
			return result, diags
		}
		backupID, idDiags := parse.UUIDString(source.BackupID, "create_from.backup_id")
		diags.Append(idDiags...)
		volumeType, typeOK := resolveType(size)
		if diags.HasError() || !typeOK {
			return result, diags
		}
		appendVolumeUnionError(&diags, result.FromVolumeCreateFromBackupSchema(blockstoragesdk.VolumeCreateFromBackupSchema{
			BackupId: backupID, Size: size, VolumeTypeId: volumeType.Id,
		}))
	default:
		diags.AddError("Invalid create_from.source_type", "Supported values are: empty, image, custom_image, snapshot, backup.")
	}
	return result, diags
}

// conflictingVolumeSourceFields lists the create_from fields that sourceType
// cannot use. includeUnknown selects the caller's stance on an unknown value:
// ValidateConfig defers it to apply, while the create path has a fully known
// plan and treats it as configured.
func conflictingVolumeSourceFields(source VolumeCreateFromModel, sourceType string, includeUnknown bool) []string {
	configured := func(value types.String) bool {
		return !value.IsNull() && (includeUnknown || !value.IsUnknown())
	}
	fields := []struct {
		name    string
		value   types.String
		forType string
	}{
		{name: "image", value: source.Image, forType: volumeSourceTypeImage},
		{name: "custom_image_id", value: source.CustomImageID, forType: volumeSourceTypeCustomImage},
		{name: "snapshot_id", value: source.SnapshotID, forType: volumeSourceTypeSnapshot},
		{name: "backup_id", value: source.BackupID, forType: volumeSourceTypeBackup},
	}
	var invalid []string
	for _, field := range fields {
		if configured(field.value) && field.forType != sourceType {
			invalid = append(invalid, "create_from."+field.name)
		}
	}
	if configured(source.VolumeType) && sourceType == volumeSourceTypeSnapshot {
		invalid = append(invalid, "create_from.volume_type")
	}
	return invalid
}

func appendVolumeUnionError(diags *diag.Diagnostics, err error) {
	if err != nil {
		diags.AddError("Invalid create_from value", fmt.Sprintf("Unable to encode create_from: %s", err))
	}
}
