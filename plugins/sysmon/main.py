#!/usr/bin/env python3
"""sysmon: the pet talks about the machine it runs on.

Two kinds of speech, each switchable from the pet's right-click menu
("说话内容"):

  * reports  - periodic "how is the computer doing" lines (CPU, memory, disk,
               temperature, load, uptime, battery, or a summary of those).
               All OFF by default; turn on the ones you want.
  * alerts   - spoken only when something is wrong (high load / memory / temp).
               ON by default.

Linux only (reads /proc and /sys). Inside Docker, CPU/memory/uptime reflect the
host, temperature needs /sys/class/thermal visible, and disk only sees the
paths mounted in the container (see "disk_paths").
Config: $PET_DATA_DIR/sysmon.json (created on first run; re-read live).
"""
import glob
import os
import shutil
import sys
import threading
import time

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "_sdk"))
from petplugin import Plugin, log  # noqa: E402

# Several machines can feed one pet: run extra copies elsewhere with
# SYSMON_NAME=<machine> (e.g. "homelab"). Name, channel ids, groups and the menu
# entry are then prefixed so they do not collide with the default instance.
NAME = os.environ.get("SYSMON_NAME", "").strip()
NS = f"{NAME}." if NAME else ""
GP = f"{NAME} · " if NAME else ""
TAG = f"[{NAME}] " if NAME else ""  # prefix of everything the pet says
REPORT_GROUP = GP + "系统播报"
ALERT_GROUP = GP + "系统告警"

# Template fields: {cpu} {mem} {mem_used_gb} {mem_total_gb} {disk} {disk_free_gb}
# {temp} {load} {uptime} {battery}. A report whose fields are unavailable on
# this machine is skipped silently.
DEFAULT = {
    "interval_sec": 30,
    "cooldown_min": 10,
    "load_per_cpu": 1.5,
    "mem_used_pct": 92,
    "temp_c": 88,
    "disk_paths": ["/data"],
    "alert_say": {
        "load": "CPU 好忙呀……（loadavg {value:.1f}）",
        "mem": "内存快满了！（已用 {value:.0f}%）",
        "temp": "好烫好烫！（{value:.0f}°C）",
    },
    "reports": {
        "summary": {"label": "资源概况（综合）", "every_min": 30, "anim": "wave"},
        "cpu": {"label": "CPU 使用率", "every_min": 10, "say": "CPU 使用率 {cpu:.0f}%", "anim": "think"},
        "mem": {"label": "内存使用", "every_min": 10,
                "say": "内存 {mem_used_gb:.1f}/{mem_total_gb:.1f} GB（{mem:.0f}%）", "anim": "think"},
        "disk": {"label": "磁盘空间", "every_min": 60, "say": "磁盘已用 {disk:.0f}%，剩 {disk_free_gb:.0f} GB", "anim": "think"},
        "temp": {"label": "温度", "every_min": 15, "say": "现在 {temp:.0f}°C", "anim": "think"},
        "load": {"label": "系统负载", "every_min": 15, "say": "负载 {load:.2f}（每核）", "anim": "think"},
        "uptime": {"label": "开机时长", "every_min": 120, "say": "已经开机 {uptime} 啦", "anim": "sit"},
        "battery": {"label": "电池电量", "every_min": 30, "say": "电池 {battery:.0f}%", "anim": "think"},
    },
}

p = Plugin(f"sysmon-{NAME}" if NAME else "sysmon")
cfg = p.load_json("sysmon.json", DEFAULT)
cfg.setdefault("reports", DEFAULT["reports"])  # configs from v1 have no reports yet
_cfg_mtime = 0.0
_cfg_lock = threading.Lock()
last_alert = {}
last_report = {}


# ---- probes -------------------------------------------------------------------
class CpuSampler:
    """CPU% from /proc/stat deltas, averaged over the last ~30 s."""

    def __init__(self):
        self.samples = []  # (time, busy, total)

    @staticmethod
    def _read():
        with open("/proc/stat") as f:
            parts = f.readline().split()[1:]
        vals = [int(x) for x in parts]
        idle = vals[3] + (vals[4] if len(vals) > 4 else 0)
        total = sum(vals[:8]) if len(vals) >= 8 else sum(vals)
        return total - idle, total

    def sample(self):
        try:
            busy, total = self._read()
        except (OSError, ValueError, IndexError):
            return
        now = time.time()
        self.samples.append((now, busy, total))
        self.samples = [s for s in self.samples if now - s[0] <= 35]

    def percent(self):
        if len(self.samples) < 2:
            return None
        (_, b0, t0), (_, b1, t1) = self.samples[0], self.samples[-1]
        return 100.0 * (b1 - b0) / (t1 - t0) if t1 > t0 else None


cpu = CpuSampler()


def load_per_cpu():
    with open("/proc/loadavg") as f:
        one = float(f.read().split()[0])
    return one / (os.cpu_count() or 1)


def mem_info():
    info = {}
    with open("/proc/meminfo") as f:
        for line in f:
            k, _, v = line.partition(":")
            info[k] = int(v.split()[0])
    total, avail = info.get("MemTotal", 0), info.get("MemAvailable", 0)
    if not total:
        return None
    return 100.0 * (total - avail) / total, (total - avail) / 1048576.0, total / 1048576.0


def mem_used_pct():
    m = mem_info()
    return m[0] if m else 0.0


def max_temp_c():
    best = None
    for path in glob.glob("/sys/class/thermal/thermal_zone*/temp"):
        try:
            with open(path) as f:
                v = int(f.read().strip()) / 1000.0
        except (OSError, ValueError):
            continue
        if 0 < v < 150:
            best = v if best is None else max(best, v)
    return best


def disk_usage():
    for path in cfg.get("disk_paths", ["/data"]):
        try:
            u = shutil.disk_usage(path)
            return 100.0 * u.used / u.total, u.free / 1073741824.0
        except OSError:
            continue
    return None


def uptime_text():
    try:
        with open("/proc/uptime") as f:
            sec = int(float(f.read().split()[0]))
    except (OSError, ValueError):
        return None
    d, rem = divmod(sec, 86400)
    h, rem = divmod(rem, 3600)
    m = rem // 60
    if d:
        return f"{d} 天 {h} 小时"
    if h:
        return f"{h} 小时 {m} 分"
    return f"{m} 分钟"


def battery_pct():
    for path in sorted(glob.glob("/sys/class/power_supply/BAT*/capacity")):
        try:
            with open(path) as f:
                return float(f.read().strip())
        except (OSError, ValueError):
            continue
    return None


def metrics():
    """Everything this machine can tell us right now (missing ones are absent)."""
    m = {}
    c = cpu.percent()
    if c is not None:
        m["cpu"] = c
    try:
        mi = mem_info()
        if mi:
            m["mem"], m["mem_used_gb"], m["mem_total_gb"] = mi
    except OSError:
        pass
    d = disk_usage()
    if d:
        m["disk"], m["disk_free_gb"] = d
    t = max_temp_c()
    if t is not None:
        m["temp"] = t
    try:
        m["load"] = load_per_cpu()
    except (OSError, ValueError):
        pass
    u = uptime_text()
    if u:
        m["uptime"] = u
    b = battery_pct()
    if b is not None:
        m["battery"] = b
    return m


def summary_text(m):
    parts = []
    if "cpu" in m:
        parts.append(f"CPU {m['cpu']:.0f}%")
    if "mem" in m:
        parts.append(f"内存 {m['mem']:.0f}%")
    if "disk" in m:
        parts.append(f"磁盘 {m['disk']:.0f}%")
    if "temp" in m:
        parts.append(f"{m['temp']:.0f}°C")
    if "battery" in m:
        parts.append(f"电量 {m['battery']:.0f}%")
    return " · ".join(parts)


def render_report(rid, spec, m):
    """Return the text for a report, or None if this machine cannot supply it."""
    if rid == "summary" and "say" not in spec:
        return summary_text(m) or None
    tmpl = spec.get("say")
    if not tmpl:
        return None
    try:
        return tmpl.format(**m)
    except (KeyError, IndexError, ValueError):
        return None


# ---- config & channels --------------------------------------------------------------
def reload_cfg():
    global cfg, _cfg_mtime
    path = os.path.join(p.data_dir, "sysmon.json")
    try:
        mt = os.path.getmtime(path)
    except OSError:
        return
    if mt == _cfg_mtime:
        return
    try:
        new = p.load_json("sysmon.json", DEFAULT)
    except (OSError, ValueError) as e:
        _cfg_mtime = mt
        log(f"[sysmon] bad sysmon.json, keeping previous config: {e}")
        return
    new.setdefault("reports", DEFAULT["reports"])
    with _cfg_lock:
        cfg, _cfg_mtime = new, mt
    declare_channels()


def declared_ids():
    ids = [f"report.{NS}{r}" for r in cfg.get("reports", {})]
    ids += [f"alert.{NS}{k}" for k in ("load", "mem", "temp")]
    return ids


_declared = set()


def declare_channels():
    want = set(declared_ids())
    for gone in _declared - want:
        p.channel_remove(gone)
    for rid, spec in cfg.get("reports", {}).items():
        p.channel(f"report.{NS}{rid}", spec.get("label", rid), REPORT_GROUP, False)
    labels = {"load": "CPU 负载过高时提醒", "mem": "内存快满时提醒", "temp": "温度过高时提醒"}
    for k, label in labels.items():
        p.channel(f"alert.{NS}{k}", label, ALERT_GROUP, True)
    _declared.clear()
    _declared.update(want)


# ---- behaviour ---------------------------------------------------------------------------
def sample():
    cpu.sample()


def check_alerts():
    now = time.time()
    cool = cfg.get("cooldown_min", 10) * 60
    probes = (
        ("load", load_per_cpu, cfg.get("load_per_cpu", 1.5), "think"),
        ("mem", mem_used_pct, cfg.get("mem_used_pct", 92), "apologize"),
        ("temp", max_temp_c, cfg.get("temp_c", 88), "react"),
    )
    for kind, probe, limit, anim in probes:
        try:
            v = probe()
        except (OSError, ValueError):
            continue
        if v is None or v < limit or now - last_alert.get(kind, 0) < cool:
            continue
        last_alert[kind] = now
        shown = v * (os.cpu_count() or 1) if kind == "load" else v
        say = cfg.get("alert_say", cfg.get("say", {})).get(kind, kind).format(value=shown)
        p.act(say=TAG + say, anim=anim, sound="notify", prio=60, channel=f"alert.{NS}{kind}")
        p.emit("alert", {"kind": kind, "value": round(v, 2)})


def check_reports():
    now = time.time()
    m = None
    for rid, spec in cfg.get("reports", {}).items():
        every = float(spec.get("every_min", 30)) * 60
        # first report fires one interval after start (never right at startup)
        last = last_report.setdefault(rid, now)
        if now - last < every - 1:
            continue
        last_report[rid] = now
        m = m or metrics()
        text = render_report(rid, spec, m)
        if text:
            p.act(say=TAG + text, anim=spec.get("anim", "think"), prio=45, hold=2500,
                  channel=f"report.{NS}{rid}")


def tick():
    reload_cfg()
    check_alerts()
    check_reports()


def report_now():
    """Menu action: speak the summary right now, regardless of channel settings."""
    text = summary_text(metrics())
    p.act(say=TAG + (text or "暂时读不到系统数据"), anim="wave", prio=65)


@p.on_connect
def _connected():
    declare_channels()
    p.menu_add("report_now", f"{GP}汇报资源概况")


@p.on("menu.click")
def _menu(data):
    if data.get("id") == "report_now":
        report_now()


p.every(5, sample)
p.every(cfg.get("interval_sec", 30), tick)

if __name__ == "__main__":
    sample()
    p.run()
