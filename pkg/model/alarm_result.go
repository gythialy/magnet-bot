package model

import (
	"bytes"
	"html/template"
)

const (
	alarmTemplate = `{{if .Title }}<b>{{ .Title }}</b>{{ end }}  
#{{.CreditName}} (#{{.CreditCode}}) 
开始时间: {{ .StartDate.Format "2006-01-02" }}
{{if .EndDate }}结束时间: {{ .EndDate.Format "2006-01-02" }}{{ end }}
{{if .HandleDepartment }}处罚部门: {{ .HandleDepartment }}{{ end }} 
{{if .DetailReason }}具体情形: {{ .DetailReason }}{{ end }} 
{{if .HandleResult }}处罚结果: {{ .HandleResult }}{{ end }} 
{{if .PageUrl1 }}相关链接1: <a href="{{.PageUrl1}}">{{ .NoticeID }}</a>{{ end }}  
{{if .PageUrl2 }}相关链接2: <a href="{{.PageUrl2}}">{{ .OriginNoticeID }}</a>{{ end }} `
)

var alarmRender = template.Must(template.New("alarm_template").Parse(alarmTemplate))

type AlarmList struct {
	Code string `json:"code"`
	Data struct {
		Total       int `json:"total"`
		Information struct {
			Overdue string `json:"overdue"`
			Env     string `json:"env"`
			Status  string `json:"status"`
		} `json:"information"`
		Rows []struct {
			BreedCategory      string `json:"breedCategory"`
			Address            string `json:"address"`
			Code               string `json:"code"`
			EndDate            string `json:"endDate"`
			HandleResult       string `json:"handleResult"`
			Type               string `json:"type"`
			CreditType         string `json:"creditType"`
			DetailReason       string `json:"detailReason"`
			NoticeID           string `json:"noticeId"`
			HandleScopeFirst   string `json:"handleScopeFirst"`
			CreditCode         string `json:"creditCode"`
			HandleDepartment   string `json:"handleDepartment"`
			LawBasic           string `json:"lawBasic"`
			HandleScope        string `json:"handleScope"`
			StatusName         string `json:"statusName"`
			HandleUnit         string `json:"handleUnit"`
			ID                 string `json:"id"`
			OriginNoticeID     string `json:"originNoticeId"`
			Pageurl            string `json:"pageurl"`
			BreedCategoryScope string `json:"breedCategoryScope"`
			DataSource         string `json:"dataSource"`
			CreditName         string `json:"creditName"`
			StartDate          string `json:"startDate"`
			Status             string `json:"status"`
		} `json:"rows"`
		QueryInfo struct {
			QueryString string `json:"queryString"`
		} `json:"queryInfo"`
	} `json:"data"`
}

type AlarmDetail struct {
	Msg  string `json:"msg,omitempty"`
	Code string `json:"code,omitempty"`
	Data struct {
		NoticeTime          string `json:"noticeTime,omitempty"`
		FieldValues         any    `json:"fieldValues,omitempty"`
		StaticState         any    `json:"staticState,omitempty"`
		ID                  string `json:"id,omitempty"`
		State               string `json:"state,omitempty"`
		StateStr            any    `json:"stateStr,omitempty"`
		Issign              string `json:"issign,omitempty"`
		Isdel               string `json:"isdel,omitempty"`
		IsdelStr            any    `json:"isdelStr,omitempty"`
		Iscomment           string `json:"iscomment,omitempty"`
		IscommentStr        any    `json:"iscommentStr,omitempty"`
		Isscore             any    `json:"isscore,omitempty"`
		IsscoreStr          any    `json:"isscoreStr,omitempty"`
		Video               any    `json:"video,omitempty"`
		Channelids          any    `json:"channelids,omitempty"`
		CheckOpenendtime    any    `json:"checkOpenendtime,omitempty"`
		MobileChannelids    any    `json:"mobileChannelids,omitempty"`
		MwebChannelids      any    `json:"mwebChannelids,omitempty"`
		AddUserLoginName    any    `json:"addUserLoginName,omitempty"`
		AddUserName         any    `json:"addUserName,omitempty"`
		Countnum            int    `json:"countnum,omitempty"`
		Infoordernum        int    `json:"infoordernum,omitempty"`
		Datename            any    `json:"datename,omitempty"`
		HTMLIndexnum        int    `json:"htmlIndexnum,omitempty"`
		HTMLIndexnumLike    any    `json:"htmlIndexnumLike,omitempty"`
		HTMLFileName        any    `json:"htmlFileName,omitempty"`
		Isimgs              any    `json:"isimgs,omitempty"`
		StateResult         any    `json:"stateResult,omitempty"`
		Htmlpath            string `json:"htmlpath,omitempty"`
		Mobilepath          any    `json:"mobilepath,omitempty"`
		SQL                 any    `json:"sql,omitempty"`
		Formid              any    `json:"formid,omitempty"`
		SiteBean            any    `json:"siteBean,omitempty"`
		NoticeID            string `json:"noticeId,omitempty"`
		RegionName          string `json:"regionName,omitempty"`
		PurchaseNature      any    `json:"purchaseNature,omitempty"`
		CatalogueNameList   any    `json:"catalogueNameList,omitempty"`
		PlanCodes           any    `json:"planCodes,omitempty"`
		PurchaseManner      any    `json:"purchaseManner,omitempty"`
		Budget              any    `json:"budget,omitempty"`
		RegionCode          string `json:"regionCode,omitempty"`
		NoticeType          string `json:"noticeType,omitempty"`
		NoticeTypeName      any    `json:"noticeTypeName,omitempty"`
		OpenTenderTime      any    `json:"openTenderTime,omitempty"`
		ExpireTime          any    `json:"expireTime,omitempty"`
		VideoType           any    `json:"videoType,omitempty"`
		SuccessfulMoney     any    `json:"successfulMoney,omitempty"`
		OpenTenderCode      any    `json:"openTenderCode,omitempty"`
		AgentManageName     any    `json:"agentManageName,omitempty"`
		AgentLinkPhone      any    `json:"agentLinkPhone,omitempty"`
		AgentLinkMan        any    `json:"agentLinkMan,omitempty"`
		AgentAddress        any    `json:"agentAddress,omitempty"`
		RecordTime          any    `json:"recordTime,omitempty"`
		DemandTime          any    `json:"demandTime,omitempty"`
		GovService          any    `json:"govService,omitempty"`
		PunishType          any    `json:"punishType,omitempty"`
		Respondents         any    `json:"respondents,omitempty"`
		TransactionType     any    `json:"transactionType,omitempty"`
		Complainant         any    `json:"complainant,omitempty"`
		PunishObject        any    `json:"punishObject,omitempty"`
		ContactPerson       any    `json:"contactPerson,omitempty"`
		TransactionTypeCode any    `json:"transactionTypeCode,omitempty"`
		ContactNumber       any    `json:"contactNumber,omitempty"`
		NoticeDetailURL     any    `json:"noticeDetailUrl,omitempty"`
		BidCompany          any    `json:"bidCompany,omitempty"`
		ProductCategoryCode any    `json:"productCategoryCode,omitempty"`
		ProductCategoryName any    `json:"productCategoryName,omitempty"`
		Site                string `json:"site,omitempty"`
		Infosite            any    `json:"infosite,omitempty"`
		Sitename            any    `json:"sitename,omitempty"`
		Indexnum            any    `json:"indexnum,omitempty"`
		Opentype            string `json:"opentype,omitempty"`
		Opentimetype        string `json:"opentimetype,omitempty"`
		Openendtime         any    `json:"openendtime,omitempty"`
		OpenendtimeSec      any    `json:"openendtimeSec,omitempty"`
		OpenendtimeStr      any    `json:"openendtimeStr,omitempty"`
		Channel             string `json:"channel,omitempty"`
		Channelname         any    `json:"channelname,omitempty"`
		ChannelPagemark     string `json:"channelPagemark,omitempty"`
		ChannelIndexnum     int    `json:"channelIndexnum,omitempty"`
		ChannelFolder       any    `json:"channelFolder,omitempty"`
		ChannelParid        any    `json:"channelParid,omitempty"`
		ChannelParPagemark  any    `json:"channelParPagemark,omitempty"`
		Title               string `json:"title,omitempty"`
		Shorttitle          string `json:"shorttitle,omitempty"`
		Showtitle           any    `json:"showtitle,omitempty"`
		TitleSuffix         any    `json:"titleSuffix,omitempty"`
		ShowtitleLen        int    `json:"showtitleLen,omitempty"`
		Pageurl             string `json:"pageurl,omitempty"`
		Noids               any    `json:"noids,omitempty"`
		Sitepath            any    `json:"sitepath,omitempty"`
		ContextPath         string `json:"contextPath,omitempty"`
		Channels            any    `json:"channels,omitempty"`
		Starttime           any    `json:"starttime,omitempty"`
		Endtime             any    `json:"endtime,omitempty"`
		SearchKey           any    `json:"searchKey,omitempty"`
		Ishot               any    `json:"ishot,omitempty"`
		Isnew               any    `json:"isnew,omitempty"`
		InRelationID        string `json:"inRelationId,omitempty"`
		OutRelationID       string `json:"outRelationId,omitempty"`
		BusinessID          string `json:"businessId,omitempty"`
		Titlecolor          any    `json:"titlecolor,omitempty"`
		Titleblod           any    `json:"titleblod,omitempty"`
		Source              any    `json:"source,omitempty"`
		Author              any    `json:"author,omitempty"`
		Description         string `json:"description,omitempty"`
		Tags                any    `json:"tags,omitempty"`
		Img                 any    `json:"img,omitempty"`
		URL                 any    `json:"url,omitempty"`
		Attchs              any    `json:"attchs,omitempty"`
		AddtimeStr          string `json:"addtimeStr,omitempty"`
		Addtimees           string `json:"addtimees,omitempty"`
		DateFormat          any    `json:"dateFormat,omitempty"`
		Templet             any    `json:"templet,omitempty"`
		Mwebtemplet         any    `json:"mwebtemplet,omitempty"`
		Istop               any    `json:"istop,omitempty"`
		Topendtime          any    `json:"topendtime,omitempty"`
		TopendtimeStr       any    `json:"topendtimeStr,omitempty"`
		Clicknum            any    `json:"clicknum,omitempty"`
		Adduser             any    `json:"adduser,omitempty"`
		AdduserLike         any    `json:"adduserLike,omitempty"`
		Content             string `json:"content,omitempty"`
		Addtime             int64  `json:"addtime,omitempty"`
	} `json:"data,omitempty"`
}

func (a *Alarm) ToMessage() (string, error) {
	var buf bytes.Buffer
	if err := alarmRender.Execute(&buf, a); err == nil {
		return buf.String(), nil
	} else {
		return "", err
	}
}
