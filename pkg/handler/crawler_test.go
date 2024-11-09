package handler

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/gythialy/magnet/pkg/config"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCrawler_Get(t *testing.T) {
	crawler := NewCrawler(&BotContext{
		Config: &config.ServiceConfig{
			MessageServerUrl: os.Getenv("SERVER_URL"),
		},
	})

	results := crawler.Projects()
	t.Log(len(results))
}

func TestCrawler_Fetch(t *testing.T) {
	f := "./alarm.db"
	defer func() {
		_ = os.Remove(f)
	}()
	db, err := gorm.Open(sqlite.Open(f), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		t.Fatal(err)
	}

	_ = db.AutoMigrate(&model.Alarm{})
	db.Debug()
	dal.SetDefault(db)

	crawler := NewCrawler(&BotContext{
		Config: &config.ServiceConfig{
			MessageServerUrl: config.MessageServerUrl(),
		},
	})

	userId := int64(1111)
	result := crawler.Alarms([]string{"中国"}, userId)

	for idx, alarm := range result {
		alarm.UserID = userId
		fmt.Printf("%d: %s(%s),%s\n", idx, alarm.CreditName, alarm.CreditCode, alarm.EndDate)
	}

	dao := dal.Alarm
	if len(result) > 0 {
		if err := dao.Insert(result); err == nil {
		} else {
			t.Error(err)
		}
		if err := dao.Insert(result); err == nil {
		} else {
			t.Error(err)
		}

		fmt.Println(strings.Repeat("-", 20))
	}

	list, _ := dao.SearchByName(userId, "", 1, 20)
	for idx, alarm := range list {
		fmt.Printf("%d: %s(%s),%s\n", idx, alarm.CreditName, alarm.CreditCode, alarm.EndDate)
	}
	fmt.Println(strings.Repeat("-", 20))

	cache := dao.Cache(userId)
	idx := 0
	for _, alarm := range cache {
		fmt.Printf("%d: %s(%s),%s\n", idx, alarm.CreditName, alarm.CreditCode, alarm.EndDate)
		idx++
	}
}
