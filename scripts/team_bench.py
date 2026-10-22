#!/usr/bin/env python3
"""团队协作场景：1 架构师 + 2 后端 + 1 前端。

流程（这就是产品要宣传的那条链路）：
  1. 架构师在群里拆解任务、定分工
  2. 两个后端 + 一个前端**同时**开工，各改各的目录
  3. 全部完成后，架构师以只读权限审核：跑 go test / -race，逐项对照需求

用法：
  python3 scripts/team_bench.py --case auth --root /tmp/dsh-team            # 团队
  python3 scripts/team_bench.py --case auth --root /tmp/dsh-team-solo --solo # 单 agent 对照
"""
import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor

BASE = "http://127.0.0.1:8080"

# 角色：key, 名字, 岗位, emoji, 颜色, 权限, scope, prompt
AGENTS = [
    ("arch", "架构师", "主持人", "D", "#4F46E5", "audit", None,
     "你是架构师。收到需求先定接口与边界，把工作拆成互不阻塞的独立模块，"
     "明确每个模块归谁、验收标准是什么。你自己不写业务代码。"),
    ("auth", "后端A", "鉴权", "B", "#0369A1", "approval", ["auth/"],
     "你只负责 auth/ 目录。改完必须跑测试。"),
    ("store", "后端B", "存储", "S", "#047857", "approval", ["store/"],
     "你只负责 store/ 目录。改完必须跑测试。"),
    ("cache", "后端C", "缓存", "C", "#7C3AED", "approval", ["cache/"],
     "你只负责 cache/ 目录。改完必须跑测试。"),
    ("queue", "后端D", "队列", "Q", "#B45309", "approval", ["queue/"],
     "你只负责 queue/ 目录。改完必须跑测试。"),
    ("api", "后端E", "接口", "A", "#0E7490", "approval", ["api/"],
     "你只负责 api/ 目录。改完必须跑测试。"),
    ("web", "前端", "界面", "F", "#DB2777", "approval", ["web/"],
     "你只负责 web/ 目录。改完说明改了什么。"),
]

WORK = {
    "auth": ("后端A", "auth/", "auth：Sign/Verify（HS256），测试覆盖过期、篡改、格式错、空密钥"),
    "store": ("后端B", "store/", "store：带 TTL 的并发 KV，测试覆盖并发（-race）与过期"),
    "cache": ("后端C", "cache/", "cache：固定容量 LRU，测试覆盖淘汰顺序"),
    "queue": ("后端D", "queue/", "queue：并发安全队列，测试覆盖空/非空 Pop"),
    "api": ("后端E", "api/", "api：NewMux + 耗时中间件 + /health，测试覆盖中间件被调用"),
    "web": ("前端", "web/", "web：login.html，/api/login 提交，三态渲染"),
}


def api(path, body=None, token=None, method=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data,
                                 method=method or ("POST" if body is not None else "GET"))
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=180) as r:
            return json.loads(r.read().decode())
    except urllib.error.HTTPError as e:
        raise RuntimeError(f"{path}: HTTP {e.code} {e.read().decode()[:300]}")


def sse(path, body, token, idle_timeout=240):
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


def create_conv(token, title):
    d = api("/v1/conversations", {
        "title": title,
        "studio": "custom",
        "agents": [
            {"key": k, "name": n, "title": t, "emoji": e, "color": c,
             "prompt": p, "permission": perm, "scope": scope or []}
            for (k, n, t, e, c, perm, scope, p) in AGENTS
        ],
    }, token)
    return d["data"]["id"]


def bind_ws(token, root, conv=None):
    body = {"root": root}
    if conv:
        body["conversationId"] = conv
    d = api("/v1/workspaces", body, token)
    return d["data"]["id"]


def discuss(token, conv, task):
    t0 = time.time()
    said = []
    for ev in sse(f"/v1/conversations/{conv}/orchestrate", {"content": task}, token):
        if ev.get("type") == "end" and ev.get("content"):
            said.append({"agent": ev.get("name"), "text": ev["content"].strip(),
                         "tokens": ev.get("tokens", 0)})
    return {"seconds": round(time.time() - t0, 1), "said": said,
            "tokens": sum(s.get("tokens", 0) for s in said)}


def run_agent(token, ws, task, permission, operator, scope=None, max_steps=14):
    body = {"task": task, "permission": permission, "maxSteps": max_steps,
            "operatorKey": operator}
    if scope:
        body["scope"] = scope
    d = api(f"/v1/workspaces/{ws}/runs", body, token)
    rid = d["data"]["id"]
    t0 = time.time()
    approvals = 0
    while True:
        time.sleep(1.5)
        r = api(f"/v1/runs/{rid}", token=token)["data"]
        if r["status"] == "waiting_approval":
            for tc in r.get("toolCalls") or []:
                if tc.get("status") == "pending_approval" and tc.get("approvalId"):
                    try:
                        api(f"/v1/runs/{rid}/approve",
                            {"approvalId": tc["approvalId"], "approve": True, "scope": "run"}, token)
                        approvals += 1
                    except RuntimeError as e:
                        if "already" not in str(e):
                            raise
            continue
        if r["status"] in ("done", "failed", "aborted", "cancelled"):
            break
        if time.time() - t0 > 600:
            raise RuntimeError(f"run {rid} timeout")
    return {
        "agent": operator, "runId": rid, "status": r["status"], "steps": r["step"],
        "tokensIn": r["tokensIn"], "tokensOut": r["tokensOut"],
        "seconds": round(time.time() - t0, 1), "toolCalls": len(r.get("toolCalls") or []),
        "approvals": approvals,
        "diffs": [(x["path"], x["status"]) for x in (r.get("diffs") or [])],
        "summary": (r.get("summary") or "")[:1200],
        "error": r.get("error", ""),
    }


def review_prompt():
    return ("三个模块都做完了。你只有 6 步预算，按这个顺序做完就下结论：\n"
            "1. `go test ./... -count=1`（一次）\n"
            "2. `go test -race ./... -count=1`（一次）\n"
            "3. 读一个你最怀疑的测试文件，判断断言是不是恒真、有没有漏边界\n"
            "然后立刻用一段话给结论：三个模块是否达标、测试有没有空转、漏了什么。\n"
            "不要逐个文件读，不要重复跑同一条命令。最后一行写 VERDICT: clean 或 VERDICT: issues")


TASK = ("按 README 把六个模块全做完：auth、store、cache、queue、api、web，"
        "每个都要实现 + 测试 + go test 通过。六个模块互不依赖，可以并行。")


def solo(token, ws, task):
    """单 agent：一个人把三个模块全做了（串行），同样跑一轮只读审核。"""
    t0 = time.time()
    r = run_agent(token, ws, task + "\n\n六个模块都由你一个人完成，按 auth → store → cache → queue → api → web 的顺序。",
                  "full", "solo", max_steps=60)
    review_task = review_prompt()
    rv = run_agent(token, ws, review_task, "audit", "审核", max_steps=6)
    print(f"  审核 {rv['seconds']}s")
    return {"seconds": round(time.time() - t0, 1), "runs": [r], "review": rv}


def team(token, ws, conv, task, args_root="."):
    t0 = time.time()
    disc = discuss(token, conv, task)
    plan = next((s["text"] for s in disc["said"] if s["agent"] == "架构师"), "")
    print(f"  讨论 {disc['seconds']}s · {len(disc['said'])} 人发言 · 架构师拆解 {len(plan)} 字")

    workers = []
    dirs = [d for d in WORK if os.path.isdir(os.path.join(args_root, d))]
    picked = {k: v for k, v in WORK.items() if k in dirs}
    for key, (name, scope_dir, brief) in picked.items():
        wt = (f"{task}\n\n你负责的模块：{scope_dir}\n你只改 {scope_dir} 下的文件，"
              f"不要碰别的目录。做完跑 `go test ./{scope_dir.rstrip('/')}/...` 确认。\n\n"
              f"架构师的拆解：\n{plan}")
        workers.append((name, key, scope_dir, wt))

    par_start = time.time()
    results = []
    with ThreadPoolExecutor(max_workers=6) as ex:
        futs = {
            ex.submit(run_agent, token, ws, wt,
                      next(a[5] for a in AGENTS if a[0] == key), key,
                      next(a[6] for a in AGENTS if a[0] == key)): name
            for (name, key, _, wt) in workers
        }
        for f in futs:
            results.append(f.result())
    par_wall = round(time.time() - par_start, 1)
    print(f"  并行执行 {par_wall}s（串行的话是 {round(sum(r['seconds'] for r in results),1)}s）")

    review = run_agent(token, ws, review_prompt(), "audit", "架构师", max_steps=6)
    print(f"  架构师审核 {review['seconds']}s")

    return {"seconds": round(time.time() - t0, 1), "discussion": disc,
            "parallelSeconds": par_wall, "runs": results, "review": review}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--case", required=True)
    ap.add_argument("--root", required=True)
    ap.add_argument("--solo", action="store_true")
    ap.add_argument("--out", default="")
    args = ap.parse_args()

    token = login()
    before = api("/v1/me", token=token)["data"]
    t0 = time.time()

    print(f"=== {'单 agent' if args.solo else '团队协作'} · {args.root} ===")
    if args.solo:
        conv = create_conv(token, f"单 agent · {args.case}")
        ws = bind_ws(token, args.root, conv)
        res = solo(token, ws, TASK)
        report = {"case": args.case + "-solo", "mode": "solo", "root": args.root,
                  "wallSeconds": round(time.time() - t0, 1), **res}
    else:
        conv = create_conv(token, f"团队协作 · {args.case}")
        ws = bind_ws(token, args.root, conv)
        res = team(token, ws, conv, TASK, args.root)
        report = {"case": args.case + "-team", "mode": "team", "root": args.root,
                  "wallSeconds": round(time.time() - t0, 1), **res}

    after = api("/v1/me", token=token)["data"]
    report["quotaUsed"] = after["user"]["used"] - before["user"]["used"]
    report["at"] = time.strftime("%Y-%m-%d %H:%M:%S")

    runs = report.get("runs") or []
    tok = sum(r["tokensIn"] + r["tokensOut"] for r in runs)
    if "review" in report:
        tok += report["review"]["tokensIn"] + report["review"]["tokensOut"]
    report["totalTokens"] = tok

    out = args.out or f"docs/team-{args.case}{'-solo' if args.solo else ''}.json"
    os.makedirs(os.path.dirname(out) or ".", exist_ok=True)
    with open(out, "w") as f:
        json.dump(report, f, ensure_ascii=False, indent=1)

    print(f"\n墙钟 {report['wallSeconds']}s · 总 token {tok} · 额度 {report['quotaUsed']}")
    for r in runs:
        print(f"  [{r['agent']}] {r['status']} {r['steps']}步 {r['toolCalls']}工具 "
              f"{r['seconds']}s tok={r['tokensIn']}/{r['tokensOut']} 文件={len(r['diffs'])}")
    if "review" in report:
        rv = report["review"]
        print(f"  [审核] {rv['status']} {rv['steps']}步 {rv['seconds']}s")
        print("  结论:", (rv["summary"] or "")[:400].replace("\n", " "))
    print("报告：" + out)


if __name__ == "__main__":
    main()
