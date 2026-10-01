package blockstorage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	blockstoragesdk "github.com/viettelcloud-oss/sdks/go/blockstorage"
	"github.com/viettelcloud-oss/sdks/go/core"
	projectsdk "github.com/viettelcloud-oss/sdks/go/project"
)

var (
	volumeTestID        = core.UUID{1}
	volumeTestProjectID = core.UUID{9}
)

type volumeRoundTripFunc func(*http.Request) (*http.Response, error)

func (f volumeRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func volumeTestClient(t *testing.T, roundTrip volumeRoundTripFunc) *blockstoragesdk.Client {
	t.Helper()
	client, err := blockstoragesdk.NewClient(
		"https://blockstorage.test",
		blockstoragesdk.WithHTTPClient(&http.Client{Transport: roundTrip}),
		blockstoragesdk.WithRetry(core.NoRetry()),
	)
	if err != nil {
		t.Fatalf("create block storage client: %v", err)
	}
	return client
}

func volumeTestProjectClient(t *testing.T, roundTrip volumeRoundTripFunc) *projectsdk.Client {
	t.Helper()
	client, err := projectsdk.NewClient(
		"https://project.test",
		projectsdk.WithHTTPClient(&http.Client{Transport: roundTrip}),
		projectsdk.WithRetry(core.NoRetry()),
	)
	if err != nil {
		t.Fatalf("create project client: %v", err)
	}
	return client
}

func testHTTPResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func sampleVolumeDetail() *blockstoragesdk.VolumeDetailSchema {
	return &blockstoragesdk.VolumeDetailSchema{
		Id: volumeTestID, Name: "data", Size: 100, Status: blockstoragesdk.VolumeStatusAvailable,
		ProjectId: volumeTestProjectID, Zone: blockstoragesdk.NestedZoneSchema{Id: core.UUID{8}, Name: "zone-a"},
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		CreateFrom: blockstoragesdk.NestedVolumeOriginSchema{
			VolumeType: &blockstoragesdk.NestedVolumeTypeSchema{Id: core.UUID{2}, Name: "Premium"},
		},
	}
}

func volumeResponseJSON(status blockstoragesdk.VolumeStatus) string {
	volume := sampleVolumeDetail()
	volume.Status = status
	return volumeDetailJSON(volume)
}

func volumeDetailJSON(volume *blockstoragesdk.VolumeDetailSchema) string {
	body, err := json.Marshal(volume)
	if err != nil {
		panic(fmt.Sprintf("marshal volume response: %v", err))
	}
	return string(body)
}

func volumeTypesResponseJSON() string {
	label := func(value string) *string { return &value }
	page := blockstoragesdk.PagedVolumeTypeSchema{
		Count: 1,
		Results: []blockstoragesdk.VolumeTypeSchema{{
			Id:                core.UUID{2},
			Name:              "Premium",
			MaxVolumeSize:     1000,
			VolumeBackendName: "ceph",
			CreatedAt:         time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt:         time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			DiskType:          blockstoragesdk.LabeledVolumeTypeDiskType{Value: blockstoragesdk.VolumeTypeDiskTypeSsd, Label: label("SSD")},
			EncryptionKeySize: blockstoragesdk.LabeledVolumeTypeEncryptKeySize{Value: blockstoragesdk.VolumeTypeEncryptKeySizeN256},
			Speed:             blockstoragesdk.LabeledVolumeTypeSpeed{Value: blockstoragesdk.VolumeTypeSpeedN10k},
			State:             blockstoragesdk.LabeledVolumeTypeState{Value: blockstoragesdk.VolumeTypeStateUp},
			Status:            blockstoragesdk.LabeledVolumeTypeStatus{Value: blockstoragesdk.VolumeTypeStatusEnabled},
			Zone:              blockstoragesdk.NestedZoneSchema{Id: core.UUID{8}, Name: "zone-a"},
		}},
	}
	body, err := json.Marshal(page)
	if err != nil {
		panic(fmt.Sprintf("marshal volume type response: %v", err))
	}
	return string(body)
}

func volumeResourceSchema(t *testing.T) schema.Schema {
	t.Helper()
	var response resource.SchemaResponse
	(&VolumeResource{}).Schema(context.Background(), resource.SchemaRequest{}, &response)
	if response.Diagnostics.HasError() {
		t.Fatalf("build volume resource schema: %v", response.Diagnostics)
	}
	return response.Schema
}

func decodeVolumeCreateFrom(t *testing.T, source types.Object) VolumeCreateFromModel {
	t.Helper()
	var model VolumeCreateFromModel
	if diags := source.As(context.Background(), &model, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("decode create_from: %v", diags)
	}
	return model
}

func volumeCreateFromValue(t *testing.T, sourceType string, overrides map[string]attr.Value) types.Object {
	t.Helper()
	values := map[string]attr.Value{
		"source_type": types.StringValue(sourceType), "image": types.StringNull(), "custom_image_id": types.StringNull(),
		"snapshot_id": types.StringNull(), "backup_id": types.StringNull(), "volume_type": types.StringNull(),
	}
	maps.Copy(values, overrides)
	result, diags := types.ObjectValue(volumeCreateFromAttributeTypes(), values)
	if diags.HasError() {
		t.Fatalf("create create_from value: %v", diags)
	}
	return result
}
