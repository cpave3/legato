#!/usr/bin/env python3
"""Legato status for node exporter's textfile collector.

Legato has no /metrics endpoint yet, and it runs as a systemd *user* unit,
which node exporter's systemd collector does not see. This reads:
  - GET /health on Legato's web server (unauthenticated: status and version)
  - the user unit's state and restart count (systemctl --user)
  - how many legato-* tmux sessions (agents) are live
  - the unit's cgroup: memory, CPU, processes, OOM kills. KillMode=process
    keeps the tmux server Legato starts in this cgroup, so it counts the
    agents too
and writes them atomically to the textfile directory. Read-only: it never
starts, stops or changes anything.
"""
import json
import os
import ssl
import subprocess
import sys
import tempfile
import time
import urllib.request

USER = os.environ.get("LEGATO_USER", "ubuntu")
UID = os.environ.get("LEGATO_UID", "1000")
HEALTH_URL = os.environ.get("LEGATO_HEALTH_URL", "https://127.0.0.1:3080/health")
OUT = os.environ.get(
    "TEXTFILE_PATH", "/var/lib/prometheus/node-exporter/legato.prom"
)


def esc(v: str) -> str:
    return v.replace("\\", "\\\\").replace('"', '\\"').replace("\n", " ")


def health():
    # Legato serves a self-signed certificate on localhost
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE
    try:
        with urllib.request.urlopen(HEALTH_URL, timeout=5, context=ctx) as r:
            body = json.load(r)
            return body.get("status") == "ok", str(body.get("version", "unknown"))
    except Exception as e:  # down, refused, bad JSON: all mean not up
        print(f"health: {e}", file=sys.stderr)
        return False, None


def as_user(args):
    env = {"XDG_RUNTIME_DIR": f"/run/user/{UID}", "PATH": "/usr/sbin:/usr/bin:/sbin:/bin"}
    return subprocess.run(
        ["/usr/sbin/runuser", "-u", USER, "--", *args],
        capture_output=True, text=True, timeout=10, env=env,
    )


def unit():
    r = as_user(["systemctl", "--user", "show", "legato.service",
                 "-p", "ActiveState,NRestarts,ExecMainStartTimestamp,ControlGroup"])
    props = dict(l.split("=", 1) for l in r.stdout.splitlines() if "=" in l)
    if r.returncode != 0 or not props:
        return None
    start = None
    ts = props.get("ExecMainStartTimestamp", "")
    if ts:
        d = subprocess.run(["date", "-d", ts, "+%s"], capture_output=True, text=True)
        if d.returncode == 0:
            start = int(d.stdout.strip())
    return {
        "active": props.get("ActiveState") == "active",
        "restarts": int(props.get("NRestarts", "0") or 0),
        "start": start,
        "cgroup": props.get("ControlGroup", ""),
    }


def cgroup(path):
    d = "/sys/fs/cgroup" + path
    def read(name):
        with open(f"{d}/{name}") as f:
            return f.read()
    def kv(name):
        return dict(l.split() for l in read(name).splitlines())
    return {
        "memory": int(read("memory.current")),
        "cpu": int(kv("cpu.stat")["usage_usec"]) / 1e6,
        "tasks": int(read("pids.current")),
        "oom": int(kv("memory.events").get("oom_kill", 0)),
    }


def agent_sessions():
    r = as_user(["tmux", "list-sessions", "-F", "#{session_name}"])
    if r.returncode != 0:
        # No tmux server means no agents
        return 0 if "no server running" in r.stderr or "error connecting" in r.stderr else None
    return sum(1 for n in r.stdout.splitlines() if n.startswith("legato-"))


def safe(fn):
    try:
        return fn()
    except Exception as e:
        print(f"{fn.__name__}: {e}", file=sys.stderr)
        return None


def main():
    up, version = health()
    u = safe(unit)
    agents = safe(agent_sessions)
    cg = safe(lambda: cgroup(u["cgroup"])) if u and u["cgroup"] else None
    lines = [
        "# HELP legato_up 1 if Legato's /health answers ok",
        "# TYPE legato_up gauge",
        f"legato_up {int(up)}",
    ]
    if version:
        lines += [
            "# HELP legato_version_info Legato's version, from /health",
            "# TYPE legato_version_info gauge",
            f'legato_version_info{{version="{esc(version)}"}} 1',
        ]
    if u:
        lines += [
            "# HELP legato_service_active 1 if the legato.service user unit is active",
            "# TYPE legato_service_active gauge",
            f"legato_service_active {int(u['active'])}",
            "# HELP legato_service_restarts systemd's automatic restarts of legato.service",
            "# TYPE legato_service_restarts gauge",
            f"legato_service_restarts {u['restarts']}",
        ]
        if u["start"]:
            lines += [
                "# HELP legato_service_start_time_seconds when the running Legato process started",
                "# TYPE legato_service_start_time_seconds gauge",
                f"legato_service_start_time_seconds {u['start']}",
            ]
    if cg:
        lines += [
            "# HELP legato_service_memory_bytes memory of legato.service's cgroup (Legato and its agents)",
            "# TYPE legato_service_memory_bytes gauge",
            f"legato_service_memory_bytes {cg['memory']}",
            "# HELP legato_service_cpu_seconds_total CPU used by legato.service's cgroup since it started",
            "# TYPE legato_service_cpu_seconds_total counter",
            f"legato_service_cpu_seconds_total {cg['cpu']}",
            "# HELP legato_service_tasks processes and threads in legato.service's cgroup",
            "# TYPE legato_service_tasks gauge",
            f"legato_service_tasks {cg['tasks']}",
            "# HELP legato_service_oom_kills_total OOM kills inside legato.service's cgroup",
            "# TYPE legato_service_oom_kills_total counter",
            f"legato_service_oom_kills_total {cg['oom']}",
        ]
    if agents is not None:
        lines += [
            "# HELP legato_agent_sessions live legato-* tmux sessions (agents)",
            "# TYPE legato_agent_sessions gauge",
            f"legato_agent_sessions {agents}",
        ]
    lines += [
        "# HELP legato_scrape_success 1 if every check in this script ran",
        "# TYPE legato_scrape_success gauge",
        f"legato_scrape_success {int(u is not None and agents is not None and cg is not None)}",
        "# HELP legato_last_scrape_timestamp_seconds when this script last ran",
        "# TYPE legato_last_scrape_timestamp_seconds gauge",
        f"legato_last_scrape_timestamp_seconds {int(time.time())}",
    ]
    d = os.path.dirname(OUT)
    fd, tmp = tempfile.mkstemp(dir=d, prefix=".legato.prom.")
    with os.fdopen(fd, "w") as f:
        f.write("\n".join(lines) + "\n")
    os.chmod(tmp, 0o644)
    os.replace(tmp, OUT)


if __name__ == "__main__":
    main()
