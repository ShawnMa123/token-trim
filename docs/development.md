# 开发指南

## 目录

```text
.
├── cmd/rtk-go/            # 进程入口
├── internal/
│   ├── buildinfo/         # 版本注入
│   ├── cli/               # argv 解析、exec、管理命令
│   ├── codex/             # Codex 命令识别/重写分析
│   ├── config/            # schema=1 配置
│   ├── doctor/            # 只读诊断辅助
│   ├── exec/              # 进程、捕获、进程树
│   ├── filter/            # 纯函数过滤器
│   ├── recovery/          # 原文存储/recall
│   └── stats/             # 本地统计
├── docs/                  # 使用、架构、配置、安全、benchmark
├── test/e2e/              # Codex + 本地模型的端到端 harness
├── go.mod                 # go 1.25.0，零第三方模块
└── README.md
```

约定：

- `internal/filter` 不启动进程、不访问网络、不写文件；
- 新增外部依赖前先证明标准库不可行，并单独审查；
- 测试 fixture 使用合成数据，不提交真实公司日志/凭证；
- 所有路径参数化到临时目录，测试之间不共享状态。

## 构建

```sh
GOTOOLCHAIN=local CGO_ENABLED=0 go build -mod=readonly -trimpath -o rtk-go ./cmd/rtk-go
```

Windows：

```powershell
$env:GOTOOLCHAIN="local"; $env:CGO_ENABLED="0"
go build -mod=readonly -trimpath -o rtk-go.exe ./cmd/rtk-go
```

版本注入：

```sh
go build -trimpath \
  -ldflags "-X github.com/ShawnMa123/token-trim/internal/buildinfo.Version=0.2.0 \
            -X github.com/ShawnMa123/token-trim/internal/buildinfo.Commit=$(git rev-parse --short HEAD)" \
  -o rtk-go ./cmd/rtk-go
```

## 测试

```sh
go test ./...
go vet ./...
go test -race ./...
```

Go 1.25 兼容性必须用真实 `1.25.x` + `GOTOOLCHAIN=local GOPROXY=off` 验证；本机更高版本
不能替代。Linux/Windows 交叉构建：

```sh
GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o /tmp/rtk-go ./cmd/rtk-go
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o /tmp/rtk-go.exe ./cmd/rtk-go
```

`test/e2e/bench.go` 只在 Linux 构建（读取 `Rusage.Maxrss`），因此带 `//go:build linux`。

## 端到端 harness

`test/e2e` 使用 Codex CLI + 本地 magpie 模型，在隔离容器里比较 baseline、rtk-go、原版 RTK。
它需要：

- 本机 magpie 可被 Docker 访问，模型 `my-qoder-local/DeepSeek-Flash`；
- 可构建 Linux 容器；
- 本地原版 RTK 镜像 `rtk-original:0.49.0`（仅用于对比）。

主 benchmark：

```sh
cd test/e2e
RUNS=6 ./run-e2e.sh
```

更多 fixture 和说明见 [`../test/e2e/README.md`](../test/e2e/README.md)。结果摘要写入
[`benchmarks.md`](benchmarks.md)。

## 新增过滤器

1. 先写负向测试：畸形输入、非 UTF-8、超长行、机器格式、无收益、原文不可用；
2. 在 `internal/filter` 实现纯 `Apply`，显式标记 `Lossy`/`RemovedLines`；
3. 在 `Select` 加**严格**的 argv 形态匹配，未知 flags 一律返回 nil；
4. 有损候选必须经过 recovery 成功后才输出；
5. 更新 `docs/usage.md` 支持矩阵和 `docs/security.md` 边界；
6. 运行 `go test ./...`、`go vet ./...`、必要时 `-race`。

不要为了让输出更短而改变命令参数、重跑失败命令或隐藏诊断。

## 发布检查

- [ ] `go test ./...`、`go vet ./...`、`go test -race ./...` 通过
- [ ] 真实 Go 1.25.x、`GOTOOLCHAIN=local`、`GOPROXY=off` 构建通过
- [ ] Linux/Windows 目标构建通过
- [ ] `go list -m all` 只有主模块
- [ ] 二进制不包含测试 fixture、真实日志、凭证
- [ ] README/docs 的 benchmark 数字与 `docs/benchmarks.md` 一致
- [ ] 版本/commit 注入正确
- [ ] `rtk-go doctor` 在目标平台通过
