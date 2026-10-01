package lookup

import (
	"context"
	"errors"
	"iter"
	"strings"
	"testing"

	"github.com/viettelcloud-oss/sdks/go/core"
	projectsdk "github.com/viettelcloud-oss/sdks/go/project"
)

type fakeRegionLister struct {
	results   []projectsdk.ProjectRegionSchema
	err       error
	projectID core.UUID
	params    projectsdk.ListProjectRegionsParams
}

func (f *fakeRegionLister) ListProjectRegionsIter(
	_ context.Context,
	projectID core.UUID,
	params projectsdk.ListProjectRegionsParams,
) iter.Seq2[*projectsdk.ProjectRegionSchema, error] {
	f.projectID = projectID
	f.params = params
	return func(yield func(*projectsdk.ProjectRegionSchema, error) bool) {
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

func TestRegionFinderReturnsEveryExactCandidate(t *testing.T) {
	t.Parallel()

	projectID := core.UUID{8}
	lister := &fakeRegionLister{results: []projectsdk.ProjectRegionSchema{
		projectRegion(core.UUID{1}, "vn-central"),
		projectRegion(core.UUID{2}, "vn-central-extra"),
		projectRegion(core.UUID{3}, "vn-central"),
	}}
	matches, err := NewRegionFinder(lister, projectID).
		Find(context.Background(), RegionFilter{Name: new("vn-central")})
	if err != nil {
		t.Fatalf("find regions: %v", err)
	}
	if len(matches) != 2 || matches[0].Region.Id != (core.UUID{1}) || matches[1].Region.Id != (core.UUID{3}) {
		t.Fatalf("unexpected region matches: %#v", matches)
	}
	if lister.projectID != projectID || lister.params.Name == nil || *lister.params.Name != "vn-central" {
		t.Fatalf("unexpected list request: project=%s params=%#v", lister.projectID, lister.params)
	}
}

func TestRegionFinderTrimsName(t *testing.T) {
	t.Parallel()

	lister := &fakeRegionLister{results: []projectsdk.ProjectRegionSchema{
		projectRegion(core.UUID{1}, "vn-central"),
	}}
	matches, err := NewRegionFinder(lister, core.UUID{8}).
		Find(context.Background(), RegionFilter{Name: new(" vn-central ")})
	if err != nil {
		t.Fatalf("find regions: %v", err)
	}
	if len(matches) != 1 || matches[0].Region.Id != (core.UUID{1}) {
		t.Fatalf("expected the padded name to match the exact region, got %#v", matches)
	}
	if lister.params.Name == nil || *lister.params.Name != "vn-central" {
		t.Fatalf("expected trimmed lookup query, got %#v", lister.params.Name)
	}
}

func TestRegionFinderEmptyFilterListsEveryRegion(t *testing.T) {
	t.Parallel()

	lister := &fakeRegionLister{results: []projectsdk.ProjectRegionSchema{
		projectRegion(core.UUID{1}, "vn-central"),
		projectRegion(core.UUID{2}, "vn-north"),
	}}
	matches, err := NewRegionFinder(lister, core.UUID{8}).
		Find(context.Background(), RegionFilter{Name: new("  ")})
	if err != nil {
		t.Fatalf("find regions: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected a blank name to be unset, got %#v", matches)
	}
	if lister.params.Name != nil {
		t.Fatalf("expected no name in list params, got %#v", lister.params.Name)
	}
}

func TestRegionFinderReturnsListErrors(t *testing.T) {
	t.Parallel()

	listErr := errors.New("request failed")
	matches, err := NewRegionFinder(&fakeRegionLister{err: listErr}, core.UUID{8}).
		Find(context.Background(), RegionFilter{Name: new("vn-central")})
	if !errors.Is(err, listErr) || matches != nil {
		t.Fatalf("expected list error, matches=%#v error=%v", matches, err)
	}
}

func TestRegionFinderResolveRequiresUniqueCandidate(t *testing.T) {
	t.Parallel()

	region := projectRegion(core.UUID{1}, "vn-central")
	resolved, err := NewRegionFinder(
		&fakeRegionLister{results: []projectsdk.ProjectRegionSchema{region}},
		core.UUID{8},
	).Resolve(context.Background(), RegionFilter{Name: new("vn-central")})
	if err != nil || resolved.Region.Id != region.Region.Id {
		t.Fatalf("unexpected resolution: region=%#v error=%v", resolved, err)
	}

	for name, tt := range map[string]struct {
		candidates []projectsdk.ProjectRegionSchema
		want       string
	}{
		"missing": {
			want: `region was not found matching criteria (name="vn-central")`,
		},
		"ambiguous": {
			candidates: []projectsdk.ProjectRegionSchema{
				projectRegion(core.UUID{1}, "vn-central"),
				projectRegion(core.UUID{2}, "vn-central"),
			},
			want: `region matching criteria (name="vn-central") is ambiguous`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewRegionFinder(&fakeRegionLister{results: tt.candidates}, core.UUID{8}).
				Resolve(context.Background(), RegionFilter{Name: new("vn-central")})
			if err == nil {
				t.Fatal("expected selection error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestRegionFinderResolveReportsTrimmedCriteria(t *testing.T) {
	t.Parallel()

	_, err := NewRegionFinder(&fakeRegionLister{}, core.UUID{8}).
		Resolve(context.Background(), RegionFilter{Name: new("  vn-central  ")})
	if err == nil {
		t.Fatal("expected selection error")
	}
	want := `region was not found matching criteria (name="vn-central")`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got %v", want, err)
	}
}

func TestRegionFilterString(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		filter   RegionFilter
		expected string
	}{
		"unset":   {filter: RegionFilter{}, expected: "all"},
		"blank":   {filter: RegionFilter{Name: new("  ")}, expected: "all"},
		"name":    {filter: RegionFilter{Name: new("vn-central")}, expected: `name="vn-central"`},
		"trimmed": {filter: RegionFilter{Name: new("  vn-central  ")}, expected: `name="vn-central"`},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := tt.filter.String(); got != tt.expected {
				t.Errorf("RegionFilter.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func projectRegion(id core.UUID, name string) projectsdk.ProjectRegionSchema {
	return projectsdk.ProjectRegionSchema{
		Region: projectsdk.NestedRegionSchema{Id: id, Name: name},
	}
}
