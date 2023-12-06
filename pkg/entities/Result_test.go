package entities

import (
	"fmt"
	md "github.com/JohannesKaufmann/html-to-markdown"
	"index/suffixarray"
	"regexp"
	"strings"
	"testing"
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

func TestResults_ToMarkdown(t *testing.T) {
	//converter := md.NewConverter("", true, nil)
	content := "<strong>Important</strong>"
	//content = regexp.MustCompile(`\r\n`).ReplaceAllString(content, "")
	//content = strings.ReplaceAll(content, "\\r\\n", "")
	//content = strings.ReplaceAll(content, "\\t", "")
	converter := md.NewConverter("", true, nil)
	if s, err := converter.ConvertString(content); err == nil {
		t.Log(s)
	} else {
		t.Fatal(err)
	}
	r := []*Result{
		{
			OpenTenderCode: "2023-JQ01-W1313",
			Title:          "某部仓储建设征求意见",
			Pageurl:        "http://www.baidu.com/1",
			Content:        content,
		},
		{
			OpenTenderCode: "CODE2",
			Title:          "2",
			Pageurl:        "http://www.baidu.com/2",
			Content:        "Arguments may evaluate to any type; if they are pointers the implementation automatically indirects to the base type when required. If an evaluation yields a function value, such as a function-valued field of a struct, the function is not invoked automatically, but it can be used as a truth value for an if action and the like. To invoke it, use the call function, defined below.",
		},
		{
			OpenTenderCode: "CODE3",
			Title:          "3",
			Pageurl:        "http://www.baidu.com/3",
			Content:        "Arguments may evaluate to any type; if they are pointers the implementation automatically indirects to the base type when required. If an evaluation yields a function value, such as a function-valued field of a struct, the function is not invoked automatically, but it can be used as a truth value for an if action and the like. To invoke it, use the call function, defined below.",
		},
	}
	results := NewResults(r)
	results.KeywordResults = r
	results.TenderResults = r
	s := results.ToMarkdown()
	t.Log(len(s))
}

func TestResults_Filter(t *testing.T) {
	r := []*Result{
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
	results := NewResults(r)

	results.Filter([]string{"信息"}, []string{"W1313", "CODE11"})
	for _, v := range results.TenderResults {
		t.Log(v.OpenTenderCode, v.Title, v.Pageurl)
	}

	for _, v := range results.KeywordResults {
		t.Log(v.OpenTenderCode, v.Title, v.Pageurl)
	}
}
