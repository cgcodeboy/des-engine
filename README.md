# des-engine

固定步长 1ms 的离散事件仿真引擎。其他 Go 模块通过包引用使用它。

```go
import (
    "github.com/cgcodeboy/des-engine/engine"
    "github.com/cgcodeboy/des-engine/metrics"
)
```

仿真时间的单位是毫秒，一个桶就是一毫秒。默认按真实时间运行：桶 `k` 对齐到 `origin + k * 1ms`。墙钟已经晚于理论时刻时，引擎立即执行该桶，并通过滞后回调和日志交出滞后。

`AddFuture` 接收事件和延迟，立刻返回事件标识，并在向上取整后的仿真毫秒触发。延迟为 0 时追加到当前毫秒队尾。早于已关闭毫秒的事件会被拒绝。

处理函数的墙钟耗时超过 1ms 时记一条 Warn，并进入最近 128 条慢事件。事件执行路径上的日志是异步的。

指标服务：

- `GET /healthz`：指标服务是否在监听
- `GET /status`：JSON 快照，`slow_events` 列出超过 1ms 的事件
- `GET /metrics`：Prometheus 文本，只提供 `des_slow_events_total` 等计数

```bash
go run ./example/realtime
```

示例监听 `:9090`。设计说明在 `docs/概要设计.md` 和 `docs/详细设计.md`。
