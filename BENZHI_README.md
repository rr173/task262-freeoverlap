基于 Go 实现的分子动力学自由能窗口重叠诊断 Web 项目，一款后端服务，完成伞形采样窗口样本的偏置校正与重加权、相邻窗口能量分布重叠计算、断层定位与重采样裁决，并发布不可变的可靠性快照。

# BENZHI 评测说明

## 项目类型

task262-freeoverlap 是自由能计算可靠性诊断的后端服务，暴露 `/api` JSON 接口，
使用 SQLite 持久化全部实体，支持 `--smoke-test` 离线自检。

## 领域背景

伞形采样（Umbrella Sampling）沿反应坐标划分多个采样窗口，每个窗口施加谐振子
偏置势以覆盖高能垒。相邻窗口的能量分布必须足够重叠，WHAM/MBAR 重加权方程才
可靠；若某对窗口分布无交集（断层），自由能曲线在该处出现假势垒。

## 构建与测试

```bash
export CGO_ENABLED=0 GOTOOLCHAIN=local
go build ./...
go vet   ./...
go test  ./...
go run ./cmd/task262-freeoverlap --smoke-test   # 自检 + 重启恢复验证
```

评测镜像使用 `build_benzhi_docker.sh` 构建；脚本第二个参数为目标平台：

```bash
./build_benzhi_docker.sh task262-freeoverlap linux/amd64
./build_benzhi_docker.sh task262-freeoverlap-arm64 linux/arm64
docker run --rm task262-freeoverlap --smoke-test
```

`Dockerfile` 与 `benzhi.Dockerfile` 使用相同的多阶段构建内容，支持
`linux/amd64` 和 `linux/arm64`。服务监听地址由 `--addr` 指定，默认 `:8080`；
镜像不声明固定端口。

## 运行

```bash
go run ./cmd/task262-freeoverlap --addr :8080 --db task262-freeoverlap.db
```

## API 一览（前缀 /api）

| 能力 | 方法与路径 |
|---|---|
| 健康检查 | GET /api/health |
| 创建批次 | POST /api/batches |
| 批次列表/详情 | GET /api/batches、GET /api/batches/{id} |
| 批次状态推进 | POST /api/batches/{id}/advance |
| 运行诊断 | POST /api/batches/{id}/diagnose（GET /api/batches/{id}/diagnosis 只读） |
| 创建窗口 | POST /api/windows |
| 窗口列表 | GET /api/windows?batch_id= |
| 导入样本（幂等） | POST /api/windows/{id}/samples |
| 偏置校正 | POST /api/windows/{id}/correct |
| 窗口状态 | PATCH /api/windows/{id}/status |
| 重叠查询 | GET /api/overlap?a=&b= |
| 边列表 | GET /api/edges?batch_id= |
| 边裁决 | PATCH /api/edges/{id}/adjudicate |
| 快照创建/列表 | POST /api/snapshots、GET /api/snapshots?batch_id= |
| 快照发布/替代 | POST /api/snapshots/{id}/publish、POST /api/snapshots/{id}/supersede |
| 自检 | POST /api/smoke |

## 核心判定（重叠阈值）

- 重叠积分 O = ∫ min(pA, pB) dE（归一化加权直方图）。
- O >= 0.30：重叠充分（sufficient）；
- O <  0.05：断层（gap）——重加权不可靠，需要重采样或补充窗口；
- 其余：候选（candidate / marginal），由研究者裁决。

## --smoke-test 契约

不启动长驻服务；真实创建批次（300K）与三个相邻窗口，导入 200 条/窗口的
确定性样本，重复导入验证幂等，逐窗口偏置校正，运行诊断断言零断层且
min_overlap >= 0.3，创建并发布不可变快照，校验批次进入封存终态；
随后关闭并重新打开同一数据库，验证批次与快照重启后可读。
全部通过以 0 退出码结束。
