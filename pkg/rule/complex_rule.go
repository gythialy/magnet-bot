package rule

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/gythialy/magnet/pkg/utils"

	"github.com/gythialy/magnet/pkg/model"
)

type ComplexRule struct {
	IncludeTerms map[string]struct{}
	ExcludeTerms map[string]struct{}
	Rule         *model.Keyword
}

type ComplexRules []*ComplexRule

func (r ComplexRules) Len() int      { return len(r) }
func (r ComplexRules) Swap(i, j int) { r[i], r[j] = r[j], r[i] }
func (r ComplexRules) Less(i, j int) bool {
	// Check if rules have a tender code pattern
	iHasTenderCode := r[i].hasTenderCode()
	jHasTenderCode := r[j].hasTenderCode()

	// If one has tender code and the other doesn't, tender code has higher priority
	if iHasTenderCode != jHasTenderCode {
		return iHasTenderCode
	}

	// If both have or don't have tender codes, check ExcludeTerms
	iHasExclude := len(r[i].ExcludeTerms) > 0
	jHasExclude := len(r[j].ExcludeTerms) > 0

	// If one has exclude terms and the other doesn't, exclude terms has higher priority
	if iHasExclude != jHasExclude {
		return iHasExclude
	}

	// If all above conditions are equal, sort by IncludeTerms
	iTerms := r[i].getIncludeTermsSorted()
	jTerms := r[j].getIncludeTermsSorted()

	// Compare terms lexicographically
	minLen := min(len(iTerms), len(jTerms))
	for k := 0; k < minLen; k++ {
		if iTerms[k] != jTerms[k] {
			return iTerms[k] < jTerms[k]
		}
	}
	return len(iTerms) < len(jTerms)
}

// Helper method to check if rule has a tender code pattern
func (r *ComplexRule) hasTenderCode() bool {
	for term := range r.IncludeTerms {
		if utils.TenderCodeRegex.MatchString(term) {
			return true
		}
	}
	return false
}

// Helper method to get sorted include terms
func (r *ComplexRule) getIncludeTermsSorted() []string {
	terms := make([]string, 0, len(r.IncludeTerms))
	for term := range r.IncludeTerms {
		terms = append(terms, term)
	}
	sort.Strings(terms)
	return terms
}

func SortComplexRules(rules []*ComplexRule) {
	sort.Sort(ComplexRules(rules))
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
	data = normalizeString(data)
	for term := range cr.ExcludeTerms {
		if strings.Contains(data, term) {
			return false
		}
	}

	for term := range cr.IncludeTerms {
		if !strings.Contains(data, term) {
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

func NewComplexRule(k *model.Keyword) *ComplexRule {
	rule := &ComplexRule{
		IncludeTerms: make(map[string]struct{}),
		ExcludeTerms: make(map[string]struct{}),
		Rule:         k,
	}

	// Split the input string, but keep quoted parts together
	terms := splitPreservingQuotes(rule.Rule.Keyword)
	for _, term := range terms {
		term = normalizeString(term)
		if term == "" {
			continue
		}
		if strings.HasPrefix(term, "-") {
			rule.ExcludeTerms[strings.TrimPrefix(term, "-")] = struct{}{}
		} else {
			// Remove '+' if present, otherwise keep the term as is
			rule.IncludeTerms[strings.TrimPrefix(term, "+")] = struct{}{}
		}
	}

	return rule
}

// splitPreservingQuotes splits a string by whitespace but preserves quoted parts
func splitPreservingQuotes(s string) []string {
	var result []string
	var current strings.Builder
	inQuotes := false

	for _, r := range s {
		if unicode.IsSpace(r) && !inQuotes {
			if current.Len() > 0 {
				result = append(result, current.String())
				current.Reset()
			}
		} else if r == '"' {
			inQuotes = !inQuotes
		} else {
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		result = append(result, current.String())
	}

	return result
}

// normalizeString removes all types of spaces from a string
// converts Chinese brackets to English brackets
func normalizeString(s string) string {
	tmp := strings.ReplaceAll(s, "（", "(")
	tmp = strings.ReplaceAll(tmp, "）", ")")
	tmp = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1 // Drop the space
		}
		return r
	}, tmp)
	return tmp
}
