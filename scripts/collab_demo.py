#!/usr/bin/env python3
"""群聊协作 + 执行器 的真实验证脚本。

流程：
  1. 建一个「架构 / 前端 / 后端 / 测试」的群；
  2. 把需求发到群里，收集每个角色的完整发言（这是"讨论"阶段）；
  3. 把架构师的方案 + 各自职责交给执行器，串行跑 Run（这是"动手"阶段）；
  4. 输出时长、token、成本、diff、工具调用次数。

用法：
  python3 scripts/collab_demo.py --case calc-divide --root /tmp/dsh-ws-divide \
      --task "给 calc 加 Divide 函数：除零返回 error，并补测试"

不加 --task 时用内置案例。
"""
import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:8080"
DEFAULT_AGENTS = [
    ("arch", "架构师", "主持人", "D", "#4F46E5", "readonly",
     "你是架构师。收到需求先拆成清楚的任务，说明改哪些文件、接口怎么定、风险在哪。不要写代码。"),
    ("fe", "前端", "界面", "F", "#DB2777", "approval",
     "你是前端工程师。负责页面与样式，改完要说清改了什么。需要后端先定接口时，在群里直接问。"),
    ("be", "后端", "服务端", "B", "#0369A1", "approval",
     "你是后端工程师。负责 Go 代码与接口实现，改完跑测试。需要前端配合时，在群里直接说。"),
    ("qa", "测试", "质量", "Q", "#047857", "readonly",
     "你是测试。只读代码、指出风险与遗漏的用例，不写代码。发现问题直接在群里提。"),
]


def api(path, body=None, token=None, method=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method or ("POST" if body is not None else "GET"))
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=120) as r:
            return json.loads(r.read().decode())
    except urllib.error.HTTPError as e:
        raise RuntimeError(f"{path}: HTTP {e.code} {e.read().decode()[:300]}")


def sse(path, body, token, idle_timeout=180):
    req = urllib.request.Request(BASE + path, data=json.dumps(body).encode(), method="POST")
    req.add_header("Content-Type", "application/json")
    req.add_header("Authorization", "Bearer " + token)
    with urllib.request.urlopen(req, timeout=idle_timeout) as r:
        buf = b""
        for raw in r:
            buf += raw
            while b"\n\n" in buf:
                chunk, buf = buf.split(b"\n\n", 1)
                for line in chunk.decode().splitlines():
                    if line.startswith("data:"):
                        yield json.loads(line[5:].strip())


def login():
    d = api("/v1/auth/login", {"kind": "phone", "target": "13800000000", "code": ""})
    return d["data"]["token"]


def create_conv(token, title, agents):
    d = api("/v1/conversations", {
        "title": title,
        "studio": "custom",
        "agents": [
            {"key": k, "name": n, "title": t, "emoji": e, "color": c,
             "prompt": p, "permission": perm}
            for (k, n, t, e, c, perm, p) in agents
        ],
    }, token)
    return d["data"]["id"]


def bind_ws(token, root, conv_id):
    d = api("/v1/workspaces", {"root": root, "conversationId": conv_id, "label": "collab"}, token)
    return d["data"]["id"]


def talk(token, conv_id, task):
    """群聊阶段：返回每个角色的发言与耗时。"""
    t0 = time.time()
    utterances, tokens = [], 0
    for ev in sse(f"/v1/conversations/{conv_id}/orchestrate", {"content": task}, token):
        if ev.get("type") == "end" and ev.get("content"):
            utterances.append({
                "agent": ev.get("name") or ev.get("agent"),
                "text": ev.get("content", "").strip(),
                "tokens": ev.get("tokens", 0),
                "latency": ev.get("latency", 0),
            })
            tokens += ev.get("tokens", 0) or 0
    return {"seconds": round(time.time() - t0, 1), "tokens": tokens, "utterances": utterances}


def run_agent(token, ws_id, task, permission, name, max_steps=12):
    d = api(f"/v1/workspaces/{ws_id}/runs", {
        "task": task, "permission": permission, "maxSteps": max_steps,
    }, token)
    rid = d["data"]["id"]
    t0 = time.time()
    approvals = 0
    while True:
        time.sleep(2)
        r = api(f"/v1/runs/{rid}", token=token)["data"]
        # 命令需要批准时，脚本扮演「用户点允许」——这正是要测的审批闭环
        if r["status"] == "waiting_approval":
            for tc in r.get("toolCalls") or []:
                if tc.get("status") == "pending_approval" and tc.get("approvalId"):
                    api(f"/v1/runs/{rid}/approve",
                        {"approvalId": tc["approvalId"], "approve": True, "scope": "run"}, token)
                    approvals += 1
            continue
        if r["status"] in ("done", "failed", "aborted", "cancelled"):
            break
        if time.time() - t0 > 420:
            raise RuntimeError("run timeout")
    return {
        "approvals": approvals,
        "agent": name, "runId": rid, "status": r["status"], "steps": r["step"],
        "tokensIn": r["tokensIn"], "tokensOut": r["tokensOut"],
        "seconds": round(time.time() - t0, 1),
        "toolCalls": len(r.get("toolCalls") or []),
        "diffFiles": [(x["path"], x["status"], x["added"], x["removed"]) for x in (r.get("diffs") or [])],
        "summary": r.get("summary", ""),
        "error": r.get("error", ""),
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--case", required=True)
    ap.add_argument("--root", required=True)
    ap.add_argument("--task", default="")
    ap.add_argument("--out", default="")
    ap.add_argument("--skip-exec", action="store_true", help="只跑群聊，不动手")
    ap.add_argument("--solo", action="store_true", help="跳过群聊，单个 agent 直接做（基线对照）")
    args = ap.parse_args()

    task = args.task or "给这个项目补一个小功能，具体看 README。"
    token = login()
    before = api("/v1/me", token=token)["data"]
    t_start = time.time()

    if args.solo:
        # 基线：没有讨论，一个 agent 从需求直接干
        conv = create_conv(token, f"单 agent 基线 · {args.case}", DEFAULT_AGENTS[:1])
        ws = bind_ws(token, args.root, conv)
        runs = [run_agent(token, ws, task, "full", "单agent")]
        after = api("/v1/me", token=token)["data"]
        report = {
            "case": args.case + "-solo", "task": task, "root": args.root,
            "mode": "solo", "wallSeconds": round(time.time() - t_start, 1),
            "quotaUsed": after["user"]["used"] - before["user"]["used"],
            "discussion": {"seconds": 0, "tokens": 0, "utterances": []},
            "runs": runs, "at": time.strftime("%Y-%m-%d %H:%M:%S"),
        }
        out = args.out or f"docs/collab-{args.case}-solo.json"
        with open(out, "w") as f:
            json.dump(report, f, ensure_ascii=False, indent=1)
        print(f"solo: {runs[0]['seconds']}s {runs[0]['toolCalls']} 工具 {runs[0]['status']} 额度 {report['quotaUsed']}")
        print("报告：" + out)
        return

    conv = create_conv(token, f"协作测试 · {args.case}", DEFAULT_AGENTS)
    ws = bind_ws(token, args.root, conv)
    print(f"会话 {conv} / 工作区 {ws}")

    disc = talk(token, conv, task)
    print(f"\n=== 群聊 {disc['seconds']}s / {disc['tokens']} tokens ===")
    for u in disc["utterances"]:
        print(f"[{u['agent']}] {u['text'][:200]}")

    runs = []
    if not args.skip_exec:
        plan = next((u["text"] for u in disc["utterances"] if u["agent"] == "架构师"), "")
        workers = [
            ("前端", "fe", f"需求：{task}\n架构师的方案：{plan}\n你是前端，负责页面/样式部分，改完说明改了什么。"),
            ("后端", "be", f"需求：{task}\n架构师的方案：{plan}\n你是后端，负责 Go 代码与测试，改完跑 go test 确认。"),
        ]
        for name, key, rt in workers:
            perm = next(p for (k, _n, _t, _e, _c, p, _pr) in DEFAULT_AGENTS if k == key)
            rr = run_agent(token, ws, rt, perm, name)
            runs.append(rr)
            print(f"\n=== {name} {rr['seconds']}s / {rr['toolCalls']} 次工具 / {rr['status']} ===")
            print(rr["summary"][:300])

    after = api("/v1/me", token=token)["data"]
    report = {
        "case": args.case,
        "task": task,
        "root": args.root,
        "wallSeconds": round(time.time() - t_start, 1),
        "quotaUsed": after["user"]["used"] - before["user"]["used"],
        "discussion": disc,
        "runs": runs,
        "at": time.strftime("%Y-%m-%d %H:%M:%S"),
    }
    out = args.out or f"docs/collab-{args.case}.json"
    os.makedirs(os.path.dirname(out) or ".", exist_ok=True)
    with open(out, "w") as f:
        json.dump(report, f, ensure_ascii=False, indent=1)
    print(f"\n总耗时 {report['wallSeconds']}s · 额度消耗 {report['quotaUsed']}")
    print("报告：" + out)


if __name__ == "__main__":
    main()
