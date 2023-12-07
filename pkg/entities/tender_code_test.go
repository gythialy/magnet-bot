package entities

import (
	"os"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNewTenderCodeDao(t *testing.T) {
	f := "./tender_code.db"
	defer func() {
		_ = os.Remove(f)
	}()
	db, err := gorm.Open(sqlite.Open(f), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	_ = db.AutoMigrate(&TenderCode{})
	db.Debug()
	codes := []string{"test", "test2", "test3", "test4"}
	id := int64(1111)

	dao := NewTenderCodeDao(db)
	dao.Add(codes, id)
	t.Log(dao.List(id))

	print(db, t)
	keywords2 := []string{"test3", "test4"}
	dao.Delete(keywords2, id)
	t.Log(dao.List(id))
	t.Log(dao.ListTenderCodes(id))
}
