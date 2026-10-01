package lookup

import (
	"context"
	"errors"
	"iter"
	"strings"
	"testing"

	"github.com/viettelcloud-oss/sdks/go/core"
	serversdk "github.com/viettelcloud-oss/sdks/go/server"
)

type fakeFlavorLister struct {
	values []serversdk.FlavorSchema
	err    error
	params serversdk.ListFlavorsParams
}

func (f *fakeFlavorLister) ListFlavorsIter(
	_ context.Context,
	params serversdk.ListFlavorsParams,
) iter.Seq2[*serversdk.FlavorSchema, error] {
	f.params = params
	return func(yield func(*serversdk.FlavorSchema, error) bool) {
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

func TestFlavorFinderMatchesNameIDAndZone(t *testing.T) {
	t.Parallel()
	projectID := core.UUID{1}
	zoneID := core.UUID{2}
	wantID := core.UUID{3}
	name := " small "
	lister := &fakeFlavorLister{values: []serversdk.FlavorSchema{
		{Id: wantID, Name: "small", Zone: serversdk.NestedZoneSchema{Id: zoneID, Name: "zone-a"}},
		{Id: core.UUID{4}, Name: "small", Zone: serversdk.NestedZoneSchema{Id: core.UUID{5}, Name: "zone-b"}},
	}}

	got, err := NewFlavorFinder(lister, projectID).Resolve(
		context.Background(), FlavorFilter{Name: &name, ID: &wantID, ZoneID: &zoneID},
	)
	if err != nil || got.Id != wantID {
		t.Fatalf("resolve flavor: got=%#v err=%v", got, err)
	}
	if lister.params.ProjectID != projectID || lister.params.Name == nil || *lister.params.Name != "small" {
		t.Fatalf("unexpected list params: %#v", lister.params)
	}
}

func TestFlavorFinderCardinalityAndListErrors(t *testing.T) {
	t.Parallel()
	name := "small"
	for testName, values := range map[string][]serversdk.FlavorSchema{
		"missing":   nil,
		"ambiguous": {{Id: core.UUID{1}, Name: name}, {Id: core.UUID{2}, Name: name}},
	} {
		_, err := NewFlavorFinder(&fakeFlavorLister{values: values}, core.UUID{9}).Resolve(
			context.Background(), FlavorFilter{Name: &name},
		)
		if err == nil || !strings.Contains(err.Error(), map[string]string{"missing": "not found", "ambiguous": "ambiguous"}[testName]) {
			t.Fatalf("expected %s error, got %v", testName, err)
		}
	}
	wantErr := errors.New("list failed")
	_, err := NewFlavorFinder(&fakeFlavorLister{err: wantErr}, core.UUID{9}).Find(context.Background(), FlavorFilter{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected list error, got %v", err)
	}
}
