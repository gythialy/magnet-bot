package rule

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/gythialy/magnet/pkg/utils"

	"github.com/gythialy/magnet/pkg/model"
)

type ComplexRule struct {
	IncludeTerms map[string]struct{}
	ExcludeTerms map[string]struct{}
	Rule         *model.Keyword
}

// sortKey is the precomputed ordering key of a rule, so sorting does not
// rebuild the include-term slice and rescan the maps on every comparison.
type sortKey struct {
	hasTenderCode bool
	hasExclude    bool
	includeTerms  []string
}

func (r *ComplexRule) newSortKey() sortKey {
	return sortKey{
		hasTenderCode: r.hasTenderCode(),
		hasExclude:    len(r.ExcludeTerms) > 0,
		includeTerms:  slices.Sorted(maps.Keys(r.IncludeTerms)),
	}
}

// SortComplexRules orders rules by descending specificity: rules containing a
// tender code first, then rules with exclude terms, then lexicographically by
// their include terms. Equal keys keep their input order.
func SortComplexRules(rules []*ComplexRule) {
	keys := make(map[*ComplexRule]sortKey, len(rules))
	for _, r := range rules {
		keys[r] = r.newSortKey()
	}
	slices.SortStableFunc(rules, func(a, b *ComplexRule) int {
		ka, kb := keys[a], keys[b]
		if ka.hasTenderCode != kb.hasTenderCode {
			if ka.hasTenderCode {
				return -1
			}
			return 1
		}
		if ka.hasExclude != kb.hasExclude {
			if ka.hasExclude {
				return -1
			}
			return 1
		}
		return slices.Compare(ka.includeTerms, kb.includeTerms)
	})
}

// hasTenderCode reports whether any include term looks like a tender code.
func (r *ComplexRule) hasTenderCode() bool {
	for term := range r.IncludeTerms {
		if utils.TenderCodeRegex.MatchString(term) {
			return true
		}
	}
	return false
}

func (cr *ComplexRule) MarshalJSON() ([]byte, error) {
	temp := struct {
		IncludeTerms []string `json:"includeTerms"`
		ExcludeTerms []string `json:"excludeTerms"`
	}{
		IncludeTerms: slices.Sorted(maps.Keys(cr.IncludeTerms)),
		ExcludeTerms: slices.Sorted(maps.Keys(cr.ExcludeTerms)),
	}

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
	cr.IncludeTerms = sliceToSet(temp.IncludeTerms)
	cr.ExcludeTerms = sliceToSet(temp.ExcludeTerms)
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
	var sb strings.Builder
	for _, term := range slices.Sorted(maps.Keys(cr.IncludeTerms)) {
		sb.WriteString("+")
		sb.WriteString(term)
		sb.WriteString(" ")
	}
	for _, term := range slices.Sorted(maps.Keys(cr.ExcludeTerms)) {
		sb.WriteString("-")
		sb.WriteString(term)
		sb.WriteString(" ")
	}
	return strings.TrimSpace(sb.String())
}

func sliceToSet(s []string) map[string]struct{} {
	m := make(map[string]struct{}, len(s))
	for _, v := range s {
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
		if excluded, ok := strings.CutPrefix(term, "-"); ok {
			rule.ExcludeTerms[excluded] = struct{}{}
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
