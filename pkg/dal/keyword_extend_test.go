package dal

import (
	"strconv"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gythialy/magnet/pkg/model"
	"gorm.io/gorm"
)

func setupKeywordTestDB(t *testing.T) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/keyword.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Keyword{}); err != nil {
		t.Fatal(err)
	}
	SetDefault(db)
}

func TestKeywordInsertDeduplicates(t *testing.T) {
	setupKeywordTestDB(t)
	const userID = int64(1111)

	if got := Keyword.Insert([]string{"test", "test2", " test "}, userID, model.PROJECT); got != "test, test2" {
		t.Fatalf("Insert() = %q, want %q", got, "test, test2")
	}
	if got := Keyword.Insert([]string{"test", "test2"}, userID, model.PROJECT); got != "" {
		t.Fatalf("second Insert() = %q, want no new keywords", got)
	}

	keywords := Keyword.GetByUserIdAndType(userID, model.PROJECT)
	if len(keywords) != 2 {
		t.Fatalf("got %d keywords, want 2", len(keywords))
	}
	if got := Keyword.GetKeywords(userID, model.PROJECT); len(got) != 2 {
		t.Fatalf("GetKeywords() returned %v, want 2 entries", got)
	}
}

func TestKeywordDeleteByIdsIsUserScoped(t *testing.T) {
	setupKeywordTestDB(t)
	const owner, other = int64(1), int64(2)

	Keyword.Insert([]string{"owner-kw"}, owner, model.PROJECT)
	ownerKeyword := Keyword.GetByUserIdAndType(owner, model.PROJECT)[0]
	id := strconv.Itoa(int(*ownerKeyword.ID))

	// Another user must not be able to delete the keyword by guessing its id.
	if got, err := Keyword.DeleteByIds(other, id); err != nil || got != "" {
		t.Fatalf("DeleteByIds(other) = (%q, %v), want (\"\", nil)", got, err)
	}
	if len(Keyword.GetByUserIdAndType(owner, model.PROJECT)) != 1 {
		t.Fatal("another user's keyword was deleted")
	}

	if got, err := Keyword.DeleteByIds(owner, id); err != nil || got == "" {
		t.Fatalf("DeleteByIds(owner) = (%q, %v), want the deleted keyword", got, err)
	}
	if len(Keyword.GetByUserIdAndType(owner, model.PROJECT)) != 0 {
		t.Fatal("keyword was not deleted")
	}
}

func TestKeywordEditByIdIsUserScoped(t *testing.T) {
	setupKeywordTestDB(t)
	const owner, other = int64(1), int64(2)

	Keyword.Insert([]string{"owner-kw"}, owner, model.PROJECT)
	ownerKeyword := Keyword.GetByUserIdAndType(owner, model.PROJECT)[0]
	id := strconv.Itoa(int(*ownerKeyword.ID))

	if err := Keyword.EditById(other, []string{id + "=hijacked"}); err == nil {
		t.Fatal("expected an error when editing another user's keyword")
	}
	if got := Keyword.GetByUserIdAndType(owner, model.PROJECT)[0].Keyword; got != "owner-kw" {
		t.Fatalf("keyword was modified by another user: %q", got)
	}

	if err := Keyword.EditById(owner, []string{id + "=renamed"}); err != nil {
		t.Fatalf("EditById(owner) failed: %v", err)
	}
	if got := Keyword.GetByUserIdAndType(owner, model.PROJECT)[0].Keyword; got != "renamed" {
		t.Fatalf("keyword = %q, want %q", got, "renamed")
	}
}

// TestKeywordEditByIdRejectsMalformedEntry guards the panic that indexing the
// "=" split blindly used to cause for an input without "=".
func TestKeywordEditByIdRejectsMalformedEntry(t *testing.T) {
	setupKeywordTestDB(t)

	for _, entry := range []string{"42", "=keyword", "abc=keyword"} {
		if err := Keyword.EditById(1, []string{entry}); err == nil {
			t.Errorf("EditById(%q) returned no error", entry)
		}
	}
}
