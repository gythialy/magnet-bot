package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gythialy/magnet/pkg/constant"
	"github.com/gythialy/magnet/pkg/utils"

	"github.com/go-resty/resty/v2"
)

// newAPITestCrawler serves a fake procurement API over TLS and points the
// crawler at it. The crawler builds absolute https URLs from the configured
// host, so a TLS test server is the smallest faithful stand-in for the real
// endpoint.
func newAPITestCrawler(t *testing.T, handler http.HandlerFunc) *Crawler {
	t.Helper()

	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)

	crawler := NewCrawler(testBotContext(strings.TrimPrefix(srv.URL, "https://")))
	crawler.client = resty.NewWithClient(srv.Client())
	return crawler
}

func writeJSON(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

// TestCrawlerProjectsPaginatesAndMaps covers the paging loop and the mapping
// of one API row into a Project, including absolute link building.
func TestCrawlerProjectsPaginatesAndMaps(t *testing.T) {
	var requestedPages []string

	crawler := newAPITestCrawler(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/freecms/rest/v1/notice/selectInfoMoreChannel.do" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		for _, name := range []string{"siteId", "channel", "pageSize", "currPage", "operationStartTime", "operationEndTime"} {
			if q.Get(name) == "" {
				t.Errorf("missing query parameter %q", name)
			}
		}
		requestedPages = append(requestedPages, q.Get("currPage"))

		switch q.Get("currPage") {
		case "1":
			writeJSON(t, w, map[string]any{"data": []map[string]any{
				{"title": "公告一", "openTenderCode": "C1", "noticeTime": "2026-01-02", "pageurl": "/p1.html", "content": "<b>正文</b>"},
				{"title": "公告二", "openTenderCode": "C2", "noticeTime": "2026-01-02", "pageurl": "p2.html", "content": "x"},
			}})
		case "2":
			writeJSON(t, w, map[string]any{"data": []map[string]any{
				{"title": "公告三", "openTenderCode": "C3", "noticeTime": "2026-01-03", "pageurl": "https://cdn.example.com/p3.html", "content": "y"},
			}})
		default:
			writeJSON(t, w, map[string]any{"data": []map[string]any{}})
		}
	})

	projects := crawler.Projects()

	if want := []string{"1", "2", "3"}; !slices.Equal(requestedPages, want) {
		t.Fatalf("requested pages = %v, want %v", requestedPages, want)
	}
	if len(projects) != 3 {
		t.Fatalf("got %d projects, want 3", len(projects))
	}

	host := crawler.ctx.Config.MessageServerUrl
	for i, want := range []string{
		"https://" + host + "/p1.html",
		"https://" + host + "/p2.html",
		"https://cdn.example.com/p3.html", // already absolute, kept as is
	} {
		if projects[i].Pageurl != want {
			t.Errorf("projects[%d].Pageurl = %q, want %q", i, projects[i].Pageurl, want)
		}
	}

	first := projects[0]
	if first.Title != "公告一" || first.ShortTitle != "公告一" {
		t.Errorf("title mapping wrong: %+v", first)
	}
	if first.OpenTenderCode != "C1" || first.NoticeTime != "2026-01-02" {
		t.Errorf("code/time mapping wrong: %+v", first)
	}
	if want, _ := utils.SimplifyHTML("<b>正文</b>", ""); first.Content != want {
		t.Errorf("content = %q, want %q", first.Content, want)
	}
}

// TestCrawlerProjectsFlagsComplexTable checks the hand-off to the PDF path: a
// notice whose table cannot be flattened keeps a placeholder in the body and
// sets HasComplexTable, so the pipeline attaches the original page instead.
func TestCrawlerProjectsFlagsComplexTable(t *testing.T) {
	crawler := newAPITestCrawler(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("currPage") != "1" {
			writeJSON(t, w, map[string]any{"data": []map[string]any{}})
			return
		}
		var table strings.Builder
		table.WriteString("<table><tr>")
		for i := range 7 {
			fmt.Fprintf(&table, "<th>h%d</th>", i)
		}
		table.WriteString("</tr><tr>")
		for i := range 7 {
			fmt.Fprintf(&table, "<td>c%d</td>", i)
		}
		table.WriteString("</tr></table>")
		writeJSON(t, w, map[string]any{"data": []map[string]any{
			{"title": "带大表格的公告", "openTenderCode": "C9", "pageurl": "/p9.html", "content": table.String()},
		}})
	})

	projects := crawler.Projects()
	if len(projects) != 1 {
		t.Fatalf("got %d projects, want 1", len(projects))
	}
	if !projects[0].HasComplexTable {
		t.Error("HasComplexTable = false, want true for a 7-column table")
	}
	if !strings.Contains(projects[0].Content, "表格较大") {
		t.Errorf("body is missing the placeholder: %q", projects[0].Content)
	}
	if strings.Contains(projects[0].Content, "c0") {
		t.Errorf("complex table was flattened into the body: %q", projects[0].Content)
	}
}

// TestCrawlerProjectsStopsOnError verifies that a failing page does not panic
// and that already fetched pages are kept.
func TestCrawlerProjectsStopsOnError(t *testing.T) {
	t.Run("first page fails", func(t *testing.T) {
		crawler := newAPITestCrawler(t, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		})
		if projects := crawler.Projects(); len(projects) != 0 {
			t.Fatalf("got %d projects, want 0", len(projects))
		}
	})

	t.Run("later page fails", func(t *testing.T) {
		crawler := newAPITestCrawler(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("currPage") == "1" {
				writeJSON(t, w, map[string]any{"data": []map[string]any{
					{"title": "公告一", "openTenderCode": "C1", "pageurl": "/p1.html"},
				}})
				return
			}
			http.Error(w, "boom", http.StatusInternalServerError)
		})

		projects := crawler.Projects()
		if len(projects) != 1 || projects[0].Title != "公告一" {
			t.Fatalf("got %+v, want the first page only", projects)
		}
	})
}

func alarmRow(code, noticeID, originNoticeID string) map[string]any {
	return map[string]any{
		"creditName":     "公司" + code,
		"creditCode":     code,
		"id":             "BIZ-" + code,
		"noticeId":       noticeID,
		"originNoticeId": originNoticeID,
		"startDate":      "2026-01-01",
		"endDate":        "",
	}
}

// TestCrawlerAlarmsDedupesAndCachesDetails covers the merged break-faith and
// suspend loops: a credit code listed twice is pushed once, and a notice detail
// shared by several alarms is fetched once.
func TestCrawlerAlarmsDedupesAndCachesDetails(t *testing.T) {
	var (
		mu          sync.Mutex
		detailCalls = map[string]int{}
	)

	crawler := newAPITestCrawler(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/freecms/rest/v1/punish/queryPunishList.do":
			switch r.URL.Query().Get("channel") {
			case constant.CreditTypeBreakFaith.String():
				writeJSON(t, w, map[string]any{"data": map[string]any{"rows": []map[string]any{
					alarmRow("CODE-1", "N1", ""),
					alarmRow("CODE-2", "N2", "N1"), // origin notice is shared with CODE-1
				}}})
			case constant.CreditTypeSuspend.String():
				writeJSON(t, w, map[string]any{"data": map[string]any{"rows": []map[string]any{
					alarmRow("CODE-1", "N1", ""), // duplicate across lists
					alarmRow("CODE-3", "N3", ""),
				}}})
			default:
				t.Errorf("unexpected channel %q", r.URL.Query().Get("channel"))
				http.NotFound(w, r)
			}
		case "/freecms/rest/v1/punish/selectByNoticeId.do":
			id := r.URL.Query().Get("punishNoticeId")
			mu.Lock()
			detailCalls[id]++
			mu.Unlock()
			writeJSON(t, w, map[string]any{"data": map[string]any{
				"title":   "标题" + id,
				"pageurl": "/d/" + id + ".html",
			}})
		default:
			http.NotFound(w, r)
		}
	})

	alarms := crawler.Alarms([]string{"中国"}, 42)

	if len(alarms) != 3 {
		t.Fatalf("got %d alarms, want 3 (CODE-1 must be deduplicated)", len(alarms))
	}
	wantOrder := []string{"CODE-1", "CODE-2", "CODE-3"}
	for i, want := range wantOrder {
		if alarms[i].CreditCode != want {
			t.Errorf("alarms[%d].CreditCode = %q, want %q", i, alarms[i].CreditCode, want)
		}
		if alarms[i].UserID != 42 {
			t.Errorf("alarms[%d].UserID = %d, want 42", i, alarms[i].UserID)
		}
	}

	// The detail of N1 is needed for CODE-1's title and for CODE-2's origin
	// notice; the per-run cache must serve it once.
	mu.Lock()
	defer mu.Unlock()
	if detailCalls["N1"] != 1 {
		t.Errorf("notice N1 detail fetched %d times, want 1", detailCalls["N1"])
	}
	for _, id := range []string{"N2", "N3"} {
		if detailCalls[id] != 1 {
			t.Errorf("notice %s detail fetched %d times, want 1", id, detailCalls[id])
		}
	}

	host := crawler.ctx.Config.MessageServerUrl
	if got, want := alarms[0].Title, "标题N1"; got == nil || *got != want {
		t.Errorf("alarms[0].Title = %v, want %q", got, want)
	}
	if got, want := alarms[0].PageUrl1, "https://"+host+"/d/N1.html"; got != want {
		t.Errorf("alarms[0].PageUrl1 = %q, want %q", got, want)
	}
	if alarms[1].PageUrl2 == nil || *alarms[1].PageUrl2 != "https://"+host+"/d/N1.html" {
		t.Errorf("alarms[1].PageUrl2 = %v, want the origin notice link", alarms[1].PageUrl2)
	}
}

// TestCrawlerProjectsTimesOut guards the request timeout: a hung API must not
// block the scheduled crawl forever. The default is 30s, so the test lowers it.
func TestCrawlerProjectsTimesOut(t *testing.T) {
	// The handler returns once the client gives up, so srv.Close does not wait
	// for a request that is deliberately held open.
	crawler := newAPITestCrawler(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	crawler.client.SetTimeout(50 * time.Millisecond)

	start := time.Now()
	projects := crawler.Projects()
	elapsed := time.Since(start)

	if len(projects) != 0 {
		t.Fatalf("got %d projects from a hung API, want 0", len(projects))
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Projects() blocked for %v; the request timeout is not applied", elapsed)
	}
}
