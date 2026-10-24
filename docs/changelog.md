# Changelog

本文件只记录重要功能迭代和修复。版本号遵循语义化版本。

## 0.3.0 - 未发布

- 改动内容：对外创建引擎的函数由 `New` 改为 `NewEngine`，与 `NewCoordinator` 区分。
- 改动范围：`engine.NewEngine`，示例，README，概要设计第 3 章，详细设计第 3 章。
- 影响：调用方把 `engine.New` 改为 `engine.NewEngine`。内部事件循环的构造函数仍为 `New`。不兼容变更：删除对外 `engine.New`。

- 改动内容：增加可选耦合层。原子模型用端口连接，处理时间由模型自己计算。直接使用引擎的路径保持不变。
- 改动范围：`engine.Coordinator`，`engine.Snapshot` 的可空耦合计数，`example/line`，概要设计第 2、3、4 章，详细设计第 2、7、8、9 章，README。
- 影响：不创建协调器时，事件循环不变，`Engine.Snapshot` 的 `Coupled` 为空。创建协调器后，到点、转发和重排计入原有引擎事件次数，`Coordinator.Snapshot` 另有耦合计数。不兼容变更：无。

- 改动内容：去掉指标 HTTP 服务。状态 JSON 和 Prometheus 文本改由快照方法返回，实现放在 `internal/pkgs/metrics`。接入方自己决定要不要监听端口。
- 改动范围：删除对外 `metrics` 包和 Gin 依赖，新增 `internal/pkgs/metrics`，`Snapshot.StatusJSON`、`Snapshot.Prometheus`，`example/realtime`，概要设计，详细设计第 2、6、7、8 章，README，约束。
- 影响：不再提供 `Listen`。调用方读取快照，或把 `StatusJSON`、`Prometheus` 写进自己的服务。不兼容变更：删除对外 `metrics` 包。

## 0.2.0 - 2026-10-24

- 改动内容：增加暂停、恢复，以及暂停后的检查点与恢复。真实模式恢复时平移墙钟起点。检查点按事件名称接回已登记的处理函数。
- 改动范围：`engine` 的生命周期、`Event.Payload`、`Handle`、`Pause`、`Resume`、`Checkpoint`、`Restore`，概要设计第 3、4、5、6 章，详细设计第 3、8 章，README。
- 影响：已有的启动、停止和插入方式保持可用。检查点不写磁盘，调用方保存字节。未登记名称的事件不能写入检查点。暂停期间 `des_up` 为 0。不兼容变更：无。

## 0.1.0 - 2026-10-24

第一版设计基线。此版本只包含设计文档，仿真内核尚未实现。

### 设计

- 引擎以 Go 1.25 模块 `github.com/cgcodeboy/des-engine` 提供，外部通过 `engine` 与 `metrics` 两个包引用。
- 仿真步长固定为 1ms。时间戳单位是毫秒，一个桶对应一毫秒。
- 默认按真实时间运行。每个桶开始前对齐 `origin + k * 1ms`；已经滞后则立即执行，并通过回调和 Warn 日志交出滞后。
- 当前毫秒执行期间插入的同桶事件追加到待处理队列尾部，形成同一时间窗内的链式触发。
- 增加未来事件：`AddFuture` 接收事件和延迟，立即收下，并在向上取整后的仿真毫秒触发。真实模式无滞后时，该时刻与墙钟延迟一致。
- 早于当前水位的事件被拒绝，防止时间倒流。
- 未来桶、当前队列和运行状态由一把互斥锁保护，条件变量用于等待新事件和打断睡眠。
- 事件执行路径不写日志 IO。日志通过容量 4096 的异步队列写出，队列满时丢弃并计数。`Stop` 在事件循环退出后排空日志，最长等待 2 秒。
- 处理函数墙钟耗时超过 1ms 时记 Warn，并保留最近 128 条慢事件。`GET /status` 列出这些事件，`GET /metrics` 只提供累计次数。

### 文档

- 新增 [概要设计说明书](概要设计.md)。章节按引言、总体设计、接口设计、运行设计、数据结构设计、出错处理设计编排。
- 新增 [详细设计说明书](详细设计.md)。按 ENGINE、BUCKET、ASYNCLOG、METRICS、EXAMPLE 分别给出描述、功能、性能、输入输出、算法、流程、接口、存储、限制和测试。
- 新增本变更记录。
- 开发约束放在 `.cursor/rules/change-process.mdc` 与 `.cursor/rules/code-constraints.mdc`，由 Cursor 在打开本仓库时自动加载。改动顺序为盘点、评估、改动、归档。

### 实现

- 改动内容：实现第一版仿真内核。支持启动、停止、按毫秒分桶、`AddEvent`、`AddFuture`、取消、真实时间等待、追赶、慢事件和异步日志，并提供 `/healthz`、`/status`、`/metrics`。
- 改动范围：`engine`、`metrics`、`internal/bucket`、`internal/asynclog`、`example/realtime`、`go.mod`、`README.md`。`internal/bucket` 使用泛型保存调度节点。
- 影响：外部可以引用 `engine` 和 `metrics`。没有既有调用方。处理函数在引擎锁外执行；日志在独立协程写出。不兼容变更：无。

- 改动内容：指标 HTTP 改为 Gin 路由。`/healthz`、`/status`、`/metrics` 的路径和响应保持不变。
- 改动范围：`metrics`、`go.mod`、`go.sum`、概要设计、详细设计第 6 章和第 8.2 节、README。
- 影响：引用 `metrics` 的程序会带上 Gin 及其依赖。`engine` 仍只使用标准库，也不引用 `metrics`。`Listen` 的签名不变。不兼容变更：无。

- 改动内容：增加可设定测量时长的基准程序。默认每种场景 10 分钟，场景内各档平分，比较不同事件源数量和不同处理复杂度下的吞吐、CPU 与内存。
- 改动范围：`example/bench`、`README.md`、详细设计第 2 章。
- 影响：不改变引擎公开接口。基准关闭真实模式，并用每来源最多 8 个未完成事件限制队列增长。不兼容变更：无。
