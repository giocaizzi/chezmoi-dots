package matrix

import (
	"maps"
	"slices"
)

// DeclaredProfiles returns the sorted profile names the `matrix` table of the chezmoi
// data declares; nil when it declares none. Malformed declarations are not reported
// here (lint matrix does): only the well-formed profile tables count.
func DeclaredProfiles(data map[string]any) []string {
	spec, _ := ParseSpec(data)
	if len(spec.Profile) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(spec.Profile))
}
