package domain

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gburgyan/aat/internal/yamlx"
)

// PoolRefValues returns the values a poolRef names: a value pool, all of it
// with its groups ("airportCodes"), or one of its groups ("airportCodes.us").
// It is safe on a nil KnowledgeBase, which has no pools.
func (kb *KnowledgeBase) PoolRefValues(ref string) ([]string, error) {
	name, group, grouped := strings.Cut(ref, ".")
	if kb == nil {
		return nil, fmt.Errorf("poolRef %q needs a domain file, and the project has none", ref)
	}
	pool := kb.GetPool(name)
	if pool == nil {
		return nil, fmt.Errorf("poolRef %q: the domain file has no value pool %q%s", ref, name, didYouMean(name, sortedKeys(kb.ValuePools)))
	}
	if !grouped {
		return kb.AllValues(name), nil
	}
	values, ok := pool.Groups[group]
	if !ok {
		if len(pool.Groups) == 0 {
			return nil, fmt.Errorf("poolRef %q: value pool %q has no groups", ref, name)
		}
		groups := make([]string, 0, len(pool.Groups))
		for g := range pool.Groups {
			groups = append(groups, g)
		}
		sort.Strings(groups)
		return nil, fmt.Errorf("poolRef %q: value pool %q has no group %q (it has %s)", ref, name, group, strings.Join(groups, ", "))
	}
	return values, nil
}

// didYouMean suggests the name in names that name most likely misspells.
func didYouMean(name string, names []string) string {
	if c := yamlx.Closest(name, names); c != "" {
		return fmt.Sprintf(" (did you mean %q?)", c)
	}
	return ""
}
