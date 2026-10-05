package lookup

import (
	"context"
	"fmt"
	"iter"

	"github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/finder"

	"github.com/viettelcloud-oss/sdks/go/core"
	projectsdk "github.com/viettelcloud-oss/sdks/go/project"
)

type RegionLister interface {
	ListProjectRegionsIter(
		context.Context,
		core.UUID,
		projectsdk.ListProjectRegionsParams,
	) iter.Seq2[*projectsdk.ProjectRegionSchema, error]
}

type RegionFilter struct {
	Name *string
}

func (f RegionFilter) String() string {
	return finder.Criteria(finder.Text("name", f.Name))
}

func (f RegionFilter) matches(region *projectsdk.ProjectRegionSchema) bool {
	if name := finder.Trim(f.Name); name != "" && region.Region.Name != name {
		return false
	}
	return true
}

func (f RegionFilter) params() projectsdk.ListProjectRegionsParams {
	params := projectsdk.ListProjectRegionsParams{}
	if name := finder.Trim(f.Name); name != "" {
		params.Name = &name
	}
	return params
}

type RegionFinder struct {
	client    RegionLister
	projectID core.UUID
}

type RegionResolveFunc func(context.Context, RegionFilter) (projectsdk.ProjectRegionSchema, error)

var _ finder.Interface[RegionFilter, projectsdk.ProjectRegionSchema] = (*RegionFinder)(nil)

func NewRegionFinder(client RegionLister, projectID core.UUID) *RegionFinder {
	return &RegionFinder{client: client, projectID: projectID}
}

func (f *RegionFinder) Find(ctx context.Context, filter RegionFilter) ([]projectsdk.ProjectRegionSchema, error) {
	seq := f.client.ListProjectRegionsIter(ctx, f.projectID, filter.params())
	return finder.Collect(seq, "region", filter.String(), filter.matches)
}

func (f *RegionFinder) Resolve(ctx context.Context, filter RegionFilter) (projectsdk.ProjectRegionSchema, error) {
	candidates, err := f.Find(ctx, filter)
	if err != nil {
		return projectsdk.ProjectRegionSchema{}, err
	}
	return finder.ExactlyOne(candidates, "region", filter.String(), regionSummary)
}

func regionSummary(region projectsdk.ProjectRegionSchema) string {
	return fmt.Sprintf("id=%s (name=%q)", region.Region.Id, region.Region.Name)
}
