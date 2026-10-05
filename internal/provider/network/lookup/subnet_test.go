package lookup

import (
	"context"
	"errors"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/viettelcloud-oss/sdks/go/core"
	networksdk "github.com/viettelcloud-oss/sdks/go/network"
)

type fakeSubnetLister struct {
	results []networksdk.SubnetSchema
	err     error
	params  networksdk.ListSubnetsParams
}

func (f *fakeSubnetLister) ListSubnetsIter(
	_ context.Context,
	params networksdk.ListSubnetsParams,
) iter.Seq2[*networksdk.SubnetSchema, error] {
	f.params = params
	return func(yield func(*networksdk.SubnetSchema, error) bool) {
		for i := range f.results {
			if !yield(&f.results[i], nil) {
				return
			}
		}
		if f.err != nil {
			yield(nil, f.err)
		}
	}
}

func sampleSubnet(id core.UUID, name, cidr string, vpcID core.UUID, vpcName, region string) networksdk.SubnetSchema {
	return networksdk.SubnetSchema{
		Id:          id,
		Name:        name,
		DisplayName: name,
		Cidr:        cidr,
		Vpc: networksdk.NestedVPCSchema{
			Id:   vpcID,
			Name: vpcName,
		},
		Region:    networksdk.NestedRegionSchema{Name: region},
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestSubnetFinderFindMatchesAllCriteria(t *testing.T) {
	t.Parallel()

	projectID := core.UUID{9}
	vpcID := core.UUID{8}
	matchingID := core.UUID{1}
	name := "application"
	cidr := "10.0.1.0/24"
	vpcName := "production"
	region := "vn-central-1"
	lister := &fakeSubnetLister{results: []networksdk.SubnetSchema{
		sampleSubnet(matchingID, name, cidr, vpcID, vpcName, region),
		sampleSubnet(core.UUID{2}, name, "10.0.2.0/24", vpcID, vpcName, region),
	}}

	matches, err := NewSubnetFinder(lister, projectID).Find(context.Background(), SubnetFilter{
		ID:      &matchingID,
		Name:    &name,
		CIDR:    &cidr,
		VPCID:   &vpcID,
		VPCName: &vpcName,
		Region:  &region,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 || matches[0].Id != matchingID {
		t.Fatalf("unexpected matches: %#v", matches)
	}
	if lister.params.ProjectID != projectID || lister.params.Name == nil || *lister.params.Name != name ||
		lister.params.Cidr == nil || *lister.params.Cidr != cidr ||
		lister.params.VpcId == nil || *lister.params.VpcId != vpcID ||
		lister.params.VpcName == nil || *lister.params.VpcName != vpcName ||
		lister.params.Region == nil || *lister.params.Region != region {
		t.Fatalf("unexpected list params: %#v", lister.params)
	}
}

func TestSubnetFinderFindRejectsEachMismatchedCriterion(t *testing.T) {
	t.Parallel()

	subnetID := core.UUID{1}
	vpcID := core.UUID{8}
	subnet := sampleSubnet(subnetID, "application", "10.0.1.0/24", vpcID, "production", "vn-central-1")

	otherSubnetID := core.UUID{2}
	otherVPCID := core.UUID{7}
	otherName := "database"
	otherCIDR := "10.0.2.0/24"
	otherVPCName := "staging"
	otherRegion := "vn-south-1"
	tests := map[string]SubnetFilter{
		"id":       {ID: &otherSubnetID},
		"name":     {Name: &otherName},
		"cidr":     {CIDR: &otherCIDR},
		"vpc_id":   {VPCID: &otherVPCID},
		"vpc_name": {VPCName: &otherVPCName},
		"region":   {Region: &otherRegion},
	}

	for name, filter := range tests {
		t.Run(name, func(t *testing.T) {
			lister := &fakeSubnetLister{results: []networksdk.SubnetSchema{subnet}}
			matches, err := NewSubnetFinder(lister, core.UUID{9}).Find(context.Background(), filter)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(matches) != 0 {
				t.Fatalf("expected mismatched %s filter to reject subnet, got %#v", name, matches)
			}
		})
	}
}

func TestSubnetFinderTrimsTextFilters(t *testing.T) {
	t.Parallel()

	lister := &fakeSubnetLister{results: []networksdk.SubnetSchema{
		sampleSubnet(core.UUID{1}, "application", "10.0.1.0/24", core.UUID{8}, "production", "vn-central-1"),
	}}
	name := "  application  "
	cidr := " 10.0.1.0/24 "
	vpcName := " production "
	region := " vn-central-1 "

	matches, err := NewSubnetFinder(lister, core.UUID{9}).Find(context.Background(), SubnetFilter{
		Name: &name, CIDR: &cidr, VPCName: &vpcName, Region: &region,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one match, got %#v", matches)
	}
	if *lister.params.Name != "application" || *lister.params.Cidr != "10.0.1.0/24" ||
		*lister.params.VpcName != "production" || *lister.params.Region != "vn-central-1" {
		t.Fatalf("expected trimmed list params, got %#v", lister.params)
	}
}

func TestSubnetFinderReturnsListError(t *testing.T) {
	t.Parallel()

	apiErr := errors.New("list failed")
	finder := NewSubnetFinder(&fakeSubnetLister{err: apiErr}, core.UUID{9})
	_, err := finder.Find(context.Background(), SubnetFilter{})
	if !errors.Is(err, apiErr) {
		t.Fatalf("expected list error, got %v", err)
	}

	_, err = finder.Resolve(context.Background(), SubnetFilter{})
	if !errors.Is(err, apiErr) {
		t.Fatalf("expected resolve to return list error, got %v", err)
	}
}

func TestSubnetFinderResolveSuccess(t *testing.T) {
	t.Parallel()

	subnet := sampleSubnet(core.UUID{1}, "application", "10.0.1.0/24", core.UUID{8}, "production", "vn-central-1")
	name := subnet.Name
	result, err := NewSubnetFinder(
		&fakeSubnetLister{results: []networksdk.SubnetSchema{subnet}},
		core.UUID{9},
	).Resolve(context.Background(), SubnetFilter{Name: &name})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Id != subnet.Id {
		t.Fatalf("expected subnet %s, got %s", subnet.Id, result.Id)
	}
}

func TestSubnetFinderResolveZeroMatches(t *testing.T) {
	t.Parallel()

	name := "missing"
	_, err := NewSubnetFinder(&fakeSubnetLister{}, core.UUID{9}).Resolve(
		context.Background(),
		SubnetFilter{Name: &name},
	)
	if err == nil || !strings.Contains(err.Error(), `subnet was not found matching criteria (name="missing")`) {
		t.Fatalf("expected not-found error naming the criteria, got %v", err)
	}
}

func TestSubnetFinderResolveMultipleMatches(t *testing.T) {
	t.Parallel()

	name := "duplicate"
	subnets := []networksdk.SubnetSchema{
		sampleSubnet(core.UUID{1}, name, "10.0.1.0/24", core.UUID{8}, "production", "vn-central-1"),
		sampleSubnet(core.UUID{2}, name, "10.0.2.0/24", core.UUID{8}, "production", "vn-central-1"),
	}
	_, err := NewSubnetFinder(&fakeSubnetLister{results: subnets}, core.UUID{9}).Resolve(
		context.Background(),
		SubnetFilter{Name: &name},
	)
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	for _, want := range []string{
		"is ambiguous",
		subnets[0].Id.String(),
		subnets[1].Id.String(),
		`cidr="10.0.1.0/24"`,
		`cidr="10.0.2.0/24"`,
		`vpc_name="production"`,
		`region="vn-central-1"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected ambiguity error to contain %q, got %v", want, err)
		}
	}
}

func TestSubnetFilterString(t *testing.T) {
	t.Parallel()

	id := core.UUID{1}
	vpcID := core.UUID{8}
	name := " application "
	cidr := " 10.0.1.0/24 "
	vpcName := " production "
	region := " vn-central-1 "
	filter := SubnetFilter{
		ID: &id, Name: &name, CIDR: &cidr, VPCID: &vpcID, VPCName: &vpcName, Region: &region,
	}
	want := `id=01000000-0000-0000-0000-000000000000, name="application", cidr="10.0.1.0/24", vpc_id=08000000-0000-0000-0000-000000000000, vpc_name="production", region="vn-central-1"`
	if got := filter.String(); got != want {
		t.Fatalf("SubnetFilter.String() = %q, want %q", got, want)
	}

	blank := " "
	if got := (SubnetFilter{Name: &blank}).String(); got != "all" {
		t.Fatalf("blank text filter description = %q, want all", got)
	}
}
