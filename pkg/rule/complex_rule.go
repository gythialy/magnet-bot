package rule

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"
)

type ComplexRule struct {
	IncludeTerms map[string]struct{}
	ExcludeTerms map[string]struct{}
}

// Use sync.Pool to reuse slices during marshaling and unmarshaling
var slicePool = sync.Pool{
	New: func() interface{} {
		return make([]string, 0, 30)
	},
}

func (cr *ComplexRule) MarshalJSON() ([]byte, error) {
	temp := struct {
		IncludeTerms []string `json:"includeTerms"`
		ExcludeTerms []string `json:"excludeTerms"`
	}{
		IncludeTerms: *keysToSlice(cr.IncludeTerms),
		ExcludeTerms: *keysToSlice(cr.ExcludeTerms),
	}

	defer slicePool.Put(keysToSlice(cr.IncludeTerms))
	defer slicePool.Put(keysToSlice(cr.ExcludeTerms))

	return json.Marshal(temp)
}

func (cr *ComplexRule) UnmarshalJSON(data []byte) error {
	var temp struct {
		IncludeTerms []string `json:"includeTerms"`
		ExcludeTerms []string `json:"excludeTerms"`
	}
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}
	cr.IncludeTerms = sliceToMap(&temp.IncludeTerms)
	cr.ExcludeTerms = sliceToMap(&temp.ExcludeTerms)
	return nil
}

// IsMatch checks if the given data matches the rule.
//
// It first checks if any of the exclude terms are present in the data. If so,
// it returns false. Then it checks if all the include terms are present in
// the data. If any of them are not, it returns false. If both checks pass, it
// returns true.
func (cr *ComplexRule) IsMatch(data string) bool {
	lowerData := strings.Map(unicode.ToLower, data)

	for term := range cr.ExcludeTerms {
		if strings.Contains(lowerData, term) {
			return false
		}
	}

	for term := range cr.IncludeTerms {
		if !strings.Contains(lowerData, term) {
			return false
		}
	}

	return true
}

func (cr *ComplexRule) ToString() string {
	var parts []string

	// Handle include terms
	var includeTerms []string
	for term := range cr.IncludeTerms {
		includeTerms = append(includeTerms, term)
	}
	sort.Strings(includeTerms)
	for _, term := range includeTerms {
		parts = append(parts, fmt.Sprintf("+%s", term))
	}

	// Handle exclude terms
	var excludeTerms []string
	for term := range cr.ExcludeTerms {
		excludeTerms = append(excludeTerms, term)
	}
	sort.Strings(excludeTerms)
	for _, term := range excludeTerms {
		parts = append(parts, fmt.Sprintf("-%s", term))
	}

	return strings.Join(parts, " ")
}

func keysToSlice(m map[string]struct{}) *[]string {
	slice := slicePool.Get().(*[]string)
	*slice = (*slice)[:0] // Reset the slice length
	for k := range m {
		*slice = append(*slice, k)
	}
	return slice
}

func sliceToMap(s *[]string) map[string]struct{} {
	m := make(map[string]struct{}, len(*s)) // Pre-allocate map with known size
	for _, v := range *s {
		m[v] = struct{}{}
	}
	return m
}

func NewComplexRule(r string) *ComplexRule {
	rule := &ComplexRule{
		IncludeTerms: make(map[string]struct{}),
		ExcludeTerms: make(map[string]struct{}),
	}

	terms := strings.Fields(r)
	for _, term := range terms {
		if term == "" {
			continue
		}
		if strings.HasPrefix(term, "-") {
			rule.ExcludeTerms[strings.ToLower(strings.TrimPrefix(term, "-"))] = struct{}{}
		} else {
			// Remove '+' if present, otherwise keep the term as is
			rule.IncludeTerms[strings.ToLower(strings.TrimPrefix(term, "+"))] = struct{}{}
		}
	}

	return rule
}
