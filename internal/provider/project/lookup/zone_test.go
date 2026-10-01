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

type fakeZoneLister struct {
	values    []projectsdk.ProjectZoneSchema
	err       error
	projectID core.UUID
	params    projectsdk.ListProjectZonesParams
}

func (f *fakeZoneLister) ListProjectZonesIter(
	_ context.Context,
	projectID core.UUID,
	params projectsdk.ListProjectZonesParams,
) iter.Seq2[*projectsdk.ProjectZoneSchema, error] {
	f.projectID = projectID
	f.params = params
	return func(yield func(*projectsdk.ProjectZoneSchema, error) bool) {
		for i := range f.values {
			if !yield(&f.values[i], nil) {
				return
			}
		}
		if f.err != nil {
			yield(nil, f.err)
		}
	}
}

func TestZoneFinderResolvesTrimmedExactName(t *testing.T) {
	t.Parallel()
	projectID := core.UUID{1}
	name := " zone-a "
	wantID := core.UUID{2}
	lister := &fakeZoneLister{values: []projectsdk.ProjectZoneSchema{
		{Id: wantID, Name: "zone-a"},
		{Id: core.UUID{3}, Name: "zone-a-extra"},
	}}

	got, err := NewZoneFinder(lister, projectID).Resolve(context.Background(), ZoneFilter{Name: &name})
	if err != nil || got.Id != wantID {
		t.Fatalf("resolve zone: got=%#v err=%v", got, err)
	}
	if lister.projectID != projectID || lister.params.Name == nil || *lister.params.Name != "zone-a" {
		t.Fatalf("unexpected list request: project=%s params=%#v", lister.projectID, lister.params)
	}
}

func TestZoneFinderCardinalityAndListErrors(t *testing.T) {
	t.Parallel()
	name := "zone-a"
	for testName, values := range map[string][]projectsdk.ProjectZoneSchema{
		"missing":   nil,
		"ambiguous": {{Id: core.UUID{1}, Name: name}, {Id: core.UUID{2}, Name: name}},
	} {
		_, err := NewZoneFinder(&fakeZoneLister{values: values}, core.UUID{9}).Resolve(
			context.Background(), ZoneFilter{Name: &name},
		)
		if err == nil || !strings.Contains(err.Error(), map[string]string{"missing": "not found", "ambiguous": "ambiguous"}[testName]) {
			t.Fatalf("expected %s error, got %v", testName, err)
		}
	}
	wantErr := errors.New("list failed")
	_, err := NewZoneFinder(&fakeZoneLister{err: wantErr}, core.UUID{9}).Find(context.Background(), ZoneFilter{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected list error, got %v", err)
	}
}
