package handler

// ClaimHandle 定义一条可抢占推送的最小单元。
//
// 处理骨架固定为：内存锁 → DB 抢占 → 发送 → 失败回滚。
// 管线只关心骨架，具体业务（唯一键组成、发送内容、回滚语义）由回调提供。
type ClaimHandle struct {
	// Key 是进程内锁（KeyedLock）的键；为空表示调用方已自行管理并发
	//（例如 projects 的预筛锁已覆盖整个渲染+发送生命周期）。
	Key string
	// Claim 执行 DB 原子抢占（InsertIfAbsent），返回是否抢到。
	// 只有真正抢到的调用才允许发送。
	Claim func() (bool, error)
	// Send 执行发送；返回 error 表示发送失败，会触发 Rollback 回滚。
	Send func() error
	// Rollback 在发送失败时回滚抢占（Remove），供下一轮重试。
	Rollback func() error
}

// PushPipeline 串行处理一批可抢占推送。同一进程内相同 Key 的 handle 只会
// 被处理一次；跨实例的互斥由 Claim 背后的 DB 唯一约束保证。
type PushPipeline struct {
	locks *KeyedLock
}

// NewPushPipeline 返回一个空的 PushPipeline。
func NewPushPipeline() *PushPipeline {
	return &PushPipeline{locks: NewKeyedLock()}
}

// Run 依次处理 handles。单个 handle 的失败不影响其余 handle。
func (p *PushPipeline) Run(handles []ClaimHandle) {
	for _, h := range handles {
		func() {
			if h.Key != "" {
				if !p.locks.TryLock(h.Key) {
					return // 同进程内另一调用正在处理，跳过
				}
				defer p.locks.Unlock(h.Key)
			}

			claimed, err := h.Claim()
			if err != nil || !claimed {
				return // 抢占失败：已存在或出错，让给别的调用/下一轮
			}

			if err := h.Send(); err != nil {
				_ = h.Rollback() // 发送失败，回滚抢占供下一轮重试
			}
		}()
	}
}
