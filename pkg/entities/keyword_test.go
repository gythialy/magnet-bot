package entities

import (
	"os"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestNewAlarmKeywordDao(t *testing.T) {
	f := "./keyword.db"
	defer func() {
		_ = os.Remove(f)
	}()
	db, err := gorm.Open(sqlite.Open(f), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		t.Fatal(err)
	}

	_ = db.AutoMigrate(&Keyword{})
	db.Debug()
	keywords := []string{"test", "test2", "test3", "test4"}
	id := int64(1111)

	dao := NewKeywordDao(db)
	dao.Add(keywords, id, PROJECT)
	dao.Add(keywords, id, ALARM)
	t.Log(dao.List(id, PROJECT))
	t.Log(dao.List(id, ALARM))
	t.Log(dao.Ids())

	print(db, t)
	keywords2 := []string{"test3", "test4"}
	dao.Delete(keywords2, id, PROJECT)
	t.Log(dao.List(id, PROJECT))
	t.Log(dao.ListKeywords(id, PROJECT))
}
