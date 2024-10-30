package dal

import (
	"os"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gythialy/magnet/pkg/model"
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

	_ = db.AutoMigrate(&model.Keyword{})
	db.Debug()
	values := []string{"test", "test2", "test3", "test4"}
	id := int64(1111)

	SetDefault(db)

	Keyword.Insert(values, id, model.PROJECT)
	Keyword.Insert(values, id, model.ALARM)
	t.Log(Keyword.GetKeywords(id, model.PROJECT))
	t.Log(Keyword.GetByUserIdAndType(id, model.ALARM))
	t.Log(Keyword.Ids())

	print(db, t)
	keywords2 := []string{"test3", "test4"}
	if _, err := Keyword.Delete(keywords2, id, model.PROJECT); err != nil {
		t.Fatal(err)
	}
	t.Log(Keyword.GetByUserIdAndType(id, model.PROJECT))
	t.Log(Keyword.GetKeywords(id, model.PROJECT))
}
