#!/usr/bin/env python3
"""
模型能力对照实验。

同一个任务分别发给 deepseek-chat 与任务池 Flash，比较：
  - 是否能产出可用的结构化结果
  - 是否能正确调用工具（生成文件类任务的关键）
  - 耗时与 token 成本

用法：python3 scripts/model-compare.py [base] [phone]
"""
import json
import sys
import time
import urllib.request

BASE = sys.argv[1] if len(sys.argv) > 1 else "https://teamwise.cn"
PHONE = sys.argv[2] if len(sys.argv) > 2 else "13800001111"

CHAT_MODEL = "deepseek-chat"
TASK_MODELS = ["d1api-v4-flash", "cc-v4-flash", "od-v4-flash"]


def post(path, body, token=None, timeout=120):
    req = urllib.request.Request(
        BASE + path,
        data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json"},
    )
    if token:
        req.add_header("Authorization", "Bearer " + token)
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return json.loads(r.read())


def login():
    code = post("/v1/auth/send-code", {"kind": "phone", "target": PHONE})["data"]["devCode"]
    d = post("/v1/auth/login", {"kind": "phone", "target": PHONE, "code": code})
    return d["data"]["token"]


TASKS = [
    {
        "key": "docx",
        "name": "生成 Word 文档（结构化长文）",
        "prompt": (
            "写一份技术方案文档，主题是「如何给一个 20 人团队搭建内部知识库」。"
            "要求：3 个章节，每章至少 2 段，包含具体的工具选型理由和实施步骤。"
        ),
        "tools": False,
    },
    {
        "key": "xlsx",
        "name": "生成表格（数据整理）",
        "prompt": (
            "整理一份「竞标评分表」：列出 6 个评分项，每项给出权重、评分标准、"
            "满分 100 时该占多少分。用表格形式呈现。"
        ),
        "tools": False,
    },
    {
        "key": "analyze",
        "name": "长文档分析（决策类）",
        "prompt": (
            "阅读下面这份需求，指出 3 个会导致项目延期的风险点，并给每个风险点"
            "一个具体的缓解措施。\n\n"
            "需求：我们需要在 6 周内做一个客户管理系统。包含：登录注册、客户资料"
            "增删改查、销售漏斗可视化、和企业微信打通、数据导出 Excel、移动端适配。"
            "团队 3 人（1 前端 1 后端 1 测试），都没做过企微对接。"
        ),
        "tools": False,
    },
    {
        "key": "toolcall",
        "name": "工具调用（能否正确产出文件）",
        "prompt": "把刚才讨论的结论写进 docs/conclusion.md。",
        "tools": True,
    },
]


def call(model, task, token, task_flag):
    body = {
        "model": model,
        "messages": [{"role": "user", "content": task["prompt"]}],
        "stream": False,
    }
    if task["tools"]:
        body["tools"] = [{
            "type": "function",
            "function": {
                "name": "write",
                "description": "把内容写入工作区文件",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "path": {"type": "string"},
                        "content": {"type": "string"},
                    },
                    "required": ["path", "content"],
                },
            },
        }]
        body["tool_choice"] = "auto"

    t0 = time.time()
    try:
        d = post("/v1/chat/completions", body, token, timeout=180)
    except Exception as e:
        return {"err": str(e)[:120], "secs": time.time() - t0}
    secs = time.time() - t0

    if not d.get("ok"):
        return {"err": (d.get("error") or {}).get("message", "?")[:120], "secs": secs}

    data = d["data"]
    msg = (data.get("choices") or [{}])[0].get("message", {})
    usage = data.get("usage") or {}
    return {
        "secs": round(secs, 1),
        "chars": len(msg.get("content") or ""),
        "tools": [t["function"]["name"] for t in (msg.get("tool_calls") or [])],
        "tokens": usage.get("total_tokens", 0),
        "content": (msg.get("content") or "")[:300],
        "err": None,
    }


def main():
    print(f"对照实验：{BASE}\n")
    token = login()
    print(f"已登录（{PHONE}）\n")

    models = [(CHAT_MODEL, False)] + [(m, True) for m in TASK_MODELS]

    for task in TASKS:
        print("=" * 70)
        print(f"{task['name']}")
        print("=" * 70)
        for model, tf in models:
            # taskOnly 模型不能直接点名（网关会按池过滤），
            # 所以用 Task 标志 + 空模型名让它自己挑
            m = "" if tf else model
            body_note = "task池" if tf else "聊天池"
            r = call(m, task, token, tf)
            label = f"{model}({body_note})"
            if r.get("err"):
                print(f"  {label:28s} ✗ {r['err']}")
                continue
            print(f"  {label:28s} {r['secs']:>5.1f}s  {r['chars']:>5d}字  "
                  f"{r['tokens']:>5d}tok  tools={r['tools'] or '-'}")
        print()


if __name__ == "__main__":
    main()
