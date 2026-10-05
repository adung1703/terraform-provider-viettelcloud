package lookup

import (
	"context"
	"fmt"
	"iter"

	"github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/finder"

	"github.com/viettelcloud-oss/sdks/go/core"
	networksdk "github.com/viettelcloud-oss/sdks/go/network"
)

type SecurityGroupLister interface {
	ListSecurityGroupsIter(
		context.Context,
		networksdk.ListSecurityGroupsParams,
	) iter.Seq2[*networksdk.SecurityGroupSchema, error]
}

type SecurityGroupFilter struct {
	ID        *core.UUID
	Name      *string
	RegionID  *core.UUID
	IsDefault *bool
}

func (f SecurityGroupFilter) String() string {
	return finder.Criteria(
		finder.Part("id", f.ID),
		finder.Text("name", f.Name),
		finder.Part("region_id", f.RegionID),
		finder.Part("is_default", f.IsDefault),
	)
}

func (f SecurityGroupFilter) matches(sg *networksdk.SecurityGroupSchema) bool {
	if f.ID != nil && sg.Id != *f.ID {
		return false
	}
	if name := finder.Trim(f.Name); name != "" && sg.Name != name {
		return false
	}
	if f.RegionID != nil && sg.Region.Id != *f.RegionID {
		return false
	}
	if f.IsDefault != nil && sg.IsDefault != *f.IsDefault {
		return false
	}
	return true
}

func (f SecurityGroupFilter) params(projectID core.UUID) networksdk.ListSecurityGroupsParams {
	params := networksdk.ListSecurityGroupsParams{ProjectID: projectID}
	if name := finder.Trim(f.Name); name != "" {
		params.Name = &name
	}
	if f.RegionID != nil {
		params.RegionId = f.RegionID
	}
	return params
}

type SecurityGroupFinder struct {
	client    SecurityGroupLister
	projectID core.UUID
}

var _ finder.Interface[SecurityGroupFilter, networksdk.SecurityGroupSchema] = (*SecurityGroupFinder)(nil)

func NewSecurityGroupFinder(client SecurityGroupLister, projectID core.UUID) *SecurityGroupFinder {
	return &SecurityGroupFinder{client: client, projectID: projectID}
}

func (f *SecurityGroupFinder) Find(ctx context.Context, filter SecurityGroupFilter) ([]networksdk.SecurityGroupSchema, error) {
	seq := f.client.ListSecurityGroupsIter(ctx, filter.params(f.projectID))
	return finder.Collect(seq, "security group", filter.String(), filter.matches)
}

func (f *SecurityGroupFinder) Resolve(ctx context.Context, filter SecurityGroupFilter) (networksdk.SecurityGroupSchema, error) {
	candidates, err := f.Find(ctx, filter)
	if err != nil {
		return networksdk.SecurityGroupSchema{}, err
	}
	return finder.ExactlyOne(candidates, "security group", filter.String(), securityGroupSummary)
}

func securityGroupSummary(sg networksdk.SecurityGroupSchema) string {
	return fmt.Sprintf("id=%s (name=%q, region_name=%q)", sg.Id, sg.Name, sg.Region.Name)
}
