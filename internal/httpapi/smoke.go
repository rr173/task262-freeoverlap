package httpapi

import (
	"encoding/json"
	"fmt"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/service"
)

// smoke 执行一次完整自检：创建批次 -> 添加窗口 -> 导入样本 -> 校正 ->
// 运行诊断 -> 创建并发布快照 -> 用内存清单校验闭环。
//
// 这是 --smoke-test 契约的核心判据：真实创建实体、调用核心逻辑、
// 不启动长驻服务、以 0 退出码结束。
func smoke(svc *service.Service) error {
	// 1. 创建批次（温度 300K -> kT = 2.494 kJ/mol）。
	batch, err := svc.CreateBatch("smoke-batch", "umbrella", 300.0)
	if err != nil {
		return fmt.Errorf("create batch: %w", err)
	}
	if batch.Status != model.BatchReceiving {
		return fmt.Errorf("batch status = %s, want receiving", batch.Status)
	}

	// 2. 添加三个相邻窗口（中心 0/5/10，重叠良好）。
	centers := []float64{0, 5, 10}
	for i, c := range centers {
		if _, err := svc.AddWindow(batch.ID, fmt.Sprintf("w%d", i), c, 10.0); err != nil {
			return fmt.Errorf("add window %d: %w", i, err)
		}
	}
	windows, err := svc.ListWindows(batch.ID)
	if err != nil || len(windows) != 3 {
		return fmt.Errorf("list windows: %d (err %v)", len(windows), err)
	}

	// 3. 导入样本：每个窗口 200 条，分布围绕中心，窗口间有重叠。
	for _, w := range windows {
		var samples []service.ImportSample
		for j := 0; j < 200; j++ {
			// 确定性伪高斯：中心 + (j%40-20)*0.15，保证两两重叠。
			e := w.Center + float64(j%40-20)*0.15
			samples = append(samples, service.ImportSample{Seq: j + 1, Energy: e, Bias: 0.5})
		}
		res, err := svc.ImportSamples(w.ID, samples)
		if err != nil {
			return fmt.Errorf("import samples into %s: %w", w.ID, err)
		}
		if res.Inserted != 200 || res.Duplicated != 0 {
			return fmt.Errorf("import result = %+v, want 200 inserted", res)
		}
		// 重复导入应幂等（全部去重）。
		res2, err := svc.ImportSamples(w.ID, samples)
		if err != nil || res2.Inserted != 0 || res2.Duplicated != 200 {
			return fmt.Errorf("idempotent import failed: %+v (err %v)", res2, err)
		}
	}

	// 4. 校正每个窗口。
	for _, w := range windows {
		summary, err := svc.CorrectWindow(w.ID)
		if err != nil {
			return fmt.Errorf("correct window %s: %w", w.ID, err)
		}
		if summary.ESS <= 0 {
			return fmt.Errorf("window %s ESS = %v", w.ID, summary.ESS)
		}
	}

	// 5. 运行诊断：三个重叠窗口应无断层 -> 可发布。
	report, err := svc.RunDiagnosis(batch.ID)
	if err != nil {
		return fmt.Errorf("diagnose: %w", err)
	}
	if report.GapEdges != 0 {
		return fmt.Errorf("gap edges = %d, want 0 (windows should overlap)", report.GapEdges)
	}
	if report.MinOverlap <= 0.3 {
		return fmt.Errorf("min overlap = %v, want >= 0.3", report.MinOverlap)
	}
	if !report.Converged {
		return fmt.Errorf("report should be converged: %+v", report)
	}

	// 6. 创建并发布快照，校验不可变。
	sn, err := svc.CreateSnapshot(batch.ID, "smoke-snapshot")
	if err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}
	var parsed model.DiagnosisReport
	if err := json.Unmarshal([]byte(sn.Snapshot), &parsed); err != nil {
		return fmt.Errorf("snapshot content not valid JSON: %w", err)
	}
	if parsed.BatchID != batch.ID {
		return fmt.Errorf("snapshot batch = %s, want %s", parsed.BatchID, batch.ID)
	}
	published, err := svc.PublishSnapshot(sn.ID)
	if err != nil {
		return fmt.Errorf("publish snapshot: %w", err)
	}
	if published.Status != model.SnapshotPublished || published.FrozenAt == 0 {
		return fmt.Errorf("snapshot not frozen: %+v", published)
	}

	// 7. 校验批次进入封存终态。
	final, err := svc.GetBatch(batch.ID)
	if err != nil {
		return err
	}
	if final.Status != model.BatchSealed {
		return fmt.Errorf("batch status = %s, want sealed after publish", final.Status)
	}

	return nil
}
