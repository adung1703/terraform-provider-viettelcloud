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

type fakeImageLister struct {
	values []serversdk.ImageSchema
	err    error
	params serversdk.ListImagesParams
}

func (f *fakeImageLister) ListImagesIter(
	_ context.Context,
	params serversdk.ListImagesParams,
) iter.Seq2[*serversdk.ImageSchema, error] {
	f.params = params
	return func(yield func(*serversdk.ImageSchema, error) bool) {
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

func TestImageFinderMatchesExactCriteriaAndPassesBackendFilters(t *testing.T) {
	t.Parallel()
	projectID := core.UUID{9}
	wantID := core.UUID{1}
	name := " Ubuntu "
	statuses := []serversdk.ImageStatus{serversdk.ImageStatusActive}
	states := []serversdk.ImageState{serversdk.ImageStateEnabled}
	lister := &fakeImageLister{values: []serversdk.ImageSchema{
		{Id: wantID, Name: "Ubuntu"},
		{Id: core.UUID{2}, Name: "Debian"},
	}}

	got, err := NewImageFinder(lister, projectID).Resolve(context.Background(), ImageFilter{
		ID: &wantID, Name: &name, Status: &statuses, State: &states,
	})
	if err != nil || got.Id != wantID {
		t.Fatalf("resolve image: got=%#v err=%v", got, err)
	}
	if lister.params.ProjectID != projectID || lister.params.Id == nil || *lister.params.Id != wantID ||
		lister.params.Name == nil || *lister.params.Name != "Ubuntu" || lister.params.Status == nil || lister.params.State == nil {
		t.Fatalf("unexpected list params: %#v", lister.params)
	}
}

func TestImageFinderCardinalityAndListErrors(t *testing.T) {
	t.Parallel()
	name := "Ubuntu"
	for testName, values := range map[string][]serversdk.ImageSchema{
		"missing":   nil,
		"ambiguous": {{Id: core.UUID{1}, Name: name}, {Id: core.UUID{2}, Name: name}},
	} {
		_, err := NewImageFinder(&fakeImageLister{values: values}, core.UUID{9}).Resolve(
			context.Background(), ImageFilter{Name: &name},
		)
		if err == nil || !strings.Contains(err.Error(), map[string]string{"missing": "not found", "ambiguous": "ambiguous"}[testName]) {
			t.Fatalf("expected %s error, got %v", testName, err)
		}
	}
	wantErr := errors.New("list failed")
	_, err := NewImageFinder(&fakeImageLister{err: wantErr}, core.UUID{9}).Find(context.Background(), ImageFilter{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected list error, got %v", err)
	}
}
