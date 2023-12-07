package entities

import (
	"os"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestNewKeywordDao(t *testing.T) {
	f := "./keyword.db"
	defer func() {
		_ = os.Remove(f)
	}()
	db, err := gorm.Open(sqlite.Open(f), &gorm.Config{
		Logger: logger.Recorder.New(),
	})
	if err != nil {
		t.Fatal(err)
	}

	_ = db.AutoMigrate(&Keyword{})
	db.Debug()
	keywords := []string{"test", "test2", "test3", "test4"}
	id := int64(1111)

	dao := NewKeywordDao(db)
	dao.Add(keywords, id)
	t.Log(dao.List(id))
	t.Log(dao.Ids())

	print(db, t)
	keywords2 := []string{"test3", "test4"}
	dao.Delete(keywords2, id)
	t.Log(dao.List(id))
	t.Log(dao.ListKeywords(id))
}
