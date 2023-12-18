package entities

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/gythialy/magnet/pkg/utiles"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestHistoryDao_Cache(t *testing.T) {
	f := "./history.db"
	defer func() {
		_ = os.Remove(f)
	}()
	db, err := gorm.Open(sqlite.Open(f), &gorm.Config{
		Logger: logger.Recorder.New(),
	})
	if err != nil {
		t.Fatal(err)
	}

	_ = db.AutoMigrate(&History{})
	db.Debug()
	dao := NewHistoryDao(db)
	var histories []History
	userId := int64(0)
	now := time.Now()
	for i := userId; i < 10; i++ {
		histories = append(histories, History{
			UserId:    userId,
			Url:       fmt.Sprintf("https://test.com/content%d", i),
			UpdatedAt: now,
		})
	}

	if err, i := dao.Insert(histories); err != nil {
		t.Fatal(err)
	} else {
		t.Logf("insert %d rows", i)
	}

	data1 := dao.List(userId)
	t.Log(utiles.ToString(data1))

	date1 := now.AddDate(0, 0, -7)
	if err, i := dao.Insert([]History{{
		UserId:    userId,
		Url:       fmt.Sprintf("https://test.com/content%d", 2),
		UpdatedAt: date1,
	}, {
		UserId:    userId,
		Url:       fmt.Sprintf("https://test.com/content%d", 4),
		UpdatedAt: date1,
	}}); err != nil {
		t.Fatal(err)
	} else {
		t.Logf("insert %d rows", i)
	}
	data2 := dao.List(userId)
	t.Log(utiles.ToString(data2))

	if err := dao.Clean(); err != nil {
		t.Fatal(err)
	}

	data3 := dao.List(userId)
	t.Log(utiles.ToString(data3))
}
