// Package finder holds the parts of reference resolution that do not vary by
// entity: the Interface shape every finder asserts against, iterator
// collection, cardinality diagnostics, and filter-description formatting.
//
// Entity-specific listing, matching and SDK parameter mapping stay in each
// <service>/lookup package, where they read better than any encoding of them
// would. Free of SDK and Terraform imports.
package finder

// the Interface shape every finder asserts against, iterator collection,
// cardinality diagnostics, and filter-description formatting. Entity-specific
// listing, matching and SDK parameter mapping stay in each <service>/lookup/
// file, where they read better than any encoding of them would.

import (
	"context"
	"fmt"
	"iter"
	"strings"
)

// Interface is the shape every entity finder implements: the same lookup at both
// cardinalities, over one filter type. Nothing consumes it at runtime; each
// entity file asserts against it so a finder that drifts fails to compile.
type Interface[F, R any] interface {
	Find(context.Context, F) ([]R, error)
	Resolve(context.Context, F) (R, error)
}

// Collect drains an SDK list iterator, keeping the candidates that satisfy keep.
// what is the resource noun ("VPC"); desc is the filter description, used only
// for error context.
func Collect[R any](seq iter.Seq2[*R, error], what, desc string, keep func(*R) bool) ([]R, error) {
	var matches []R
	for candidate, err := range seq {
		if err != nil {
			return nil, fmt.Errorf("list %ss for criteria (%s): %w", what, desc, err)
		}
		if candidate == nil {
			return nil, fmt.Errorf("list %ss for criteria (%s): SDK returned a nil candidate", what, desc)
		}
		if keep(candidate) {
			matches = append(matches, *candidate)
		}
	}
	return matches, nil
}

// ExactlyOne narrows candidates to a single result, or explains why it cannot.
func ExactlyOne[R any](candidates []R, what, desc string, summarize func(R) string) (R, error) {
	var zero R
	switch len(candidates) {
	case 0:
		return zero, fmt.Errorf("%s was not found matching criteria (%s)", what, desc)
	case 1:
		return candidates[0], nil
	}
	summaries := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		summaries = append(summaries, summarize(candidate))
	}
	return zero, fmt.Errorf(
		"%s matching criteria (%s) is ambiguous; multiple candidates matched: %s",
		what, desc, strings.Join(summaries, ", "),
	)
}

// Criteria joins the non-empty parts of a filter description.
func Criteria(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	if len(kept) == 0 {
		return "all"
	}
	return strings.Join(kept, ", ")
}

// Part renders "key=value" for an optional value, or "" when it is unset.
func Part[T any](key string, v *T) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%s=%v", key, *v)
}

// Text renders "key=\"value\"" for an optional string, treating blank as unset.
func Text(key string, v *string) string {
	s := Trim(v)
	if s == "" {
		return ""
	}
	return fmt.Sprintf("%s=%q", key, s)
}

// Trim returns the trimmed value of an optional string, or "" when it is unset.
func Trim(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}
