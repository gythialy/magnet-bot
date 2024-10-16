package rule

import (
	"testing"
)

func TestComplexRule_Match(t *testing.T) {
	tests := []struct {
		name     string
		rule     string
		testData []struct {
			input    string
			expected bool
		}
	}{
		{
			name: "Chinese - Simple include",
			rule: "苹果",
			testData: []struct {
				input    string
				expected bool
			}{
				{"我喜欢吃苹果", true},
				{"香蕉是黄色的", false},
			},
		},
		{
			name: "Chinese - Simple exclude",
			rule: "-香蕉",
			testData: []struct {
				input    string
				expected bool
			}{
				{"我喜欢吃苹果", true},
				{"香蕉是黄色的", false},
			},
		},
		{
			name: "Chinese - Multiple includes",
			rule: "苹果 橙子",
			testData: []struct {
				input    string
				expected bool
			}{
				{"我喜欢苹果和橙子", true},
				{"我喜欢苹果", false},
				{"香蕉是黄色的", false},
			},
		},
		{
			name: "Chinese - Multiple excludes",
			rule: "-香蕉 -葡萄",
			testData: []struct {
				input    string
				expected bool
			}{
				{"我喜欢苹果", true},
				{"香蕉是黄色的", false},
				{"葡萄是紫色的", false},
			},
		},
		{
			name: "Chinese - Mixed includes and excludes",
			rule: "苹果 -香蕉 橙子",
			testData: []struct {
				input    string
				expected bool
			}{
				{"我喜欢苹果和橙子", true},
				{"我喜欢苹果和香蕉", false},
				{"我喜欢橙子", false},
			},
		},
		{
			name: "Mixed English and Chinese",
			rule: "apple 橙子 -banana -葡萄",
			testData: []struct {
				input    string
				expected bool
			}{
				{"I like apples and 橙子", true},
				{"我喜欢苹果和橙子", false},
				{"Bananas are yellow", false},
				{"葡萄很好吃", false},
				{"I like apples but not 葡萄", false},
			},
		},
		{
			name: "Empty rule",
			rule: "",
			testData: []struct {
				input    string
				expected bool
			}{
				{"Any string", true},
				{"任何字符串", true},
				{"", true},
			},
		},
		{
			name: "2023-KK01-W1295",
			rule: "2023-KK01-W1295",
			testData: []struct {
				input    string
				expected bool
			}{
				{"测试生生世世是（2023-KK01-W1295）", true},
				{"测试生生世世是     (2023-KK01-W1295)", true},
				{"测试生生世世是 2023-KK01-W1295", true},
				{"哈哈哈哈哈(2023-KK02-W1295)", false},
			},
		},
		{
			name: "Multiple hyphenated terms",
			rule: "2023-KK01-W1295 2023-JQ05-F1194",
			testData: []struct {
				input    string
				expected bool
			}{
				{"测试 2023-KK01-W1295 和 2023-JQ05-F1194", true},
				{"只有 2023-KK01-W1295", false},
				{"只有 2023-JQ05-F1194", false},
				{"都没有", false},
			},
		},
		{
			name: "Mixed hyphenated and quoted terms",
			rule: `2023-KK01-W1295 "apple pie" -"banana split"`,
			testData: []struct {
				input    string
				expected bool
			}{
				{"2023-KK01-W1295 with apple pie", true},
				{"2023-KK01-W1295 但是没有 apple pie", true},
				{"apple pie 但是没有 2023-KK01-W1295", true},
				{"2023-KK01-W1295 with banana split", false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := NewComplexRule(tt.rule)
			for _, td := range tt.testData {
				result := rule.IsMatch(td.input)
				if result != td.expected {
					t.Errorf("Rule '%s' with input '%s': expected %v, got %v", tt.rule, td.input, td.expected, result)
				}
			}
		})
	}
}

func TestNewComplexRule(t *testing.T) {
	tests := []struct {
		name            string
		ruleString      string
		expectedInclude map[string]struct{}
		expectedExclude map[string]struct{}
	}{
		{
			name:       "Simple include",
			ruleString: "apple",
			expectedInclude: map[string]struct{}{
				"apple": {},
			},
			expectedExclude: map[string]struct{}{},
		},
		{
			name:            "Simple exclude",
			ruleString:      "-banana",
			expectedInclude: map[string]struct{}{},
			expectedExclude: map[string]struct{}{
				"banana": {},
			},
		},
		{
			name:       "Multiple includes",
			ruleString: "apple orange grape",
			expectedInclude: map[string]struct{}{
				"apple":  {},
				"orange": {},
				"grape":  {},
			},
			expectedExclude: map[string]struct{}{},
		},
		{
			name:            "Multiple excludes",
			ruleString:      "-apple -banana",
			expectedInclude: map[string]struct{}{},
			expectedExclude: map[string]struct{}{
				"apple":  {},
				"banana": {},
			},
		},
		{
			name:       "Mixed includes and excludes",
			ruleString: "apple -banana orange",
			expectedInclude: map[string]struct{}{
				"apple":  {},
				"orange": {},
			},
			expectedExclude: map[string]struct{}{
				"banana": {},
			},
		},
		{
			name:       "Case insensitivity",
			ruleString: "ApPlE -BaNaNa",
			expectedInclude: map[string]struct{}{
				"ApPlE": {},
			},
			expectedExclude: map[string]struct{}{
				"BaNaNa": {},
			},
		},
		{
			name:       "Chinese characters",
			ruleString: "苹果 -香蕉",
			expectedInclude: map[string]struct{}{
				"苹果": {},
			},
			expectedExclude: map[string]struct{}{
				"香蕉": {},
			},
		},
		{
			name:       "Mixed English and Chinese",
			ruleString: "apple 橙子 -banana -葡萄",
			expectedInclude: map[string]struct{}{
				"apple": {},
				"橙子":    {},
			},
			expectedExclude: map[string]struct{}{
				"banana": {},
				"葡萄":     {},
			},
		},
		{
			name:            "Empty rule",
			ruleString:      "",
			expectedInclude: map[string]struct{}{},
			expectedExclude: map[string]struct{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := NewComplexRule(tt.ruleString)

			// Check includes
			if len(rule.IncludeTerms) != len(tt.expectedInclude) {
				t.Errorf("Expected %d include terms, got %d", len(tt.expectedInclude), len(rule.IncludeTerms))
			}
			for term := range tt.expectedInclude {
				if _, exists := rule.IncludeTerms[term]; !exists {
					t.Errorf("Expected include term '%s' not found", term)
				}
			}

			// Check excludes
			if len(rule.ExcludeTerms) != len(tt.expectedExclude) {
				t.Errorf("Expected %d exclude terms, got %d", len(tt.expectedExclude), len(rule.ExcludeTerms))
			}
			for term := range tt.expectedExclude {
				if _, exists := rule.ExcludeTerms[term]; !exists {
					t.Errorf("Expected exclude term '%s' not found", term)
				}
			}
		})
	}
}

func TestComplexRule_ToString(t *testing.T) {
	tests := []struct {
		name     string
		rule     string
		expected string
	}{
		{
			name:     "Simple include",
			rule:     "apple",
			expected: "+apple",
		},
		{
			name:     "Simple exclude",
			rule:     "-banana",
			expected: "-banana",
		},
		{
			name:     "Multiple includes",
			rule:     "apple orange",
			expected: "+apple +orange",
		},
		{
			name:     "Multiple excludes",
			rule:     "-banana -grape",
			expected: "-banana -grape",
		},
		{
			name:     "Mixed includes and excludes",
			rule:     "apple -banana orange",
			expected: "+apple +orange -banana",
		},
		{
			name:     "Empty rule",
			rule:     "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := NewComplexRule(tt.rule)
			result := rule.ToString()
			if result != tt.expected {
				t.Errorf("Expected '%s', but got '%s'", tt.expected, result)
			}
		})
	}
}
