<p align="center">
  <img src="docs/banner.png" alt="des-engine：离散事件仿真" width="920">
</p>

# des-engine

<p align="center">按 1 毫秒推进的离散事件仿真引擎。嵌在其他 Go 程序里用。</p>

<p align="center">
  <a href="#这是什么">这是什么</a>
  ·
  <a href="#快速开始">快速开始</a>
  ·
  <a href="#观测">观测</a>
  ·
  <a href="#基准测试">基准测试</a>
  ·
  <a href="#文档">文档</a>
</p>

| 能力 | 说明 |
| --- | --- |
| 时钟 | 固定 1 ms。空毫秒跳过。默认对齐墙钟，滞后则追赶 |
| 插入 | `AddEvent` 用绝对时间，`AddFuture` 用相对延迟。同一毫秒可以链式触发，过去的事件会被拒绝 |
| 内核 | 一个协程按顺序执行。处理函数不做 IO |
| 观测 | 慢事件、异步日志，以及 `/healthz`、`/status`、`/metrics` |

## 这是什么

仿真时间是整数毫秒。每一毫秒是一个桶，桶按时间从小到大执行，中间没有事件的毫秒会直接跳过。

默认和墙钟对齐：第 `k` 毫秒的理论时刻是 `origin + k * 1ms`。墙钟还没到，引擎就等到那个时刻。墙钟已经过了，引擎立刻执行这一毫秒，并通过回调和日志交出滞后。时间不会倒退。

事件循环是一个协程。日志在另一条协程里写出，指标服务用 Gin 提供 HTTP。`engine` 不引用 `metrics`，指标只读快照，不把状态写回引擎。

仿真内核只使用 Go 标准库。指标路由使用 Gin。

## 边界

des-engine 负责仿真时钟和事件执行。网络模型、排队规则和业务状态由调用方写在处理函数里。

处理函数在同一个协程里顺序执行，不会并行。处理函数和滞后回调不持有引擎锁，因此不能调用 `Stop` 或 `Wait`，也不能做日志、网络或磁盘 IO。需要留下结果时，写到调用方自己的内存里。

步长和慢事件阈值都固定为 1 毫秒，不能配置。状态只存在于进程内存。

## 为什么是 1 毫秒

离散事件仿真要同时说清两件事：事件发生在哪个仿真时刻，以及这个时刻和墙钟是什么关系。

只用 `time.Timer`，随机插入的事件没有统一的仿真时钟，也拒绝不了“插入一个已经过去的时刻”。只用尽快跑完的虚拟时钟，又看不出程序有没有跟上真实时间。

固定 1 毫秒把这两件事合成一个时钟。同一毫秒里新插入的事件排在当前队列尾部，仍然赶在下一毫秒之前执行。早于已关闭毫秒的事件直接拒绝。落后墙钟时不再补等，而是追赶，并把滞后留下来。

## 安装

需要 Go 1.25。

```bash
go get github.com/cgcodeboy/des-engine
```

对外只有两个包：

```go
import (
	"github.com/cgcodeboy/des-engine/engine"
	"github.com/cgcodeboy/des-engine/metrics"
)
```

## 快速开始

下面的程序在启动前排入一个 20 毫秒后触发的事件。默认开着真实模式，所以它会等到相应的墙钟时刻。队列空了以后引擎退出。

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cgcodeboy/des-engine/engine"
)

func main() {
	eng, err := engine.New(engine.WithExitWhenIdle(true))
	if err != nil {
		log.Fatal(err)
	}

	fired := make(chan engine.Time, 1)
	_, err = eng.AddFuture(20*time.Millisecond, engine.Event{
		Name: "later",
		Handler: func(c engine.Context) error {
			fired <- c.Now()
			return nil
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := eng.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
	if err := eng.Wait(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("fired at", <-fired, "ms")
}
```

处理函数通过带缓冲的通道把仿真时间交出去，避免在引擎协程里阻塞。

## 排入事件

`AddFuture(after, ev)` 立刻返回事件标识，并在 `Now()` 加上延迟之后触发。延迟向上取整到毫秒。延迟为 0 时，如果这一毫秒正在执行，事件追加到当前队列尾部。

`AddEvent(at, ev)` 使用绝对仿真时间，单位是毫秒。

```go
id, err := eng.AddFuture(100*time.Millisecond, engine.Event{
	Name: "work",
	Handler: func(c engine.Context) error {
		_, err := c.AddFuture(0, engine.Event{
			Name:    "follow-up",
			Handler: func(engine.Context) error { return nil },
		})
		return err
	},
})
if err != nil {
	log.Fatal(err)
}
eng.Cancel(id)
```

还没开始执行的事件可以 `Cancel`。正在执行的事件取消会返回 false。

时间早于当前水位时返回 `engine.ErrPast`。名字为空、处理函数为 nil、引擎已经停止，分别返回 `ErrName`、`ErrHandler`、`ErrClosed`。`Start` 只能调用一次，再次调用返回 `ErrStarted`。

## 和墙钟对齐

真实模式是默认行为。`WithOrigin` 指定仿真时间 0 对应的墙钟时刻，不指定就用 `Start` 的时刻。某一毫秒开始得晚了，`WithLagHandler` 会收到这一毫秒的滞后；同一毫秒只回调一次。

要测量处理能力，或者让仿真尽快跑完，关掉真实模式：

```go
eng, err := engine.New(
	engine.WithRealtime(false),
	engine.WithExitWhenIdle(true),
)
```

`WithHorizon` 在到达给定仿真时间后停止。`WithSeed` 决定处理函数里 `Context.Rand()` 的随机序列，默认种子是 1。

## 暂停与检查点

`Pause` 等当前毫秒执行完后停住，不生成检查点。`Resume` 从这里继续。真实模式下，恢复会把墙钟起点往后推，暂停的那段时间不会被追赶。

`Checkpoint` 只能在暂停后调用，返回一份字节。引擎不保存它，调用方可以保留多份。同一进程里暂停后直接恢复，不需要检查点。

进程重新拉起时，先用 `Handle` 登记同名处理函数，再 `Restore`。引擎按名称把队列接上，结果仍是暂停，然后 `Resume`。事件自己的数据放在 `Payload` 里，处理函数用 `Context.Payload` 读取。

## 观测

处理函数的墙钟耗时超过 1 毫秒时，引擎记一条 Warn，并保留最近 128 条慢事件。事件路径上的日志只进入队列，队列容量 4096，满了就丢弃并计数。`Stop` 会在事件循环退出后把队列排空，最多等 2 秒。

指标服务和引擎分开启动：

```go
go metrics.Listen(ctx, ":9090", eng, nil)
```

| 方法 | 路径 | 内容 |
| --- | --- | --- |
| GET | `/healthz` | 指标服务在监听时返回 `ok`。引擎已经停止也仍然返回成功 |
| GET | `/status` | JSON 快照。`slow_events` 是最近的慢事件 |
| GET | `/metrics` | Prometheus 文本。慢事件只有累计次数 `des_slow_events_total`，不按事件名拆标签 |

`/healthz` 表示指标端口是否可用，不表示仿真仍在运行。仿真是否在跑看 `/status` 的 `state`，或 Prometheus 的 `des_up`。

## 示例

真实模式、同毫秒链式事件、未来事件、慢事件和指标端口：

```bash
go run ./example/realtime
```

进程监听 `:9090`。中断信号会停掉引擎。

## 测试

```bash
go test ./...
```

<!-- bench:start -->
## 基准测试

下面的数字来自一次完整运行。真实模式是关闭的，所以测到的是事件循环的处理能力，而不是睡眠。

每种场景测量 10 分钟，场景内各档平分。场景 A 每档 2 分钟，场景 B 每档 3 分 20 秒。每档开始前另有 5 秒预热，预热不计入结果。事件源在整个窗口内持续插入，触发时刻在 0 到 10 秒仿真时间之间均匀随机。每个来源最多同时有 8 个未完成事件。复杂度是处理函数中的整数运算次数。

运行环境是 darwin/arm64，8 个逻辑 CPU。CPU 列是测量窗口内进程用户态加内核态时间除以墙钟，单位是核。堆峰值是窗口内 Go `HeapInuse` 的最大值。MaxRSS 是进程启动以来的驻留高水位，后面的档不会低于前面的档。

![每秒完成事件数](example/bench/result.png)

### 场景 A：中等复杂度（256 次运算）

| 来源数 | 完成事件 | 每秒事件 | CPU 核 | 堆峰值 MiB | MaxRSS MiB |
| --- | ---: | ---: | ---: | ---: | ---: |
| 1 | 88442314 | 737019 | 1.41 | 4.5 | 11.4 |
| 10 | 70001788 | 583348 | 2.07 | 5.2 | 11.4 |
| 100 | 68497012 | 570808 | 2.13 | 4.3 | 11.6 |
| 1000 | 70702046 | 589184 | 2.25 | 9.2 | 19.9 |
| 4000 | 76091369 | 634095 | 2.46 | 24.6 | 47.3 |

### 场景 B：1000 个来源

| 复杂度 | 完成事件 | 每秒事件 | CPU 核 | 堆峰值 MiB | MaxRSS MiB |
| --- | ---: | ---: | ---: | ---: | ---: |
| 轻 | 150237665 | 751188 | 2.67 | 12.1 | 47.3 |
| 中 | 125162193 | 625811 | 2.21 | 12.1 | 47.3 |
| 重 | 32374941 | 161875 | 1.55 | 12.0 | 47.3 |

事件循环只有一个协程。来源变多不会让处理函数并行，多出来的开销主要在插入时的锁上。复杂度升高后，时间花在处理函数里，吞吐下降。

`-duration` 是每种场景的总测量时间。不传则是 10 分钟。

```bash
go run ./example/bench
go run ./example/bench -duration 30s
```

<!-- bench:end -->

## 文档

使用和总体结构见 [概要设计](docs/概要设计.md)。包、接口和流程见 [详细设计](docs/详细设计.md)。重要功能变更见 [变更记录](docs/changelog.md)。
