# 修复发布流程：只有确认可发布的批次才能发布并封存

## 问题根因

`PublishSnapshot` 通过 `ValidatePublication(batch.Status)` 用**批次的已存状态**判断可发布性。
但批次状态只由 `RunDiagnosis` 更新；`CreateSnapshot` 每次都**重新跑诊断**并把报告固化进快照内容，
**不更新批次状态**。当批次先被诊断为 `publishable`、随后数据变化（新增窗口/产生断层/排除窗口）时，
快照内容反映了断层，而批次状态仍是过时的 `publishable`——发布校验放行，快照被冻结、批次被错误封存，
研究者拿到一份带断层的"可靠结论"。已用复现测试确认（`TestReproStalePublishable`：批次先 publishable、
再加远端窗口、CreateSnapshot 得到 gapEdges=1 converged=false 的快照，PublishSnapshot 仍成功且批次 sealed）。

发布流程必须根据**快照所承载的实际诊断结论**判断可发布性，而不是可能过时的批次状态；
原子封存也必须以"批次当前确实可发布"为前置条件。

## 修改方案

### 1. `internal/diag/diagnose.go` — 新增结论推导函数
新增 `StatusFromReport(report *model.DiagnosisReport) model.BatchStatus`：
返回 `BatchInsufficient`（当 `!report.Converged`）否则 `BatchPublishable`。
集中 `svc_diag.go` 中已有的 `next := BatchPublishable; if !report.Converged { next = BatchInsufficient }` 逻辑，
供发布流程从固化报告重新推导可发布性。

### 2. `internal/snapshot/freeze.go` — 把校验改为基于报告
将 `ValidatePublication(batchStatus model.BatchStatus) error` 改为
`ValidatePublication(report *model.DiagnosisReport) error`：
要求 `report != nil && report.Converged`，否则返回 `ErrStateMismatch`（"诊断结论不可发布"）。
这样封存仅当快照实际承载的诊断已收敛（即可发布）时才放行。

### 3. `internal/service/svc_snapshot.go` — 发布按快照内容校验
`PublishSnapshot` 中：用 `encoding/json` 把 `sn.Snapshot` 反序列化为 `model.DiagnosisReport`，
调用 `snapshot.ValidatePublication(&report)` 替代原来的 `ValidatePublication(batch.Status)`。
保留从 store 取 batch（仅用于 CommitSnapshotPublication 需要 batchID；封存的硬性前置条件交给 store 层）。

### 4. `internal/store/snapshot_store.go` — 原子封存以可发布为条件
`CommitSnapshotPublication` 的批次更新加 `AND status = ?`（= `publishable`）条件，
与既有"快照须为 draft"的条件更新对齐。若批次不再是可发布（被并发诊断为 insufficient 等），
`RowsAffected != 1` 则回滚并返回 `ErrStateMismatch`（"batch %s is not publishable, cannot seal"）。
确保并发与状态变更下只有可发布批次能被封存。

### 5. `internal/service/svc_diag.go` — 复用集中逻辑（一致性收尾）
`RunDiagnosis` 改用 `diag.StatusFromReport(report)` 计算 `next`，避免逻辑重复。

## 测试

- 在 `internal/service` 新增 `svc_snapshot_test.go`（用 `:memory:` store 走真实 service 闭环）：
  - `TestPublishRejectsGapSnapshot`：批次先 publishable、再加远端窗口产生断层、CreateSnapshot 得到断层快照，
    调用 `PublishSnapshot` 应失败（`ErrStateMismatch`），批次保持非 sealed、快照保持 draft。
  - `TestPublishAcceptsConvergedSnapshot`：正常重叠窗口、诊断 publishable、CreateSnapshot、PublishSnapshot 成功，
    快照 published、批次 sealed（即修复不破坏正常路径，对齐 smoke 契约）。
  - `TestCreateSnapshotKeepsInsufficientBatch`：诊断 insufficient 后 CreateSnapshot 再 PublishSnapshot 应失败，
    批次保持 insufficient（覆盖简单路径）。
- 删除临时复现脚本（不入库）。
- 运行 `go vet ./... && go test ./...` 与 `go run ./cmd/task262-freeoverlap --smoke-test` 全绿。

## 影响面与不变性
- 不改批次状态机、不改诊断算法、不改快照 schema/迁移。
- `ValidatePublication` 签名变更：唯一调用点在 `svc_snapshot.go`，同步更新即可。
- smoke 测试（收敛批次发布封存）仍走通；新增的封存条件 `status = publishable` 对 smoke 的正常路径满足。
