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

type fakeKeyPairLister struct {
	values []serversdk.KeyPairSchema
	err    error
	params serversdk.ListKeyPairsParams
}

func (f *fakeKeyPairLister) ListKeyPairsIter(
	_ context.Context,
	params serversdk.ListKeyPairsParams,
) iter.Seq2[*serversdk.KeyPairSchema, error] {
	f.params = params
	return func(yield func(*serversdk.KeyPairSchema, error) bool) {
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

func TestKeyPairFinderMatchesNameAndID(t *testing.T) {
	t.Parallel()

	projectID := core.UUID{1}
	wantID := core.UUID{2}
	name := " my-key "
	lister := &fakeKeyPairLister{values: []serversdk.KeyPairSchema{
		{Id: wantID, Name: "my-key", Fingerprint: "11:22"},
		{Id: core.UUID{3}, Name: "other-key", Fingerprint: "33:44"},
	}}

	got, err := NewKeyPairFinder(lister, projectID).Resolve(
		context.Background(), KeyPairFilter{Name: &name, ID: &wantID},
	)
	if err != nil || got.Id != wantID {
		t.Fatalf("resolve key pair: got=%#v err=%v", got, err)
	}
	if lister.params.ProjectID != projectID || lister.params.Name == nil || *lister.params.Name != "my-key" {
		t.Fatalf("unexpected list params: %#v", lister.params)
	}
}

func TestKeyPairFinderMatchesFingerprint(t *testing.T) {
	t.Parallel()

	projectID := core.UUID{1}
	wantID := core.UUID{2}
	fp := " 11:22:AA "
	lister := &fakeKeyPairLister{values: []serversdk.KeyPairSchema{
		{Id: wantID, Name: "my-key", Fingerprint: "11:22:aa"},
		{Id: core.UUID{3}, Name: "other-key", Fingerprint: "33:44:bb"},
	}}

	got, err := NewKeyPairFinder(lister, projectID).Resolve(
		context.Background(), KeyPairFilter{Fingerprint: &fp},
	)
	if err != nil || got.Id != wantID {
		t.Fatalf("resolve key pair by fingerprint: got=%#v err=%v", got, err)
	}
}

func TestKeyPairFinderCardinalityAndListErrors(t *testing.T) {
	t.Parallel()

	name := "my-key"
	for testName, values := range map[string][]serversdk.KeyPairSchema{
		"missing":   nil,
		"ambiguous": {{Id: core.UUID{1}, Name: name}, {Id: core.UUID{2}, Name: name}},
	} {
		_, err := NewKeyPairFinder(&fakeKeyPairLister{values: values}, core.UUID{9}).Resolve(
			context.Background(), KeyPairFilter{Name: &name},
		)
		if err == nil || !strings.Contains(err.Error(), map[string]string{"missing": "not found", "ambiguous": "ambiguous"}[testName]) {
			t.Fatalf("expected %s error, got %v", testName, err)
		}
	}

	wantErr := errors.New("list failed")
	_, err := NewKeyPairFinder(&fakeKeyPairLister{err: wantErr}, core.UUID{9}).Find(context.Background(), KeyPairFilter{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected list error, got %v", err)
	}
}
