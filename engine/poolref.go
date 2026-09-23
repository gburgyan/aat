package engine

import (
	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/plan"
)

// withDomainPool returns sv with the pool its poolRef names filled in from the
// knowledge base. A value without a poolRef is returned unchanged.
func withDomainPool(sv plan.StepValue, kb *domain.KnowledgeBase) (plan.StepValue, error) {
	if sv.PoolRef == "" {
		return sv, nil
	}
	values, err := kb.PoolRefValues(sv.PoolRef)
	if err != nil {
		return sv, err
	}
	sv.Pool = make([]any, len(values))
	for i, v := range values {
		sv.Pool[i] = v
	}
	return sv, nil
}
