package dynamic

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/demiurgos-hub/navmesh-go/geom"
)

// normalizeRevisions rejects explicit stale writes; omitted revisions are
// assigned by the registry. Repeating the same desired object is idempotent.
func normalizeRevisions(old geom.Scene, next *geom.Scene) error {
	if err := revisions(old.Instances, next.Instances, func(v geom.Instance) string { return v.ID }, func(v *geom.Instance) *uint64 { return &v.Revision }); err != nil {
		return err
	}
	if err := revisions(old.Volumes, next.Volumes, func(v geom.Volume) string { return v.ID }, func(v *geom.Volume) *uint64 { return &v.Revision }); err != nil {
		return err
	}
	return revisions(old.Links, next.Links, func(v geom.Link) string { return v.ID }, func(v *geom.Link) *uint64 { return &v.Revision })
}

func revisions[T any](old, next []T, id func(T) string, revision func(*T) *uint64) error {
	previous := make(map[string]T, len(old))
	for _, v := range old {
		previous[id(v)] = v
	}
	for i := range next {
		v := next[i]
		before, exists := previous[id(v)]
		requested := *revision(&v)
		if !exists {
			if requested == 0 {
				*revision(&next[i]) = 1
			}
			continue
		}
		prior := *revision(&before)
		*revision(&v), *revision(&before) = 0, 0
		a, err := json.Marshal(v)
		if err != nil {
			return err
		}
		b, err := json.Marshal(before)
		if err != nil {
			return err
		}
		equal := bytes.Equal(a, b)
		if requested == 0 {
			if equal {
				*revision(&next[i]) = prior
				continue
			}
			if prior == ^uint64(0) {
				return fmt.Errorf("dynamic: object %q revision exhausted", id(v))
			}
			*revision(&next[i]) = prior + 1
		} else if requested < prior || (!equal && requested == prior) {
			return fmt.Errorf("dynamic: stale revision %d for %q (current %d)", requested, id(v), prior)
		}
	}
	return nil
}
