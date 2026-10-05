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

type fakeVolumeTypeLister struct {
	values []blockstoragesdk.VolumeTypeSchema
	err    error
	params blockstoragesdk.ListVolumeTypesParams
}

func (f *fakeVolumeTypeLister) ListVolumeTypesIter(
	_ context.Context,
	params blockstoragesdk.ListVolumeTypesParams,
) iter.Seq2[*blockstoragesdk.VolumeTypeSchema, error] {
	f.params = params
	return func(yield func(*blockstoragesdk.VolumeTypeSchema, error) bool) {
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

func testVolumeType(id core.UUID, name, backend string, zoneID core.UUID, ready bool, maxSize int) blockstoragesdk.VolumeTypeSchema {
	status := blockstoragesdk.VolumeTypeStatusDisabled
	state := blockstoragesdk.VolumeTypeStateDown
	if ready {
		status = blockstoragesdk.VolumeTypeStatusEnabled
		state = blockstoragesdk.VolumeTypeStateUp
	}
	return blockstoragesdk.VolumeTypeSchema{
		Id: id, Name: name, VolumeBackendName: backend, MaxVolumeSize: maxSize,
		Zone:   blockstoragesdk.NestedZoneSchema{Id: zoneID, Name: "zone-a"},
		Status: blockstoragesdk.LabeledVolumeTypeStatus{Value: status},
		State:  blockstoragesdk.LabeledVolumeTypeState{Value: state},
	}
}

func TestVolumeTypeFinderSupportsServerAndVolumeCriteria(t *testing.T) {
	t.Parallel()
	zoneID := core.UUID{8}
	wantID := core.UUID{2}
	name := " Premium "
	backend := " ceph-ssd "
	lister := &fakeVolumeTypeLister{values: []blockstoragesdk.VolumeTypeSchema{
		testVolumeType(core.UUID{1}, "Premium", "ceph-hdd", zoneID, true, 500),
		testVolumeType(wantID, "Premium", "ceph-ssd", zoneID, true, 500),
	}}

	got, err := NewVolumeTypeFinder(lister).Resolve(context.Background(), VolumeTypeFilter{
		Name: &name, BackendName: &backend, ZoneID: &zoneID,
	})
	if err != nil || got.Id != wantID {
		t.Fatalf("resolve volume type: got=%#v err=%v", got, err)
	}
	if lister.params.Name == nil || *lister.params.Name != "Premium" || lister.params.ZoneId == nil || *lister.params.ZoneId != zoneID {
		t.Fatalf("unexpected list params: %#v", lister.params)
	}

	withoutZone := &fakeVolumeTypeLister{values: []blockstoragesdk.VolumeTypeSchema{testVolumeType(wantID, "Premium", "ceph-ssd", zoneID, true, 500)}}
	if _, err := NewVolumeTypeFinder(withoutZone).ResolveForCreate(
		context.Background(), VolumeTypeResolveRequest{Name: name, RequestedSize: 100},
	); err != nil {
		t.Fatalf("resolve volume type without zone: %v", err)
	}
	if withoutZone.params.ZoneId != nil || withoutZone.params.RegionId != nil {
		t.Fatalf("unsupplied scope must remain omitted: %#v", withoutZone.params)
	}
}

func TestVolumeTypeFinderCreateValidation(t *testing.T) {
	t.Parallel()
	zoneID := core.UUID{8}
	name := "Premium"
	tests := []struct {
		name   string
		values []blockstoragesdk.VolumeTypeSchema
		size   int
		want   string
	}{
		{name: "blank name", size: 100, want: "must not be empty"},
		{name: "unavailable", values: []blockstoragesdk.VolumeTypeSchema{testVolumeType(core.UUID{1}, name, "ceph", zoneID, false, 500)}, size: 100, want: "no enabled, up candidate"},
		{name: "ambiguous", values: []blockstoragesdk.VolumeTypeSchema{testVolumeType(core.UUID{1}, name, "a", zoneID, true, 500), testVolumeType(core.UUID{2}, name, "b", zoneID, true, 500)}, size: 100, want: "ambiguous"},
		{name: "zero size", values: []blockstoragesdk.VolumeTypeSchema{testVolumeType(core.UUID{1}, name, "a", zoneID, true, 500)}, want: "greater than zero"},
		{name: "oversize", values: []blockstoragesdk.VolumeTypeSchema{testVolumeType(core.UUID{1}, name, "a", zoneID, true, 50)}, size: 100, want: "maximum size"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestName := name
			if tt.name == "blank name" {
				requestName = "   "
			}
			_, err := NewVolumeTypeFinder(&fakeVolumeTypeLister{values: tt.values}).ResolveForCreate(
				context.Background(), VolumeTypeResolveRequest{Name: requestName, RequestedSize: tt.size},
			)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q error, got %v", tt.want, err)
			}
		})
	}

	wantErr := errors.New("list failed")
	_, err := NewVolumeTypeFinder(&fakeVolumeTypeLister{err: wantErr}).Find(context.Background(), VolumeTypeFilter{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected list error, got %v", err)
	}
}
