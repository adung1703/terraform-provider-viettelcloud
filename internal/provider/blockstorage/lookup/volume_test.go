package lookup

import (
	"context"
	"errors"
	"iter"
	"strings"
	"testing"

	blockstoragesdk "github.com/viettelcloud-oss/sdks/go/blockstorage"
	"github.com/viettelcloud-oss/sdks/go/core"
)

type fakeVolumeLister struct {
	values []blockstoragesdk.VolumeSchema
	err    error
	params blockstoragesdk.ListVolumeParams
}

func (f *fakeVolumeLister) ListVolumeIter(
	_ context.Context,
	params blockstoragesdk.ListVolumeParams,
) iter.Seq2[*blockstoragesdk.VolumeSchema, error] {
	f.params = params
	return func(yield func(*blockstoragesdk.VolumeSchema, error) bool) {
		for i := range f.values {
			if !yield(&f.values[i], nil) {
				return
			}
		}
		if f.err != nil {
			yield(nil, f.err)
		}
	}
}

func testVolume(id core.UUID, name string, size int, status blockstoragesdk.VolumeStatus, zone string, bootable bool) blockstoragesdk.VolumeSchema {
	return blockstoragesdk.VolumeSchema{
		Id: id, Name: name, Size: size, Status: status,
		Zone: blockstoragesdk.NestedZoneSchema{Name: zone}, Bootable: bootable,
	}
}

func TestVolumeFinderMatchesAllCriteriaAndPassesBackendFilters(t *testing.T) {
	t.Parallel()
	projectID := core.UUID{9}
	wantID := core.UUID{1}
	name := " data "
	status := blockstoragesdk.VolumeStatusAvailable
	zone := " zone-a "
	size := 100
	bootable := false
	lister := &fakeVolumeLister{values: []blockstoragesdk.VolumeSchema{
		testVolume(wantID, "data", size, status, "zone-a", false),
		testVolume(core.UUID{2}, "data", 50, status, "zone-a", false),
	}}

	got, err := NewVolumeFinder(lister, projectID).Resolve(context.Background(), VolumeFilter{
		ID: &wantID, Name: &name, Status: &status, Zone: &zone, Size: &size, Bootable: &bootable,
	})
	if err != nil || got.Id != wantID {
		t.Fatalf("resolve volume: got=%#v err=%v", got, err)
	}
	if lister.params.ProjectID != projectID || lister.params.Name == nil || *lister.params.Name != "data" ||
		lister.params.Zone == nil || *lister.params.Zone != "zone-a" || lister.params.Size == nil || *lister.params.Size != size ||
		lister.params.Status == nil || len(*lister.params.Status) != 1 || (*lister.params.Status)[0] != status ||
		lister.params.Bootable == nil || *lister.params.Bootable != bootable {
		t.Fatalf("unexpected list params: %#v", lister.params)
	}
}

func TestVolumeFinderCardinalityAndListErrors(t *testing.T) {
	t.Parallel()
	name := "data"
	status := blockstoragesdk.VolumeStatusAvailable
	for testName, values := range map[string][]blockstoragesdk.VolumeSchema{
		"missing": nil,
		"ambiguous": {
			testVolume(core.UUID{1}, name, 10, status, "zone-a", false),
			testVolume(core.UUID{2}, name, 20, status, "zone-b", false),
		},
	} {
		_, err := NewVolumeFinder(&fakeVolumeLister{values: values}, core.UUID{9}).Resolve(
			context.Background(), VolumeFilter{Name: &name},
		)
		if err == nil || !strings.Contains(err.Error(), map[string]string{"missing": "not found", "ambiguous": "ambiguous"}[testName]) {
			t.Fatalf("expected %s error, got %v", testName, err)
		}
	}
	wantErr := errors.New("list failed")
	_, err := NewVolumeFinder(&fakeVolumeLister{err: wantErr}, core.UUID{9}).Find(context.Background(), VolumeFilter{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected list error, got %v", err)
	}
}
