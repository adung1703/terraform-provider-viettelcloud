package lookup

import (
	"context"
	"fmt"
	"iter"

	"github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/finder"

	"github.com/viettelcloud-oss/sdks/go/core"
	networksdk "github.com/viettelcloud-oss/sdks/go/network"
)

type VPCLister interface {
	ListVpcsIter(
		context.Context,
		networksdk.ListVpcsParams,
	) iter.Seq2[*networksdk.VPCSchema, error]
}

type VPCFilter struct {
	ID       *core.UUID
	Name     *string
	CIDR     *string
	RegionID *core.UUID
}

func (f VPCFilter) String() string {
	return finder.Criteria(
		finder.Part("id", f.ID),
		finder.Text("name", f.Name),
		finder.Text("cidr", f.CIDR),
		finder.Part("region_id", f.RegionID),
	)
}

func (f VPCFilter) matches(vpc *networksdk.VPCSchema) bool {
	if f.ID != nil && vpc.Id != *f.ID {
		return false
	}
	if name := finder.Trim(f.Name); name != "" && vpc.Name != name {
		return false
	}
	if cidr := finder.Trim(f.CIDR); cidr != "" && vpc.Cidr != cidr {
		return false
	}
	if f.RegionID != nil && vpc.Region.Id != *f.RegionID {
		return false
	}
	return true
}

func (f VPCFilter) params(projectID core.UUID) networksdk.ListVpcsParams {
	params := networksdk.ListVpcsParams{ProjectID: projectID}
	if name := finder.Trim(f.Name); name != "" {
		params.Name = &name
	}
	if f.RegionID != nil {
		params.RegionId = f.RegionID
	}
	return params
}

type VPCFinder struct {
	client    VPCLister
	projectID core.UUID
}

var _ finder.Interface[VPCFilter, networksdk.VPCSchema] = (*VPCFinder)(nil)

func NewVPCFinder(client VPCLister, projectID core.UUID) *VPCFinder {
	return &VPCFinder{client: client, projectID: projectID}
}

func (f *VPCFinder) Find(ctx context.Context, filter VPCFilter) ([]networksdk.VPCSchema, error) {
	seq := f.client.ListVpcsIter(ctx, filter.params(f.projectID))
	return finder.Collect(seq, "VPC", filter.String(), filter.matches)
}

func (f *VPCFinder) Resolve(ctx context.Context, filter VPCFilter) (networksdk.VPCSchema, error) {
	candidates, err := f.Find(ctx, filter)
	if err != nil {
		return networksdk.VPCSchema{}, err
	}
	return finder.ExactlyOne(candidates, "VPC", filter.String(), vpcSummary)
}

func vpcSummary(vpc networksdk.VPCSchema) string {
	return fmt.Sprintf("id=%s (name=%q, cidr=%q)", vpc.Id, vpc.Name, vpc.Cidr)
}
