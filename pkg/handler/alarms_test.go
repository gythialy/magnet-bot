package handler

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestAlarmConcurrentGuard verifies that the in-process processingAlarms lock
// prevents two concurrent invocations from both passing the IsExist check and
// pushing the same alarm message twice.
func TestAlarmConcurrentGuard(t *testing.T) {
	f := "./alarm_guard_test.db"
	defer func() { _ = os.Remove(f) }()
	db, err := gorm.Open(sqlite.Open(f), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.AutoMigrate(&model.Alarm{})
	dal.SetDefault(db)

	processor := &InfoProcessor{
		processingAlarms: make(map[string]bool),
	}

	userId := int64(2222)
	key := fmt.Sprintf("%d:%s", userId, "CODE-X")

	// Invocation A acquires the lock first and holds it while "sending".
	started := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		if !processor.tryLockAlarm(key) {
			t.Error("first invocation should acquire the alarm lock")
			return
		}
		close(started)
		<-release
		processor.unlockAlarm(key)
	}()

	// Invocation B starts while A still holds the lock and must be rejected.
	bChecked := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-started
		if processor.tryLockAlarm(key) {
			t.Error("second invocation acquired the alarm lock while first still holds it: duplicate push possible")
			processor.unlockAlarm(key)
		}
		close(bChecked)
	}()

	<-bChecked     // wait until B has attempted the lock (A is guaranteed to hold it)
	close(release) // now let A finish and release the lock
	wg.Wait()

	// After A releases, the lock must be free again so a later run can retry.
	if !processor.tryLockAlarm(key) {
		t.Error("alarm lock should be released after the first invocation finishes")
	}
	processor.unlockAlarm(key)
}
