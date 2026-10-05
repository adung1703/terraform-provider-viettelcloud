package lookup

import (
	"context"
	"fmt"
	"iter"
	"strings"

	"github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/finder"

	"github.com/viettelcloud-oss/sdks/go/core"
	serversdk "github.com/viettelcloud-oss/sdks/go/server"
)

type KeyPairLister interface {
	ListKeyPairsIter(context.Context, serversdk.ListKeyPairsParams) iter.Seq2[*serversdk.KeyPairSchema, error]
}

type KeyPairFilter struct {
	Name        *string
	ID          *core.UUID
	Fingerprint *string
}

func (f KeyPairFilter) String() string {
	return finder.Criteria(
		finder.Text("name", f.Name),
		finder.Part("id", f.ID),
		finder.Text("fingerprint", f.Fingerprint),
	)
}

func (f KeyPairFilter) matches(kp *serversdk.KeyPairSchema) bool {
	if name := finder.Trim(f.Name); name != "" && kp.Name != name {
		return false
	}
	if f.ID != nil && kp.Id != *f.ID {
		return false
	}
	if fp := finder.Trim(f.Fingerprint); fp != "" && !strings.EqualFold(kp.Fingerprint, fp) {
		return false
	}
	return true
}

func (f KeyPairFilter) params(projectID core.UUID) serversdk.ListKeyPairsParams {
	params := serversdk.ListKeyPairsParams{ProjectID: projectID}
	if name := finder.Trim(f.Name); name != "" {
		params.Name = &name
	}
	return params
}

type KeyPairFinder struct {
	client    KeyPairLister
	projectID core.UUID
}

type KeyPairResolveFunc func(context.Context, KeyPairFilter) (serversdk.KeyPairSchema, error)

var _ finder.Interface[KeyPairFilter, serversdk.KeyPairSchema] = (*KeyPairFinder)(nil)

func NewKeyPairFinder(client KeyPairLister, projectID core.UUID) *KeyPairFinder {
	return &KeyPairFinder{client: client, projectID: projectID}
}

func (f *KeyPairFinder) Find(ctx context.Context, filter KeyPairFilter) ([]serversdk.KeyPairSchema, error) {
	return finder.Collect(f.client.ListKeyPairsIter(ctx, filter.params(f.projectID)), "key pair", filter.String(), filter.matches)
}

func (f *KeyPairFinder) Resolve(ctx context.Context, filter KeyPairFilter) (serversdk.KeyPairSchema, error) {
	candidates, err := f.Find(ctx, filter)
	if err != nil {
		return serversdk.KeyPairSchema{}, err
	}
	return finder.ExactlyOne(candidates, "key pair", filter.String(), keyPairSummary)
}

func keyPairSummary(kp serversdk.KeyPairSchema) string {
	return fmt.Sprintf("id=%s (name=%q, fingerprint=%q)", kp.Id, kp.Name, kp.Fingerprint)
}
