package lookup

import (
	"context"
	"fmt"
	"iter"

	"github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/finder"

	"github.com/viettelcloud-oss/sdks/go/core"
	serversdk "github.com/viettelcloud-oss/sdks/go/server"
)

type ImageLister interface {
	ListImagesIter(context.Context, serversdk.ListImagesParams) iter.Seq2[*serversdk.ImageSchema, error]
}

type ImageFilter struct {
	ID     *core.UUID
	Name   *string
	Status *[]serversdk.ImageStatus
	State  *[]serversdk.ImageState
}

func (f ImageFilter) String() string {
	return finder.Criteria(finder.Part("id", f.ID), finder.Text("name", f.Name), finder.Part("status", f.Status), finder.Part("state", f.State))
}

// matches reports whether the image satisfies every criterion the filter can
// check locally. ImageSchema does not expose status or state, so those two are
// enforced by the backend list filters instead.
func (f ImageFilter) matches(image *serversdk.ImageSchema) bool {
	if f.ID != nil && image.Id != *f.ID {
		return false
	}
	if name := finder.Trim(f.Name); name != "" && image.Name != name {
		return false
	}
	return true
}

func (f ImageFilter) params(projectID core.UUID) serversdk.ListImagesParams {
	params := serversdk.ListImagesParams{ProjectID: projectID, Id: f.ID, Status: f.Status, State: f.State}
	if name := finder.Trim(f.Name); name != "" {
		params.Name = &name
	}
	return params
}

type ImageFinder struct {
	client    ImageLister
	projectID core.UUID
}

type ImageResolveFunc func(context.Context, ImageFilter) (serversdk.ImageSchema, error)

var _ finder.Interface[ImageFilter, serversdk.ImageSchema] = (*ImageFinder)(nil)

func NewImageFinder(client ImageLister, projectID core.UUID) *ImageFinder {
	return &ImageFinder{client: client, projectID: projectID}
}

func (f *ImageFinder) Find(ctx context.Context, filter ImageFilter) ([]serversdk.ImageSchema, error) {
	return finder.Collect(f.client.ListImagesIter(ctx, filter.params(f.projectID)), "image", filter.String(), filter.matches)
}

func (f *ImageFinder) Resolve(ctx context.Context, filter ImageFilter) (serversdk.ImageSchema, error) {
	candidates, err := f.Find(ctx, filter)
	if err != nil {
		return serversdk.ImageSchema{}, err
	}
	return finder.ExactlyOne(candidates, "image", filter.String(), imageSummary)
}

func imageSummary(image serversdk.ImageSchema) string {
	return fmt.Sprintf("id=%s (name=%q)", image.Id, image.Name)
}
