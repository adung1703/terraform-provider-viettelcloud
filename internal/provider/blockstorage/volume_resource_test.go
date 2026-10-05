package blockstorage

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	blockstoragesdk "github.com/viettelcloud-oss/sdks/go/blockstorage"
	"github.com/viettelcloud-oss/sdks/go/core"
	projectsdk "github.com/viettelcloud-oss/sdks/go/project"
	serversdk "github.com/viettelcloud-oss/sdks/go/server"

	blockstoragelookup "github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/provider/blockstorage/lookup"
	"github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/provider/providerdata"
	serverlookup "github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/provider/server/lookup"
)

func TestVolumeModelsMatchSchemas(t *testing.T) {
	t.Parallel()
	var resourceResponse resource.SchemaResponse
	(&VolumeResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resourceResponse)
	plan := tfsdk.Plan{Schema: resourceResponse.Schema}
	if diags := plan.Set(context.Background(), &VolumeResourceModel{CreateFrom: types.ObjectNull(volumeCreateFromAttributeTypes())}); diags.HasError() {
		t.Fatalf("resource model does not match schema: %v", diags)
	}
}

func TestVolumeResourceZonePlanModifiers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	volumeSchema := volumeResourceSchema(t)
	attribute, ok := volumeSchema.Attributes["zone"].(resourceschema.StringAttribute)
	if !ok {
		t.Fatal("zone is not a string attribute")
	}
	if !attribute.Optional || !attribute.Computed || len(attribute.PlanModifiers) != 2 {
		t.Fatalf("unexpected zone schema: %#v", attribute)
	}

	model := VolumeResourceModel{
		VolumeModel: VolumeModel{
			ID: types.StringValue(volumeTestID.String()), Name: types.StringValue("data"),
			Size: types.Int64Value(30), Zone: types.StringValue("zone-a"),
		},
		CreateFrom: volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{
			"volume_type": types.StringValue("Premium"),
		}),
	}
	state := tfsdk.State{Schema: volumeSchema}
	if diags := state.Set(ctx, &model); diags.HasError() {
		t.Fatalf("set state: %v", diags)
	}
	planned := model
	planned.Zone = types.StringValue("zone-b")
	plan := tfsdk.Plan{Schema: volumeSchema}
	if diags := plan.Set(ctx, &planned); diags.HasError() {
		t.Fatalf("set plan: %v", diags)
	}

	var changed planmodifier.StringResponse
	attribute.PlanModifiers[1].PlanModifyString(ctx, planmodifier.StringRequest{
		State:       state,
		Plan:        plan,
		ConfigValue: types.StringValue("zone-b"),
		StateValue:  types.StringValue("zone-a"),
		PlanValue:   types.StringValue("zone-b"),
	}, &changed)
	if !changed.RequiresReplace {
		t.Fatal("changing a configured zone must replace the volume")
	}
	var unchanged planmodifier.StringResponse
	attribute.PlanModifiers[1].PlanModifyString(ctx, planmodifier.StringRequest{
		State:       state,
		Plan:        plan,
		ConfigValue: types.StringValue("zone-a"),
		StateValue:  types.StringValue("zone-a"),
		PlanValue:   types.StringValue("zone-a"),
	}, &unchanged)
	if unchanged.RequiresReplace {
		t.Fatal("an unchanged configured zone must not replace the volume")
	}
	var deferred planmodifier.StringResponse
	attribute.PlanModifiers[1].PlanModifyString(ctx, planmodifier.StringRequest{
		State:       state,
		Plan:        plan,
		ConfigValue: types.StringUnknown(),
		StateValue:  types.StringValue("zone-a"),
		PlanValue:   types.StringUnknown(),
	}, &deferred)
	if !deferred.RequiresReplace {
		t.Fatal("an unknown configured zone must conservatively replace the volume")
	}

	var kept planmodifier.StringResponse
	kept.PlanValue = types.StringUnknown()
	attribute.PlanModifiers[0].PlanModifyString(ctx, planmodifier.StringRequest{
		State:       state,
		Plan:        plan,
		ConfigValue: types.StringNull(),
		StateValue:  types.StringValue("zone-a"),
		PlanValue:   types.StringUnknown(),
	}, &kept)
	if kept.PlanValue.IsUnknown() || kept.PlanValue.ValueString() != "zone-a" {
		t.Fatalf("omitting zone must keep the state value, got %v", kept.PlanValue)
	}
	var omitted planmodifier.StringResponse
	attribute.PlanModifiers[1].PlanModifyString(ctx, planmodifier.StringRequest{
		State:       state,
		Plan:        plan,
		ConfigValue: types.StringNull(),
		StateValue:  types.StringValue("zone-a"),
		PlanValue:   kept.PlanValue,
	}, &omitted)
	if omitted.RequiresReplace {
		t.Fatal("removing zone from configuration must not replace the volume")
	}
}

func TestVolumeResourceConfigure(t *testing.T) {
	t.Parallel()
	blockClient := volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected request: %s", req.URL)
		return nil, nil
	})
	serverClient, err := serversdk.NewClient("https://server.test", serversdk.WithRetry(core.NoRetry()))
	if err != nil {
		t.Fatalf("create server client: %v", err)
	}
	projectClient, err := projectsdk.NewClient("https://project.test", projectsdk.WithRetry(core.NoRetry()))
	if err != nil {
		t.Fatalf("create project client: %v", err)
	}
	r := &VolumeResource{}
	var invalid resource.ConfigureResponse
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: "invalid"}, &invalid)
	if !invalid.Diagnostics.HasError() {
		t.Fatal("expected invalid provider data diagnostic")
	}
	var response resource.ConfigureResponse
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: &providerdata.Configured{
		BlockStorage: blockClient, Server: serverClient, Project: projectClient, ProjectID: core.UUID{9},
	}}, &response)
	if response.Diagnostics.HasError() || r.client != blockClient || r.server != serverClient || r.projectClient != projectClient || r.projectID != (core.UUID{9}) {
		t.Fatalf("unexpected configure result: resource=%#v diagnostics=%v", r, response.Diagnostics)
	}
}

func TestVolumeResourceResolveZoneID(t *testing.T) {
	t.Parallel()
	zoneID := core.UUID{8}
	projectClient, err := projectsdk.NewClient("https://project.test", projectsdk.WithHTTPClient(&http.Client{Transport: volumeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Path != "/v2/projects/"+volumeTestProjectID.String()+"/zones/" {
			return testHTTPResponse(req, http.StatusNotFound, `{}`), nil
		}
		if got := req.URL.Query().Get("name"); got != "zone-a" {
			t.Errorf("expected zone name filter %q, got %q", "zone-a", got)
		}
		body, err := json.Marshal(&projectsdk.PagedProjectZoneSchema{
			Count: 1,
			Results: []projectsdk.ProjectZoneSchema{{
				Id: zoneID, Name: "zone-a",
				Region: projectsdk.NestedRegionSchema{Id: core.UUID{7}, Name: "region-a"},
			}},
		})
		if err != nil {
			return nil, err
		}
		return testHTTPResponse(req, http.StatusOK, string(body)), nil
	})}), projectsdk.WithRetry(core.NoRetry()))
	if err != nil {
		t.Fatalf("create project client: %v", err)
	}
	r := &VolumeResource{projectClient: projectClient, projectID: volumeTestProjectID}
	got, diags := r.resolveZoneID(context.Background(), types.StringValue("  zone-a  "))
	if diags.HasError() || got == nil || *got != zoneID {
		t.Fatalf("expected zone ID %s, got %v with diagnostics %v", zoneID, got, diags)
	}
	for _, zone := range []types.String{types.StringNull(), types.StringUnknown()} {
		got, diags = r.resolveZoneID(context.Background(), zone)
		if diags.HasError() || got != nil {
			t.Fatalf("unset zone must not resolve, got %v with diagnostics %v", got, diags)
		}
	}
}

func TestVolumeResourceModifyPlanChecksPlacementBeforeReplacement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	volumeSchema := volumeResourceSchema(t)
	zoneID := core.UUID{8}
	tests := []struct {
		name          string
		currentZone   string
		initial       bool
		replaceSource bool
		unknownZone   bool
		omitZone      bool
		zoneCount     int
		volumeTypes   string
		wantError     string
		wantZoneCalls int
		wantTypeCalls int
	}{
		{name: "missing zone", currentZone: "zone-b", wantError: "Unable to resolve zone", wantZoneCalls: 1},
		{name: "ambiguous zone", currentZone: "zone-b", zoneCount: 2, wantError: "Unable to resolve zone", wantZoneCalls: 1},
		{name: "missing type in new zone", currentZone: "zone-b", zoneCount: 1, volumeTypes: `{"count":0,"results":[]}`, wantError: "Unable to resolve volume type", wantZoneCalls: 1, wantTypeCalls: 1},
		{name: "valid replacement", currentZone: "zone-b", zoneCount: 1, volumeTypes: volumeTypesResponseJSON(), wantZoneCalls: 1, wantTypeCalls: 1},
		{name: "initial create", initial: true, zoneCount: 1, volumeTypes: volumeTypesResponseJSON(), wantZoneCalls: 1, wantTypeCalls: 1},
		{name: "source replacement", currentZone: "zone-a", replaceSource: true, zoneCount: 1, volumeTypes: volumeTypesResponseJSON(), wantZoneCalls: 1, wantTypeCalls: 1},
		{name: "unknown zone", currentZone: "zone-b", unknownZone: true},
		{name: "omitted zone", currentZone: "zone-b", omitZone: true},
		{name: "unchanged zone", currentZone: "zone-a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zoneCalls := 0
			projectClient := volumeTestProjectClient(t, func(req *http.Request) (*http.Response, error) {
				zoneCalls++
				if req.Method != http.MethodGet || req.URL.Path != "/v2/projects/"+volumeTestProjectID.String()+"/zones/" || req.URL.Query().Get("name") != "zone-a" {
					t.Errorf("unexpected zone lookup: %s %s", req.Method, req.URL)
				}
				zones := make([]projectsdk.ProjectZoneSchema, tt.zoneCount)
				for i := range zones {
					zones[i] = projectsdk.ProjectZoneSchema{Id: core.UUID{byte(i + 8)}, Name: "zone-a", Region: projectsdk.NestedRegionSchema{Id: core.UUID{7}, Name: "region-a"}}
				}
				body, err := json.Marshal(projectsdk.PagedProjectZoneSchema{Count: tt.zoneCount, Results: zones})
				if err != nil {
					return nil, err
				}
				return testHTTPResponse(req, http.StatusOK, string(body)), nil
			})
			typeCalls := 0
			client := volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
				typeCalls++
				if req.Method != http.MethodGet || req.URL.Path != "/v2/block-storage/volume-types/" || req.URL.Query().Get("zone_id") != zoneID.String() || req.URL.Query().Get("name") != "Premium" {
					t.Errorf("unexpected volume type lookup: %s %s", req.Method, req.URL)
				}
				return testHTTPResponse(req, http.StatusOK, tt.volumeTypes), nil
			})
			r := &VolumeResource{client: client, projectClient: projectClient, projectID: volumeTestProjectID}
			current := VolumeResourceModel{
				VolumeModel: VolumeModel{ID: types.StringValue(volumeTestID.String()), Name: types.StringValue("data"), Size: types.Int64Value(100), Zone: types.StringValue(tt.currentZone), ZoneID: types.StringValue((core.UUID{7}).String())},
				CreateFrom:  volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringValue("Premium")}),
			}
			planned := current
			planned.Zone = types.StringValue("zone-a")
			if tt.replaceSource {
				planned.CreateFrom = volumeCreateFromValue(t, volumeSourceTypeImage, map[string]attr.Value{
					"image": types.StringValue("Ubuntu"), "volume_type": types.StringValue("Premium"),
				})
			}
			if tt.unknownZone {
				planned.Zone = types.StringUnknown()
			}
			if tt.omitZone {
				planned.Zone = current.Zone
			}
			if tt.initial {
				planned.ID = types.StringUnknown()
				planned.ZoneID = types.StringUnknown()
			}
			plan := tfsdk.Plan{Schema: volumeSchema}
			if diags := plan.Set(ctx, &planned); diags.HasError() {
				t.Fatalf("set plan: %v", diags)
			}
			state := tfsdk.State{Schema: volumeSchema}
			if !tt.initial {
				if diags := state.Set(ctx, &current); diags.HasError() {
					t.Fatalf("set state: %v", diags)
				}
			}
			configured := planned
			if tt.omitZone {
				configured.Zone = types.StringNull()
			}
			config := tfsdk.State{Schema: volumeSchema}
			if diags := config.Set(ctx, &configured); diags.HasError() {
				t.Fatalf("set config: %v", diags)
			}
			response := resource.ModifyPlanResponse{Plan: plan}
			r.ModifyPlan(ctx, resource.ModifyPlanRequest{Config: tfsdk.Config(config), Plan: plan, State: state}, &response)
			if tt.wantError == "" {
				if response.Diagnostics.HasError() {
					t.Fatalf("unexpected plan diagnostics: %v", response.Diagnostics)
				}
				if tt.wantZoneCalls > 0 && !tt.initial {
					var result VolumeResourceModel
					if diags := response.Plan.Get(ctx, &result); diags.HasError() || !result.ZoneID.IsUnknown() {
						t.Fatalf("replacement must recompute zone_id, plan=%#v diagnostics=%v", result, diags)
					}
				}
			} else if !response.Diagnostics.HasError() || response.Diagnostics[0].Summary() != tt.wantError {
				t.Fatalf("expected %q before replacement, got %v", tt.wantError, response.Diagnostics)
			}
			if zoneCalls != tt.wantZoneCalls || typeCalls != tt.wantTypeCalls {
				t.Fatalf("unexpected lookup counts: zone=%d type=%d", zoneCalls, typeCalls)
			}
		})
	}
}

func TestVolumeResourceValidateConfigRejectsInvalidImmutableSource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var schemaResponse resource.SchemaResponse
	(&VolumeResource{}).Schema(ctx, resource.SchemaRequest{}, &schemaResponse)

	configModel := VolumeResourceModel{
		VolumeModel: VolumeModel{Name: types.StringValue("data"), Size: types.Int64Value(50)},
		CreateFrom: volumeCreateFromValue(t, "invalid", map[string]attr.Value{
			"volume_type": types.StringValue("Premium"),
		}),
	}
	plan := tfsdk.Plan{Schema: schemaResponse.Schema}
	if diags := plan.Set(ctx, &configModel); diags.HasError() {
		t.Fatalf("set validation config: %v", diags)
	}

	var response resource.ValidateConfigResponse
	(&VolumeResource{}).ValidateConfig(ctx, resource.ValidateConfigRequest{
		Config: tfsdk.Config{Raw: plan.Raw, Schema: schemaResponse.Schema},
	}, &response)
	if !response.Diagnostics.HasError() || response.Diagnostics[0].Summary() != "Invalid create_from.source_type" {
		t.Fatalf("expected plan-time source type diagnostic, got %v", response.Diagnostics)
	}
}

func TestVolumeResourceValidateConfigRejectsBlankOrUntrimmedZone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	volumeSchema := volumeResourceSchema(t)
	for _, zone := range []string{"  ", " zone-a", "zone-a ", "  zone-a  "} {
		configModel := VolumeResourceModel{
			VolumeModel: VolumeModel{
				Name: types.StringValue("data"), Size: types.Int64Value(50), Zone: types.StringValue(zone),
			},
			CreateFrom: volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{
				"volume_type": types.StringValue("Premium"),
			}),
		}
		configState := tfsdk.State{Schema: volumeSchema}
		if diags := configState.Set(ctx, &configModel); diags.HasError() {
			t.Fatalf("set config: %v", diags)
		}
		var response resource.ValidateConfigResponse
		(&VolumeResource{}).ValidateConfig(ctx, resource.ValidateConfigRequest{Config: tfsdk.Config(configState)}, &response)
		if !response.Diagnostics.HasError() || response.Diagnostics[0].Summary() != "Invalid volume zone" {
			t.Fatalf("expected zone diagnostic for %q, got %v", zone, response.Diagnostics)
		}
	}
}

func TestVolumeResourceRejectsSnapshotZoneBeforeCreate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	volumeSchema := volumeResourceSchema(t)
	model := VolumeResourceModel{
		VolumeModel: VolumeModel{Name: types.StringValue("snapshot-volume"), Zone: types.StringValue("zone-a"), Size: types.Int64Unknown()},
		CreateFrom: volumeCreateFromValue(t, volumeSourceTypeSnapshot, map[string]attr.Value{
			"snapshot_id": types.StringValue((core.UUID{5}).String()),
		}),
	}
	plan := tfsdk.Plan{Schema: volumeSchema}
	if diags := plan.Set(ctx, &model); diags.HasError() {
		t.Fatalf("set snapshot plan: %v", diags)
	}
	config := tfsdk.Config{Raw: plan.Raw, Schema: volumeSchema}
	var validation resource.ValidateConfigResponse
	(&VolumeResource{}).ValidateConfig(ctx, resource.ValidateConfigRequest{Config: config}, &validation)
	if !validation.Diagnostics.HasError() || validation.Diagnostics[0].Summary() != "Invalid volume zone" {
		t.Fatalf("expected snapshot zone validation error, got %v", validation.Diagnostics)
	}
	planned := resource.ModifyPlanResponse{Plan: plan}
	(&VolumeResource{}).ModifyPlan(ctx, resource.ModifyPlanRequest{
		Config: config, Plan: plan, State: tfsdk.State{Schema: volumeSchema},
	}, &planned)
	if !planned.Diagnostics.HasError() || planned.Diagnostics[0].Summary() != "Invalid volume zone" {
		t.Fatalf("expected snapshot zone plan error, got %v", planned.Diagnostics)
	}

	// Create must also stop when a zone becomes known only during apply.
	unknownZone := model
	unknownZone.Zone = types.StringUnknown()
	unknownPlan := tfsdk.Plan{Schema: volumeSchema}
	if diags := unknownPlan.Set(ctx, &unknownZone); diags.HasError() {
		t.Fatalf("set unknown-zone config: %v", diags)
	}
	unknownConfig := tfsdk.Config{Raw: unknownPlan.Raw, Schema: volumeSchema}
	validation = resource.ValidateConfigResponse{}
	(&VolumeResource{}).ValidateConfig(ctx, resource.ValidateConfigRequest{Config: unknownConfig}, &validation)
	if validation.Diagnostics.HasError() {
		t.Fatalf("an unknown zone must be deferred until apply, got %v", validation.Diagnostics)
	}
	response := resource.CreateResponse{State: tfsdk.State{Schema: volumeSchema}}
	(&VolumeResource{}).Create(ctx, resource.CreateRequest{Config: unknownConfig, Plan: plan}, &response)
	if !response.Diagnostics.HasError() || response.Diagnostics[0].Summary() != "Invalid volume zone" {
		t.Fatalf("expected snapshot zone create error before any API call, got %v", response.Diagnostics)
	}
}

func TestValidateVolumeCreateFromConfigSourceRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		sourceType string
		size       types.Int64
		extra      map[string]attr.Value
		want       string
	}{
		{
			name: "missing image", sourceType: volumeSourceTypeImage, size: types.Int64Value(50),
			extra: map[string]attr.Value{"volume_type": types.StringValue("Premium")}, want: "Missing image",
		},
		{
			name: "invalid custom image ID", sourceType: volumeSourceTypeCustomImage, size: types.Int64Value(50),
			extra: map[string]attr.Value{"custom_image_id": types.StringValue("invalid"), "volume_type": types.StringValue("Premium")}, want: "Invalid UUID",
		},
		{
			name: "source field conflict", sourceType: volumeSourceTypeEmpty, size: types.Int64Value(50),
			extra: map[string]attr.Value{"image": types.StringValue("Ubuntu"), "volume_type": types.StringValue("Premium")}, want: "Invalid create_from fields",
		},
		{
			name: "missing size", sourceType: volumeSourceTypeEmpty, size: types.Int64Null(),
			extra: map[string]attr.Value{"volume_type": types.StringValue("Premium")}, want: "Missing volume size",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := VolumeResourceModel{
				VolumeModel: VolumeModel{Name: types.StringValue("data"), Size: tt.size},
				CreateFrom:  volumeCreateFromValue(t, tt.sourceType, tt.extra),
			}
			diags := validateVolumeCreateFromConfig(context.Background(), config.CreateFrom, config.Size)
			if !diags.HasError() {
				t.Fatalf("expected %q diagnostic", tt.want)
			}
			found := false
			for _, diagnostic := range diags {
				if diagnostic.Summary() == tt.want {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected %q diagnostic, got %v", tt.want, diags)
			}
		})
	}
}

// A snapshot fixes the size at creation but the volume can still be grown
// afterwards. ValidateConfig runs on every operation and cannot tell create from
// update, so it must accept a configured size; only the create path rejects it.
func TestValidateVolumeCreateFromConfigAllowsResizingASnapshotVolume(t *testing.T) {
	t.Parallel()
	config := VolumeResourceModel{
		VolumeModel: VolumeModel{Name: types.StringValue("data"), Size: types.Int64Value(50)},
		CreateFrom: volumeCreateFromValue(t, volumeSourceTypeSnapshot, map[string]attr.Value{
			"snapshot_id": types.StringValue((core.UUID{5}).String()),
		}),
	}
	if diags := validateVolumeCreateFromConfig(context.Background(), config.CreateFrom, config.Size); diags.HasError() {
		t.Fatalf("a snapshot-sourced volume must remain resizable, got %v", diags)
	}

	// The same configuration is still rejected while creating the volume.
	if _, diags := buildVolumeCreateBody(context.Background(), config, nil, fixedImageResolver(volumeTestID, nil), fixedVolumeTypeResolver(volumeTestID)); !diags.HasError() ||
		diags[0].Summary() != "Invalid size for snapshot source" {
		t.Fatalf("expected the create path to reject an explicit snapshot size, got %v", diags)
	}
}

func TestValidateVolumeCreateFromConfigAllowsUnknownValues(t *testing.T) {
	t.Parallel()
	config := VolumeResourceModel{
		VolumeModel: VolumeModel{Name: types.StringValue("data"), Size: types.Int64Unknown()},
		CreateFrom: volumeCreateFromValue(t, volumeSourceTypeImage, map[string]attr.Value{
			"image": types.StringUnknown(), "volume_type": types.StringUnknown(),
		}),
	}
	if diags := validateVolumeCreateFromConfig(context.Background(), config.CreateFrom, config.Size); diags.HasError() {
		t.Fatalf("unknown values must be deferred until apply, got %v", diags)
	}
}

func TestBuildVolumeCreateBodyEmpty(t *testing.T) {
	t.Parallel()
	body, diags := buildVolumeCreateBody(context.Background(), VolumeResourceModel{
		VolumeModel: VolumeModel{
			Name:        types.StringValue("  data  "),
			Description: types.StringValue("database"),
			Size:        types.Int64Value(100),
			IOPS:        types.Int64Value(3000),
		},
		CreateFrom: volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{
			"volume_type": types.StringValue("Premium"),
		}),
	}, nil, fixedImageResolver(volumeTestID, nil), fixedVolumeTypeResolver(volumeTestID))
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	createFrom, err := body.CreateFrom.AsVolumeCreateEmptySchema()
	if err != nil || createFrom.Size != 100 || createFrom.VolumeTypeId != volumeTestID {
		t.Fatalf("unexpected empty source: %#v err=%v", createFrom, err)
	}
	if body.Name != "data" || body.Description == nil || *body.Description != "database" || body.Iops == nil || *body.Iops != 3000 {
		t.Fatalf("unexpected create body: %#v", body)
	}
	encoded, err := json.Marshal(body)
	if err != nil || !strings.Contains(string(encoded), `"source_type":"empty"`) {
		t.Fatalf("create body omitted union discriminator: %s, err=%v", encoded, err)
	}
}

func TestBuildVolumeCreateFromVariants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		sourceType string
		extra      map[string]attr.Value
		check      func(t *testing.T, source blockstoragesdk.VolumeCreateSourceOptions)
	}{
		{
			name: "image", sourceType: volumeSourceTypeImage,
			extra: map[string]attr.Value{"image": types.StringValue("Ubuntu"), "volume_type": types.StringValue("Premium")},
			check: func(t *testing.T, source blockstoragesdk.VolumeCreateSourceOptions) {
				value, err := source.AsVolumeCreateFromImageSchema()
				if err != nil || value.ImageId != (core.UUID{2}) || value.VolumeTypeId != volumeTestID {
					t.Fatalf("unexpected image source: %#v err=%v", value, err)
				}
			},
		},
		{
			name: "custom image", sourceType: volumeSourceTypeCustomImage,
			extra: map[string]attr.Value{"custom_image_id": types.StringValue((core.UUID{3}).String()), "volume_type": types.StringValue("Premium")},
			check: func(t *testing.T, source blockstoragesdk.VolumeCreateSourceOptions) {
				value, err := source.AsVolumeCreateFromCustomImageSchema()
				if err != nil || value.CustomImageId != (core.UUID{3}) {
					t.Fatalf("unexpected custom image source: %#v err=%v", value, err)
				}
			},
		},
		{
			name: "backup", sourceType: volumeSourceTypeBackup,
			extra: map[string]attr.Value{"backup_id": types.StringValue((core.UUID{4}).String()), "volume_type": types.StringValue("Premium")},
			check: func(t *testing.T, source blockstoragesdk.VolumeCreateSourceOptions) {
				value, err := source.AsVolumeCreateFromBackupSchema()
				if err != nil || value.BackupId != (core.UUID{4}) {
					t.Fatalf("unexpected backup source: %#v err=%v", value, err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := VolumeResourceModel{
				VolumeModel: VolumeModel{Name: types.StringValue("data"), Size: types.Int64Value(50)},
				CreateFrom:  volumeCreateFromValue(t, tt.sourceType, tt.extra),
			}
			body, diags := buildVolumeCreateBody(context.Background(), plan, nil, fixedImageResolver(core.UUID{2}, nil), fixedVolumeTypeResolver(volumeTestID))
			if diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			tt.check(t, body.CreateFrom)
		})
	}
}

func TestBuildVolumeCreateFromSnapshotAndValidation(t *testing.T) {
	t.Parallel()
	plan := VolumeResourceModel{
		VolumeModel: VolumeModel{Name: types.StringValue("snapshot-volume"), Size: types.Int64Unknown()},
		CreateFrom: volumeCreateFromValue(t, volumeSourceTypeSnapshot, map[string]attr.Value{
			"snapshot_id": types.StringValue((core.UUID{5}).String()),
		}),
	}
	body, diags := buildVolumeCreateBody(context.Background(), plan, nil, fixedImageResolver(volumeTestID, nil), fixedVolumeTypeResolver(volumeTestID))
	if diags.HasError() {
		t.Fatalf("unexpected snapshot diagnostics: %v", diags)
	}
	value, err := body.CreateFrom.AsVolumeCreateFromSnapshotSchema()
	if err != nil || value.SnapshotId != (core.UUID{5}) {
		t.Fatalf("unexpected snapshot source: %#v err=%v", value, err)
	}

	plan.Size = types.Int64Value(50)
	if _, diags = buildVolumeCreateBody(context.Background(), plan, nil, fixedImageResolver(volumeTestID, nil), fixedVolumeTypeResolver(volumeTestID)); !diags.HasError() {
		t.Fatal("expected snapshot size diagnostic")
	}
	plan.CreateFrom = volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{
		"image":       types.StringValue("Ubuntu"),
		"volume_type": types.StringValue("Premium"),
	})
	if _, diags = buildVolumeCreateBody(context.Background(), plan, nil, fixedImageResolver(volumeTestID, nil), fixedVolumeTypeResolver(volumeTestID)); !diags.HasError() {
		t.Fatal("expected diagnostic for source-specific field mismatch")
	}
}

func TestBuildVolumeCreateFromChecksImageMinimumSize(t *testing.T) {
	t.Parallel()
	minimum := 60
	plan := VolumeResourceModel{
		VolumeModel: VolumeModel{Name: types.StringValue("image-volume"), Size: types.Int64Value(50)},
		CreateFrom: volumeCreateFromValue(t, volumeSourceTypeImage, map[string]attr.Value{
			"image": types.StringValue("Ubuntu"), "volume_type": types.StringValue("Premium"),
		}),
	}
	_, diags := buildVolumeCreateBody(context.Background(), plan, nil, fixedImageResolver(volumeTestID, &minimum), fixedVolumeTypeResolver(volumeTestID))
	if !diags.HasError() || !strings.Contains(diags[0].Detail(), "requires at least 60") {
		t.Fatalf("expected image minimum-size diagnostic, got %v", diags)
	}
}

func TestBuildVolumeCreateFromImageDelegatesUsabilityToBackend(t *testing.T) {
	t.Parallel()
	plan := VolumeResourceModel{
		VolumeModel: VolumeModel{Name: types.StringValue("image-volume"), Size: types.Int64Value(50)},
		CreateFrom: volumeCreateFromValue(t, volumeSourceTypeImage, map[string]attr.Value{
			"image": types.StringValue("Ubuntu"), "volume_type": types.StringValue("Premium"),
		}),
	}
	resolveImage := func(_ context.Context, filter serverlookup.ImageFilter) (serversdk.ImageSchema, error) {
		if filter.Name == nil || *filter.Name != "Ubuntu" || filter.ID != nil || filter.Status != nil || filter.State != nil {
			t.Fatalf("expected an image name filter only, got %#v", filter)
		}
		return serversdk.ImageSchema{Id: volumeTestID, Name: *filter.Name}, nil
	}

	if _, diags := buildVolumeCreateBody(context.Background(), plan, nil, resolveImage, fixedVolumeTypeResolver(volumeTestID)); diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}

func TestBuildVolumeCreateBodyPassesZoneIDToVolumeTypeResolver(t *testing.T) {
	t.Parallel()
	plan := VolumeResourceModel{
		VolumeModel: VolumeModel{Name: types.StringValue("data"), Size: types.Int64Value(50)},
		CreateFrom: volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{
			"volume_type": types.StringValue("Ceph_HDD"),
		}),
	}
	zoneID := core.UUID{9}
	resolveVolumeType := func(_ context.Context, request blockstoragelookup.VolumeTypeResolveRequest) (blockstoragesdk.VolumeTypeSchema, error) {
		if request.ZoneID == nil || *request.ZoneID != zoneID {
			t.Fatalf("expected the resolved zone ID in the volume type request, got %#v", request.ZoneID)
		}
		return blockstoragesdk.VolumeTypeSchema{Id: volumeTestID, Name: request.Name, MaxVolumeSize: 500}, nil
	}

	if _, diags := buildVolumeCreateBody(context.Background(), plan, &zoneID, fixedImageResolver(volumeTestID, nil), resolveVolumeType); diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}

func TestPopulateVolumePendingStatePreservesConfiguredZone(t *testing.T) {
	t.Parallel()
	configured := VolumeResourceModel{VolumeModel: VolumeModel{Zone: types.StringValue("zone-a")}}
	populateVolumePendingState(&configured, volumeTestID)
	if configured.Zone.ValueString() != "zone-a" {
		t.Fatalf("pending state changed the configured zone: %v", configured.Zone)
	}

	unconfigured := VolumeResourceModel{VolumeModel: VolumeModel{Zone: types.StringUnknown()}}
	populateVolumePendingState(&unconfigured, volumeTestID)
	if !unconfigured.Zone.IsNull() {
		t.Fatalf("pending state must clear an unknown computed zone, got %v", unconfigured.Zone)
	}
}

func TestBuildVolumeUpdatePlanSparseAndLifecycleRules(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	state := VolumeResourceModel{
		VolumeModel: VolumeModel{
			Name: types.StringValue("before"), Description: types.StringValue("keep"), Size: types.Int64Value(100), IOPS: types.Int64Value(1000),
		},
		CreateFrom: volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringValue("Premium")}),
	}
	plan := state
	plan.Name = types.StringValue("  after  ")
	plan.Description = types.StringNull()
	plan.Size = types.Int64Value(200)
	// A resize travels through the extend action, so it must never appear in the
	// patch body: the backend accepts a size there and silently ignores it.
	update, diags := buildVolumeUpdatePlan(ctx, plan, state)
	if diags.HasError() || !update.patch || update.body.Name == nil || *update.body.Name != "after" ||
		update.body.Description != nil || update.body.Iops != nil || update.body.Size != nil {
		t.Fatalf("unexpected sparse update: %#v, %v", update, diags)
	}
	if update.extendSize == nil || *update.extendSize != 200 {
		t.Fatalf("expected the resize to be routed to the extend action, got %#v", update.extendSize)
	}
	if update.retypeName != "" {
		t.Fatalf("an unchanged volume type must not retype, got %q", update.retypeName)
	}

	plan = state
	plan.Description = types.StringValue("")
	update, diags = buildVolumeUpdatePlan(ctx, plan, state)
	if diags.HasError() || !update.patch || update.body.Description == nil || *update.body.Description != "" || update.extendSize != nil {
		t.Fatalf("expected explicit description clear without a resize, got %#v, %v", update, diags)
	}

	// A resize on its own must not send an empty patch alongside the extend.
	plan = state
	plan.Size = types.Int64Value(200)
	update, diags = buildVolumeUpdatePlan(ctx, plan, state)
	if diags.HasError() || update.patch || update.extendSize == nil || *update.extendSize != 200 {
		t.Fatalf("expected an extend-only update, got %#v, %v", update, diags)
	}

	// The backend retypes in place, so a new volume type name is an update, not
	// a reason to destroy a volume that holds data.
	plan = state
	plan.CreateFrom = volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringValue("  Standard  ")})
	update, diags = buildVolumeUpdatePlan(ctx, plan, state)
	if diags.HasError() || update.retypeName != "Standard" || update.patch || update.extendSize != nil {
		t.Fatalf("expected a retype-only update, got %#v, %v", update, diags)
	}

	// Omitting the volume type is not a request to change it.
	plan = state
	plan.CreateFrom = volumeCreateFromValue(t, volumeSourceTypeEmpty, nil)
	update, diags = buildVolumeUpdatePlan(ctx, plan, state)
	if diags.HasError() || update.retypeName != "" || update.patch || update.extendSize != nil {
		t.Fatalf("an omitted volume type must leave the volume alone, got %#v, %v", update, diags)
	}

	plan = state
	plan.Size = types.Int64Value(50)
	if _, diags = buildVolumeUpdatePlan(ctx, plan, state); !diags.HasError() {
		t.Fatal("expected size-shrink diagnostic")
	}
}

// Both size rules compare configuration against prior state, so ValidateConfig
// cannot see them. Reporting them at plan time keeps an apply from destroying a
// volume it then cannot recreate, or from stopping halfway through a config.
func TestVolumeResourceModifyPlanReportsSizeRulesBeforeApply(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	volumeSchema := volumeResourceSchema(t)
	r := &VolumeResource{}

	buildState := func(t *testing.T, model VolumeResourceModel) tfsdk.State {
		t.Helper()
		state := tfsdk.State{Schema: volumeSchema}
		if diags := state.Set(ctx, &model); diags.HasError() {
			t.Fatalf("set volume: %v", diags)
		}
		return state
	}
	buildPlan := func(t *testing.T, model VolumeResourceModel) tfsdk.Plan {
		t.Helper()
		return tfsdk.Plan(buildState(t, model))
	}
	buildConfig := func(t *testing.T, model VolumeResourceModel) tfsdk.Config {
		t.Helper()
		return tfsdk.Config(buildState(t, model))
	}
	empty := func(size int64) VolumeResourceModel {
		return VolumeResourceModel{
			VolumeModel: VolumeModel{ID: types.StringValue(volumeTestID.String()), Name: types.StringValue("data"), Size: types.Int64Value(size)},
			CreateFrom:  volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringValue("Premium")}),
		}
	}
	image := func(name string, size int64) VolumeResourceModel {
		return VolumeResourceModel{
			VolumeModel: VolumeModel{ID: types.StringValue(volumeTestID.String()), Name: types.StringValue("data"), Size: types.Int64Value(size)},
			CreateFrom: volumeCreateFromValue(t, volumeSourceTypeImage, map[string]attr.Value{
				"image": types.StringValue(name), "volume_type": types.StringValue("Premium"),
			}),
		}
	}
	snapshot := func(size types.Int64) VolumeResourceModel {
		return VolumeResourceModel{
			VolumeModel: VolumeModel{Name: types.StringValue("data"), Size: size},
			CreateFrom: volumeCreateFromValue(t, volumeSourceTypeSnapshot, map[string]attr.Value{
				"snapshot_id": types.StringValue(volumeTestID.String()),
			}),
		}
	}
	// The framework hands ModifyPlan an empty RequiresReplace list and appends
	// the attribute modifiers' paths only afterwards, so every case below runs
	// the same way the framework runs it: without one.
	modifyPlan := func(t *testing.T, config, plan, state VolumeResourceModel) resource.ModifyPlanResponse {
		t.Helper()
		response := resource.ModifyPlanResponse{Plan: buildPlan(t, plan)}
		r.ModifyPlan(ctx, resource.ModifyPlanRequest{
			Config: buildConfig(t, config),
			Plan:   buildPlan(t, plan),
			State:  buildState(t, state),
		}, &response)
		return response
	}

	shrink := modifyPlan(t, empty(50), empty(50), empty(100))
	if !shrink.Diagnostics.HasError() || !strings.Contains(shrink.Diagnostics[0].Detail(), "can only increase") {
		t.Fatalf("expected a plan-time shrink diagnostic, got %v", shrink.Diagnostics)
	}
	if grow := modifyPlan(t, empty(200), empty(200), empty(100)); grow.Diagnostics.HasError() {
		t.Fatalf("growing a volume must plan cleanly, got %v", grow.Diagnostics)
	}

	// Shrinking is only impossible in place. A plan that changes the immutable
	// source replaces the volume, and the new one is free to be smaller, so the
	// in-place rule must not be applied to it.
	replaced := modifyPlan(t, image("Ubuntu 24.04", 50), image("Ubuntu 24.04", 50), image("Ubuntu 22.04", 100))
	if replaced.Diagnostics.HasError() {
		t.Fatalf("replacing the source while shrinking must plan cleanly, got %v", replaced.Diagnostics)
	}

	// Terraform carries the prior value of Optional+Computed size into the plan
	// when it is omitted from configuration. A changed snapshot ID replaces the
	// volume, but that state-carried size must not be treated as configured.
	other := snapshot(types.Int64Value(40))
	other.CreateFrom = volumeCreateFromValue(t, volumeSourceTypeSnapshot, map[string]attr.Value{
		"snapshot_id": types.StringValue((core.UUID{6}).String()),
	})
	configuredReplacement := other
	configuredReplacement.Size = types.Int64Null()
	if replace := modifyPlan(t, configuredReplacement, other, snapshot(types.Int64Value(30))); replace.Diagnostics.HasError() {
		t.Fatalf("replacing a snapshot-backed volume with size omitted must plan cleanly, got %v", replace.Diagnostics)
	}
	priorSnapshot := snapshot(types.Int64Value(30))
	priorSnapshot.Zone = types.StringValue("zone-a")
	priorSnapshot.ZoneID = types.StringValue((core.UUID{8}).String())
	other.Zone = priorSnapshot.Zone
	other.ZoneID = priorSnapshot.ZoneID
	zoneReplacement := modifyPlan(t, configuredReplacement, other, priorSnapshot)
	if zoneReplacement.Diagnostics.HasError() {
		t.Fatalf("replacing a snapshot-backed volume must plan cleanly, got %v", zoneReplacement.Diagnostics)
	}
	var plannedReplacement VolumeResourceModel
	if diags := zoneReplacement.Plan.Get(ctx, &plannedReplacement); diags.HasError() {
		t.Fatalf("read snapshot replacement plan: %v", diags)
	}
	if !plannedReplacement.Zone.IsUnknown() || !plannedReplacement.ZoneID.IsUnknown() {
		t.Fatalf("an omitted zone must be recomputed for a new snapshot volume, got %#v", plannedReplacement)
	}

	// A size explicitly present in configuration remains invalid for the create
	// half of that replacement.
	replaceWithSize := modifyPlan(t, other, other, snapshot(types.Int64Value(30)))
	if !replaceWithSize.Diagnostics.HasError() || !strings.Contains(replaceWithSize.Diagnostics[0].Detail(), "size must be omitted") {
		t.Fatalf("expected a configured replacement size to be refused at plan time, got %v", replaceWithSize.Diagnostics)
	}

	// Growing that same volume in place stays allowed.
	if inPlace := modifyPlan(t, snapshot(types.Int64Value(40)), snapshot(types.Int64Value(40)), snapshot(types.Int64Value(30))); inPlace.Diagnostics.HasError() {
		t.Fatalf("growing a snapshot-backed volume must plan cleanly, got %v", inPlace.Diagnostics)
	}

	// Creating without a size is the supported shape and must stay clean.
	create := resource.ModifyPlanResponse{Plan: buildPlan(t, snapshot(types.Int64Unknown()))}
	r.ModifyPlan(ctx, resource.ModifyPlanRequest{
		Config: buildConfig(t, snapshot(types.Int64Null())),
		Plan:   buildPlan(t, snapshot(types.Int64Unknown())),
		State:  tfsdk.State{Schema: volumeSchema},
	}, &create)
	if create.Diagnostics.HasError() {
		t.Fatalf("creating from a snapshot without a size must plan cleanly, got %v", create.Diagnostics)
	}

	createWithSize := resource.ModifyPlanResponse{Plan: buildPlan(t, snapshot(types.Int64Value(30)))}
	r.ModifyPlan(ctx, resource.ModifyPlanRequest{
		Config: buildConfig(t, snapshot(types.Int64Value(30))),
		Plan:   buildPlan(t, snapshot(types.Int64Value(30))),
		State:  tfsdk.State{Schema: volumeSchema},
	}, &createWithSize)
	if !createWithSize.Diagnostics.HasError() || !strings.Contains(createWithSize.Diagnostics[0].Detail(), "size must be omitted") {
		t.Fatalf("expected a configured create size to be refused at plan time, got %v", createWithSize.Diagnostics)
	}
}

// A retype is the only in-place change that can move a value UseStateForUnknown
// freezes. Leaving the prior zone or encryption flag in the plan would fail the
// apply with an inconsistent result once the volume has already been retyped.
func TestVolumeResourceModifyPlanWithdrawsValuesARetypeCanMove(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	volumeSchema := volumeResourceSchema(t)
	r := &VolumeResource{}

	current := VolumeResourceModel{
		VolumeModel: VolumeModel{
			ID: types.StringValue(volumeTestID.String()), Name: types.StringValue("data"), Size: types.Int64Value(100),
			Zone: types.StringValue("zone-a"), ZoneID: types.StringValue((core.UUID{8}).String()), Encrypted: types.BoolValue(false),
			Bootable: types.BoolValue(false),
		},
		CreateFrom: volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringValue("Standard")}),
	}
	state := tfsdk.State{Schema: volumeSchema}
	if diags := state.Set(ctx, &current); diags.HasError() {
		t.Fatalf("set state: %v", diags)
	}
	buildPlan := func(t *testing.T, model VolumeResourceModel) tfsdk.Plan {
		t.Helper()
		plan := tfsdk.Plan{Schema: volumeSchema}
		if diags := plan.Set(ctx, &model); diags.HasError() {
			t.Fatalf("set plan: %v", diags)
		}
		return plan
	}
	// zone is Optional+Computed, so the configuration decides whether the plan
	// may withdraw it. Every case states the configured zone it runs with.
	buildConfig := func(t *testing.T, model VolumeResourceModel, zone types.String) tfsdk.Config {
		t.Helper()
		model.Zone = zone
		config := tfsdk.State{Schema: volumeSchema}
		if diags := config.Set(ctx, &model); diags.HasError() {
			t.Fatalf("set config: %v", diags)
		}
		return tfsdk.Config(config)
	}

	retyped := current
	retyped.CreateFrom = volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringValue("Premium")})
	response := resource.ModifyPlanResponse{Plan: buildPlan(t, retyped)}
	r.ModifyPlan(ctx, resource.ModifyPlanRequest{
		Config: buildConfig(t, retyped, types.StringNull()),
		Plan:   buildPlan(t, retyped),
		State:  state,
	}, &response)
	if response.Diagnostics.HasError() {
		t.Fatalf("a retype must plan cleanly, got %v", response.Diagnostics)
	}
	var planned VolumeResourceModel
	if diags := response.Plan.Get(ctx, &planned); diags.HasError() {
		t.Fatalf("get plan after retype: %v", diags)
	}
	if !planned.Zone.IsUnknown() || !planned.ZoneID.IsUnknown() || !planned.Encrypted.IsUnknown() {
		t.Fatalf("a retype must withdraw zone, zone_id, and encrypted from the plan: %#v", planned)
	}
	// Nothing a retype cannot move may be withdrawn: that would only add
	// "(known after apply)" noise to every retype plan.
	if planned.Bootable.IsUnknown() || planned.ID.IsUnknown() {
		t.Fatalf("a retype must not withdraw bootable or id: %#v", planned)
	}

	// An unknown volume type cannot be ruled out as a retype, and the plan is the
	// only place left to withdraw the values one moves.
	deferred := current
	deferred.CreateFrom = volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringUnknown()})
	unknown := resource.ModifyPlanResponse{Plan: buildPlan(t, deferred)}
	r.ModifyPlan(ctx, resource.ModifyPlanRequest{
		Config: buildConfig(t, deferred, types.StringNull()),
		Plan:   buildPlan(t, deferred),
		State:  state,
	}, &unknown)
	if unknown.Diagnostics.HasError() {
		t.Fatalf("an unknown volume type must plan cleanly, got %v", unknown.Diagnostics)
	}
	if diags := unknown.Plan.Get(ctx, &planned); diags.HasError() {
		t.Fatalf("get plan with an unknown volume type: %v", diags)
	}
	if !planned.Zone.IsUnknown() || !planned.ZoneID.IsUnknown() || !planned.Encrypted.IsUnknown() {
		t.Fatalf("an unknown volume type must withdraw the values a retype moves: %#v", planned)
	}

	// A configured zone is the practitioner's own value, and a retype cannot move
	// the volume out of it. Withdrawing it would fail the plan with "planned
	// value does not match config value".
	pinned := resource.ModifyPlanResponse{Plan: buildPlan(t, retyped)}
	r.ModifyPlan(ctx, resource.ModifyPlanRequest{
		Config: buildConfig(t, retyped, types.StringValue("zone-a")),
		Plan:   buildPlan(t, retyped),
		State:  state,
	}, &pinned)
	if pinned.Diagnostics.HasError() {
		t.Fatalf("a retype with a configured zone must plan cleanly, got %v", pinned.Diagnostics)
	}
	if diags := pinned.Plan.Get(ctx, &planned); diags.HasError() {
		t.Fatalf("get plan after a retype with a configured zone: %v", diags)
	}
	if planned.Zone.ValueString() != "zone-a" || !planned.ZoneID.IsUnknown() {
		t.Fatalf("a retype must keep a configured zone and still withdraw zone_id: %#v", planned)
	}

	// An update that does not retype keeps the frozen values.
	renamed := current
	renamed.Name = types.StringValue("data-after")
	quiet := resource.ModifyPlanResponse{Plan: buildPlan(t, renamed)}
	r.ModifyPlan(ctx, resource.ModifyPlanRequest{
		Config: buildConfig(t, renamed, types.StringNull()),
		Plan:   buildPlan(t, renamed),
		State:  state,
	}, &quiet)
	if quiet.Diagnostics.HasError() {
		t.Fatalf("a rename must plan cleanly, got %v", quiet.Diagnostics)
	}
	if diags := quiet.Plan.Get(ctx, &planned); diags.HasError() {
		t.Fatalf("get plan after rename: %v", diags)
	}
	if planned.Zone.IsUnknown() || planned.ZoneID.IsUnknown() || planned.Encrypted.IsUnknown() {
		t.Fatalf("an update without a retype must keep the frozen values known: %#v", planned)
	}
}

func TestPopulateVolumeResourceStateAndImportSource(t *testing.T) {
	t.Parallel()
	description := "database"
	encrypted := true
	iops := 3000
	volume := sampleVolumeDetail()
	volume.Description = &description
	volume.Encrypted = &encrypted
	volume.Iops = &iops
	volume.CreateFrom.Image = &blockstoragesdk.NestedImageSchema{Id: core.UUID{3}, Name: "Ubuntu"}
	state := VolumeResourceModel{
		VolumeModel: VolumeModel{Name: types.StringValue("  data  "), Zone: types.StringValue("zone-a")},
		CreateFrom:  types.ObjectNull(volumeCreateFromAttributeTypes()),
	}
	if diags := populateVolumeResourceState(context.Background(), volume, &state); diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if state.Name.ValueString() != "  data  " || state.Zone.ValueString() != "zone-a" || state.VolumeType.ValueString() != "Premium" || state.IOPS.ValueInt64() != 3000 {
		t.Fatalf("unexpected populated state: %#v", state)
	}
	source := decodeVolumeCreateFrom(t, state.CreateFrom)
	if source.SourceType.ValueString() != volumeSourceTypeImage || source.Image.ValueString() != "Ubuntu" || source.VolumeType.ValueString() != "Premium" {
		t.Fatalf("unexpected imported source: %#v", source)
	}
}

// The backend represents a cleared description as a present empty string.
func TestPopulateVolumeResourceStateMapsClearedDescription(t *testing.T) {
	t.Parallel()
	volume := sampleVolumeDetail()
	volume.Description = new("")
	state := VolumeResourceModel{
		VolumeModel: VolumeModel{Name: types.StringValue("data"), Description: types.StringValue("")},
		CreateFrom:  types.ObjectNull(volumeCreateFromAttributeTypes()),
	}
	if diags := populateVolumeResourceState(context.Background(), volume, &state); diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if state.Description.IsNull() || state.Description.ValueString() != "" {
		t.Fatalf("a cleared description must map to an empty string, got %#v", state.Description)
	}

	// A nil SDK value stays null instead of borrowing the configured empty value.
	volume.Description = nil
	missing := VolumeResourceModel{
		VolumeModel: VolumeModel{Name: types.StringValue("data"), Description: types.StringValue("")},
		CreateFrom:  types.ObjectNull(volumeCreateFromAttributeTypes()),
	}
	if diags := populateVolumeResourceState(context.Background(), volume, &missing); diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !missing.Description.IsNull() {
		t.Fatalf("a nil SDK description must map to null, got %#v", missing.Description)
	}
}

func TestPopulateVolumeResourceStateRefreshesCreateFrom(t *testing.T) {
	t.Parallel()
	volume := sampleVolumeDetail()
	volume.CreateFrom.VolumeType = &blockstoragesdk.NestedVolumeTypeSchema{Id: core.UUID{7}, Name: "Standard"}
	state := VolumeResourceModel{
		VolumeModel: VolumeModel{Name: types.StringValue("data")},
		CreateFrom:  volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringValue("Premium")}),
	}
	if diags := populateVolumeResourceState(context.Background(), volume, &state); diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	source := decodeVolumeCreateFrom(t, state.CreateFrom)
	if source.VolumeType.ValueString() != "Standard" || state.VolumeType.ValueString() != "Standard" {
		t.Fatalf("out-of-band retype must reach both views of the volume type: nested=%q top-level=%q",
			source.VolumeType.ValueString(), state.VolumeType.ValueString())
	}
}

func TestPopulateVolumeResourceStatePreservesConfiguredSourceRepresentation(t *testing.T) {
	t.Parallel()
	snapshotID := core.UUID{5}
	volume := sampleVolumeDetail()
	volume.CreateFrom.VolumeType = nil
	volume.CreateFrom.Snapshot = &blockstoragesdk.NestedVolumeSnapshotSchema{Id: snapshotID, Name: "nightly"}
	state := VolumeResourceModel{
		VolumeModel: VolumeModel{Name: types.StringValue("data")},
		CreateFrom: volumeCreateFromValue(t, "  snapshot  ", map[string]attr.Value{
			"snapshot_id": types.StringValue(strings.ToUpper(snapshotID.String())),
		}),
	}
	if diags := populateVolumeResourceState(context.Background(), volume, &state); diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	source := decodeVolumeCreateFrom(t, state.CreateFrom)
	if source.SourceType.ValueString() != "  snapshot  " || source.SnapshotID.ValueString() != strings.ToUpper(snapshotID.String()) {
		t.Fatalf("semantically equal configured values were not preserved: %#v", source)
	}
	if !source.VolumeType.IsNull() {
		t.Fatalf("a snapshot source must not carry create_from.volume_type, got %q", source.VolumeType.ValueString())
	}
}

func TestPopulateVolumeResourceStateMultipleOrigins(t *testing.T) {
	t.Parallel()
	snapshotID := core.UUID{5}
	volume := sampleVolumeDetail()
	volume.CreateFrom.Image = &blockstoragesdk.NestedImageSchema{Id: core.UUID{3}, Name: "Ubuntu"}
	volume.CreateFrom.Snapshot = &blockstoragesdk.NestedVolumeSnapshotSchema{Id: snapshotID, Name: "nightly"}

	imported := VolumeResourceModel{CreateFrom: types.ObjectNull(volumeCreateFromAttributeTypes())}
	diags := populateVolumeResourceState(context.Background(), volume, &imported)
	if !diags.HasError() || diags[0].Summary() != "Ambiguous volume source" {
		t.Fatalf("expected an ambiguous-source diagnostic instead of a silent guess, got %v", diags)
	}

	configured := VolumeResourceModel{
		CreateFrom: volumeCreateFromValue(t, volumeSourceTypeSnapshot, map[string]attr.Value{
			"snapshot_id": types.StringValue(snapshotID.String()),
		}),
	}
	if diags := populateVolumeResourceState(context.Background(), volume, &configured); diags.HasError() {
		t.Fatalf("configured source_type must disambiguate the response: %v", diags)
	}
	source := decodeVolumeCreateFrom(t, configured.CreateFrom)
	if source.SourceType.ValueString() != volumeSourceTypeSnapshot || source.SnapshotID.ValueString() != snapshotID.String() || !source.Image.IsNull() {
		t.Fatalf("unexpected disambiguated source: %#v", source)
	}
}

// NestedVolumeOriginSchema declares every origin optional and carries no
// discriminator. A response that reports no origin at all is therefore not
// evidence that the volume was created empty: a deleted snapshot simply stops
// being reported. Every source field requires replacement, so recording
// "empty" would plan the destruction of a healthy volume whose replacement
// could not even be created.
func TestPopulateVolumeResourceStateKeepsAKnownSourceWhenNoneIsReported(t *testing.T) {
	t.Parallel()
	snapshotID := core.UUID{5}
	volume := sampleVolumeDetail()
	volume.CreateFrom = blockstoragesdk.NestedVolumeOriginSchema{}

	state := VolumeResourceModel{
		VolumeModel: VolumeModel{Name: types.StringValue("data")},
		CreateFrom: volumeCreateFromValue(t, volumeSourceTypeSnapshot, map[string]attr.Value{
			"snapshot_id": types.StringValue(snapshotID.String()),
		}),
	}
	if diags := populateVolumeResourceState(context.Background(), volume, &state); diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	source := decodeVolumeCreateFrom(t, state.CreateFrom)
	if source.SourceType.ValueString() != volumeSourceTypeSnapshot || source.SnapshotID.ValueString() != snapshotID.String() {
		t.Fatalf("an unreported origin must not erase the known source: %#v", source)
	}

	// A volume with nothing recorded on either side, such as a first import, is
	// the one case where "empty" is the honest answer.
	imported := VolumeResourceModel{CreateFrom: types.ObjectNull(volumeCreateFromAttributeTypes())}
	if diags := populateVolumeResourceState(context.Background(), volume, &imported); diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if got := decodeVolumeCreateFrom(t, imported.CreateFrom); got.SourceType.ValueString() != volumeSourceTypeEmpty {
		t.Fatalf("an import with no recorded origin is an empty volume, got %#v", got)
	}
}

// core.ParseUUID accepts the braced and URN spellings as well as the plain one,
// so two IDs can name the same volume without matching as text. Comparing them
// as text would overwrite the configured spelling and fail the apply.
func TestPopulateVolumeResourceStatePreservesAlternateUUIDSpellings(t *testing.T) {
	t.Parallel()
	snapshotID := core.UUID{5}
	volume := sampleVolumeDetail()
	volume.CreateFrom.VolumeType = nil
	volume.CreateFrom.Snapshot = &blockstoragesdk.NestedVolumeSnapshotSchema{Id: snapshotID, Name: "nightly"}

	braced := "{" + snapshotID.String() + "}"
	state := VolumeResourceModel{
		VolumeModel: VolumeModel{Name: types.StringValue("data")},
		CreateFrom: volumeCreateFromValue(t, volumeSourceTypeSnapshot, map[string]attr.Value{
			"snapshot_id": types.StringValue(braced),
		}),
	}
	if diags := populateVolumeResourceState(context.Background(), volume, &state); diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if got := decodeVolumeCreateFrom(t, state.CreateFrom); got.SnapshotID.ValueString() != braced {
		t.Fatalf("a braced UUID names the same snapshot and must be preserved, got %q", got.SnapshotID.ValueString())
	}

	// An ID that parses but names something else is still drift.
	other := VolumeResourceModel{
		VolumeModel: VolumeModel{Name: types.StringValue("data")},
		CreateFrom: volumeCreateFromValue(t, volumeSourceTypeSnapshot, map[string]attr.Value{
			"snapshot_id": types.StringValue("{" + (core.UUID{6}).String() + "}"),
		}),
	}
	if diags := populateVolumeResourceState(context.Background(), volume, &other); diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if got := decodeVolumeCreateFrom(t, other.CreateFrom); got.SnapshotID.ValueString() != snapshotID.String() {
		t.Fatalf("a different snapshot must be refreshed into state, got %q", got.SnapshotID.ValueString())
	}
}

func TestVolumeWaitersHandleReadyFailureAndDeletion(t *testing.T) {
	t.Parallel()
	responses := []struct {
		status int
		body   string
	}{
		{status: http.StatusOK, body: volumeResponseJSON(blockstoragesdk.VolumeStatusCreating)},
		{status: http.StatusOK, body: volumeResponseJSON(blockstoragesdk.VolumeStatusAvailable)},
	}
	client := volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
		response := responses[0]
		responses = responses[1:]
		return testHTTPResponse(req, response.status, response.body), nil
	})
	r := &VolumeResource{client: client, projectID: core.UUID{9}, pollInterval: time.Millisecond, timeout: time.Second}
	if err := r.waitUntilReady(context.Background(), volumeTestID); err != nil {
		t.Fatalf("unexpected ready waiter error: %v", err)
	}

	r.client = volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
		return testHTTPResponse(req, http.StatusOK, volumeResponseJSON(blockstoragesdk.VolumeStatusExtendFailed)), nil
	})
	if err := r.waitUntilReady(context.Background(), volumeTestID); err == nil || !strings.Contains(err.Error(), "terminal status") {
		t.Fatalf("expected terminal failure, got %v", err)
	}

	r.client = volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
		return testHTTPResponse(req, http.StatusNotFound, `{"detail":"not found"}`), nil
	})
	if err := r.waitUntilDeleted(context.Background(), volumeTestID); err != nil {
		t.Fatalf("unexpected deletion waiter error: %v", err)
	}
}

func TestVolumeDeleteWaiterStopsWhenDeletionSettlesBack(t *testing.T) {
	t.Parallel()
	statuses := []blockstoragesdk.VolumeStatus{
		blockstoragesdk.VolumeStatusAvailable, // not yet transitioned; must not be treated as failure
		blockstoragesdk.VolumeStatusDeleting,
		blockstoragesdk.VolumeStatusAvailable,
	}
	polls := 0
	client := volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
		status := statuses[min(polls, len(statuses)-1)]
		polls++
		return testHTTPResponse(req, http.StatusOK, volumeResponseJSON(status)), nil
	})
	r := &VolumeResource{client: client, projectID: volumeTestProjectID, pollInterval: time.Millisecond, timeout: 5 * time.Second}

	err := r.waitUntilDeleted(context.Background(), volumeTestID)
	if err == nil || !strings.Contains(err.Error(), "stopped deleting") {
		t.Fatalf("expected a failed-deletion error instead of polling to the timeout, got %v", err)
	}
	if polls != 3 {
		t.Fatalf("expected the waiter to stop on the third poll, got %d", polls)
	}
}

// The extend action is only accepted, never applied synchronously, so the volume
// keeps reporting the status and size it already had for a poll or two. A waiter
// that stops at a ready status would hand back the old size and fail the apply
// with an inconsistent result.
func TestVolumeResizeWaiterWaitsForTheNewSize(t *testing.T) {
	t.Parallel()
	sizes := []int{30, 30, 40}
	polls := 0
	client := volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
		volume := sampleVolumeDetail()
		volume.Size = sizes[min(polls, len(sizes)-1)]
		polls++
		return testHTTPResponse(req, http.StatusOK, volumeDetailJSON(volume)), nil
	})
	r := &VolumeResource{client: client, projectID: volumeTestProjectID, pollInterval: time.Millisecond, timeout: 5 * time.Second}
	if err := r.waitUntilResized(context.Background(), volumeTestID, 40); err != nil {
		t.Fatalf("unexpected resize waiter error: %v", err)
	}
	if polls != 3 {
		t.Fatalf("expected the waiter to poll until the new size appeared, got %d polls", polls)
	}

	r.client = volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
		volume := sampleVolumeDetail()
		volume.Size = 30
		volume.Status = blockstoragesdk.VolumeStatusExtendFailed
		return testHTTPResponse(req, http.StatusOK, volumeDetailJSON(volume)), nil
	})
	err := r.waitUntilResized(context.Background(), volumeTestID, 40)
	if err == nil || !strings.Contains(err.Error(), "terminal status") {
		t.Fatalf("expected a failed resize to stop the waiter, got %v", err)
	}
}

// A patch is applied asynchronously too, so the volume can answer the next read
// with the values it had before. Storing those would fail the apply with an
// inconsistent result, and refreshing them back later would look like drift.
func TestVolumePatchWaiterWaitsForTheRequestedFields(t *testing.T) {
	t.Parallel()
	name := "data-after"
	iops := 3000
	body := blockstoragesdk.VolumePartialUpdateSchema{Name: &name, Iops: &iops}

	polls := 0
	client := volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
		volume := sampleVolumeDetail()
		if polls > 0 {
			volume.Name = name
			volume.Iops = &iops
		}
		polls++
		return testHTTPResponse(req, http.StatusOK, volumeDetailJSON(volume)), nil
	})
	r := &VolumeResource{client: client, projectID: volumeTestProjectID, pollInterval: time.Millisecond, timeout: 5 * time.Second}
	if err := r.waitUntilPatched(context.Background(), volumeTestID, body); err != nil {
		t.Fatalf("unexpected patch waiter error: %v", err)
	}
	if polls != 2 {
		t.Fatalf("expected the waiter to poll past the stale response, got %d polls", polls)
	}

	// A field the backend never applies must be named, not hidden behind a bare
	// timeout: the practitioner needs to know which value never took.
	r.timeout = 10 * time.Millisecond
	r.client = volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
		return testHTTPResponse(req, http.StatusOK, volumeDetailJSON(sampleVolumeDetail())), nil
	})
	err := r.waitUntilPatched(context.Background(), volumeTestID, body)
	if err == nil || !strings.Contains(err.Error(), "name") || !strings.Contains(err.Error(), "iops") {
		t.Fatalf("expected the outstanding fields to be reported, got %v", err)
	}
}

// The backend retypes in place, so a changed volume type is an update. Sending
// the retype but storing the type before it lands would fail the apply.
func TestVolumeResourceRetypesInPlace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	volumeSchema := volumeResourceSchema(t)
	current := sampleVolumeDetail()
	current.CreateFrom.VolumeType = &blockstoragesdk.NestedVolumeTypeSchema{Id: core.UUID{7}, Name: "Standard"}

	requests := 0
	client := volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		switch {
		case requests == 1 && req.Method == http.MethodGet && req.URL.Path == "/v2/block-storage/volume-types/":
			if req.URL.Query().Get("name") != "Premium" {
				t.Errorf("expected the retype target to be resolved by name, got %q", req.URL.RawQuery)
			}
			if req.URL.Query().Has("zone_id") {
				t.Errorf("an omitted zone must not scope retype, got %q", req.URL.RawQuery)
			}
			return testHTTPResponse(req, http.StatusOK, volumeTypesResponseJSON()), nil
		case requests == 2 && req.Method == http.MethodPost && req.URL.Path == "/v2/block-storage/volumes/"+volumeTestID.String()+"/retype/":
			var body blockstoragesdk.VolumeRetypeSchema
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Errorf("decode retype body: %v", err)
			}
			if body.VolumeTypeId != (core.UUID{2}) {
				t.Errorf("expected the resolved volume type ID, got %#v", body)
			}
			return testHTTPResponse(req, http.StatusAccepted, ""), nil
		case requests == 3:
			// Retype is accepted asynchronously: the volume still reports the old type.
			return testHTTPResponse(req, http.StatusOK, volumeDetailJSON(current)), nil
		case requests == 4, requests == 5:
			return testHTTPResponse(req, http.StatusOK, volumeDetailJSON(sampleVolumeDetail())), nil
		default:
			t.Errorf("unexpected request %d: %s %s", requests, req.Method, req.URL.Path)
			return testHTTPResponse(req, http.StatusInternalServerError, `{}`), nil
		}
	})
	r := &VolumeResource{client: client, projectID: volumeTestProjectID, pollInterval: time.Millisecond, timeout: 5 * time.Second}

	model := VolumeResourceModel{
		VolumeModel: VolumeModel{ID: types.StringValue(volumeTestID.String()), Name: types.StringValue("data"), Size: types.Int64Value(100), ZoneID: types.StringValue((core.UUID{8}).String())},
		CreateFrom:  volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringValue("Standard")}),
	}
	state := tfsdk.State{Schema: volumeSchema}
	if diags := state.Set(ctx, &model); diags.HasError() {
		t.Fatalf("set state: %v", diags)
	}
	model.CreateFrom = volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringValue("Premium")})
	plan := tfsdk.Plan{Schema: volumeSchema}
	if diags := plan.Set(ctx, &model); diags.HasError() {
		t.Fatalf("set plan: %v", diags)
	}

	response := resource.UpdateResponse{State: tfsdk.State{Schema: volumeSchema}}
	r.Update(ctx, resource.UpdateRequest{Config: tfsdk.Config{Raw: plan.Raw, Schema: volumeSchema}, Plan: plan, State: state}, &response)
	if response.Diagnostics.HasError() {
		t.Fatalf("retype diagnostics: %v", response.Diagnostics)
	}
	if requests != 5 {
		t.Fatalf("expected the retype to be sent and awaited, got %d requests", requests)
	}
	var retyped VolumeResourceModel
	if diags := response.State.Get(ctx, &retyped); diags.HasError() {
		t.Fatalf("get state after retype: %v", diags)
	}
	source := decodeVolumeCreateFrom(t, retyped.CreateFrom)
	if retyped.VolumeType.ValueString() != "Premium" || source.VolumeType.ValueString() != "Premium" {
		t.Fatalf("both views of the volume type must hold the new one: %#v", retyped)
	}
}

func TestVolumeResourceRetypeScopesConfiguredZone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	volumeSchema := volumeResourceSchema(t)
	zoneID := core.UUID{8}
	requests := 0
	client := volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		switch {
		case requests == 1 && req.Method == http.MethodGet && req.URL.Path == "/v2/block-storage/volume-types/":
			if req.URL.Query().Get("zone_id") != zoneID.String() || req.URL.Query().Get("name") != "Premium" {
				t.Errorf("expected retype lookup in the configured zone, got %q", req.URL.RawQuery)
			}
			return testHTTPResponse(req, http.StatusOK, volumeTypesResponseJSON()), nil
		case requests == 2 && req.Method == http.MethodPost && req.URL.Path == "/v2/block-storage/volumes/"+volumeTestID.String()+"/retype/":
			return testHTTPResponse(req, http.StatusAccepted, ""), nil
		case requests == 3 || requests == 4:
			return testHTTPResponse(req, http.StatusOK, volumeDetailJSON(sampleVolumeDetail())), nil
		default:
			t.Errorf("unexpected request %d: %s %s", requests, req.Method, req.URL.Path)
			return testHTTPResponse(req, http.StatusInternalServerError, `{}`), nil
		}
	})
	r := &VolumeResource{client: client, projectID: volumeTestProjectID, pollInterval: time.Millisecond, timeout: 5 * time.Second}
	model := VolumeResourceModel{
		VolumeModel: VolumeModel{
			ID: types.StringValue(volumeTestID.String()), Name: types.StringValue("data"), Size: types.Int64Value(100),
			Zone: types.StringValue("zone-a"), ZoneID: types.StringValue(zoneID.String()),
		},
		CreateFrom: volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringValue("Standard")}),
	}
	state := tfsdk.State{Schema: volumeSchema}
	if diags := state.Set(ctx, &model); diags.HasError() {
		t.Fatalf("set retype state: %v", diags)
	}
	model.CreateFrom = volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringValue("Premium")})
	plan := tfsdk.Plan{Schema: volumeSchema}
	if diags := plan.Set(ctx, &model); diags.HasError() {
		t.Fatalf("set retype plan: %v", diags)
	}
	response := resource.UpdateResponse{State: tfsdk.State{Schema: volumeSchema}}
	r.Update(ctx, resource.UpdateRequest{Config: tfsdk.Config{Raw: plan.Raw, Schema: volumeSchema}, Plan: plan, State: state}, &response)
	if response.Diagnostics.HasError() || requests != 4 {
		t.Fatalf("expected a completed retype in the configured zone, requests=%d diagnostics=%v", requests, response.Diagnostics)
	}
}

// A retype that the backend rejects must stop the apply rather than leave state
// claiming a type the volume never reached.
func TestVolumeResourceReportsRetypeFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	volumeSchema := volumeResourceSchema(t)
	client := volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/v2/block-storage/volume-types/":
			return testHTTPResponse(req, http.StatusOK, volumeTypesResponseJSON()), nil
		case req.Method == http.MethodPost:
			return testHTTPResponse(req, http.StatusConflict, `{"detail":"volume is attached"}`), nil
		}
		return testHTTPResponse(req, http.StatusOK, volumeResponseJSON(blockstoragesdk.VolumeStatusAvailable)), nil
	})
	r := &VolumeResource{client: client, projectID: volumeTestProjectID, pollInterval: time.Millisecond, timeout: time.Second}

	model := VolumeResourceModel{
		VolumeModel: VolumeModel{ID: types.StringValue(volumeTestID.String()), Name: types.StringValue("data"), Size: types.Int64Value(100)},
		CreateFrom:  volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringValue("Standard")}),
	}
	state := tfsdk.State{Schema: volumeSchema}
	if diags := state.Set(ctx, &model); diags.HasError() {
		t.Fatalf("set state: %v", diags)
	}
	model.CreateFrom = volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringValue("Premium")})
	plan := tfsdk.Plan{Schema: volumeSchema}
	if diags := plan.Set(ctx, &model); diags.HasError() {
		t.Fatalf("set plan: %v", diags)
	}

	response := resource.UpdateResponse{State: tfsdk.State{Schema: volumeSchema}}
	r.Update(ctx, resource.UpdateRequest{Config: tfsdk.Config{Raw: plan.Raw, Schema: volumeSchema}, Plan: plan, State: state}, &response)
	if !response.Diagnostics.HasError() || response.Diagnostics[0].Summary() != "Error retyping volume" {
		t.Fatalf("expected a retype error diagnostic, got %v", response.Diagnostics)
	}
}

func TestVolumeResourceReportsResizeFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	volumeSchema := volumeResourceSchema(t)
	client := volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost {
			return testHTTPResponse(req, http.StatusConflict, `{"detail":"volume is attached"}`), nil
		}
		return testHTTPResponse(req, http.StatusOK, volumeResponseJSON(blockstoragesdk.VolumeStatusAvailable)), nil
	})
	r := &VolumeResource{client: client, projectID: volumeTestProjectID, pollInterval: time.Millisecond, timeout: time.Second}

	model := VolumeResourceModel{
		VolumeModel: VolumeModel{ID: types.StringValue(volumeTestID.String()), Name: types.StringValue("data"), Size: types.Int64Value(100)},
		CreateFrom:  volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringValue("Premium")}),
	}
	state := tfsdk.State{Schema: volumeSchema}
	if diags := state.Set(ctx, &model); diags.HasError() {
		t.Fatalf("set state: %v", diags)
	}
	model.Size = types.Int64Value(200)
	plan := tfsdk.Plan{Schema: volumeSchema}
	if diags := plan.Set(ctx, &model); diags.HasError() {
		t.Fatalf("set plan: %v", diags)
	}

	response := resource.UpdateResponse{State: tfsdk.State{Schema: volumeSchema}}
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, &response)
	if !response.Diagnostics.HasError() || response.Diagnostics[0].Summary() != "Error resizing volume" {
		t.Fatalf("expected a resize error diagnostic, got %v", response.Diagnostics)
	}
}

func TestVolumeResourceCreateWithConfiguredZone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	volumeSchema := volumeResourceSchema(t)
	zoneID := core.UUID{8}
	projectCalls := 0
	projectClient := volumeTestProjectClient(t, func(req *http.Request) (*http.Response, error) {
		projectCalls++
		if req.Method != http.MethodGet || req.URL.Path != "/v2/projects/"+volumeTestProjectID.String()+"/zones/" || req.URL.Query().Get("name") != "zone-a" {
			t.Errorf("unexpected zone lookup: %s %s", req.Method, req.URL)
		}
		body, err := json.Marshal(projectsdk.PagedProjectZoneSchema{
			Count: 1, Results: []projectsdk.ProjectZoneSchema{{Id: zoneID, Name: "zone-a", Region: projectsdk.NestedRegionSchema{Id: core.UUID{7}, Name: "region-a"}}},
		})
		if err != nil {
			return nil, err
		}
		return testHTTPResponse(req, http.StatusOK, string(body)), nil
	})
	blockCalls := 0
	client := volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
		blockCalls++
		switch blockCalls {
		case 1:
			if req.Method != http.MethodGet || req.URL.Path != "/v2/block-storage/volume-types/" || req.URL.Query().Get("name") != "Premium" || req.URL.Query().Get("zone_id") != zoneID.String() {
				t.Errorf("unexpected scoped volume type lookup: %s %s", req.Method, req.URL)
			}
			return testHTTPResponse(req, http.StatusOK, volumeTypesResponseJSON()), nil
		case 2:
			if req.Method != http.MethodPost || req.URL.Path != "/v2/block-storage/volumes/" || req.Header.Get("Project-ID") != volumeTestProjectID.String() {
				t.Errorf("unexpected volume create request: %s %s", req.Method, req.URL)
			}
			var body blockstoragesdk.VolumeCreateSchema
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatalf("decode volume create body: %v", err)
			}
			source, err := body.CreateFrom.AsVolumeCreateEmptySchema()
			if err != nil || source.VolumeTypeId != (core.UUID{2}) || source.Size != 100 {
				t.Errorf("unexpected volume create source: %#v, error=%v", source, err)
			}
			return testHTTPResponse(req, http.StatusCreated, volumeDetailJSON(sampleVolumeDetail())), nil
		case 3, 4:
			if req.Method != http.MethodGet || req.URL.Path != "/v2/block-storage/volumes/"+volumeTestID.String()+"/" {
				t.Errorf("unexpected volume read request: %s %s", req.Method, req.URL)
			}
			return testHTTPResponse(req, http.StatusOK, volumeDetailJSON(sampleVolumeDetail())), nil
		default:
			t.Errorf("unexpected block storage request: %s %s", req.Method, req.URL)
			return testHTTPResponse(req, http.StatusInternalServerError, `{}`), nil
		}
	})
	r := &VolumeResource{client: client, projectClient: projectClient, projectID: volumeTestProjectID, pollInterval: time.Millisecond, timeout: 5 * time.Second}
	model := VolumeResourceModel{
		VolumeModel: VolumeModel{Name: types.StringValue("data"), Size: types.Int64Value(100), Zone: types.StringValue("zone-a")},
		CreateFrom:  volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringValue("Premium")}),
	}
	plan := tfsdk.Plan{Schema: volumeSchema}
	if diags := plan.Set(ctx, &model); diags.HasError() {
		t.Fatalf("set create plan: %v", diags)
	}
	response := resource.CreateResponse{State: tfsdk.State{Schema: volumeSchema}}
	r.Create(ctx, resource.CreateRequest{Config: tfsdk.Config{Raw: plan.Raw, Schema: volumeSchema}, Plan: plan}, &response)
	if response.Diagnostics.HasError() {
		t.Fatalf("create diagnostics: %v", response.Diagnostics)
	}
	var state VolumeResourceModel
	if diags := response.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("get created state: %v", diags)
	}
	if state.ID.ValueString() != volumeTestID.String() || state.Zone.ValueString() != "zone-a" || state.ZoneID.ValueString() != zoneID.String() {
		t.Fatalf("unexpected created state: %#v", state)
	}
	if projectCalls != 1 || blockCalls != 4 {
		t.Fatalf("unexpected request counts: project=%d block=%d", projectCalls, blockCalls)
	}
}

func TestVolumeResourceCreateReportsZoneLookupFailures(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		count int
	}{
		{name: "missing zone"},
		{name: "ambiguous zone", count: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			volumeSchema := volumeResourceSchema(t)
			projectClient := volumeTestProjectClient(t, func(req *http.Request) (*http.Response, error) {
				zones := make([]projectsdk.ProjectZoneSchema, tt.count)
				for i := range zones {
					zones[i] = projectsdk.ProjectZoneSchema{Id: core.UUID{byte(i + 8)}, Name: "zone-a", Region: projectsdk.NestedRegionSchema{Id: core.UUID{7}, Name: "region-a"}}
				}
				body, err := json.Marshal(projectsdk.PagedProjectZoneSchema{Count: tt.count, Results: zones})
				if err != nil {
					return nil, err
				}
				return testHTTPResponse(req, http.StatusOK, string(body)), nil
			})
			client := volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
				t.Errorf("block storage must not be called after a zone lookup error: %s", req.URL)
				return testHTTPResponse(req, http.StatusInternalServerError, `{}`), nil
			})
			r := &VolumeResource{client: client, projectClient: projectClient, projectID: volumeTestProjectID}
			model := VolumeResourceModel{
				VolumeModel: VolumeModel{Name: types.StringValue("data"), Size: types.Int64Value(100), Zone: types.StringValue("zone-a")},
				CreateFrom:  volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringValue("Premium")}),
			}
			plan := tfsdk.Plan{Schema: volumeSchema}
			if diags := plan.Set(ctx, &model); diags.HasError() {
				t.Fatalf("set create plan: %v", diags)
			}
			response := resource.CreateResponse{State: tfsdk.State{Schema: volumeSchema}}
			r.Create(ctx, resource.CreateRequest{Config: tfsdk.Config{Raw: plan.Raw, Schema: volumeSchema}, Plan: plan}, &response)
			if !response.Diagnostics.HasError() || response.Diagnostics[0].Summary() != "Unable to resolve zone" {
				t.Fatalf("expected zone lookup error, got %v", response.Diagnostics)
			}
		})
	}
}

func TestVolumeResourceLifecycleUsesPublicSDK(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	volumeSchema := volumeResourceSchema(t)
	created := sampleVolumeDetail()
	created.Size = 30
	updatedDescription := "after"
	// The patch settles the metadata; the size only follows once the separate
	// extend action has run, so the two are distinct backend states.
	patched := sampleVolumeDetail()
	patched.Size = 30
	patched.Name = "data-after"
	patched.Description = &updatedDescription
	updated := sampleVolumeDetail()
	updated.Size = 40
	updated.Name = patched.Name
	updated.Description = &updatedDescription

	requests := 0
	client := volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		// The volume-types catalogue is not project-scoped; every volume call is.
		if got := req.Header.Get("Project-ID"); requests > 1 && got != volumeTestProjectID.String() {
			t.Errorf("expected Project-ID %q on request %d, got %q", volumeTestProjectID, requests, got)
		}
		switch {
		case requests == 1 && req.Method == http.MethodGet && req.URL.Path == "/v2/block-storage/volume-types/":
			if req.URL.Query().Get("name") != "Premium" {
				t.Errorf("expected the volume type name to be pushed to the backend, got %q", req.URL.RawQuery)
			}
			if req.URL.Query().Has("zone_id") {
				t.Errorf("volume types must not be scoped by an unset zone: %q", req.URL.RawQuery)
			}
			return testHTTPResponse(req, http.StatusOK, volumeTypesResponseJSON()), nil
		case requests == 2 && req.Method == http.MethodPost && req.URL.Path == "/v2/block-storage/volumes/":
			var body blockstoragesdk.VolumeCreateSchema
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			source, err := body.CreateFrom.AsVolumeCreateEmptySchema()
			if err != nil || body.Name != "data" || source.Size != 30 || source.VolumeTypeId != (core.UUID{2}) {
				t.Errorf("unexpected create body: name=%q source=%#v err=%v", body.Name, source, err)
			}
			creating := sampleVolumeDetail()
			creating.Status = blockstoragesdk.VolumeStatusCreating
			return testHTTPResponse(req, http.StatusCreated, volumeDetailJSON(creating)), nil
		case requests == 3, requests == 4:
			// The create waiter polls until the volume settles, then Create re-reads it.
			return testHTTPResponse(req, http.StatusOK, volumeDetailJSON(created)), nil
		case requests == 5:
			return testHTTPResponse(req, http.StatusOK, volumeDetailJSON(created)), nil
		case requests == 6 && req.Method == http.MethodPatch:
			var body map[string]any
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Errorf("decode update body: %v", err)
			}
			// A size in the patch body is accepted and ignored by the backend, so
			// sending one would silently skip the resize.
			if len(body) != 2 || body["name"] != "data-after" || body["description"] != "after" {
				t.Errorf("unexpected sparse update body: %#v", body)
			}
			return testHTTPResponse(req, http.StatusOK, volumeDetailJSON(patched)), nil
		case requests == 7:
			// The patch waiter sees the metadata settled at the old size.
			return testHTTPResponse(req, http.StatusOK, volumeDetailJSON(patched)), nil
		case requests == 8 && req.Method == http.MethodPost && req.URL.Path == "/v2/block-storage/volumes/"+volumeTestID.String()+"/extend/":
			var body blockstoragesdk.VolumeExtendSchema
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Errorf("decode extend body: %v", err)
			}
			if body.Size != 40 {
				t.Errorf("expected the resize to request 40 GiB, got %#v", body)
			}
			return testHTTPResponse(req, http.StatusAccepted, ""), nil
		case requests == 9:
			// Extend is accepted asynchronously: the volume is still ready at its
			// old size, and stopping here would store 30 GiB against a plan of 40.
			return testHTTPResponse(req, http.StatusOK, volumeDetailJSON(patched)), nil
		case requests == 10, requests == 11:
			return testHTTPResponse(req, http.StatusOK, volumeDetailJSON(updated)), nil
		case requests == 12 && req.Method == http.MethodDelete:
			return testHTTPResponse(req, http.StatusAccepted, ""), nil
		case requests == 13:
			return testHTTPResponse(req, http.StatusNotFound, `{"detail":"not found"}`), nil
		default:
			t.Errorf("unexpected request %d: %s %s", requests, req.Method, req.URL.Path)
			return testHTTPResponse(req, http.StatusInternalServerError, `{}`), nil
		}
	})
	r := &VolumeResource{
		client: client, projectID: volumeTestProjectID,
		pollInterval: time.Millisecond, timeout: 5 * time.Second,
	}

	plan := tfsdk.Plan{Schema: volumeSchema}
	if diags := plan.Set(ctx, &VolumeResourceModel{
		VolumeModel: VolumeModel{Name: types.StringValue("data"), Size: types.Int64Value(30)},
		CreateFrom: volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{
			"volume_type": types.StringValue("Premium"),
		}),
	}); diags.HasError() {
		t.Fatalf("set create plan: %v", diags)
	}
	createResponse := resource.CreateResponse{State: tfsdk.State{Schema: volumeSchema}}
	r.Create(ctx, resource.CreateRequest{Config: tfsdk.Config{Raw: plan.Raw, Schema: volumeSchema}, Plan: plan}, &createResponse)
	if createResponse.Diagnostics.HasError() {
		t.Fatalf("create diagnostics: %v", createResponse.Diagnostics)
	}

	readResponse := resource.ReadResponse{State: createResponse.State}
	r.Read(ctx, resource.ReadRequest{State: createResponse.State}, &readResponse)
	if readResponse.Diagnostics.HasError() {
		t.Fatalf("read diagnostics: %v", readResponse.Diagnostics)
	}

	var state VolumeResourceModel
	if diags := readResponse.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("get state after read: %v", diags)
	}
	if state.ID.ValueString() != volumeTestID.String() || state.Size.ValueInt64() != 30 || state.VolumeType.ValueString() != "Premium" {
		t.Fatalf("unexpected state after read: %#v", state)
	}

	updatePlanModel := state
	updatePlanModel.Name = types.StringValue("data-after")
	updatePlanModel.Description = types.StringValue("after")
	updatePlanModel.Size = types.Int64Value(40)
	updatePlan := tfsdk.Plan{Schema: volumeSchema}
	if diags := updatePlan.Set(ctx, &updatePlanModel); diags.HasError() {
		t.Fatalf("set update plan: %v", diags)
	}
	updateResponse := resource.UpdateResponse{State: tfsdk.State{Schema: volumeSchema}}
	r.Update(ctx, resource.UpdateRequest{Plan: updatePlan, State: readResponse.State}, &updateResponse)
	if updateResponse.Diagnostics.HasError() {
		t.Fatalf("update diagnostics: %v", updateResponse.Diagnostics)
	}

	// Storing anything other than the planned size fails the apply with an
	// inconsistent result, which is what a resize that never happened looks like.
	var updatedState VolumeResourceModel
	if diags := updateResponse.State.Get(ctx, &updatedState); diags.HasError() {
		t.Fatalf("get state after update: %v", diags)
	}
	if updatedState.Size.ValueInt64() != 40 || updatedState.Name.ValueString() != "data-after" {
		t.Fatalf("unexpected state after update: %#v", updatedState)
	}

	deleteResponse := resource.DeleteResponse{State: updateResponse.State}
	r.Delete(ctx, resource.DeleteRequest{State: updateResponse.State}, &deleteResponse)
	if deleteResponse.Diagnostics.HasError() {
		t.Fatalf("delete diagnostics: %v", deleteResponse.Diagnostics)
	}
	if requests != 13 {
		t.Fatalf("expected thirteen SDK requests, got %d", requests)
	}
}

func TestVolumeResourceCreatePreservesIDWhenWaiterFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	volumeSchema := volumeResourceSchema(t)
	requests := 0
	client := volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		switch {
		case req.URL.Path == "/v2/block-storage/volume-types/":
			return testHTTPResponse(req, http.StatusOK, volumeTypesResponseJSON()), nil
		case req.Method == http.MethodPost:
			return testHTTPResponse(req, http.StatusCreated, volumeResponseJSON(blockstoragesdk.VolumeStatusCreating)), nil
		default:
			return testHTTPResponse(req, http.StatusOK, volumeResponseJSON(blockstoragesdk.VolumeStatusFailed)), nil
		}
	})
	r := &VolumeResource{
		client: client, projectID: volumeTestProjectID,
		pollInterval: time.Millisecond, timeout: 5 * time.Second,
	}

	plan := tfsdk.Plan{Schema: volumeSchema}
	if diags := plan.Set(ctx, &VolumeResourceModel{
		VolumeModel: VolumeModel{Name: types.StringValue("data"), Size: types.Int64Value(30)},
		CreateFrom: volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{
			"volume_type": types.StringValue("Premium"),
		}),
	}); diags.HasError() {
		t.Fatalf("set create plan: %v", diags)
	}
	response := resource.CreateResponse{State: tfsdk.State{Schema: volumeSchema}}
	r.Create(ctx, resource.CreateRequest{Config: tfsdk.Config{Raw: plan.Raw, Schema: volumeSchema}, Plan: plan}, &response)
	if !response.Diagnostics.HasError() {
		t.Fatal("expected a create waiter diagnostic")
	}

	var state VolumeResourceModel
	if diags := response.State.Get(ctx, &state); diags.HasError() {
		t.Fatalf("get state after failed create: %v", diags)
	}
	if state.ID.ValueString() != volumeTestID.String() {
		t.Fatalf("the created volume ID must stay in state to avoid an untracked duplicate, got %q", state.ID.ValueString())
	}
	if state.Status.IsUnknown() || state.Zone.IsUnknown() || state.CreatedAt.IsUnknown() {
		t.Fatalf("state must not keep unknown values after a failed create: %#v", state)
	}
}

func TestVolumeResourceReadRemovesMissingVolumeAndDeleteAcceptsNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	volumeSchema := volumeResourceSchema(t)
	client := volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
		return testHTTPResponse(req, http.StatusNotFound, `{"detail":"not found"}`), nil
	})
	r := &VolumeResource{client: client, projectID: volumeTestProjectID, pollInterval: time.Millisecond, timeout: time.Second}

	state := tfsdk.State{Schema: volumeSchema}
	if diags := state.Set(ctx, &VolumeResourceModel{
		VolumeModel: VolumeModel{ID: types.StringValue(volumeTestID.String()), Name: types.StringValue("data")},
		CreateFrom:  volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringValue("Premium")}),
	}); diags.HasError() {
		t.Fatalf("set state: %v", diags)
	}

	readResponse := resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, &readResponse)
	if readResponse.Diagnostics.HasError() {
		t.Fatalf("read diagnostics: %v", readResponse.Diagnostics)
	}
	if !readResponse.State.Raw.IsNull() {
		t.Fatal("expected the resource to be removed from state when the backend no longer has it")
	}

	deleteResponse := resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, &deleteResponse)
	if deleteResponse.Diagnostics.HasError() {
		t.Fatalf("delete must treat not-found as success, got %v", deleteResponse.Diagnostics)
	}
}

func TestVolumeResourceReportsAPIErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	volumeSchema := volumeResourceSchema(t)
	client := volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/v2/block-storage/volume-types/" {
			return testHTTPResponse(req, http.StatusOK, volumeTypesResponseJSON()), nil
		}
		return testHTTPResponse(req, http.StatusInternalServerError, `{"detail":"boom"}`), nil
	})
	r := &VolumeResource{client: client, projectID: volumeTestProjectID, pollInterval: time.Millisecond, timeout: time.Second}

	plan := tfsdk.Plan{Schema: volumeSchema}
	if diags := plan.Set(ctx, &VolumeResourceModel{
		VolumeModel: VolumeModel{Name: types.StringValue("data"), Size: types.Int64Value(30)},
		CreateFrom:  volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringValue("Premium")}),
	}); diags.HasError() {
		t.Fatalf("set create plan: %v", diags)
	}
	createResponse := resource.CreateResponse{State: tfsdk.State{Schema: volumeSchema}}
	r.Create(ctx, resource.CreateRequest{Config: tfsdk.Config{Raw: plan.Raw, Schema: volumeSchema}, Plan: plan}, &createResponse)
	if !createResponse.Diagnostics.HasError() || createResponse.Diagnostics[0].Summary() != "Error creating volume" {
		t.Fatalf("expected a create error diagnostic, got %v", createResponse.Diagnostics)
	}

	state := tfsdk.State{Schema: volumeSchema}
	if diags := state.Set(ctx, &VolumeResourceModel{
		VolumeModel: VolumeModel{ID: types.StringValue(volumeTestID.String()), Name: types.StringValue("data"), Size: types.Int64Value(30)},
		CreateFrom:  volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringValue("Premium")}),
	}); diags.HasError() {
		t.Fatalf("set state: %v", diags)
	}
	readResponse := resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, &readResponse)
	if !readResponse.Diagnostics.HasError() || readResponse.Diagnostics[0].Summary() != "Error reading volume" {
		t.Fatalf("expected a read error diagnostic, got %v", readResponse.Diagnostics)
	}
	if readResponse.State.Raw.IsNull() {
		t.Fatal("a transport error must not remove the resource from state")
	}

	deleteResponse := resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, &deleteResponse)
	if !deleteResponse.Diagnostics.HasError() || deleteResponse.Diagnostics[0].Summary() != "Error deleting volume" {
		t.Fatalf("expected a delete error diagnostic, got %v", deleteResponse.Diagnostics)
	}
}

func TestVolumeResourceUpdateWithoutChangesSkipsWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	volumeSchema := volumeResourceSchema(t)
	requests := 0
	client := volumeTestClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		if req.Method != http.MethodGet {
			t.Errorf("an unchanged plan must not write, got %s %s", req.Method, req.URL.Path)
		}
		return testHTTPResponse(req, http.StatusOK, volumeResponseJSON(blockstoragesdk.VolumeStatusAvailable)), nil
	})
	r := &VolumeResource{client: client, projectID: volumeTestProjectID, pollInterval: time.Millisecond, timeout: time.Second}

	model := VolumeResourceModel{
		VolumeModel: VolumeModel{ID: types.StringValue(volumeTestID.String()), Name: types.StringValue("data"), Size: types.Int64Value(100)},
		CreateFrom:  volumeCreateFromValue(t, volumeSourceTypeEmpty, map[string]attr.Value{"volume_type": types.StringValue("Premium")}),
	}
	state := tfsdk.State{Schema: volumeSchema}
	if diags := state.Set(ctx, &model); diags.HasError() {
		t.Fatalf("set state: %v", diags)
	}
	plan := tfsdk.Plan{Schema: volumeSchema}
	if diags := plan.Set(ctx, &model); diags.HasError() {
		t.Fatalf("set plan: %v", diags)
	}

	response := resource.UpdateResponse{State: tfsdk.State{Schema: volumeSchema}}
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, &response)
	if response.Diagnostics.HasError() {
		t.Fatalf("update diagnostics: %v", response.Diagnostics)
	}
	if requests != 1 {
		t.Fatalf("expected a single refresh request, got %d", requests)
	}
}

func TestVolumeImportState(t *testing.T) {
	t.Parallel()
	var schemaResponse resource.SchemaResponse
	(&VolumeResource{}).Schema(context.Background(), resource.SchemaRequest{}, &schemaResponse)
	response := resource.ImportStateResponse{State: tfsdk.State{Schema: schemaResponse.Schema}}
	if diags := response.State.Set(context.Background(), &VolumeResourceModel{CreateFrom: types.ObjectNull(volumeCreateFromAttributeTypes())}); diags.HasError() {
		t.Fatalf("initialize import state: %v", diags)
	}
	(&VolumeResource{}).ImportState(context.Background(), resource.ImportStateRequest{ID: volumeTestID.String()}, &response)
	if response.Diagnostics.HasError() {
		t.Fatalf("import diagnostics: %v", response.Diagnostics)
	}
	var id types.String
	response.Diagnostics.Append(response.State.GetAttribute(context.Background(), path.Root("id"), &id)...)
	if response.Diagnostics.HasError() || id.ValueString() != volumeTestID.String() {
		t.Fatalf("unexpected imported ID %q: %v", id.ValueString(), response.Diagnostics)
	}
	var source types.Object
	response.Diagnostics.Append(response.State.GetAttribute(context.Background(), path.Root("create_from"), &source)...)
	if response.Diagnostics.HasError() || !source.IsNull() {
		t.Fatalf("a bare ID must leave create_from for Read to fill in: %#v, %v", source, response.Diagnostics)
	}
}

// A volume whose backend response reports several origins cannot be imported
// by ID alone, and Read cannot consult configuration to break the tie. The
// source type therefore has to arrive with the import ID.
func TestVolumeImportStateCompositeID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	volumeSchema := volumeResourceSchema(t)
	newResponse := func(t *testing.T) resource.ImportStateResponse {
		t.Helper()
		response := resource.ImportStateResponse{State: tfsdk.State{Schema: volumeSchema}}
		if diags := response.State.Set(ctx, &VolumeResourceModel{CreateFrom: types.ObjectNull(volumeCreateFromAttributeTypes())}); diags.HasError() {
			t.Fatalf("initialize import state: %v", diags)
		}
		return response
	}

	response := newResponse(t)
	(&VolumeResource{}).ImportState(ctx, resource.ImportStateRequest{ID: volumeTestID.String() + ", snapshot "}, &response)
	if response.Diagnostics.HasError() {
		t.Fatalf("composite import diagnostics: %v", response.Diagnostics)
	}
	var imported VolumeResourceModel
	if diags := response.State.Get(ctx, &imported); diags.HasError() {
		t.Fatalf("get imported state: %v", diags)
	}
	if imported.ID.ValueString() != volumeTestID.String() {
		t.Fatalf("unexpected imported ID %q", imported.ID.ValueString())
	}
	source := decodeVolumeCreateFrom(t, imported.CreateFrom)
	if source.SourceType.ValueString() != volumeSourceTypeSnapshot || !source.SnapshotID.IsNull() {
		t.Fatalf("the import ID must seed source_type and nothing else: %#v", source)
	}

	// The seeded source type then lets Read pick the intended origin out of an
	// otherwise ambiguous response.
	volume := sampleVolumeDetail()
	volume.CreateFrom.Image = &blockstoragesdk.NestedImageSchema{Id: core.UUID{3}, Name: "Ubuntu"}
	volume.CreateFrom.Snapshot = &blockstoragesdk.NestedVolumeSnapshotSchema{Id: core.UUID{5}, Name: "nightly"}
	if diags := populateVolumeResourceState(ctx, volume, &imported); diags.HasError() {
		t.Fatalf("a seeded source type must disambiguate the response: %v", diags)
	}
	if got := decodeVolumeCreateFrom(t, imported.CreateFrom); got.SnapshotID.ValueString() != (core.UUID{5}).String() || !got.Image.IsNull() {
		t.Fatalf("unexpected disambiguated source: %#v", got)
	}

	for _, tt := range []struct {
		name string
		id   string
		want string
	}{
		{name: "empty ID", id: "", want: "Invalid volume import ID"},
		{name: "missing volume ID", id: ",snapshot", want: "Invalid volume import ID"},
		{name: "not a UUID", id: "not-a-uuid,snapshot", want: "Invalid UUID"},
		{name: "unsupported source type", id: volumeTestID.String() + ",volume", want: "Invalid volume import source type"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rejected := newResponse(t)
			(&VolumeResource{}).ImportState(ctx, resource.ImportStateRequest{ID: tt.id}, &rejected)
			if !rejected.Diagnostics.HasError() || rejected.Diagnostics[0].Summary() != tt.want {
				t.Fatalf("expected a %q diagnostic, got %v", tt.want, rejected.Diagnostics)
			}
		})
	}
}

func fixedImageResolver(id core.UUID, minSize *int) serverlookup.ImageResolveFunc {
	return func(_ context.Context, filter serverlookup.ImageFilter) (serversdk.ImageSchema, error) {
		return serversdk.ImageSchema{Id: id, Name: strings.TrimSpace(*filter.Name), MinVolumeSize: minSize}, nil
	}
}

func fixedVolumeTypeResolver(id core.UUID) blockstoragelookup.VolumeTypeResolveFunc {
	return func(_ context.Context, request blockstoragelookup.VolumeTypeResolveRequest) (blockstoragesdk.VolumeTypeSchema, error) {
		return blockstoragesdk.VolumeTypeSchema{Id: id, Name: strings.TrimSpace(request.Name), MaxVolumeSize: 500}, nil
	}
}
