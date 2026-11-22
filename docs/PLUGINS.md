# 写插件

插件 = 一个独立进程，连上 petd 的 TCP 端口，订阅事件、发送动作。语言不限。

## 目录与清单

```
plugins/<名字>/
  plugin.json     清单
  main.py         你的代码（或任何可执行文件）
```

`plugin.json`：
```json
{
  "name": "hello",
  "description": "一句话说明",
  "command": ["python3", "-u", "main.py"],
  "autostart": true,
  "env": { "SOME_OPTION": "x" }
}
```

- 以 `_` 或 `.` 开头的目录会被忽略（`_sdk` 就是这样）。
- 进程的工作目录就是插件目录；petd 会注入环境变量：`PET_HOST` `PET_PORT` `PET_TOKEN` `PET_PLUGIN` `PET_DATA_DIR`（该插件专属的可写数据目录，位于 `data/plugins/<名字>/`）。petd 自身的环境变量也会透传（`docker-compose.yml` 里加的变量插件能读到）。
- 退出码 0 = 正常结束，不再重启（`brain-mnemo` 未配置时就这样）；非 0 = 崩溃，按 1s、2s、4s…（最长约 1 分钟）退避重启。
- 在 `config.json` 里 `plugins.disabled` 可关闭，`plugins.env.<名字>` 可追加环境变量。
- 把插件放进镜像里的 `/app/plugins`，或者用 compose 额外挂载目录并设置 `PET_PLUGINS_DIR`。

**插件不一定要托管**：任何地方（宿主机、别的容器）的程序，只要能连上 `PET_PORT` 端口并发 `hello`，就是插件。需要时设置 `PET_TOKEN`。

## Python（SDK，仅标准库）

```python
import os, sys
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "_sdk"))
from petplugin import Plugin

p = Plugin("hello", subscribe=["click", "time"])

@p.on("click")
def on_click(data):
    p.act(say="摸摸~", anim="happy", prio=60)

@p.on("time")
def on_minute(data):
    if data["hm"] == "09:00":
        p.act(say="早上好", anim="wave", sound="notify")

@p.on_connect
def connected():
    p.menu_add("greet", "打个招呼")

@p.on("menu.click")
def menu(data):
    if data.get("id") == "greet":
        p.act(say="你好！", anim="wave")

p.every(60, lambda: p.emit("tick"))   # 定时任务；事件会变成 plugin.hello.tick

p.run()   # 自动重连
```

### 让用户能开关你的"说话"

主动开口的插件（定时播报、提醒…）应该声明 **说话频道**：用户在右键菜单"说话内容"里一键开关，不用改你的配置文件。

```python
p.channel("report.cpu", "CPU 使用率", group="系统播报", enabled=False)   # enabled 只是默认值
...
p.act(say="CPU 使用率 12%", anim="think", channel="report.cpu")
```

- 用户关掉后，带这个 `channel` 的 `act` 整条被丢弃（动画、气泡、提示音），你不需要自己判断。
- 用户的选择由 petd 保存在 `state.json`；SDK 会在每次重连时自动重新声明所有 channel。
- 默认值要克制：会打扰人的（周期播报）默认关，真正的告警默认开。
- 对"用户刚点了菜单项"这类即时响应，不要带 `channel`，否则开关关掉时点了没反应。
- 配置里增删条目时，用 `p.channel_remove(id)` 撤掉不再存在的项。

SDK 提供：`on(pattern)` `on_connect` `every(seconds, fn)` `act(**kw)` `emit(name, data)` `menu_add/menu_remove` `channel/channel_remove` `load_json(file, default)`（首次使用时把默认值写进数据目录）。处理函数里抛异常不会让插件退出。

## 其它语言

只要会开 TCP 连接和读写 JSON 行。bash 例子（需要 `jq`）：

```bash
exec 3<>/dev/tcp/${PET_HOST:-127.0.0.1}/${PET_PORT:-23817}
echo '{"t":"hello","role":"plugin","name":"sh-demo","subscribe":["click"]}' >&3
while read -r line <&3; do
  [ "$(echo "$line" | jq -r .name)" = click ] &&
    echo '{"t":"act","say":"来自 shell","anim":"wave"}' >&3
done
```

## 不需要常驻的情况：直接用 HTTP

脚本、cron、CI 里一行 curl 就够：

```bash
curl -s -X POST localhost:$PET_HTTP_PORT/api/act -d '{"say":"构建完成","anim":"happy","sound":"notify","prio":70}'
```

## 约定

- 事件名：插件发的事件会被加上 `plugin.<名字>.` 前缀；规则和别的插件可以订阅它们。
- 优先级：想打断当前动作用更高的 `prio`；普通提示 50–70；不要滥用 100（那是拖动）。
- `hold`：想让某个动作持续一段时间（比如“思考中”）用 `hold`（毫秒）；之后用另一个 `act` 覆盖即可提前结束。
- 动作名限于皮肤里有的；核心会忽略不存在的动作名。默认皮肤有：`idle blink walk sit sleep wave happy think apologize drag react notify`。
- 不要往 stdout 打印敏感信息：托管插件的输出会进入 petd 的日志。
