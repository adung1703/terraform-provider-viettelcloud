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

type fakeSecurityGroupLister struct {
	results   []networksdk.SecurityGroupSchema
	err       error
	params    networksdk.ListSecurityGroupsParams
	callCount int
}

func (f *fakeSecurityGroupLister) ListSecurityGroupsIter(
	_ context.Context,
	params networksdk.ListSecurityGroupsParams,
) iter.Seq2[*networksdk.SecurityGroupSchema, error] {
	f.callCount++
	f.params = params
	return func(yield func(*networksdk.SecurityGroupSchema, error) bool) {
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

func sampleSecurityGroup(
	id core.UUID,
	name string,
	description *string,
	isDefault bool,
	regionID core.UUID,
	regionName string,
) networksdk.SecurityGroupSchema {
	return networksdk.SecurityGroupSchema{
		Id:          id,
		Name:        name,
		DisplayName: name + "-display",
		Description: description,
		IsDefault:   isDefault,
		Region: networksdk.NestedRegionSchema{
			Id:   regionID,
			Name: regionName,
		},
		Project: networksdk.NestedProjectSchema{
			Id: core.UUID{9},
		},
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestSecurityGroupFinderFindByName(t *testing.T) {
	t.Parallel()

	projectID := core.UUID{9}
	regionID := core.UUID{1}
	desc := "sg desc"
	sgs := []networksdk.SecurityGroupSchema{
		sampleSecurityGroup(core.UUID{10}, "sg-a", &desc, false, regionID, "vn-central"),
		sampleSecurityGroup(core.UUID{11}, "sg-b", &desc, false, regionID, "vn-central"),
		sampleSecurityGroup(core.UUID{12}, "sg-a", &desc, false, regionID, "vn-north"),
	}
	lister := &fakeSecurityGroupLister{results: sgs}
	finder := NewSecurityGroupFinder(lister, projectID)

	name := "sg-a"
	matches, err := finder.Find(context.Background(), SecurityGroupFilter{Name: &name})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 2 || matches[0].Id != (core.UUID{10}) || matches[1].Id != (core.UUID{12}) {
		t.Fatalf("unexpected matches: %#v", matches)
	}
	if lister.params.ProjectID != projectID || lister.params.Name == nil || *lister.params.Name != "sg-a" {
		t.Fatalf("unexpected list params: %#v", lister.params)
	}
}

func TestSecurityGroupFinderFindByIsDefaultAndRegionID(t *testing.T) {
	t.Parallel()

	projectID := core.UUID{9}
	regionID1 := core.UUID{1}
	regionID2 := core.UUID{2}
	sgs := []networksdk.SecurityGroupSchema{
		sampleSecurityGroup(core.UUID{10}, "default", nil, true, regionID1, "vn-central"),
		sampleSecurityGroup(core.UUID{11}, "default", nil, true, regionID2, "vn-north"),
		sampleSecurityGroup(core.UUID{12}, "custom", nil, false, regionID1, "vn-central"),
	}
	lister := &fakeSecurityGroupLister{results: sgs}
	finder := NewSecurityGroupFinder(lister, projectID)

	isDefault := true
	matches, err := finder.Find(context.Background(), SecurityGroupFilter{
		IsDefault: &isDefault,
		RegionID:  &regionID1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 || matches[0].Id != (core.UUID{10}) {
		t.Fatalf("unexpected matches: %#v", matches)
	}
	if lister.params.RegionId == nil || *lister.params.RegionId != regionID1 {
		t.Fatalf("expected RegionId param, got %#v", lister.params.RegionId)
	}
}

func TestSecurityGroupFinderFindMatchesAllCriteria(t *testing.T) {
	t.Parallel()

	projectID := core.UUID{9}
	regionID := core.UUID{1}
	matchingID := core.UUID{10}
	name := "sg-a"
	desc := "web sg"
	isDefault := false
	lister := &fakeSecurityGroupLister{results: []networksdk.SecurityGroupSchema{
		sampleSecurityGroup(matchingID, name, &desc, isDefault, regionID, "vn-central"),
		sampleSecurityGroup(core.UUID{11}, name, nil, isDefault, regionID, "vn-north"),
	}}

	matches, err := NewSecurityGroupFinder(lister, projectID).Find(context.Background(), SecurityGroupFilter{
		ID:        &matchingID,
		Name:      &name,
		RegionID:  &regionID,
		IsDefault: &isDefault,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 || matches[0].Id != matchingID {
		t.Fatalf("unexpected matches: %#v", matches)
	}
	if lister.params.Name == nil || *lister.params.Name != name {
		t.Fatalf("expected name filter in list params, got %#v", lister.params)
	}
}

func TestSecurityGroupFinderReturnsListError(t *testing.T) {
	t.Parallel()

	apiErr := errors.New("list failed")
	finder := NewSecurityGroupFinder(&fakeSecurityGroupLister{err: apiErr}, core.UUID{9})
	_, err := finder.Find(context.Background(), SecurityGroupFilter{})
	if !errors.Is(err, apiErr) {
		t.Fatalf("expected apiErr, got %v", err)
	}
}

func TestSecurityGroupFinderResolveSuccess(t *testing.T) {
	t.Parallel()

	projectID := core.UUID{9}
	regionID := core.UUID{1}
	desc := "unique sg"
	sg := sampleSecurityGroup(core.UUID{10}, "unique-sg", &desc, false, regionID, "vn-central")
	finder := NewSecurityGroupFinder(&fakeSecurityGroupLister{results: []networksdk.SecurityGroupSchema{sg}}, projectID)

	name := "unique-sg"
	res, err := finder.Resolve(context.Background(), SecurityGroupFilter{Name: &name})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Id != sg.Id {
		t.Fatalf("expected resolved Security Group %s, got %s", sg.Id, res.Id)
	}
}

func TestSecurityGroupFinderResolveZeroMatches(t *testing.T) {
	t.Parallel()

	finder := NewSecurityGroupFinder(&fakeSecurityGroupLister{results: nil}, core.UUID{9})
	name := "non-existent"
	_, err := finder.Resolve(context.Background(), SecurityGroupFilter{Name: &name})
	if err == nil {
		t.Fatal("expected error when no Security Group matches")
	}
	if !strings.Contains(err.Error(), `security group was not found matching criteria (name="non-existent")`) {
		t.Fatalf("expected not-found error naming the criteria, got %v", err)
	}
}

func TestSecurityGroupFinderResolveMultipleMatches(t *testing.T) {
	t.Parallel()

	regionID := core.UUID{1}
	sgs := []networksdk.SecurityGroupSchema{
		sampleSecurityGroup(core.UUID{10}, "dup-sg", nil, false, regionID, "vn-central"),
		sampleSecurityGroup(core.UUID{11}, "dup-sg", nil, false, regionID, "vn-central"),
	}
	finder := NewSecurityGroupFinder(&fakeSecurityGroupLister{results: sgs}, core.UUID{9})
	name := "dup-sg"
	_, err := finder.Resolve(context.Background(), SecurityGroupFilter{Name: &name})
	if err == nil {
		t.Fatal("expected error when multiple Security Groups match")
	}
	for _, want := range []string{
		"is ambiguous",
		`id=` + sgs[0].Id.String(),
		`id=` + sgs[1].Id.String(),
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected ambiguity error to contain %q, got %v", want, err)
		}
	}
}

func TestSecurityGroupFilterString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		filter   SecurityGroupFilter
		expected string
	}{
		{
			name:     "empty filter",
			filter:   SecurityGroupFilter{},
			expected: "all",
		},
		{
			name: "single name filter",
			filter: SecurityGroupFilter{
				Name: new("my-sg"),
			},
			expected: `name="my-sg"`,
		},
		{
			name: "all criteria populated",
			filter: SecurityGroupFilter{
				ID:        &core.UUID{10},
				Name:      new("my-sg"),
				RegionID:  &core.UUID{1},
				IsDefault: new(true),
			},
			expected: `id=0a000000-0000-0000-0000-000000000000, name="my-sg", region_id=01000000-0000-0000-0000-000000000000, is_default=true`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.filter.String(); got != tt.expected {
				t.Errorf("SecurityGroupFilter.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}
