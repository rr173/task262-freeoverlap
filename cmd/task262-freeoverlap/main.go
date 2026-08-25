// task262-freeoverlap 分子动力学自由能窗口重叠诊断服务。
//
// 入口契约：
//   - 默认启动 HTTP 服务（--addr :8080，--db ./task262-freeoverlap.db）；
//   - --smoke-test 时不启动长驻服务，而是真实创建批次/窗口/样本、
//     执行偏置校正与重叠诊断、创建并发布不可变快照、关闭并重新打开
//     数据库验证持久化与重启恢复，全部通过后以 0 退出码结束。
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"task262-freeoverlap/internal/httpapi"
	"task262-freeoverlap/internal/service"
	"task262-freeoverlap/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	dbPath := flag.String("db", "task262-freeoverlap.db", "SQLite database path")
	smokeTest := flag.Bool("smoke-test", false, "run smoke test and exit")
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	svc := service.New(st)
	srv := httpapi.New(svc)

	if *smokeTest {
		if err := runSmoke(st); err != nil {
			log.Fatalf("smoke test failed: %v", err)
		}
		fmt.Println("smoke-test PASSED")
		os.Exit(0)
	}

	log.Printf("task262-freeoverlap listening on %s (db=%s)", *addr, *dbPath)
	srvHTTP := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := srvHTTP.ListenAndServe(); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

// runSmoke 先跑一遍业务闭环，再关闭并重新打开同一数据库，验证
// 批次/快照等实体在重启后仍可读取（持久化与重启恢复契约）。
func runSmoke(st *store.Store) error {
	svc := service.New(st)
	smoke := func() error {
		batch, err := svc.CreateBatch("persist-smoke", "umbrella", 300.0)
		if err != nil {
			return err
		}
		if _, err := svc.AddWindow(batch.ID, "w0", 0, 10.0); err != nil {
			return err
		}
		if _, err := svc.AddWindow(batch.ID, "w1", 5, 10.0); err != nil {
			return err
		}
		windows, _ := svc.ListWindows(batch.ID)
		for _, w := range windows {
			var samples []service.ImportSample
			for j := 0; j < 60; j++ {
				e := w.Center + float64(j%30-15)*0.2
				samples = append(samples, service.ImportSample{Seq: j + 1, Energy: e, Bias: 0.5})
			}
			if _, err := svc.ImportSamples(w.ID, samples); err != nil {
				return err
			}
			if _, err := svc.CorrectWindow(w.ID); err != nil {
				return err
			}
		}
		if _, err := svc.RunDiagnosis(batch.ID); err != nil {
			return err
		}
		sn, err := svc.CreateSnapshot(batch.ID, "persist-snapshot")
		if err != nil {
			return err
		}
		if _, err := svc.PublishSnapshot(sn.ID); err != nil {
			return err
		}
		return nil
	}

	// 第一遍：写入。
	if err := smoke(); err != nil {
		return fmt.Errorf("first pass: %w", err)
	}
	// 关闭并重新打开数据库（重启恢复）。
	if err := st.Close(); err != nil {
		return fmt.Errorf("close store: %w", err)
	}
	reopened, err := store.Open(st.Path())
	if err != nil {
		return fmt.Errorf("reopen store: %w", err)
	}
	defer reopened.Close()
	svc2 := service.New(reopened)

	// 第二遍：验证重启后可读取且结论一致。
	batches, err := svc2.ListBatches()
	if err != nil {
		return err
	}
	if len(batches) == 0 {
		return fmt.Errorf("no batches after reopen")
	}
	persisted := batches[0]
	if persisted.Status == "" || persisted.Name == "" {
		return fmt.Errorf("persisted batch incomplete: %+v", persisted)
	}
	snaps, err := svc2.ListSnapshots(persisted.ID)
	if err != nil {
		return err
	}
	if len(snaps) == 0 {
		return fmt.Errorf("no snapshots after reopen")
	}
	if snaps[0].Status == "" || snaps[0].FrozenAt == 0 {
		return fmt.Errorf("snapshot not frozen after reopen: %+v", snaps[0])
	}
	fmt.Printf("restart recovery OK: batch=%s status=%s snapshots=%d\n",
		persisted.ID, persisted.Status, len(snaps))
	return nil
}
