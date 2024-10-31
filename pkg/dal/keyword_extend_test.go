package dal

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
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
	find, err := Keyword.Where(Keyword.Keyword.In(keywords2...)).Find()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, f := range find {
		ids = append(ids, strconv.FormatInt(int64(*f.ID), 10))
	}

	var tmp []string
	for _, id := range ids {
		tmp = append(tmp, fmt.Sprintf("%s=%s", id, generateRandomString(10)))
	}
	if err = Keyword.EditById(tmp); err != nil {
		t.Fatal(err)
	}

	joinIds := strings.Join(ids, ",")
	if info, err := Keyword.DeleteByIds(joinIds); err != nil {
		t.Fatal(err)
	} else {
		t.Log(info)
	}
	t.Log(Keyword.GetByUserIdAndType(id, model.PROJECT))
	t.Log(Keyword.GetKeywords(id, model.PROJECT))
}

func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	var sb strings.Builder
	for i := 0; i < length; i++ {
		sb.WriteByte(charset[rand.Intn(len(charset))])
	}
	return sb.String()
}
