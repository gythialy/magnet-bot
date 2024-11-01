package model

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestAlarm_ToMarkdown(t *testing.T) {
	alarm := `{
    "CreditName": "达到发达的方式大所",
    "CreditCode": "dafdasdfdsafdadE",
    "StartDate": "2023-10-23T00:00:00Z",
    "EndDate": null,
    "DetailReason": "打发打发防掉发速度案发当时多少",
    "HandleDepartment": "嘎咕嘎",
    "HandleUnit": "达尔文全额千万个",
    "HandleResult": "啊打发掉沙发上大法师大法师",
	"Title":"哈哈哈",
	"PageUrl1": "https://a.com/111",
	"PageUrl2": "https://a.com/222",
	"OriginNoticeID": "1111111111",
	"NoticeID": "2222222"
  }`
	var a Alarm
	if err := json.Unmarshal([]byte(alarm), &a); err != nil {
		t.Fatal(err)
	}

	if markdown, err := a.ToTelegramMessage(); err == nil {
		fmt.Println(markdown)
	} else {
		t.Fatal(err)
	}
}
