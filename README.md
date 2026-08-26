# 构建说明

task262-freeoverlap 是分子动力学自由能窗口重叠诊断服务。本目录为 Git 根。

## 环境

- Go 1.26.3（GOTOOLCHAIN=local）
- SQLite：纯 Go 驱动 modernc.org/sqlite v1.52.0（CGO_ENABLED=0 可离线构建）
- 组件版本锁定：见 `component-versions.json`

## 构建与校验

```bash
export CGO_ENABLED=0 GOTOOLCHAIN=local
go build ./...
go vet   ./...
go test  ./...
go run ./cmd/task262-freeoverlap --smoke-test
```

`--smoke-test` 契约：真实创建批次/窗口/样本，执行偏置校正与重叠诊断，
创建并发布不可变快照，关闭并重新打开同一 SQLite 数据库验证重启恢复，
全部通过后以 0 退出码结束。

## Docker

- `Dockerfile` 与 `benzhi.Dockerfile`：内容完全一致；使用 Go 1.26.3 Bookworm builder 和 Alpine 3.20 runtime 的多阶段构建，产物为 `task262-freeoverlap` 二进制。
- `build_benzhi_docker.sh <镜像名> <平台>`：一键构建，支持 `linux/amd64` 与 `linux/arm64`。
- 镜像默认执行 `--smoke-test`；服务监听地址通过 `--addr` 指定，Dockerfile 不声明端口。

本仓库只生成 Docker 文件，不在本环节执行 docker build。
