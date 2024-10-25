package pkg

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gythialy/magnet/pkg/entities"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCrawler_Get(t *testing.T) {
	crawler := NewCrawler(&BotContext{
		MessageServerUrl: os.Getenv("SERVER_URL"),
	})

	results := crawler.FetchProjects()
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

	_ = db.AutoMigrate(&entities.Alarm{})
	db.Debug()
	dao := entities.NewAlarmDao(db)

	crawler := NewCrawler(&BotContext{
		MessageServerUrl: os.Getenv("SERVER_URL"),
	})

	userId := int64(1111)
	result := crawler.Fetch([]string{"中国"}, userId)

	for idx, alarm := range result {
		alarm.UserId = userId
		fmt.Printf("%d: %s(%s),%s\n", idx, alarm.CreditName, alarm.CreditCode, alarm.EndDate)
	}

	if len(result) > 0 {
		if err, i := dao.Insert(result); err == nil {
			t.Log(i)
		} else {
			t.Error(err)
		}
		if err, i := dao.Insert(result); err == nil {
			t.Log(i)
		} else {
			t.Error(err)
		}

		fmt.Println(strings.Repeat("-", 20))
	}

	list, _ := dao.List(userId, 1, 20)
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
