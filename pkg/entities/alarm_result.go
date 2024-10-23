package entities

import (
	"bytes"
	"html/template"
	"time"

	"github.com/rs/zerolog/log"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

type Alarm struct {
	UserId           int64 `gorm:"primaryKey;autoIncrement:false"`
	CreditType       string
	CreditName       string
	CreditCode       string `gorm:"primaryKey;autoIncrement:false"`
	StartDate        time.Time
	EndDate          time.Time
	DetailReason     string
	HandleDepartment string
	HandleUnit       string
	HandleResult     string
}

func (a *Alarm) ToMarkdown() string {
	var buf bytes.Buffer
	if err := alarmRender.Execute(&buf, a); err == nil {
		return buf.String()
	} else {
		log.Err(err)
		return ""
	}
}

type AlarmDao struct {
	db *gorm.DB
}

func NewAlarmDao(db *gorm.DB) *AlarmDao {
	return &AlarmDao{db: db}
}

func (a *AlarmDao) Clean() error {
	now := time.Now()
	if err := a.db.Where("end_date != '0001-01-01 00:00:00+00:00' and end_date < ?", now).Delete(&Alarm{}).Error; err != nil {
		return err
	}
	return nil
}

func (a *AlarmDao) Cache(userId int64) map[string]Alarm {
	result := make(map[string]Alarm)
	var alarms []Alarm
	now := time.Now()
	if err := a.db.Where("user_id = ? and (end_date > ? or end_date = '0001-01-01 00:00:00+00:00')", userId, now).Find(&alarms).Error; err == nil {
		for _, alarm := range alarms {
			result[alarm.CreditCode] = alarm
		}
	}
	return result
}

func (a *AlarmDao) List(userId int64, page, pageSize int) ([]Alarm, int64) {
	var result []Alarm
	var total int64

	offset := (page - 1) * pageSize

	if err := a.db.Model(&Alarm{}).Where("user_id = ?", userId).Count(&total).Error; err != nil {
		return nil, 0
	}

	if err := a.db.Where("user_id = ?", userId).Offset(offset).Limit(pageSize).Find(&result).Error; err != nil {
		return nil, 0
	}

	return result, total
}

func (a *AlarmDao) Insert(data []*Alarm) (error, int64) {
	if tx := a.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "credit_code"}},
		UpdateAll: true,
	}).Create(&data); tx.Error != nil {
		return tx.Error, 0
	} else {
		return nil, tx.RowsAffected
	}
}
