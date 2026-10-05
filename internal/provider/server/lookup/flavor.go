package lookup

import (
	"context"
	"fmt"
	"iter"

	"github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/finder"

	"github.com/viettelcloud-oss/sdks/go/core"
	serversdk "github.com/viettelcloud-oss/sdks/go/server"
)

type FlavorLister interface {
	ListFlavorsIter(context.Context, serversdk.ListFlavorsParams) iter.Seq2[*serversdk.FlavorSchema, error]
}

type FlavorFilter struct {
	Name   *string
	ID     *core.UUID
	ZoneID *core.UUID
}

func (f FlavorFilter) String() string {
	return finder.Criteria(finder.Text("name", f.Name), finder.Part("id", f.ID), finder.Part("zone_id", f.ZoneID))
}

func (f FlavorFilter) matches(flavor *serversdk.FlavorSchema) bool {
	if name := finder.Trim(f.Name); name != "" && flavor.Name != name {
		return false
	}
	if f.ID != nil && flavor.Id != *f.ID {
		return false
	}
	return f.ZoneID == nil || flavor.Zone.Id == *f.ZoneID
}

func (f FlavorFilter) params(projectID core.UUID) serversdk.ListFlavorsParams {
	params := serversdk.ListFlavorsParams{ProjectID: projectID}
	if name := finder.Trim(f.Name); name != "" {
		params.Name = &name
	}
	return params
}

type FlavorFinder struct {
	client    FlavorLister
	projectID core.UUID
}

type FlavorResolveFunc func(context.Context, FlavorFilter) (serversdk.FlavorSchema, error)

var _ finder.Interface[FlavorFilter, serversdk.FlavorSchema] = (*FlavorFinder)(nil)

func NewFlavorFinder(client FlavorLister, projectID core.UUID) *FlavorFinder {
	return &FlavorFinder{client: client, projectID: projectID}
}

func (f *FlavorFinder) Find(ctx context.Context, filter FlavorFilter) ([]serversdk.FlavorSchema, error) {
	return finder.Collect(f.client.ListFlavorsIter(ctx, filter.params(f.projectID)), "flavor", filter.String(), filter.matches)
}

func (f *FlavorFinder) Resolve(ctx context.Context, filter FlavorFilter) (serversdk.FlavorSchema, error) {
	candidates, err := f.Find(ctx, filter)
	if err != nil {
		return serversdk.FlavorSchema{}, err
	}
	return finder.ExactlyOne(candidates, "flavor", filter.String(), flavorSummary)
}

func flavorSummary(flavor serversdk.FlavorSchema) string {
	return fmt.Sprintf("id=%s (name=%q, zone=%q)", flavor.Id, flavor.Name, flavor.Zone.Name)
}
