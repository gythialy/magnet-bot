package model

import (
	"bytes"
	"html/template"
)

const (
	alarmTemplate = `#{{.CreditName}} (#{{.CreditCode}}) 
开始时间: {{ .StartDate.Format "2006-01-02" }} 
{{if .EndDate }}结束时间: {{ .EndDate.Format "2006-01-02" }}{{ end }} 
{{if .HandleDepartment }}处罚部门: {{ .HandleDepartment }}{{ end }} 
{{if .DetailReason }}具体情形: {{ .DetailReason }}{{ end }} 
{{if .HandleResult }}处罚结果: {{ .HandleResult }}{{ end }} `
)

var alarmRender = template.Must(template.New("alarm_template").Parse(alarmTemplate))

type AlarmResult struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Rows []struct {
			ID               string `json:"id"`
			CreditType       string `json:"creditType"`
			CreditName       string `json:"creditName"`
			CreditCode       string `json:"creditCode"`
			Address          string `json:"address"`
			StartDate        string `json:"startDate"`
			EndDate          string `json:"endDate"`
			DetailReason     string `json:"detailReason"`
			HandleDepartment string `json:"handleDepartment"`
			LawBasic         any    `json:"lawBasic"`
			HandleUnit       string `json:"handleUnit"`
			HandleResult     string `json:"handleResult"`
		} `json:"rows"`
		Total       int `json:"total"`
		Information any `json:"information"`
	} `json:"data"`
}

func (a *Alarm) ToMarkdown() (string, error) {
	var buf bytes.Buffer
	if err := alarmRender.Execute(&buf, a); err == nil {
		return buf.String(), nil
	} else {
		return "", err
	}
}
