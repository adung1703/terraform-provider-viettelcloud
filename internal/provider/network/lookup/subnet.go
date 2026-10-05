package lookup

import (
	"context"
	"fmt"
	"iter"

	"github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/finder"

	"github.com/viettelcloud-oss/sdks/go/core"
	networksdk "github.com/viettelcloud-oss/sdks/go/network"
)

type SubnetLister interface {
	ListSubnetsIter(
		context.Context,
		networksdk.ListSubnetsParams,
	) iter.Seq2[*networksdk.SubnetSchema, error]
}

type SubnetFilter struct {
	ID      *core.UUID
	Name    *string
	CIDR    *string
	VPCID   *core.UUID
	VPCName *string
	Region  *string
}

func (f SubnetFilter) String() string {
	return finder.Criteria(
		finder.Part("id", f.ID),
		finder.Text("name", f.Name),
		finder.Text("cidr", f.CIDR),
		finder.Part("vpc_id", f.VPCID),
		finder.Text("vpc_name", f.VPCName),
		finder.Text("region", f.Region),
	)
}

func (f SubnetFilter) matches(subnet *networksdk.SubnetSchema) bool {
	if f.ID != nil && subnet.Id != *f.ID {
		return false
	}
	if name := finder.Trim(f.Name); name != "" && subnet.Name != name {
		return false
	}
	if cidr := finder.Trim(f.CIDR); cidr != "" && subnet.Cidr != cidr {
		return false
	}
	if f.VPCID != nil && subnet.Vpc.Id != *f.VPCID {
		return false
	}
	if vpcName := finder.Trim(f.VPCName); vpcName != "" && subnet.Vpc.Name != vpcName {
		return false
	}
	if region := finder.Trim(f.Region); region != "" && subnet.Region.Name != region {
		return false
	}
	return true
}

func (f SubnetFilter) params(projectID core.UUID) networksdk.ListSubnetsParams {
	params := networksdk.ListSubnetsParams{ProjectID: projectID}
	if name := finder.Trim(f.Name); name != "" {
		params.Name = &name
	}
	if cidr := finder.Trim(f.CIDR); cidr != "" {
		params.Cidr = &cidr
	}
	if f.VPCID != nil {
		params.VpcId = f.VPCID
	}
	if vpcName := finder.Trim(f.VPCName); vpcName != "" {
		params.VpcName = &vpcName
	}
	if region := finder.Trim(f.Region); region != "" {
		params.Region = &region
	}
	return params
}

type SubnetFinder struct {
	client    SubnetLister
	projectID core.UUID
}

var _ finder.Interface[SubnetFilter, networksdk.SubnetSchema] = (*SubnetFinder)(nil)

func NewSubnetFinder(client SubnetLister, projectID core.UUID) *SubnetFinder {
	return &SubnetFinder{client: client, projectID: projectID}
}

func (f *SubnetFinder) Find(ctx context.Context, filter SubnetFilter) ([]networksdk.SubnetSchema, error) {
	seq := f.client.ListSubnetsIter(ctx, filter.params(f.projectID))
	return finder.Collect(seq, "subnet", filter.String(), filter.matches)
}

func (f *SubnetFinder) Resolve(ctx context.Context, filter SubnetFilter) (networksdk.SubnetSchema, error) {
	candidates, err := f.Find(ctx, filter)
	if err != nil {
		return networksdk.SubnetSchema{}, err
	}
	return finder.ExactlyOne(candidates, "subnet", filter.String(), subnetSummary)
}

func subnetSummary(subnet networksdk.SubnetSchema) string {
	return fmt.Sprintf(
		"id=%s (name=%q, cidr=%q, vpc_name=%q, region=%q)",
		subnet.Id,
		subnet.Name,
		subnet.Cidr,
		subnet.Vpc.Name,
		subnet.Region.Name,
	)
}
