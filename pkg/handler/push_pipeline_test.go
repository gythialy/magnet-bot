package handler

import (
	"errors"
	"sync/atomic"
	"testing"
)

// TestPushPipelineClaimAndSend verifies the pipeline skeleton: only the handle
// that claims the resource proceeds to send; a failed send triggers rollback.
func TestPushPipelineClaimAndSend(t *testing.T) {
	p := NewPushPipeline()

	var claimed, sent, rolledBack int32
	handles := []ClaimHandle{
		{
			Key: "a",
			Claim: func() (bool, error) {
				atomic.AddInt32(&claimed, 1)
				return true, nil
			},
			Send: func() error {
				atomic.AddInt32(&sent, 1)
				return errors.New("send failed")
			},
			Rollback: func() error {
				atomic.AddInt32(&rolledBack, 1)
				return nil
			},
		},
		{
			Key: "b",
			Claim: func() (bool, error) {
				atomic.AddInt32(&claimed, 1)
				return false, nil // lost the claim, must not send
			},
			Send: func() error {
				atomic.AddInt32(&sent, 1)
				return nil
			},
		},
	}

	p.Run(handles)

	if claimed != 2 {
		t.Errorf("expected both handles to attempt claim, got %d", claimed)
	}
	if sent != 1 {
		t.Errorf("expected exactly 1 send, got %d", sent)
	}
	if rolledBack != 1 {
		t.Errorf("expected rollback for the failed send, got %d", rolledBack)
	}
}

// TestPushPipelineKeyLock verifies that a handle whose in-process Key is
// already held is skipped entirely (no claim, no send).
func TestPushPipelineKeyLock(t *testing.T) {
	p := NewPushPipeline()

	// Pre-hold the key, simulating another invocation currently processing it.
	if !p.locks.TryLock("same") {
		t.Fatal("expected to hold the key")
	}

	var sent int32
	p.Run([]ClaimHandle{{
		Key: "same",
		Claim: func() (bool, error) {
			t.Error("claim must not run while the key is held")
			return true, nil
		},
		Send: func() error {
			atomic.AddInt32(&sent, 1)
			return nil
		},
	}})

	if sent != 0 {
		t.Fatalf("expected the handle to be skipped, got %d sends", sent)
	}
	p.locks.Unlock("same")
}

// TestPushPipelineNoKeySkipsLock verifies that a handle without a Key is not
// gated by the in-process lock (the caller manages concurrency itself).
func TestPushPipelineNoKeySkipsLock(t *testing.T) {
	p := NewPushPipeline()

	var sent int32
	handles := make([]ClaimHandle, 0, 3)
	for i := 0; i < 3; i++ {
		handles = append(handles, ClaimHandle{
			Claim: func() (bool, error) { return true, nil },
			Send: func() error {
				atomic.AddInt32(&sent, 1)
				return nil
			},
		})
	}
	p.Run(handles)

	if sent != 3 {
		t.Fatalf("expected all 3 handles to send without a Key, got %d", sent)
	}
}
