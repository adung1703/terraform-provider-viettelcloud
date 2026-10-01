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

type fakeProjectLister struct {
	values []projectsdk.ProjectSchema
	err    error
	params projectsdk.ListProjectsParams
}

func (f *fakeProjectLister) ListProjectsIter(
	_ context.Context,
	params projectsdk.ListProjectsParams,
) iter.Seq2[*projectsdk.ProjectSchema, error] {
	f.params = params
	return func(yield func(*projectsdk.ProjectSchema, error) bool) {
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

func TestProjectFinderResolvesTrimmedExactSlug(t *testing.T) {
	t.Parallel()
	slug := " project-a "
	wantID := core.UUID{1}
	lister := &fakeProjectLister{values: []projectsdk.ProjectSchema{
		{Id: wantID, Name: "Project A", Slug: "project-a"},
		{Id: core.UUID{2}, Name: "Project AB", Slug: "project-ab"},
	}}

	got, err := NewProjectFinder(lister).Resolve(context.Background(), ProjectFilter{Slug: &slug})
	if err != nil || got.Id != wantID {
		t.Fatalf("resolve project: got=%#v err=%v", got, err)
	}
	if lister.params.Slug == nil || *lister.params.Slug != "project-a" {
		t.Fatalf("expected trimmed slug filter, got %#v", lister.params)
	}
}

func TestProjectFinderRequiresUniqueCandidate(t *testing.T) {
	t.Parallel()
	slug := "project-a"
	for name, values := range map[string][]projectsdk.ProjectSchema{
		"missing": nil,
		"ambiguous": {
			{Id: core.UUID{1}, Name: "Project A", Slug: slug},
			{Id: core.UUID{2}, Name: "Project A copy", Slug: slug},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewProjectFinder(&fakeProjectLister{values: values}).Resolve(
				context.Background(), ProjectFilter{Slug: &slug},
			)
			if err == nil || (name == "missing" && !strings.Contains(err.Error(), "not found")) ||
				(name == "ambiguous" && !strings.Contains(err.Error(), "ambiguous")) {
				t.Fatalf("expected %s error, got %v", name, err)
			}
		})
	}
}

func TestProjectFinderPropagatesListError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("project API unavailable")
	_, err := NewProjectFinder(&fakeProjectLister{err: wantErr}).Find(context.Background(), ProjectFilter{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected list error, got %v", err)
	}
}
