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

// TestKeywordInsertManyReportsAddedAndSkipped pins the batch contract the API
// exposes: trimmed input, dropped blanks, batch-internal duplicates reported as
// skipped, and type/user separation.
func TestKeywordInsertManyReportsAddedAndSkipped(t *testing.T) {
	setupKeywordTestDB(t)
	const (
		owner = int64(1)
		other = int64(2)
	)

	added, skipped, err := Keyword.InsertMany(owner, model.PROJECT, []string{" alpha ", "beta", "alpha", "   "})
	if err != nil {
		t.Fatalf("InsertMany() error: %v", err)
	}
	assertKeywordSlice(t, "added", added, []string{"alpha", "beta"})
	assertKeywordSlice(t, "skipped", skipped, []string{"alpha"})

	added, skipped, err = Keyword.InsertMany(owner, model.PROJECT, []string{"alpha", " gamma "})
	if err != nil {
		t.Fatalf("InsertMany() error: %v", err)
	}
	assertKeywordSlice(t, "added", added, []string{"gamma"})
	assertKeywordSlice(t, "skipped", skipped, []string{"alpha"})

	// An input that normalises to nothing must return empty, non-nil slices: the
	// API encodes them as [] and the client iterates them unconditionally.
	added, skipped, err = Keyword.InsertMany(owner, model.PROJECT, []string{"", "  "})
	if err != nil {
		t.Fatalf("InsertMany() error: %v", err)
	}
	if added == nil || skipped == nil {
		t.Fatalf("InsertMany() returned nil slices (%v, %v), want empty", added, skipped)
	}
	if len(added) != 0 || len(skipped) != 0 {
		t.Fatalf("InsertMany() = (%v, %v), want empty slices", added, skipped)
	}

	// The same text is a different keyword for the alarm list and for another
	// account.
	added, skipped, err = Keyword.InsertMany(owner, model.ALARM, []string{"alpha"})
	if err != nil {
		t.Fatalf("InsertMany(ALARM) error: %v", err)
	}
	assertKeywordSlice(t, "alarm added", added, []string{"alpha"})
	assertKeywordSlice(t, "alarm skipped", skipped, []string{})

	added, _, err = Keyword.InsertMany(other, model.PROJECT, []string{"alpha"})
	if err != nil {
		t.Fatalf("InsertMany(other) error: %v", err)
	}
	assertKeywordSlice(t, "other added", added, []string{"alpha"})

	if got := len(Keyword.GetByUserIdAndType(owner, model.PROJECT)); got != 3 {
		t.Fatalf("owner project keywords = %d, want 3", got)
	}
	if got := len(Keyword.GetByUserIdAndType(owner, model.ALARM)); got != 1 {
		t.Fatalf("owner alarm keywords = %d, want 1", got)
	}
	if got := len(Keyword.GetByUserIdAndType(other, model.PROJECT)); got != 1 {
		t.Fatalf("other project keywords = %d, want 1", got)
	}
}

// TestKeywordWritesReportErrors: InsertMany must surface a real storage failure
// (the API answers 500), while the legacy Insert keeps swallowing it into "".
func TestKeywordWritesReportErrors(t *testing.T) {
	setupKeywordTestDB(t)

	sqlDB, err := Keyword.UnderlyingDB().DB()
	if err != nil {
		t.Fatalf("access underlying database: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close test database: %v", err)
	}

	if got := Keyword.Insert([]string{"alpha"}, 1, model.PROJECT); got != "" {
		t.Fatalf("Insert() = %q after a storage failure, want the legacy empty result", got)
	}

	added, skipped, err := Keyword.InsertMany(1, model.PROJECT, []string{"alpha"})
	if err == nil {
		t.Fatalf("InsertMany() error = nil, want the storage failure (added=%v skipped=%v)", added, skipped)
	}
}

// TestKeywordDeleteOwnedIsUserScoped proves the ownership predicate is part of
// the DELETE and that the boolean distinguishes "removed" from "nothing there".
func TestKeywordDeleteOwnedIsUserScoped(t *testing.T) {
	setupKeywordTestDB(t)
	const (
		owner = int64(1)
		other = int64(2)
	)

	if _, _, err := Keyword.InsertMany(owner, model.PROJECT, []string{"keep"}); err != nil {
		t.Fatalf("seed keyword: %v", err)
	}
	id := *Keyword.GetByUserIdAndType(owner, model.PROJECT)[0].ID

	deleted, err := Keyword.DeleteOwned(other, id)
	if err != nil {
		t.Fatalf("DeleteOwned(other) error: %v", err)
	}
	if deleted {
		t.Fatal("DeleteOwned(other) = true, want false")
	}
	if got := len(Keyword.GetByUserIdAndType(owner, model.PROJECT)); got != 1 {
		t.Fatal("another user's keyword was deleted")
	}

	deleted, err = Keyword.DeleteOwned(owner, id)
	if err != nil {
		t.Fatalf("DeleteOwned(owner) error: %v", err)
	}
	if !deleted {
		t.Fatal("DeleteOwned(owner) = false, want true")
	}
	if got := len(Keyword.GetByUserIdAndType(owner, model.PROJECT)); got != 0 {
		t.Fatalf("keywords after delete = %d, want 0", got)
	}

	// A second delete removes nothing and must report that, so the API can
	// answer 404 instead of a misleading 204.
	deleted, err = Keyword.DeleteOwned(owner, id)
	if err != nil {
		t.Fatalf("second DeleteOwned(owner) error: %v", err)
	}
	if deleted {
		t.Fatal("second DeleteOwned(owner) = true, want false")
	}
}

// TestKeywordUpdateOwnedIsUserScoped proves the ownership predicate is part of
// the UPDATE and that a same-value rename still counts as a match (the API must
// not turn a no-op into a 404).
func TestKeywordUpdateOwnedIsUserScoped(t *testing.T) {
	setupKeywordTestDB(t)
	const (
		owner = int64(1)
		other = int64(2)
	)

	if _, _, err := Keyword.InsertMany(owner, model.PROJECT, []string{"original"}); err != nil {
		t.Fatalf("seed keyword: %v", err)
	}
	id := *Keyword.GetByUserIdAndType(owner, model.PROJECT)[0].ID

	updated, err := Keyword.UpdateOwned(other, id, "stolen")
	if err != nil {
		t.Fatalf("UpdateOwned(other) error: %v", err)
	}
	if updated {
		t.Fatal("UpdateOwned(other) = true, want false")
	}
	if got := Keyword.GetByUserIdAndType(owner, model.PROJECT)[0].Keyword; got != "original" {
		t.Fatalf("keyword = %q, want the other user's attempt to have changed nothing", got)
	}

	updated, err = Keyword.UpdateOwned(owner, id, "renamed")
	if err != nil {
		t.Fatalf("UpdateOwned(owner) error: %v", err)
	}
	if !updated {
		t.Fatal("UpdateOwned(owner) = false, want true")
	}
	if got := Keyword.GetByUserIdAndType(owner, model.PROJECT)[0].Keyword; got != "renamed" {
		t.Fatalf("keyword = %q, want renamed", got)
	}

	// Renaming to the value it already has is still a hit.
	updated, err = Keyword.UpdateOwned(owner, id, "renamed")
	if err != nil {
		t.Fatalf("same-value UpdateOwned(owner) error: %v", err)
	}
	if !updated {
		t.Fatal("same-value UpdateOwned(owner) = false, want true")
	}

	updated, err = Keyword.UpdateOwned(owner, 999999, "x")
	if err != nil {
		t.Fatalf("UpdateOwned(missing) error: %v", err)
	}
	if updated {
		t.Fatal("UpdateOwned(missing) = true, want false")
	}
}

// assertKeywordSlice compares string slices and reports the values on a
// mismatch; both empty and populated expectations are covered.
func assertKeywordSlice(t *testing.T, name string, got, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}
}
