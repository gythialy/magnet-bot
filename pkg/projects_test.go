package pkg

import (
	"fmt"
	"index/suffixarray"
	"regexp"
	"strings"
	"testing"

	"github.com/gythialy/magnet/pkg/entities"
	"gorm.io/gorm"

	"github.com/gythialy/magnet/pkg/rule"
)

func TestMatch2(t *testing.T) {
	patterns := []string{
		"mercury", "venus", "earth", "mars",
		"jupiter", "saturn", "uranus", "pluto",
	}
	r := regexp.MustCompile(strings.Join(patterns, "|"))
	index := suffixarray.New([]byte(`XXearthXXvenusaturnXX`))
	res := index.FindAllIndex(r, -1)
	fmt.Println("found patterns", res)
}

func TestResults_Filter(t *testing.T) {
	r := []*Project{
		{
			OpenTenderCode: "W1313",
			Title:          "某部仓储建设征求意见公告",
			Pageurl:        "http://www.baidu.com/1",
			Content:        "content",
		},
		{
			OpenTenderCode: "CODE2",
			Title:          "2",
			Pageurl:        "http://www.baidu.com/2",
			Content:        "Arguments may evaluate to any type; if they are pointers the implementation automatically indirects to the base type when required. If an evaluation yields a function value, such as a function-valued field of a struct, the function is not invoked automatically, but it can be used as a truth value for an if action and the like. To invoke it, use the call function, defined below.",
		},
		{
			OpenTenderCode: "CODE3",
			Title:          "某部综合信息系统(二次)公告",
			Pageurl:        "http://www.baidu.com/3",
			Content:        "Arguments may evaluate to any type; if they are pointers the implementation automatically indirects to the base type when required. If an evaluation yields a function value, such as a function-valued field of a struct, the function is not invoked automatically, but it can be used as a truth value for an if action and the like. To invoke it, use the call function, defined below.",
		},
	}
	results := NewProjects(nil, r, []*rule.ComplexRule{rule.NewComplexRule(&entities.Keyword{
		Model:   gorm.Model{},
		Keyword: "信息",
		UserId:  0,
		Type:    0,
		Counter: 0,
	})})

	// results.filter()

	for _, v := range results.keywordProjects {
		t.Log(v.OpenTenderCode, v.Title, v.Pageurl)
	}
}

func TestMergeLines(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "Merge lines between (一) and (二)",
			input: `（五）不得参加采购活动。

五、招标文件申领时间、地点、方式

(一)申领时间:
2024年10月22日
至
2024年10月28日
，每天上午
08:30
至
12:00
，下午
14:00
至
17:30
(北京时间,日历日)

(二)申领地址(社会代理机构):
河北省秦皇岛市海港区北环路

(三)申领方式:线下申领`,
			expected: `（五）不得参加采购活动。

五、招标文件申领时间、地点、方式

(一)申领时间: 2024年10月22日 至 2024年10月28日 ，每天上午 08:30 至 12:00 ，下午 14:00 至 17:30 (北京时间,日历日)

(二)申领地址(社会代理机构):
河北省秦皇岛市海港区北环路

(三)申领方式:线下申领`,
		},
		// ... (keep the other test cases unchanged)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := cleanContent(tc.input)
			if result != tc.expected {
				t.Errorf("cleanContent() produced unexpected result.\nGot:\n%s\nWant:\n%s", result, tc.expected)
			}
		})
	}
}
