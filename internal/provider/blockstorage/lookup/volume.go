package lookup

import (
	"context"
	"fmt"
	"iter"

	"github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/finder"

	blockstoragesdk "github.com/viettelcloud-oss/sdks/go/blockstorage"
	"github.com/viettelcloud-oss/sdks/go/core"
)

type VolumeLister interface {
	ListVolumeIter(context.Context, blockstoragesdk.ListVolumeParams) iter.Seq2[*blockstoragesdk.VolumeSchema, error]
}

type VolumeFilter struct {
	ID       *core.UUID
	Name     *string
	Status   *blockstoragesdk.VolumeStatus
	Zone     *string
	Size     *int
	Bootable *bool
}

func (f VolumeFilter) String() string {
	return finder.Criteria(
		finder.Part("id", f.ID), finder.Text("name", f.Name), finder.Part("status", f.Status), finder.Text("zone", f.Zone),
		finder.Part("size", f.Size), finder.Part("bootable", f.Bootable),
	)
}

func (f VolumeFilter) matches(volume *blockstoragesdk.VolumeSchema) bool {
	if f.ID != nil && volume.Id != *f.ID {
		return false
	}
	if name := finder.Trim(f.Name); name != "" && volume.Name != name {
		return false
	}
	if f.Status != nil && volume.Status != *f.Status {
		return false
	}
	if zone := finder.Trim(f.Zone); zone != "" && volume.Zone.Name != zone {
		return false
	}
	if f.Size != nil && volume.Size != *f.Size {
		return false
	}
	return f.Bootable == nil || volume.Bootable == *f.Bootable
}

func (f VolumeFilter) params(projectID core.UUID) blockstoragesdk.ListVolumeParams {
	params := blockstoragesdk.ListVolumeParams{
		ProjectID: projectID,
		Size:      f.Size,
		Bootable:  f.Bootable,
	}
	if name := finder.Trim(f.Name); name != "" {
		params.Name = &name
	}
	if f.Status != nil {
		statuses := []blockstoragesdk.VolumeStatus{*f.Status}
		params.Status = &statuses
	}
	if zone := finder.Trim(f.Zone); zone != "" {
		params.Zone = &zone
	}
	return params
}

type VolumeFinder struct {
	client    VolumeLister
	projectID core.UUID
}

var _ finder.Interface[VolumeFilter, blockstoragesdk.VolumeSchema] = (*VolumeFinder)(nil)

func NewVolumeFinder(client VolumeLister, projectID core.UUID) *VolumeFinder {
	return &VolumeFinder{client: client, projectID: projectID}
}

func (f *VolumeFinder) Find(ctx context.Context, filter VolumeFilter) ([]blockstoragesdk.VolumeSchema, error) {
	return finder.Collect(f.client.ListVolumeIter(ctx, filter.params(f.projectID)), "volume", filter.String(), filter.matches)
}

func (f *VolumeFinder) Resolve(ctx context.Context, filter VolumeFilter) (blockstoragesdk.VolumeSchema, error) {
	candidates, err := f.Find(ctx, filter)
	if err != nil {
		return blockstoragesdk.VolumeSchema{}, err
	}
	return finder.ExactlyOne(candidates, "volume", filter.String(), volumeSummary)
}

func volumeSummary(volume blockstoragesdk.VolumeSchema) string {
	return fmt.Sprintf("id=%s (name=%q, size=%d, status=%s, zone=%q)", volume.Id, volume.Name, volume.Size, volume.Status, volume.Zone.Name)
}
