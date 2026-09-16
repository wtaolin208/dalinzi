# 《未来战争》Go 参赛程序

## 作战策略实现

当前决策按 [作战策略](docs/作战策略.md) 组织；模块职责、扩展接口、参数与待实测边界见 [实现对照](docs/作战策略实现对照.md)。全局风险与返航计划先于角色分工，应急物品先于炮控，经济候选统一参与资源和路径联合分配。策略参数位于 `configs/default.json` 的 `strategy` 段。

这是按 [程序设计说明书](程序设计说明书.md) 实现的可运行决策服务与离线复现工具。只依赖 Go 标准库，当前在 Go 1.24.1 上开发。不是官方游戏引擎，也没有连接尚未提供地址与接口的平台。

## 比赛平台源码提交

在项目根目录执行 `python pack.py`，生成 `CoreGeek.tar.gz`。解压后的编译目录为 `CoreGeek/src/`，含根入口 `main.go`、`go.mod`、`run.sh`、`Makefile` 以及 `cmd/`、`configs/`、`internal/`、`scripts/`。当前只依赖 Go 标准库，没有 `go.sum`；将来生成该文件后会自动纳入包。

参照 Demo 的直接入口方式，Go 程序直接接收第一个位置参数作为端口，不依赖 `run.sh`。在 `CoreGeek/src/` 下等价的编译与启动示例为（最终编译命令和二进制名称以平台为准）：

```sh
GO111MODULE=on CGO_ENABLED=0 go build -trimpath -o main main.go
./main 18080
```

根入口与 `cmd/agent` 共用 `internal/agentapp`，避免两份服务逻辑分叉。程序自动读取可执行文件旁的 `configs/default.json`，不存在时从当前目录查找；可通过 `AGENT_CONFIG` 或 `-config` 指定配置。`AGENT_DEBUG=false` 可关闭调试日志，无需经过 shell 脚本。`Makefile`、`run.sh` 仅为本地编译启动辅助及旧发布物兼容入口，不是平台启动前提。

打包只修改包内 `go.mod` 的版本为 `1.24.7`，不修改本地 `go 1.24.0`。版本替换后重新计算 TAR 条目长度，并将 shell 脚本转换为 LF、赋予可执行权限。`*.md`、隐藏目录、文档、日志和旧构建产物不纳入提交；单测样例位于 `internal/protocol/testdata/`，解包后可运行 `go test ./...`。新增运行时资源应放入白名单目录，或同步调整 `pack.py` 白名单。

提交前检查 `tar -tzf CoreGeek.tar.gz`，并在解包目录重新构建、测试。平台源码提交使用此 TAR.GZ；下文 `scripts/package.ps1` 的 ZIP 继续用于本地版本冻结和已有迭代工具。生成源码包不表示地图配置已核验，正式参赛仍须完成下文配置检查。

`python scripts/verify_submission.py CoreGeek.tar.gz` 检查解包后的测试、构建以及直接启动时的 HTTP 端口与请求处理。如果只使用本机已有 Go，可加 `--local-go`，仅修改临时解包副本的 Go 版本，原压缩包和本地工程保持不变；该结果不代表 Go 1.24.7 平台环境验证通过。

## 本地运行

本机原有环境设置了 `GO111MODULE=off`，本项目需要模块模式。仅为当前终端设置，不必修改系统全局配置：

```powershell
$env:GO111MODULE='on'
go test ./...
go build -trimpath -o bin/agent.exe ./cmd/agent
go build -trimpath -o bin/replay.exe ./cmd/replay
./bin/agent.exe -config configs/default.json -logs logs 18080
```

HTTP 服务监听 `0.0.0.0:18080`；`GET /healthz` 检查存活，POST 路径兼容 Demo。发送接口样例：

```powershell
Invoke-RestMethod -Method Post -Uri http://127.0.0.1:18080/ -ContentType 'application/json' -InFile docs/request.txt
```

Go flags 在端口参数之前。请求返回 `roleCommandMap`、`prompt`、`executeCmd`。模型和命令由判题器执行，下一回合返回结果；程序不在本机运行模型生成的 shell 命令。

## 必须配置的地图信息

**正式比赛前必须补齐已核验建造区。** 文档输入没有建造掩码，本地也没有官方地图坐标文件。默认配置不猜测合法建造位置，因此可以运行采集、交易、任务、已有炮塔防守等逻辑，但不会新建炮塔/墙，不能用此默认配置宣称能守住完整比赛。

`profiles` 按 `challenger`、`defender` 分别配置，每个 profile 包含：

- `verified: true`：表示这些坐标已由配置提供者按官方地图核验。
- `weapons: [{"pos":{"x":整数,"y":整数},"kind":"gatling|railgun|rocket"}]`。
- `walls: [{"x":整数,"y":整数}]`，含允许施工的墙格。

没有匹配的 verified profile 时不建造。`configs/demo-experimental.json` 可显式开启 Demo 风格的基地方向布局，**只供联调，不是确认合法的比赛地图**：

```powershell
./bin/agent.exe -config configs/demo-experimental.json 18080
```

`followMoves` 默认 false：在跟随移动语义未核验前，不进入其他角色本回合初始格；最优性限定在这个保守约束下。`allowDuplicateShots` 默认 false，升级武器需要足够的互不重复合法落点，否则本轮不发射。

## 已实现内容

| 模块 | 实际行为 |
| --- | --- |
| 协议 | 全量请求 DTO、可缺字段指针、基地四格、八方向距离、运行时射程优先 |
| 导航 | 多目标 BFS、三人联合状态 A*、等待/让路/互换限制、角色锁定、上下界及停止原因 |
| 仲裁 | 统一金币预算、个人背包检查、建造数量与位置检查、控塔互斥、移动冲突递归消除 |
| 经济 | 合法配置建塔/建墙、采矿、批量卖矿、升级券购买与送达使用、药剂和修墙 |
| 防守 | 按角色与武器分配比较联合返防路线，提前返防，冷却控制 |
| 攻击 | 加特林锥角/首个命中，电磁总能量消耗，火箭 3×3 溅射、多塔联合候选评分 |
| 任务 | 接取、活动任务原地保持、LLM/沙盒跨回合关联、答案提交、超时前答案复用 |
| 经验 | 版本化可信任务模板，参数用 JSON/base64 传给沙盒脚本，结果直接提交，省去模型往返 |
| 新闻宝藏 | 跨日原文记忆、每日受限语义请求、高置信宝藏计划、采购献祭、结果码清理 |
| 服务 | 按回合串行提交、语义相同请求缓存、同回合冲突拒绝、panic 空响应、健康检查 |
| 复现 | 原始字节、完整 Memory 前后、实际配置/响应、路径与战斗 trace、哈希校验、回放 CLI |

任务模板示例见 [recipe-example.json](configs/recipe-example.json)。模板 `pattern` 的命名捕获参数以 JSON 放入 Python 的 `sys.argv[1]`，脚本必须输出 `{"answer":"答案字符串"}`，且以退出码 0 结束。模板由项目配置提供；程序不会把未经证实的模型解法自动提升为可信模板。模型探索仍可处理未覆盖题型。

## 寻路最优性与边界

静态单人 BFS 完整求解。三人 A* 以同步回合为边代价，优化所有分配角色同时到位时间；完整搜索时返回 `optimal=true`，受扩展数、状态数或 deadline 限制时返回 `optimal=false`，必要时采用明确标记的安全一步降级。

每个角色—炮塔分配分别保存搜索证据。若任一有竞争力的分配未完成，不能将选中分配的局部最优说成全局分配最优。战斗候选使用截断/束搜索，与导航精确算法分开。

当前动态处理是“按当前可见占格每回合重规划”，没有实现对未知敌方未来动作的全知求解，也未接入机器人未来轨迹的时间展开模型。返防路线之外的采销目标选择是收益启发式，不是整个生产流程的全局最优规划。

弹道目前采用“线段与单位方格相交、同交点按 ID 排序”的几何假设；起点为炮塔，尚未实现建筑遮挡，因为官方文字未明确其结算方式。PvP 与主动机器人召唤暂未启用，避免套用未验证数值。新闻用于宝藏线索，未来矿价预测尚未作为采销计划的硬依据。

## 日志与故障复现

默认开启调试日志。**正式比赛一键关闭**（无需修改配置或重新编译）：

```sh
AGENT_DEBUG=false bash run.sh 18080
```

直接运行 Windows/Linux 二进制时使用 `agent -debug=false -config configs/default.json 18080`。参数必须放在端口之前。

关闭后不创建日志目录、不写回放文件、不输出启动/逐回合摘要，并跳过日志专用的 Memory 拷贝、记录哈希及二进制摘要计算；保留警告和错误。`-logs` 指定的目录在此模式下不会使用，也不会删除以前的日志。关闭期间无法生成完整回放包。

调试时改回 `AGENT_DEBUG=true bash run.sh 18080` 或 `agent -debug=true ...`。环境变量由 `run.sh` 读取；直接运行二进制请传 `-debug` 参数。

每次请求写入 `logs/<runId>/<sequence>-e<epoch>-r<round>.json`。`requestRaw`、`responseRaw` 为原始字节的 base64 编码，避免失去原文；配置、Memory 和 trace 为可读 JSON。控制台仅输出回合摘要。

```powershell
./bin/replay.exe -verify logs/<runId>/<record>.json
./bin/replay.exe logs/<runId>/<record>.json
./bin/replay.exe -timeline logs/<runId>
./bin/replay.exe -export repro.zip logs/<runId>/<record>.json
```

`-verify` 核对原消息、配置和前后状态的哈希；回放读取原始请求和决策前 Memory，比较响应与 Memory 后状态。重放需使用相同版本程序；启动日志记录二进制 SHA-256，保存对应二进制即可追溯。

限时搜索若当时因墙钟截止中断，重新计算可能得到不同结果，CLI 会以非零退出码和提示报告差异，不能将其自动诊断为算法错误。扩展数和状态数上限保存在配置中，完整搜索及确定性预算截止可重复。回放不访问 LLM、不执行沙盒命令。

下一条原始请求含上回合动作/任务/工具反馈，可通过回合号关联。`-timeline` 列出回合事件与判题器错误；`-export` 导出同半场前 20/后 5 回合的 ZIP 包，附完整性清单和反馈是否存在的标志，不覆盖已有包。完整记录可独立复现决策，但未实现交互式 explain、不同二进制自动 diff 或日志自动轮转；长时间对战需管理日志容量。队列满或写盘失败会显式报错；正常关闭后有丢失时写 `INCOMPLETE.txt`。硬崩溃可能丢失尚未落盘的异步记录。

## 构建 Linux 发布物

官方编译环境尚未提供，以下目标为常见 Linux amd64，需按平台要求调整：

```powershell
$env:GO111MODULE='on'
$env:GOOS='linux'
$env:GOARCH='amd64'
$env:CGO_ENABLED='0'
go build -trimpath -o dist/linux-amd64/agent ./cmd/agent
go build -trimpath -o dist/linux-amd64/replay ./cmd/replay
```

把 `agent`、`run.sh` 与所需 `configs` 放在同一发布目录，Linux 上为 `agent` 设置执行权限。平台按 `bash run.sh port` 启动；环境变量 `AGENT_CONFIG` 可指定已核验配置。不要把 logs、历史对战包、账户信息打进源码包。

提供 `scripts/build.ps1` 和 `scripts/test.ps1` 便于本地使用。若系统执行策略禁止脚本，直接运行上面的 Go 命令，不需要修改系统执行策略。构建脚本会恢复它临时修改的环境变量。

## 测试

```powershell
$env:GO111MODULE='on'
go test ./...
go vet ./...
go test ./internal/navigation ./internal/game -run '^$' -bench . -benchmem
```

测试包含小图独立联合 BFS oracle、35 组固定种子的随机布局、最优性与搜索中止、共享金币、移动争格、控塔冲突、战斗伤害、采销建设升级、任务反馈、模板快速路径和 HTTP 并发幂等/日志重放。另有完整两个半场 2600 回合的合成快照测试，不代表官方实战。当前已在 Ubuntu/WSL、Go 1.24.1、gcc 环境通过 `go test -race ./...`，并验证 Linux 服务启动与 SIGTERM 日志刷新；Windows 原生 race 环境仍未配置。

## 发布预检与平台流程

- `cmd/preflight`：核对配置与初始快照中的坐标、占格、操作位；默认配置会报告未通过。
- `scripts/package.ps1`：白名单源码冻结、冻结版本测试、Linux ZIP 与 SHA-256 清单；`-LocalOnly` 用于本地未核验包。
- `cmd/iterate`：候选包冻结、可恢复的上传/开赛/取日志状态机；真实平台适配器需按官方资料实现。
- `cmd/review`：逐条记录自动重放、响应/Memory 对比、判题异常和延迟摘要。

命令、适配器契约、证据路径和剩余资料见 [平台接入与验收](docs/平台接入与验收.md)。平台工作仍遵循 [循环迭代skill.md](循环迭代skill.md)。尚无实际平台胜率；比赛前还需核验地图、弹道/冷却/结算、真实任务沙盒，并用官方日志校准策略。
