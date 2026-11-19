# 协议

petd 在 TCP 端口（`.env` 里的 `PET_PORT`，随机生成，兜底默认 `:23817`）上说**按行 JSON**（NDJSON）：每条消息是一个 JSON 对象加 `\n`，双向。单行最大 1 MiB。渲染器（GNOME 扩展）和插件使用同一个端口，用第一条消息区分。

## 握手

连接后 5 秒内必须先发 `hello`，否则断开。

渲染器：
```json
{"t":"hello","role":"render","token":"可选"}
```
插件：
```json
{"t":"hello","role":"plugin","name":"reminder","subscribe":["time","menu.click"],"caps":["brain"],"token":"可选"}
```

- 设置了 `token` 时，不匹配会收到 `{"t":"error","msg":"bad token"}` 并被断开。
- 同一时间只有一个渲染器（新的会顶掉旧的）；插件名必须唯一，重名收到 `error` 并断开。
- `subscribe` 是事件名模式：精确名、`*`、或以 `*` 结尾的前缀（`time.*`）。注意 `time.*` 不匹配 `time`。
- `caps` 目前只有 `brain`：表示该插件处理 `user_text`，核心据此显示“聊天…”菜单并不再给出无大脑的兜底回复。
- 成功后收到 `{"t":"welcome","version":"…","role":"…"}`。

## 核心 → 渲染器

| 消息 | 字段 | 含义 |
|---|---|---|
| `config` | `skin scale gravity floor_margin idle_marks sensors sounds bubble_ttl walk_speed` | 渲染器连上后立即发送 |
| `anim` | `name loop` | 播放动画；`loop:false` 播完后渲染器发 `anim_end` |
| `move` | `x` 或 `to`(`left/right/center/random`)、`speed anim` | 走过去，到达后渲染器发 `arrived` |
| `say` | `text ttl` | 显示气泡 |
| `sound` | `name` | 播放提示音（`sounds` 映射表里的逻辑名） |
| `skin` | `skin scale` | 换皮肤（手动或按计划）。渲染器重新加载该皮肤的帧，核心随后重发当前动画和菜单 |
| `menu` | `items:[MenuItem]` | 右键菜单，见下 |
| `pong` | | 回复 `ping` |

**MenuItem** 有四种形态：

| 形态 | 字段 | 点击时渲染器发送 |
|---|---|---|
| 动作（默认） | `{id,label}`；`local:"chat"` 表示由渲染器自己打开输入框 | `menu.click{id}` |
| 开关 | `{type:"switch",id,label,checked}` | `menu.click{id,checked}`（`checked` 是切换后的状态） |
| 分组标题 | `{type:"header",label}` | 不可点击 |
| 子菜单 | `{label,children:[MenuItem…]}` | 由子项发送 |

菜单打开期间收到的新 `menu` 应延后到菜单关闭再重建，否则正在操作的项会被销毁。核心自己使用的 id：`skin:<名>`、`skin:auto`、`channel:<id>`，这些点击由核心消化，不会转发给插件。

## 渲染器 → 核心

全部是 `{"t":"event","name":"…","data":{…}}`：

`click{x,y}` `double_click` `drag_start` `drag_end{x,y}` `arrived{x}` `anim_end{name}` `idle{sec}` `active{idle_sec}` `focus{app,id,title?}` `notify{app,title?,body?}` `menu.click{id[,checked]}` `user_text{text}`

## 核心 → 插件

```json
{"t":"event","name":"click","data":{"x":10,"y":20}}
```
只会收到 `subscribe` 匹配的事件。

## 插件 → 核心

| 消息 | 例子 |
|---|---|
| `act` | `{"t":"act","say":"该休息了","anim":"wave","sound":"alarm","prio":70,"hold":3000}` |
| `event` | `{"t":"event","name":"alert","data":{…}}` ——核心会改名为 `plugin.<插件名>.alert`，插件不能冒充核心事件 |
| `menu.add` | `{"t":"menu.add","id":"pomodoro","label":"番茄钟：开始"}`（同 id 再发 = 改标签） |
| `menu.remove` | `{"t":"menu.remove","id":"pomodoro"}` |
| `channel.add` | `{"t":"channel.add","id":"report.cpu","label":"CPU 使用率","group":"系统播报","enabled":false}`——声明一个用户可开关的说话来源（`enabled` 只是默认值；用户的选择由核心保存）。同 id 再发 = 更新标签。**每次重连后都要重新声明** |
| `channel.remove` | `{"t":"channel.remove","id":"report.cpu"}` |
| `ping` | `{"t":"ping"}` → `{"t":"pong"}` |

`act` 字段：`anim loop hold say say_any ttl sound move{x|to,speed} prio channel`。未写 `prio` 时插件默认 50。

`channel` 指明这条动作属于哪个说话来源。用户关掉该来源后，整条 `act`（动画、气泡、提示音）都会被丢弃；没声明过的 channel 名会被自动登记到"其它"组（默认开）。不带 `channel` 的 act 不可开关——适合"用户刚点了按钮"这类即时响应。

插件断开时，它注册的菜单项和 channel 自动从菜单移除，但用户对它们的开关选择仍被记住，重连后恢复。

## 背压

每个客户端有 128 条的发送队列；队列满了新消息会被丢弃（日志里有 warn），不会阻塞核心。

## HTTP

同一份能力也可以通过 `PET_HTTP_PORT` 端口的 HTTP 使用：`POST /api/act`（body 就是 act 字段）、`POST /api/event`（`{"name","data"}`）、`GET /api/state`、`GET /api/skins`、`POST /api/skin`、`GET /api/channels`、`POST /api/channel`、`GET /api/stream`（SSE）、`GET /healthz`。设了 token 时用 `Authorization: Bearer <token>` 或 `?token=`。
