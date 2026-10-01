package lookup

import (
	"context"
	"fmt"
	"iter"

	"github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/finder"

	"github.com/viettelcloud-oss/sdks/go/core"
	projectsdk "github.com/viettelcloud-oss/sdks/go/project"
)

type ZoneLister interface {
	ListProjectZonesIter(
		context.Context,
		core.UUID,
		projectsdk.ListProjectZonesParams,
	) iter.Seq2[*projectsdk.ProjectZoneSchema, error]
}

type ZoneFilter struct {
	Name *string
}

func (f ZoneFilter) String() string {
	return finder.Criteria(finder.Text("name", f.Name))
}

func (f ZoneFilter) matches(zone *projectsdk.ProjectZoneSchema) bool {
	name := finder.Trim(f.Name)
	return name == "" || zone.Name == name
}

func (f ZoneFilter) params() projectsdk.ListProjectZonesParams {
	params := projectsdk.ListProjectZonesParams{}
	if name := finder.Trim(f.Name); name != "" {
		params.Name = &name
	}
	return params
}

type ZoneFinder struct {
	client    ZoneLister
	projectID core.UUID
}

type ZoneResolveFunc func(context.Context, ZoneFilter) (projectsdk.ProjectZoneSchema, error)

var _ finder.Interface[ZoneFilter, projectsdk.ProjectZoneSchema] = (*ZoneFinder)(nil)

func NewZoneFinder(client ZoneLister, projectID core.UUID) *ZoneFinder {
	return &ZoneFinder{client: client, projectID: projectID}
}

func (f *ZoneFinder) Find(ctx context.Context, filter ZoneFilter) ([]projectsdk.ProjectZoneSchema, error) {
	return finder.Collect(f.client.ListProjectZonesIter(ctx, f.projectID, filter.params()), "zone", filter.String(), filter.matches)
}

func (f *ZoneFinder) Resolve(ctx context.Context, filter ZoneFilter) (projectsdk.ProjectZoneSchema, error) {
	candidates, err := f.Find(ctx, filter)
	if err != nil {
		return projectsdk.ProjectZoneSchema{}, err
	}
	return finder.ExactlyOne(candidates, "zone", filter.String(), zoneSummary)
}

func zoneSummary(zone projectsdk.ProjectZoneSchema) string {
	return fmt.Sprintf("id=%s (name=%q, region=%q)", zone.Id, zone.Name, zone.Region.Name)
}
