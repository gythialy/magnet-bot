# magnet-bot 推送管线重构方案

> 状态：方案 v1（2026-08-04），**Step 1-3 已在 PR #362 落地，Step 4-5 待评估**
> 背景：PR #361 落地了"DB 原子抢占 + KeyedLock"修复后，alarm/project 两条推送管线仍有大量重复与状态交织，本方案目标是简化实现、消除重复，且不改变外部行为。

---

## 一、现状痛点

### 1. 两条管线的 claim → send → rollback 逻辑重复（核心）

`processProjects` 与 `processAlarms` 各自手写了一遍相同的模式：

```
内存锁 TryLock → DB 抢占(InsertIfAbsent) → 发送 → 失败回滚(Remove)
```

差异只在：唯一键的组成、发送内容、失败语义。目前是复制粘贴两份，后续加第三条管线（如新增通知类型）会再复制一份。

### 2. `processProjects` 单个函数状态交织（复杂度的主来源）

一个函数里同时管理：

- `pending` 预筛选 + 并发渲染
- 多 chunk 发送循环（`isSuccessful` 部分成功语义）
- `failed` / `filterFailed` 失败汇总（最后补发一条汇总消息）
- `processedURL` 强制模式落库
- claim 回滚 + 内存锁释放（已用闭包 + defer 加固，但闭包加深了嵌套）

函数 160+ 行，读起来要在 6 个状态变量之间跳转。

### 3. `processAlarms` 过期占位重抢逻辑嵌套深

`!inserted` 分支里：再查 IsExist → Remove 旧记录 → 重抢 → 可能再失败，4 层嵌套，且与"并发抢先"分支的区分靠注释。

### 4. 双重检查冗余

`shouldSkipProcessing`（内存锁 + IsUrlExist）之后马上又做 `InsertIfAbsent` claim。内存锁和 DB claim 各查一遍，语义有重叠（内存锁其实已被 DB 唯一约束完全覆盖，仅剩"省一次渲染"的优化价值）。

### 5. `ProcessData` 把两类数据捆在一起串行处理

`Handler` 对同一用户先跑 projects 再跑 alarms，两个 pipeline 互相拖慢（项目多时告警延迟）。

---

## 二、目标架构

```
                    ┌────────────────────────────┐
                    │   PushPipeline (泛型原语)   │
                    │  - KeyedLock 内存锁         │
                    │  - InsertIfAbsent 抢占      │
                    │  - send / rollback 回调     │
                    └────────────────────────────┘
                          ▲            ▲
        processProjects   │            │  processAlarms
        （实现多chunk/汇总） │            │  （实现单条发送）
                          │            │
                  ┌───────┴────┐  ┌───┴────────┐
                  │ HistoryDAO │  │ AlarmDAO   │
                  └────────────┘  └────────────┘
```

- **统一原语**：`PushPipeline` 泛型处理"锁 → 抢占 → 发送 → 回滚"骨架，管线只需提供 4 个回调。
- **行为不变**：projects 的多 chunk / 失败汇总 / IsForced 强制模式、alarms 的过期占位重抢，全部保留在各自管线内部。

---

## 三、分步实施计划（每步可独立成 PR）

✅ ### Step 1：提取通用 `PushPipeline` 泛型原语（核心步骤）

新增 `pkg/handler/push_pipeline.go`：

```go
// ClaimHandle 定义一条可抢占推送的最小单元。
type ClaimHandle struct {
    Key      string        // 唯一键，如 "userId:creditCode"
    Claim    func() (bool, error) // DB 抢占，返回是否抢到
    Rollback func() error  // 发送失败回滚
    Send     func() error  // 真正发送
}

// PushPipeline 串行处理一批 handle：内存锁 → claim → send → 失败回滚。
type PushPipeline struct {
    locks *KeyedLock
    log   *utils.Logger
}

func (p *PushPipeline) Run(handles []ClaimHandle) {
    for _, h := range handles {
        func() {
            if !p.locks.TryLock(h.Key) { return }
            defer p.locks.Unlock(h.Key)

            claimed, err := h.Claim()
            if err != nil || !claimed { return }

            if err := h.Send(); err != nil {
                _ = h.Rollback()
            }
        }()
    }
}
```

改造后：

- `processAlarms` 变成"构建 []ClaimHandle → 跑 Run"，删除全部手写锁/回滚样板，`!inserted` 的过期重抢收进一个 `claimOrRefresh` 回调。
- `processProjects` 的主循环同样改为构建 handles，把多 chunk 发送 + 汇总逻辑留在 `Send` 回调里（单 URL 处理抽成方法，闭包+大函数消失）。

**验收**：现有 `-race` 测试全过；`TestAlarmClaimPreventsDuplicateAcrossInstances` / `TestHistoryClaimPreventsDuplicateAcrossInstances` 语义不变。

✅ ### Step 2：`processProjects` 瘦身

- 单 URL 处理抽成 `r.pushProject(userId, project, results[j], isForced) (success bool)` 方法，主循环只剩"收集 handles / 跑管线"几行。
- `failed` / `filterFailed` / `processedURL` 从函数级状态收进小结构体或局部闭包。
- 目标：`processProjects` 从 160+ 行降到 ~40 行骨架。

**验收**：行为等价（对比重构前后日志输出 `notify:` / failed 汇总）。

✅ ### Step 3：`processAlarms` 瘦身

- 过期占位重抢封装为 `claimOrRefresh(alarm) (bool, error)`（Remove 旧记录 + 重抢一次，失败回 false）。
- 主循环只保留"去重 → 构建 handle → Run"。

**验收**：跨实例并发测试 + 过期记录单元测试。

### Step 4：消除双重检查（可选，收益评估后决定）

- 保留 `shouldSkipProcessing` 作为渲染前的快速过滤（省 CPU），但明确注释其与 claim 的分工：**预筛 vs 权威**。
- 或：删掉内存锁只留 DB claim（渲染浪费可接受时）。**倾向保留**——预筛能避免大量白渲染。

### Step 5：Pipeline 并行化（独立 PR，性能向）

- `Handler` 里 projects 与 alarms 改为并发执行（两个 goroutine + WaitGroup），互不拖慢。
- 注意：两者共用同一 SQLite，写并发本身有锁；收益需实测。此步不做也无损正确性。

---

## 四、风险与验证

| 风险 | 缓解 |
|---|---|
| 泛型 Pipeline 过度抽象导致难读 | ClaimHandle 保持纯数据 + 回调，不引入框架感；单测覆盖 |
| 行为漂移（汇总/强制模式/部分成功） | 每步后跑全量测试 + `-race`；Step 2/3 对比重构前后日志 |
| 并行化后 SQLite 写竞争 | Step 5 独立评估，可回退 |

**测试基线**：`go test ./pkg/handler/ ./pkg/dal/ -race`（`TestMergeLines`、`Test_alarmQuery`、`Test_historyQuery` 为既有失败，与本次无关）。

---

## 五、不做的事（明确边界）

- 不改推送消息内容/格式
- 不改 `Crawler.Alarms()` 的抓取与去重（按 CreditCode）
- 不改调度方式（gocron SingletonMode）
- 不引入消息队列/任务框架——当前规模用不上
