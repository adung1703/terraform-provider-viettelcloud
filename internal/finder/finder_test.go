package finder

import (
	"errors"
	"fmt"
	"iter"
	"strings"
	"testing"

	"github.com/viettelcloud-oss/sdks/go/core"
)

type stubItem struct {
	id   int
	name string
}

func stubSeq(items []stubItem, err error) iter.Seq2[*stubItem, error] {
	return func(yield func(*stubItem, error) bool) {
		for i := range items {
			if !yield(&items[i], nil) {
				return
			}
		}
		if err != nil {
			yield(nil, err)
		}
	}
}

func stubSummary(item stubItem) string {
	return fmt.Sprintf("id=%d (name=%q)", item.id, item.name)
}

func keepName(want string) func(*stubItem) bool {
	return func(candidate *stubItem) bool { return candidate.name == want }
}

func TestCollectKeepsMatchingCandidates(t *testing.T) {
	t.Parallel()

	items := []stubItem{{1, "a"}, {2, "b"}, {3, "a"}}
	got, err := Collect(stubSeq(items, nil), "widget", `name="a"`, keepName("a"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0].id != 1 || got[1].id != 3 {
		t.Fatalf("unexpected matches: %#v", got)
	}
}

func TestCollectReturnsNilWhenNothingMatches(t *testing.T) {
	t.Parallel()

	got, err := Collect(stubSeq([]stubItem{{1, "a"}}, nil), "widget", `name="z"`, keepName("z"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil matches, got %#v", got)
	}
}

func TestCollectWrapsListError(t *testing.T) {
	t.Parallel()

	listErr := errors.New("request failed")
	_, err := Collect(stubSeq(nil, listErr), "widget", `name="a"`, keepName("a"))
	if !errors.Is(err, listErr) {
		t.Fatalf("expected wrapped list error, got %v", err)
	}
	if want := `list widgets for criteria (name="a")`; !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got %v", want, err)
	}
}

func TestCollectRejectsNilCandidate(t *testing.T) {
	t.Parallel()

	seq := func(yield func(*stubItem, error) bool) {
		yield(nil, nil)
	}
	_, err := Collect(iter.Seq2[*stubItem, error](seq), "widget", "all", keepName("a"))
	if err == nil {
		t.Fatal("expected error for a nil candidate")
	}
	if want := "SDK returned a nil candidate"; !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got %v", want, err)
	}
}

func TestExactlyOne(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		candidates []stubItem
		wantID     int
		wantErr    string
	}{
		{
			name:    "zero candidates",
			wantErr: `widget was not found matching criteria (name="a")`,
		},
		{
			name:       "single candidate",
			candidates: []stubItem{{7, "a"}},
			wantID:     7,
		},
		{
			name:       "multiple candidates",
			candidates: []stubItem{{7, "a"}, {8, "a"}},
			wantErr:    `widget matching criteria (name="a") is ambiguous; multiple candidates matched: id=7 (name="a"), id=8 (name="a")`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ExactlyOne(tt.candidates, "widget", `name="a"`, stubSummary)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("ExactlyOne() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.id != tt.wantID {
				t.Fatalf("ExactlyOne() = %#v, want id %d", got, tt.wantID)
			}
		})
	}
}

func TestCriteria(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		parts []string
		want  string
	}{
		{name: "no parts", want: "all"},
		{name: "every part unset", parts: []string{"", ""}, want: "all"},
		{name: "single part", parts: []string{`name="a"`}, want: `name="a"`},
		{
			name:  "skips unset parts",
			parts: []string{"id=1", "", `name="a"`, ""},
			want:  `id=1, name="a"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Criteria(tt.parts...); got != tt.want {
				t.Errorf("Criteria(%q) = %q, want %q", tt.parts, got, tt.want)
			}
		})
	}
}

func TestPart(t *testing.T) {
	t.Parallel()

	id, err := core.ParseUUID("00000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatalf("parse uuid: %v", err)
	}
	count := 42

	if got := Part[int]("count", nil); got != "" {
		t.Errorf("Part with nil value = %q, want empty", got)
	}
	if got, want := Part("count", &count), "count=42"; got != want {
		t.Errorf("Part() = %q, want %q", got, want)
	}
	if got, want := Part("id", &id), "id=00000000-0000-0000-0000-000000000001"; got != want {
		t.Errorf("Part() = %q, want %q", got, want)
	}
}

func TestText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value *string
		want  string
	}{
		{name: "unset", want: ""},
		{name: "empty", value: new(""), want: ""},
		{name: "blank", value: new("   "), want: ""},
		{name: "quoted", value: new("my-vpc"), want: `name="my-vpc"`},
		{name: "trimmed", value: new("  my-vpc  "), want: `name="my-vpc"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Text("name", tt.value); got != tt.want {
				t.Errorf("Text() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTrim(t *testing.T) {
	t.Parallel()

	if got := Trim(nil); got != "" {
		t.Errorf("Trim(nil) = %q, want empty", got)
	}
	if got, want := Trim(new("  spaced  ")), "spaced"; got != want {
		t.Errorf("Trim() = %q, want %q", got, want)
	}
}
