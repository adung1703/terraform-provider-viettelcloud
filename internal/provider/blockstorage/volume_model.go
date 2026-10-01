package blockstorage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/compare"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	blockstoragesdk "github.com/viettelcloud-oss/sdks/go/blockstorage"
	"github.com/viettelcloud-oss/sdks/go/core"
)

type VolumeModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Size        types.Int64  `tfsdk:"size"`
	Status      types.String `tfsdk:"status"`
	Bootable    types.Bool   `tfsdk:"bootable"`
	Encrypted   types.Bool   `tfsdk:"encrypted"`
	Zone        types.String `tfsdk:"zone"`
	ZoneID      types.String `tfsdk:"zone_id"`
	VolumeType  types.String `tfsdk:"volume_type"`
	ProjectID   types.String `tfsdk:"project_id"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
	IOPS        types.Int64  `tfsdk:"iops"`
}

type VolumeResourceModel struct {
	VolumeModel
	CreateFrom types.Object `tfsdk:"create_from"`
}

type VolumeDataSourceModel struct {
	VolumeModel
}

type VolumeCreateFromModel struct {
	SourceType    types.String `tfsdk:"source_type"`
	Image         types.String `tfsdk:"image"`
	CustomImageID types.String `tfsdk:"custom_image_id"`
	SnapshotID    types.String `tfsdk:"snapshot_id"`
	BackupID      types.String `tfsdk:"backup_id"`
	VolumeType    types.String `tfsdk:"volume_type"`
}

const (
	volumeSourceTypeEmpty       = "empty"
	volumeSourceTypeImage       = "image"
	volumeSourceTypeCustomImage = "custom_image"
	volumeSourceTypeSnapshot    = "snapshot"
	volumeSourceTypeBackup      = "backup"
)

type volumeOrigin struct {
	sourceType string
	attribute  string
	value      types.String
}

func volumeCreateFromAttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"source_type":     types.StringType,
		"image":           types.StringType,
		"custom_image_id": types.StringType,
		"snapshot_id":     types.StringType,
		"backup_id":       types.StringType,
		"volume_type":     types.StringType,
	}
}

func populateVolumeModel(volume *blockstoragesdk.VolumeDetailSchema, state *VolumeModel) {
	if volume == nil {
		return
	}

	state.ID = types.StringValue(volume.Id.String())
	state.Name = types.StringValue(volume.Name)
	state.Size = types.Int64Value(int64(volume.Size))
	state.Status = types.StringValue(string(volume.Status))
	state.Bootable = types.BoolValue(volume.Bootable)
	state.Zone = types.StringValue(volume.Zone.Name)
	state.ZoneID = types.StringValue(volume.Zone.Id.String())
	state.ProjectID = types.StringValue(volume.ProjectId.String())
	state.CreatedAt = types.StringValue(volume.CreatedAt.Format(time.RFC3339))
	state.UpdatedAt = types.StringValue(volume.UpdatedAt.Format(time.RFC3339))

	state.Description = types.StringNull()
	if volume.Description != nil {
		state.Description = types.StringValue(*volume.Description)
	}
	state.Encrypted = types.BoolNull()
	if volume.Encrypted != nil {
		state.Encrypted = types.BoolValue(*volume.Encrypted)
	}
	state.IOPS = types.Int64Null()
	if volume.Iops != nil {
		state.IOPS = types.Int64Value(int64(*volume.Iops))
	}
	state.VolumeType = types.StringNull()
	if volume.CreateFrom.VolumeType != nil {
		state.VolumeType = types.StringValue(volume.CreateFrom.VolumeType.Name)
	}
}

// populateVolumeResourceState refreshes state from a volume response, nested
// create_from included. The top-level volume_type is read from that same
// object. Leaving create_from at its prior value would let an out-of-band
// retype produce a state whose two views of the volume type disagree while the
// plan stays clean.
func populateVolumeResourceState(ctx context.Context, volume *blockstoragesdk.VolumeDetailSchema, state *VolumeResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if volume == nil {
		return diags
	}

	configuredName := state.Name
	configuredSource := state.CreateFrom
	populateVolumeModel(volume, &state.VolumeModel)
	if !configuredName.IsNull() && !configuredName.IsUnknown() && strings.TrimSpace(configuredName.ValueString()) == volume.Name {
		state.Name = configuredName
	}
	createFrom, sourceDiags := volumeCreateFromState(ctx, volume, configuredSource)
	diags.Append(sourceDiags...)
	if diags.HasError() {
		return diags
	}
	state.CreateFrom = createFrom
	return diags
}

func volumeCreateFromState(ctx context.Context, volume *blockstoragesdk.VolumeDetailSchema, configured types.Object) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	attributeTypes := volumeCreateFromAttributeTypes()

	var prior VolumeCreateFromModel
	if !configured.IsNull() && !configured.IsUnknown() {
		diags.Append(configured.As(ctx, &prior, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return types.ObjectNull(attributeTypes), diags
		}
	}
	priorSourceType := strings.TrimSpace(prior.SourceType.ValueString())

	origins := volumeOrigins(volume)
	var selected *volumeOrigin
	// retainPrior records that the backend reported no origin at all while a
	// source is already known. "Not reported" is not evidence of "created
	// empty": a deleted snapshot or backup stops being reported while the
	// volume it produced lives on. Every create_from field that names a source
	// requires replacement, so recording "empty" would plan the destruction of
	// a healthy volume that no surviving source could rebuild.
	retainPrior := false
	switch {
	case len(origins) == 0:
		// Only a volume with nothing recorded at all, such as a first import,
		// is genuinely empty.
		retainPrior = priorSourceType != "" && priorSourceType != volumeSourceTypeEmpty
	case len(origins) == 1:
		selected = &origins[0]
	default:
		for i := range origins {
			if origins[i].sourceType == priorSourceType {
				selected = &origins[i]
			}
		}
		if selected == nil {
			reported := make([]string, 0, len(origins))
			for _, origin := range origins {
				reported = append(reported, origin.sourceType)
			}
			// Read is handed prior state, never configuration, so editing
			// create_from in the configuration cannot resolve this. The source
			// type has to arrive with the import ID itself.
			diags.AddError(
				"Ambiguous volume source",
				fmt.Sprintf(
					"Volume %s reports several origins (%s) and nothing in state selects one, so the source it was "+
						"created from cannot be determined. Import the volume with a composite ID that names the "+
						"intended origin, for example \"%s,%s\". Changing create_from in the configuration has no "+
						"effect here, because refreshing a volume reads state rather than configuration.",
					volume.Id, strings.Join(reported, ", "), volume.Id, reported[0],
				),
			)
			return types.ObjectNull(attributeTypes), diags
		}
	}

	values := map[string]attr.Value{
		"source_type":     types.StringValue(volumeSourceTypeEmpty),
		"image":           types.StringNull(),
		"custom_image_id": types.StringNull(),
		"snapshot_id":     types.StringNull(),
		"backup_id":       types.StringNull(),
		"volume_type":     types.StringNull(),
	}
	sourceType := volumeSourceTypeEmpty
	switch {
	case selected != nil:
		sourceType = selected.sourceType
		values["source_type"] = types.StringValue(selected.sourceType)
		values[selected.attribute] = selected.value
	case retainPrior:
		sourceType = priorSourceType
		values["source_type"] = types.StringValue(priorSourceType)
		if attribute, value, ok := priorVolumeSourceID(prior, priorSourceType); ok {
			values[attribute] = value
		}
	}
	// A snapshot source rejects create_from.volume_type in configuration because
	// the snapshot picks the type, so the response value stays out of state.
	if volume.CreateFrom.VolumeType != nil && sourceType != volumeSourceTypeSnapshot {
		values["volume_type"] = types.StringValue(volume.CreateFrom.VolumeType.Name)
	}
	preserveConfiguredVolumeSource(values, prior)

	result, objectDiags := types.ObjectValue(attributeTypes, values)
	diags.Append(objectDiags...)
	if diags.HasError() {
		return types.ObjectNull(attributeTypes), diags
	}
	return result, diags
}

// volumeOrigins lists every source the response reports for the volume.
// NestedVolumeOriginSchema declares each origin field as independent and
// optional, and carries no discriminator. More than one can therefore be
// populated at once: a snapshot of a volume that itself came from an image.
func volumeOrigins(volume *blockstoragesdk.VolumeDetailSchema) []volumeOrigin {
	var origins []volumeOrigin
	if volume.CreateFrom.Image != nil {
		origins = append(origins, volumeOrigin{volumeSourceTypeImage, "image", types.StringValue(volume.CreateFrom.Image.Name)})
	}
	if volume.CreateFrom.CustomImage != nil {
		origins = append(origins, volumeOrigin{volumeSourceTypeCustomImage, "custom_image_id", types.StringValue(volume.CreateFrom.CustomImage.Id.String())})
	}
	if volume.CreateFrom.Snapshot != nil {
		origins = append(origins, volumeOrigin{volumeSourceTypeSnapshot, "snapshot_id", types.StringValue(volume.CreateFrom.Snapshot.Id.String())})
	}
	if volume.CreateFrom.Backup != nil {
		origins = append(origins, volumeOrigin{volumeSourceTypeBackup, "backup_id", types.StringValue(volume.CreateFrom.Backup.Id.String())})
	}
	return origins
}

func priorVolumeSourceID(prior VolumeCreateFromModel, sourceType string) (string, types.String, bool) {
	switch sourceType {
	case volumeSourceTypeImage:
		return "image", prior.Image, true
	case volumeSourceTypeCustomImage:
		return "custom_image_id", prior.CustomImageID, true
	case volumeSourceTypeSnapshot:
		return "snapshot_id", prior.SnapshotID, true
	case volumeSourceTypeBackup:
		return "backup_id", prior.BackupID, true
	default:
		return "", types.StringNull(), false
	}
}

func preserveConfiguredVolumeSource(values map[string]attr.Value, prior VolumeCreateFromModel) {
	keep := func(attribute string, configured types.String, equal func(configured types.String, refreshed string) bool) {
		refreshed, ok := values[attribute].(types.String)
		if !ok || refreshed.IsNull() || configured.IsNull() || configured.IsUnknown() {
			return
		}
		if equal(configured, refreshed.ValueString()) {
			values[attribute] = configured
		}
	}
	exact := func(configured types.String, refreshed string) bool {
		return strings.TrimSpace(configured.ValueString()) == refreshed
	}
	// An ID is the same ID however it is spelled, so it is compared as a UUID
	// rather than as text.
	sameUUID := func(configured types.String, refreshed string) bool {
		parsed, err := core.ParseUUID(refreshed)
		return err == nil && compare.SameUUID(configured, parsed)
	}

	keep("source_type", prior.SourceType, exact)
	keep("image", prior.Image, exact)
	keep("volume_type", prior.VolumeType, exact)
	keep("custom_image_id", prior.CustomImageID, sameUUID)
	keep("snapshot_id", prior.SnapshotID, sameUUID)
	keep("backup_id", prior.BackupID, sameUUID)
}
