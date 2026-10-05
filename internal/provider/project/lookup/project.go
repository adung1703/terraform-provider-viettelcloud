package lookup

import (
	"context"
	"fmt"
	"iter"

	"github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/finder"

	projectsdk "github.com/viettelcloud-oss/sdks/go/project"
)

type ProjectLister interface {
	ListProjectsIter(context.Context, projectsdk.ListProjectsParams) iter.Seq2[*projectsdk.ProjectSchema, error]
}

type ProjectFilter struct {
	Slug *string
}

func (f ProjectFilter) String() string {
	return finder.Criteria(finder.Text("slug", f.Slug))
}

func (f ProjectFilter) matches(project *projectsdk.ProjectSchema) bool {
	slug := finder.Trim(f.Slug)
	return slug == "" || project.Slug == slug
}

func (f ProjectFilter) params() projectsdk.ListProjectsParams {
	params := projectsdk.ListProjectsParams{}
	if slug := finder.Trim(f.Slug); slug != "" {
		params.Slug = &slug
	}
	return params
}

type ProjectFinder struct {
	client ProjectLister
}

type ProjectResolveFunc func(context.Context, ProjectFilter) (projectsdk.ProjectSchema, error)

var _ finder.Interface[ProjectFilter, projectsdk.ProjectSchema] = (*ProjectFinder)(nil)

func NewProjectFinder(client ProjectLister) *ProjectFinder {
	return &ProjectFinder{client: client}
}

func (f *ProjectFinder) Find(ctx context.Context, filter ProjectFilter) ([]projectsdk.ProjectSchema, error) {
	return finder.Collect(f.client.ListProjectsIter(ctx, filter.params()), "project", filter.String(), filter.matches)
}

func (f *ProjectFinder) Resolve(ctx context.Context, filter ProjectFilter) (projectsdk.ProjectSchema, error) {
	candidates, err := f.Find(ctx, filter)
	if err != nil {
		return projectsdk.ProjectSchema{}, err
	}
	return finder.ExactlyOne(candidates, "project", filter.String(), projectSummary)
}

func projectSummary(project projectsdk.ProjectSchema) string {
	return fmt.Sprintf("id=%s (name=%q, slug=%q)", project.Id, project.Name, project.Slug)
}
