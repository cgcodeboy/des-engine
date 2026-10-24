<p align="center">
  <img src="docs/banner.png" alt="des-engine：离散事件仿真" width="920">
</p>

# des-engine

<p align="center">按 1 毫秒推进的离散事件仿真引擎。嵌在其他 Go 程序里用。</p>

<p align="center">
  <a href="#核心能力">核心能力</a>
  ·
  <a href="#des">DES</a>
  ·
  <a href="#devs">DEVS</a>
  ·
  <a href="#墙钟">墙钟</a>
  ·
  <a href="#暂停与恢复">暂停与恢复</a>
  ·
  <a href="#检查点">检查点</a>
  ·
  <a href="#指标">指标</a>
  ·
  <a href="#示例">示例</a>
  ·
  <a href="#基准测试">基准测试</a>
  ·
  <a href="#文档">文档</a>
</p>

## 核心能力

两种运行方式共用同一台引擎、同一条事件循环。不创建协调器时，循环里只有调用方排入的事件。

| 能力 | 入口 | 用法 |
| --- | --- | --- |
| [DES](#des) | `NewEngine`、`AddEvent`、`AddFuture` | 调用方直接排事件，并在处理函数里写业务 |
| [DEVS](#devs) | `NewCoordinator`、`Atomic` | 模型自己计算下一次到点，端口负责把输出接到输入 |
| [墙钟](#墙钟) | `WithRealtime`、`WithOrigin`、`WithLagHandler` | 默认按墙钟对齐；关掉之后尽快跑完 |
| [暂停与恢复](#暂停与恢复) | `Pause`、`Resume` | 当前毫秒排空后停住，再从原处继续 |
| [检查点](#检查点) | `Handle`、`Checkpoint`、`Restore` | 暂停后取出字节，另一台引擎按名称接回 |
| [指标](#指标) | `Snapshot`、`StatusJSON`、`Prometheus` | 库返回正文。接入方自己写服务 |

## 这是什么

仿真时间是整数毫秒。每一毫秒是一个桶，桶按时间从小到大执行，中间没有事件的毫秒会直接跳过。

默认和墙钟对齐：第 `k` 毫秒的理论时刻是 `origin + k * 1ms`。墙钟还没到，引擎就等到那个时刻。墙钟已经过了，引擎立刻执行这一毫秒，并通过回调和日志交出滞后。时间不会倒退。

事件循环是一个协程。日志在另一条协程里写出。计数和慢事件通过 `Snapshot` 读取，库本身不监听端口。

仿真内核只使用 Go 标准库。

## 边界

des-engine 负责仿真时钟和事件执行。网络模型、排队规则和业务状态由调用方写在处理函数里。

处理函数在同一个协程里顺序执行。处理函数和滞后回调不持有引擎锁，不能调用 `Stop`、`Wait`、`Pause`、`Resume`、`Checkpoint`，也不能做日志、网络或磁盘 IO。需要留下结果时，写到调用方自己的内存里。

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

对外只有 `engine`：

```go
import "github.com/cgcodeboy/des-engine/engine"
```

## DES

DES 由调用方决定每个事件的仿真时刻。`AddEvent` 用绝对毫秒，`AddFuture` 用相对延迟。处理函数、排队和服务时间都写在调用方自己的代码里。

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
	eng, err := engine.NewEngine(engine.WithExitWhenIdle(true))
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

处理函数通过带缓冲的通道把仿真时间交出去，避免在引擎协程里阻塞。处理函数和滞后回调不持有引擎锁，不能调用 `Stop`、`Wait`、`Pause`、`Resume`、`Checkpoint`，也不能做日志、网络或磁盘 IO。

`Start` 只能调用一次，再次调用返回 `ErrStarted`。`Wait` 阻塞到事件循环结束，并且日志已经排空。`Stop` 在当前处理函数返回后退出循环。`WithExitWhenIdle(true)` 在没有事件时结束。默认会一直等到 `Stop` 或上下文取消。`WithHorizon` 不执行给定仿真时间之后的桶，等于该时刻的桶仍会执行。

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

时间早于当前水位时返回 `engine.ErrPast`。名字为空、处理函数为 nil、引擎已经停止，分别返回 `ErrName`、`ErrHandler`、`ErrClosed`。

## DEVS

DEVS 在同一台引擎上增加一个协调器。调用方实现 `Atomic`，用 `Add` 登记，用 `Link` 把一个模型的输出端口接到另一个模型的输入端口。协调器只向引擎插入最近的一个到点事件。服务时间和排队仍在模型里。

`Advance` 返回下一次内部事件的延迟。第二个返回值为 false 时，模型等待输入。负延迟按立即到点处理。到点时先 `Output`，再 `Internal`，然后把输出送到下一跳的 `External`。`External` 的 `elapsed` 是距上一次转移的仿真时间。

模型方法跑在事件循环上，限制与 DES 的处理函数相同。它们不能调用 `Inject` 或 `Add`。外部输入用根协调器的 `Inject`，它只把消息放进队列。

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cgcodeboy/des-engine/engine"
)

type source struct{ sent bool }

func (s *source) Advance() (time.Duration, bool) {
	if s.sent {
		return 0, false
	}
	return 5 * time.Millisecond, true
}

func (s *source) Output() []engine.Message {
	return []engine.Message{{Port: "out", Payload: []byte("box")}}
}

func (s *source) Internal()                         { s.sent = true }
func (s *source) External(time.Duration, engine.Message) {}

type sink struct{ got []byte }

func (s *sink) Advance() (time.Duration, bool) { return 0, false }
func (s *sink) Output() []engine.Message       { return nil }
func (s *sink) Internal()                      {}
func (s *sink) External(_ time.Duration, msg engine.Message) {
	s.got = append([]byte(nil), msg.Payload...)
}

func main() {
	eng, err := engine.NewEngine(
		engine.WithRealtime(false),
		engine.WithExitWhenIdle(true),
	)
	if err != nil {
		log.Fatal(err)
	}
	root, err := engine.NewCoordinator(eng)
	if err != nil {
		log.Fatal(err)
	}
	box := &sink{}
	if err := root.Add("source", &source{}); err != nil {
		log.Fatal(err)
	}
	if err := root.Add("sink", box); err != nil {
		log.Fatal(err)
	}
	if err := root.Link("source", "out", "sink", "in"); err != nil {
		log.Fatal(err)
	}
	if err := eng.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
	if err := eng.Wait(); err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(box.got))
}
```

同一毫秒有多个模型到点时，默认按登记顺序选一个。`WithSelect` 可以换成自己的选择函数。`WithOutput` 接收离开根协调器的消息，它也跑在事件循环上。`Group` 可以嵌套一层网络。到货、扫描、打包的完整时间线在 `example/line`。

## 墙钟

真实模式是默认行为。`WithOrigin` 指定仿真时间 0 对应的墙钟时刻，不指定就用 `Start` 的时刻。某一毫秒开始得晚了，`WithLagHandler` 会收到这一毫秒的滞后；同一毫秒只回调一次。

要测量处理能力，或者让仿真尽快跑完，关掉真实模式：

```go
eng, err := engine.NewEngine(
	engine.WithRealtime(false),
	engine.WithExitWhenIdle(true),
)
```

`WithHorizon` 在到达给定仿真时间后停止。`WithSeed` 决定处理函数里 `Context.Rand()` 的随机序列，默认种子是 1。`WithLogger` 更换异步日志的去处，省略时用 `slog.Default`。日志在另一条协程写出，事件路径上不写 IO。队列容量 4096，满了就丢弃并计入 `Snapshot.LogDropped`。`Stop` 在事件循环退出后把队列排空，最多等 2 秒。

## 暂停与恢复

`Pause` 等当前毫秒执行完后停住，不生成检查点。暂停期间可以继续 `AddEvent`。`Resume` 从这里继续。真实模式下，恢复会把墙钟起点往后推，暂停的那段时间不会被追赶。同一进程里这样往返即可，不需要检查点。

```go
if err := eng.Pause(); err != nil {
	log.Fatal(err)
}
if _, err := eng.AddFuture(10*time.Millisecond, engine.Event{
	Name:    "later",
	Handler: func(engine.Context) error { return nil },
}); err != nil {
	log.Fatal(err)
}
if err := eng.Resume(); err != nil {
	log.Fatal(err)
}
```

`Pause` 在引擎不处于运行中时返回 `ErrNotRunning`。已经暂停时再次调用返回 nil。引擎正在停止或已经停止时返回 `ErrClosed`。`Resume` 只在暂停后有效，否则返回 `ErrNotPaused`。

## 检查点

`Checkpoint` 只能在暂停后调用，返回一份字节。引擎不保存它，调用方可以保留多份。字节里有事件名称和 `Payload`，没有处理函数。

进程重新拉起时，先用 `Handle` 登记同名处理函数，再 `Restore`。引擎按名称把队列接上，结果仍是暂停，然后 `Resume`。未登记的名称返回 `ErrHandlerName`。事件自己的数据用 `Context.Payload` 读取。

```go
handler := func(c engine.Context) error {
	_ = c.Payload()
	return nil
}
eng, err := engine.NewEngine(engine.WithRealtime(false))
if err != nil {
	log.Fatal(err)
}
if err := eng.Start(context.Background()); err != nil {
	log.Fatal(err)
}
if err := eng.Pause(); err != nil {
	log.Fatal(err)
}
if _, err := eng.AddEvent(10, engine.Event{
	Name:    "job",
	Payload: []byte("gamma"),
	Handler: handler,
}); err != nil {
	log.Fatal(err)
}
data, err := eng.Checkpoint()
if err != nil {
	log.Fatal(err)
}

next, err := engine.NewEngine(engine.WithRealtime(false))
if err != nil {
	log.Fatal(err)
}
if err := next.Handle("job", handler); err != nil {
	log.Fatal(err)
}
if err := next.Restore(context.Background(), data); err != nil {
	log.Fatal(err)
}
if err := next.Resume(); err != nil {
	log.Fatal(err)
}
```

协调器另有自己的 `Checkpoint` 和 `Restore`。它在引擎字节之外保存每个模型的 `Persistent.Save()`、上次和下次时间。模型没有实现 `Persistent` 时，`Checkpoint` 返回 `ErrState`，不产生字节。同一进程的 `Pause` 和 `Resume` 不调用 `Persistent`。

## 指标

处理函数的墙钟耗时超过 1 毫秒时，引擎记一条 Warn，并在 `Snapshot.SlowEvents` 里保留最近 128 条，其中有事件名、仿真时间和耗时。`StatusJSON` 与 `Prometheus` 是同一份快照的两种正文。

DES 读取引擎自己的快照，`Coupled` 为空，正文里没有 `coupled_*`。DEVS 读取协调器的快照，`Coupled` 里是原子模型数量、转发次数、到点次数、选择次数和重排次数。`des_up` 只在 `state` 为 `running` 时为 1。正文不带事件名标签。

```go
snap := eng.Snapshot()
body, err := snap.StatusJSON()
text := snap.Prometheus()

// 使用了协调器时改为：
snap = root.Snapshot()
```

库不打开端口。接入方使用 `net/http` 和 `io` 写自己的服务，并自己调用 `ListenAndServe`：

```go
import (
	"io"
	"net/http"
)

mux := http.NewServeMux()
mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
	body, err := eng.Snapshot().StatusJSON()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
})
mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	io.WriteString(w, eng.Snapshot().Prometheus())
})
srv := &http.Server{Addr: ":8080", Handler: mux}
if err := srv.ListenAndServe(); err != nil {
	log.Fatal(err)
}
```

使用协调器时，两个处理函数都改为 `root.Snapshot()`，`Coupled` 才会填上。字段清单见详细设计第 8 章。

## 示例

`queue`、`checkpoint`、`lag`、`realtime` 是 DES。`line` 是 DEVS。基准测试单独放在后面。

| 程序 | 看什么 |
| --- | --- |
| `example/queue` | 单服务台。同一毫秒先插入的先执行，服务结束的同一毫秒接上下一件。取消和过期插入不会发生，时间上限之后的事件不执行 |
| `example/checkpoint` | 暂停后直接恢复。再暂停时取出检查点，另一台引擎按名称读到原来的 `Payload` |
| `example/lag` | 墙钟起点放在过去。已经错过的桶立即追赶，还没到的桶继续等 |
| `example/realtime` | 真实模式、同毫秒追加、慢事件。结束后用 `Snapshot` 读取状态 |

队列在示例自己的处理函数里，不是引擎内置的资源库。

```bash
go run ./example/queue
```

```text
单服务台，每件服务 4 ms。同一毫秒里先插入的先执行。
E 在启动前取消。时间上限是 20 ms，F 排在 30 ms。
   0 ms  拒绝更早的时刻
   0 ms  到达 A
   0 ms  开始 A
   4 ms  完成 A
   4 ms  空闲
   5 ms  到达 B
   5 ms  开始 B
   5 ms  到达 C
   5 ms  排队 C  等待 1
   9 ms  完成 B
   9 ms  同一毫秒接上 C
   9 ms  开始 C
  12 ms  到达 D
  12 ms  排队 D  等待 1
  13 ms  完成 C
  13 ms  同一毫秒接上 D
  13 ms  开始 D
  17 ms  完成 D
  17 ms  空闲
结束  状态 stopped  仿真时间 18 ms
时间线里没有 E，也没有 F
```

```bash
go run ./example/checkpoint
```

`alpha` 跑完后暂停，不取检查点就恢复，暂停期间排入的 `beta` 接着执行。再次暂停后把还没跑的 `gamma` 放进检查点。新引擎 `Restore` 时仍是暂停，`Resume` 后 `gamma` 带着原来的数据执行。

```bash
go run ./example/lag
```

起点设在 40 ms 之前，并排入 0 ms、15 ms、80 ms。前两档在进程刚启动时执行，80 ms 会等到墙钟目标。醒来时若已经越过目标，会再记一次很小的滞后。仿真走到 81 ms 时，进程墙钟大约是剩下的那段等待，而不是 80 ms。

```bash
go run ./example/realtime
```

标准错误上会看到 `0 ms tick`、`0 ms follow`、`20 ms later`、`30 ms slow`。中断后停止引擎，并打印快照里的状态、仿真时间和慢事件次数。

耦合层不改事件循环。到货、扫描、打包各自决定处理时间，完成和开始由端口接在同一毫秒：

```bash
go run ./example/line
```

```text
到货口、扫描台、打包台靠端口连接。处理时间由包裹自己决定。
   0 ms  到货 小包
   0 ms  开始扫描 小包
   2 ms  完成扫描 小包
   2 ms  开始打包 小包
   4 ms  完成打包 小包
   5 ms  到货 大包
   5 ms  开始扫描 大包
   9 ms  完成扫描 大包
   9 ms  开始打包 大包
  12 ms  完成打包 大包
```

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
