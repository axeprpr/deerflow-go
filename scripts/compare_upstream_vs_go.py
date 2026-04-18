#!/usr/bin/env python3
import csv
import json
import os
import re
import time
import uuid
from dataclasses import dataclass
from typing import Dict, List, Set, Tuple

import requests


KNOWN_TOOLS = {
    "ask_clarification",
    "read_file",
    "write_file",
    "present_files",
    "web_search",
    "web_fetch",
    "image_search",
}

DETAIL_TOKENS = [
    "clarification",
    "clarify",
    "detail",
    "more information",
    "具体",
    "细节",
    "更多",
    "需求",
    "偏好",
    "什么类型",
]


@dataclass
class Backend:
    name: str
    base_url: str


def text_from_content(content) -> str:
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        out = []
        for item in content:
            if isinstance(item, dict):
                text = item.get("text")
                if isinstance(text, str):
                    out.append(text)
        return " ".join(out)
    return ""


def detect_tools(sse_text: str) -> Set[str]:
    found = set()
    for match in re.finditer(r'"name"\s*:\s*"([^"]+)"', sse_text):
        name = match.group(1).strip()
        if name in KNOWN_TOOLS:
            found.add(name)
    for match in re.finditer(r'"tool_name"\s*:\s*"([^"]+)"', sse_text):
        name = match.group(1).strip()
        if name in KNOWN_TOOLS:
            found.add(name)
    return found


def upload_files(base_url: str, thread_id: str, files: List[Tuple[str, bytes]]) -> Tuple[bool, str]:
    if not files:
        return True, ""
    multipart = []
    for name, content in files:
        multipart.append(("files", (name, content)))
    try:
        resp = requests.post(f"{base_url}/api/threads/{thread_id}/uploads", files=multipart, timeout=120)
        if resp.status_code != 200:
            return False, f"upload_status={resp.status_code} body={resp.text[:200]}"
        return True, ""
    except Exception as exc:
        return False, f"upload_exception={exc}"


def run_stream(base_url: str, thread_id: str, prompt: str, model_name: str, timeout_seconds: int) -> Dict:
    payload = {
        "assistant_id": "lead_agent",
        "input": {
            "messages": [
                {
                    "type": "human",
                    "content": [{"type": "text", "text": prompt}],
                    "additional_kwargs": {},
                }
            ]
        },
        "stream_mode": ["messages-tuple", "events"],
        "stream_subgraphs": True,
        "stream_resumable": True,
        "context": {
            "mode": "flash",
            "model_name": model_name,
            "thinking_enabled": False,
            "is_plan_mode": False,
            "subagent_enabled": False,
            "reasoning_effort": "minimal",
        },
        "config": {"recursion_limit": 1000},
    }
    t0 = time.time()
    try:
        resp = requests.post(
            f"{base_url}/api/threads/{thread_id}/runs/stream",
            headers={"Content-Type": "application/json", "Accept": "text/event-stream"},
            data=json.dumps(payload),
            timeout=timeout_seconds,
        )
        elapsed = round(time.time() - t0, 2)
        text = resp.text
        return {
            "http_status": resp.status_code,
            "elapsed_s": elapsed,
            "has_end": "event: end" in text,
            "has_error": "event: error" in text,
            "tools": sorted(detect_tools(text)),
            "raw_size": len(text),
            "error": "",
        }
    except Exception as exc:
        return {
            "http_status": 0,
            "elapsed_s": round(time.time() - t0, 2),
            "has_end": False,
            "has_error": True,
            "tools": [],
            "raw_size": 0,
            "error": str(exc),
        }


def fetch_thread(base_url: str, thread_id: str) -> Dict:
    try:
        resp = requests.get(f"{base_url}/api/threads/{thread_id}", timeout=60)
        if resp.status_code != 200:
            return {"status_code": resp.status_code, "status": "unknown", "artifacts": [], "final_assistant": "", "messages": []}
        data = resp.json()
        messages = data.get("values", {}).get("messages", []) or []
        final_assistant = ""
        assistant_texts: List[str] = []
        thread_tools: Set[str] = set()
        for msg in messages:
            role = msg.get("role") or msg.get("type")
            content = text_from_content(msg.get("content"))
            if role in ("assistant", "ai") and content.strip():
                final_assistant = content.strip()
                assistant_texts.append(content.strip())
            for call in msg.get("tool_calls") or []:
                name = str(call.get("name", "")).strip()
                if name in KNOWN_TOOLS:
                    thread_tools.add(name)
            tool_result = msg.get("tool_result") or {}
            name = str(tool_result.get("tool_name", "")).strip()
            if name in KNOWN_TOOLS:
                thread_tools.add(name)
            mname = str(msg.get("name", "")).strip()
            if mname in KNOWN_TOOLS:
                thread_tools.add(mname)
        return {
            "status_code": 200,
            "status": data.get("status", "unknown"),
            "artifacts": data.get("values", {}).get("artifacts", []) or [],
            "final_assistant": final_assistant,
            "assistant_texts": assistant_texts,
            "thread_tools": sorted(thread_tools),
            "messages": messages,
        }
    except Exception as exc:
        return {"status_code": 0, "status": "unknown", "artifacts": [], "final_assistant": "", "messages": [], "error": str(exc)}


def looks_like_clarification(text: str) -> bool:
    low = (text or "").strip().lower()
    return any(token in low for token in DETAIL_TOKENS)


def evaluate(expect: Dict, result: Dict) -> Tuple[bool, List[str]]:
    reasons: List[str] = []

    if expect.get("must_all_steps_ok", True):
        for idx, step in enumerate(result["steps"], start=1):
            if step["http_status"] != 200:
                reasons.append(f"step{idx}:http={step['http_status']}")
            if not step["has_end"]:
                reasons.append(f"step{idx}:missing_end")
            if step["has_error"]:
                reasons.append(f"step{idx}:event_error")
            if step["error"]:
                reasons.append(f"step{idx}:exception")

    artifacts = result["thread"]["artifacts"]
    art_count = len(artifacts)
    if "artifact_min" in expect and art_count < expect["artifact_min"]:
        reasons.append(f"artifact_count={art_count}<min={expect['artifact_min']}")
    if "artifact_max" in expect and art_count > expect["artifact_max"]:
        reasons.append(f"artifact_count={art_count}>max={expect['artifact_max']}")
    for want in expect.get("artifact_contains", []):
        if want not in artifacts:
            reasons.append(f"missing_artifact={want}")

    all_tools = set(result["tools"])
    for need in expect.get("must_tools", []):
        if need not in all_tools:
            reasons.append(f"missing_tool={need}")
    if expect.get("must_any_tools"):
        if not any(tool in all_tools for tool in expect["must_any_tools"]):
            reasons.append(f"missing_any_tool={','.join(expect['must_any_tools'])}")
    for forbidden in expect.get("forbid_tools", []):
        if forbidden in all_tools:
            reasons.append(f"forbidden_tool={forbidden}")

    clarification = result["saw_clarification"]
    if "clarification" in expect:
        if bool(expect["clarification"]) != clarification:
            reasons.append(f"clarification={clarification}!=expected_{expect['clarification']}")

    return len(reasons) == 0, reasons


def build_cases() -> List[Dict]:
    note_txt = (
        "Project A shipped 12 days late because vendor parts arrived late.\n"
        "Project B is on time but has a 9% budget overrun.\n"
        "Project C hit quality target but overtime cost rose 15%.\n"
    )
    metrics_json = json.dumps(
        {
            "entity": "North Plant",
            "period": "2026-03",
            "revenue": 4200011,
            "expense": 4512300,
            "cash_balance": 2900000,
            "anomalies": ["late invoicing", "high rework"],
        },
        ensure_ascii=False,
    ).encode("utf-8")
    sales_csv = (
        "order_id,region,sales\n"
        "1001,east,120\n1002,east,80\n1003,west,150\n1004,north,90\n"
        "1005,west,60\n1006,north,110\n1007,east,40\n1008,south,70\n"
    ).encode("utf-8")
    long_csv = (
        "order_id,region,sales\n"
        "1001,east,120\n1002,east,80\n1003,west,150\n1004,north,\n"
        "1005,west,60\n1006,north,110\n1007,east,40\n1007,east,40\n"
        "1008,south,70\n1009,south,95\n1010,west,130\n"
    ).encode("utf-8")
    incident_txt = (
        "2026-03-01 payment service timeout spike for 27 minutes.\n"
        "2026-03-05 deployment rollback due to config mismatch.\n"
        "2026-03-09 DB CPU reached 92% and query queue increased.\n"
    ).encode("utf-8")
    ops_csv = (
        "service,latency_ms,error_rate\n"
        "gateway,120,0.2\nworker,230,0.9\nstate,180,0.4\nsandbox,260,1.1\n"
    ).encode("utf-8")
    product_txt = (
        "Feature A delayed by dependency.\n"
        "Feature B completed with lower test coverage.\n"
        "Feature C blocked by API quota increase request.\n"
    ).encode("utf-8")

    core_cases = [
        {
            "id": "C01",
            "group": "core",
            "desc": "simple reply",
            "steps": [{"prompt": "你好，回复 ok 即可。", "timeout": 120}],
            "expect": {"artifact_max": 0, "clarification": False},
        },
        {
            "id": "C02",
            "group": "core",
            "desc": "concrete single-page output",
            "steps": [{"prompt": "请用纯 HTML/CSS 生成一个简洁欢迎页，输出到 /mnt/user-data/outputs/index.html 并展示。", "timeout": 240}],
            "expect": {"artifact_min": 1, "must_tools": ["write_file"], "clarification": False},
        },
        {
            "id": "C03",
            "group": "core",
            "desc": "concrete animation page",
            "steps": [{"prompt": "帮我生成一个小鱼游泳的页面，输出到 /mnt/user-data/outputs/index.html。", "timeout": 420}],
            "expect": {"artifact_min": 1, "must_tools": ["write_file"], "clarification": False},
        },
        {
            "id": "C04",
            "group": "core",
            "desc": "ambiguous request should clarify",
            "steps": [{"prompt": "帮我做一个页面。", "timeout": 180}],
            "expect": {"artifact_max": 0, "clarification": True, "forbid_tools": ["write_file", "present_files"]},
        },
        {
            "id": "C05",
            "group": "core",
            "desc": "web search pricing",
            "steps": [{"prompt": "联网搜索 OpenAI API pricing 官方页面，给我标题和链接。", "timeout": 240}],
            "expect": {"must_tools": ["web_search"]},
        },
        {
            "id": "C06",
            "group": "core",
            "desc": "web fetch summary",
            "steps": [{"prompt": "联网获取 https://platform.openai.com/docs/pricing ，总结 3 点。", "timeout": 240}],
            "expect": {"must_any_tools": ["web_fetch", "web_search"]},
        },
        {
            "id": "C07",
            "group": "core",
            "desc": "upload txt summary",
            "uploads": [("weekly_note.txt", note_txt.encode("utf-8"))],
            "steps": [{"prompt": "基于我上传的 weekly_note.txt，输出 3 条风险和 2 条建议。", "timeout": 240}],
            "expect": {"must_tools": ["read_file"]},
        },
        {
            "id": "C08",
            "group": "core",
            "desc": "upload json insight",
            "uploads": [("metrics.json", metrics_json)],
            "steps": [{"prompt": "基于 metrics.json 做经营分析，给 3 条结论并标注依据。", "timeout": 240}],
            "expect": {"must_tools": ["read_file"]},
        },
        {
            "id": "C09",
            "group": "core",
            "desc": "upload csv quick stats",
            "uploads": [("sales.csv", sales_csv)],
            "steps": [{"prompt": "读取 sales.csv，按 region 汇总 sales 并给异常点。", "timeout": 240}],
            "expect": {"must_tools": ["read_file"]},
        },
        {
            "id": "C10",
            "group": "core",
            "desc": "same-thread clarify then execute",
            "steps": [
                {"prompt": "帮我做一个页面。", "timeout": 180},
                {"prompt": "请用纯 HTML/CSS/JS 生成深蓝科技风欢迎页，输出到 /mnt/user-data/outputs/index.html 并展示。", "timeout": 300},
            ],
            "expect": {"artifact_min": 1, "must_tools": ["write_file"], "clarification": True},
        },
        {
            "id": "C11",
            "group": "core",
            "desc": "same-thread generate then modify",
            "steps": [
                {"prompt": "生成一个简单 landing page 到 /mnt/user-data/outputs/index.html。", "timeout": 260},
                {"prompt": "基于刚才生成的 index.html，把主按钮改成橙色并保持其它结构。", "timeout": 260},
            ],
            "expect": {"artifact_min": 1, "must_tools": ["write_file"]},
        },
        {
            "id": "C12",
            "group": "core",
            "desc": "csv long-run dual outputs",
            "uploads": [("orders_dirty.csv", long_csv)],
            "steps": [
                {
                    "prompt": "我上传了 orders_dirty.csv。请清洗空值和重复行，按 region 汇总，并生成 /mnt/user-data/outputs/orders-summary.md 和 /mnt/user-data/outputs/orders-summary.json，最后展示文件。",
                    "timeout": 420,
                }
            ],
            "expect": {
                "artifact_min": 2,
                "artifact_contains": [
                    "/mnt/user-data/outputs/orders-summary.md",
                    "/mnt/user-data/outputs/orders-summary.json",
                ],
                "must_tools": ["write_file"],
                "clarification": False,
            },
        },
    ]

    extended_cases = [
        {
            "id": "C13",
            "group": "extended",
            "desc": "multi-upload cross-file synthesis",
            "uploads": [("weekly_note.txt", note_txt.encode("utf-8")), ("metrics.json", metrics_json)],
            "steps": [{"prompt": "综合 weekly_note.txt 和 metrics.json，输出 4 条跨文件风险判断并给依据。", "timeout": 280}],
            "expect": {"must_tools": ["read_file"]},
        },
        {
            "id": "C14",
            "group": "extended",
            "desc": "same-thread upload summarize then action plan",
            "uploads": [("incident_log.txt", incident_txt)],
            "steps": [
                {"prompt": "基于 incident_log.txt，总结 3 条主要故障模式。", "timeout": 240},
                {"prompt": "在上一步基础上，给 5 条可执行改进动作，按优先级排序。", "timeout": 240},
            ],
            "expect": {"must_tools": ["read_file"], "artifact_max": 0},
        },
        {
            "id": "C15",
            "group": "extended",
            "desc": "plan only without writing files",
            "steps": [{"prompt": "给一个“企业数据分析页”的实施方案和里程碑，不要创建任何文件。", "timeout": 220}],
            "expect": {"artifact_max": 0, "forbid_tools": ["write_file", "present_files"]},
        },
        {
            "id": "C16",
            "group": "extended",
            "desc": "dual output artifacts from prompt",
            "steps": [
                {
                    "prompt": (
                        "以下是发布信息：version=1.4.2；date=2026-04-16；changes=[新增审计日志导出,修复批量上传超时,优化首页加载性能]；"
                        "known_issues=[移动端筛选弹窗偶发卡顿]。"
                        "请直接生成发布说明，写入 /mnt/user-data/outputs/release-notes.md 和 /mnt/user-data/outputs/release-notes.json，并展示文件，不要追问。"
                    ),
                    "timeout": 320,
                }
            ],
            "expect": {"artifact_min": 2, "must_tools": ["write_file"]},
        },
        {
            "id": "C17",
            "group": "extended",
            "desc": "web compare two official pricing pages",
            "steps": [{"prompt": "联网搜索 OpenAI 和 Anthropic 的官方 pricing 页面，给我两个链接和一句差异说明。", "timeout": 260}],
            "expect": {"must_tools": ["web_search"]},
        },
        {
            "id": "C18",
            "group": "extended",
            "desc": "web fetch docs key points",
            "steps": [{"prompt": "联网获取 https://platform.openai.com/docs/api-reference ，提炼 3 条关键点。", "timeout": 260}],
            "expect": {"must_any_tools": ["web_fetch", "web_search"]},
        },
        {
            "id": "C19",
            "group": "extended",
            "desc": "ambiguous analysis should clarify",
            "steps": [{"prompt": "帮我分析一下这个月情况。", "timeout": 180}],
            "expect": {"artifact_max": 0, "clarification": True},
        },
        {
            "id": "C20",
            "group": "extended",
            "desc": "same-thread iterative page refinement x3",
            "steps": [
                {"prompt": "先生成基础 landing page 到 /mnt/user-data/outputs/index.html。", "timeout": 260},
                {"prompt": "把这个页面改成深色主题并增加指标卡片区域。", "timeout": 260},
                {"prompt": "再新增一个 CTA 区块并展示最终文件。", "timeout": 260},
            ],
            "expect": {"artifact_min": 1, "must_tools": ["write_file"]},
        },
        {
            "id": "C21",
            "group": "extended",
            "desc": "ops csv to markdown report artifact",
            "uploads": [("ops_metrics.csv", ops_csv)],
            "steps": [
                {
                    "prompt": "读取 ops_metrics.csv，分析瓶颈并写入 /mnt/user-data/outputs/ops-report.md，最后展示。",
                    "timeout": 300,
                }
            ],
            "expect": {"artifact_min": 1, "must_tools": ["read_file", "write_file"]},
        },
        {
            "id": "C22",
            "group": "extended",
            "desc": "multilingual upload and output to file",
            "uploads": [("product_note.txt", product_txt)],
            "steps": [
                {
                    "prompt": "请读取 product_note.txt，先中文总结 3 条风险，再英文写入 /mnt/user-data/outputs/product-risk-en.md 并展示。",
                    "timeout": 300,
                }
            ],
            "expect": {"artifact_min": 1, "must_tools": ["read_file", "write_file"]},
        },
    ]

    return core_cases + extended_cases


def run_case(backend: Backend, case: Dict, model_name: str) -> Dict:
    thread_id = str(uuid.uuid4())
    upload_ok, upload_err = upload_files(backend.base_url, thread_id, case.get("uploads", []))
    steps_out = []
    all_tools: Set[str] = set()

    if not upload_ok:
        steps_out.append(
            {
                "http_status": 0,
                "elapsed_s": 0.0,
                "has_end": False,
                "has_error": True,
                "tools": [],
                "raw_size": 0,
                "error": upload_err,
            }
        )
    else:
        for step in case["steps"]:
            out = run_stream(
                backend.base_url,
                thread_id,
                step["prompt"],
                model_name=model_name,
                timeout_seconds=step.get("timeout", 240),
            )
            steps_out.append(out)
            all_tools.update(out["tools"])

    thread = fetch_thread(backend.base_url, thread_id)
    all_tools.update(thread.get("thread_tools", []))
    final_assistant = thread.get("final_assistant", "")
    assistant_texts = thread.get("assistant_texts", [])
    saw_clarification = ("ask_clarification" in all_tools) or looks_like_clarification(final_assistant) or any(
        looks_like_clarification(text) for text in assistant_texts
    )

    result = {
        "backend": backend.name,
        "base_url": backend.base_url,
        "case_id": case["id"],
        "case_desc": case["desc"],
        "thread_id": thread_id,
        "steps": steps_out,
        "tools": sorted(all_tools),
        "thread": thread,
        "saw_clarification": saw_clarification,
    }
    ok, reasons = evaluate(case["expect"], result)
    result["pass"] = ok
    result["reasons"] = reasons
    return result


def main():
    go_base = os.environ.get("GO_BASE_URL", "http://127.0.0.1:58080").rstrip("/")
    upstream_base = os.environ.get("UPSTREAM_BASE_URL", "http://192.168.23.35:8001").rstrip("/")
    model_name = os.environ.get("MODEL_NAME", "qwen3.5-27b")
    case_filter = os.environ.get("CASE_FILTER", "").strip()
    case_group = os.environ.get("CASE_GROUP", "").strip()

    backends = [
        Backend(name="go", base_url=go_base),
        Backend(name="upstream", base_url=upstream_base),
    ]

    cases = build_cases()
    if case_group:
        allow_groups = {x.strip() for x in case_group.split(",") if x.strip()}
        cases = [c for c in cases if c.get("group", "core") in allow_groups]
    if case_filter:
        allow = {x.strip() for x in case_filter.split(",") if x.strip()}
        cases = [c for c in cases if c["id"] in allow]

    print(f"Running {len(cases)} cases against {len(backends)} backends...")
    results: List[Dict] = []

    for case in cases:
        for backend in backends:
            print(f"[{backend.name}] {case['id']} {case['desc']} ...")
            result = run_case(backend, case, model_name)
            steps_ok = sum(1 for s in result["steps"] if s["http_status"] == 200 and s["has_end"] and not s["has_error"])
            print(
                f"  -> pass={result['pass']} status={result['thread']['status']} artifacts={len(result['thread']['artifacts'])} "
                f"clarify={result['saw_clarification']} steps_ok={steps_ok}/{len(result['steps'])} tools={','.join(result['tools']) or '-'}"
            )
            if result["reasons"]:
                print(f"     reasons={';'.join(result['reasons'])}")
            results.append(result)

    summary_path = os.environ.get("COMPARE_SUMMARY_TSV", "/tmp/df_compare_summary.tsv")
    detail_path = os.environ.get("COMPARE_DETAIL_JSON", "/tmp/df_compare_detail.json")

    with open(summary_path, "w", newline="", encoding="utf-8") as f:
        w = csv.writer(f, delimiter="\t")
        w.writerow(
            [
                "case_id",
                "case_desc",
                "backend",
                "pass",
                "thread_status",
                "artifact_count",
                "clarification",
                "tools",
                "step_count",
                "step_ok_count",
                "reasons",
                "thread_id",
            ]
        )
        for r in results:
            step_ok_count = sum(1 for s in r["steps"] if s["http_status"] == 200 and s["has_end"] and not s["has_error"])
            w.writerow(
                [
                    r["case_id"],
                    r["case_desc"],
                    r["backend"],
                    "PASS" if r["pass"] else "FAIL",
                    r["thread"]["status"],
                    len(r["thread"]["artifacts"]),
                    str(r["saw_clarification"]).lower(),
                    ",".join(r["tools"]),
                    len(r["steps"]),
                    step_ok_count,
                    ";".join(r["reasons"]),
                    r["thread_id"],
                ]
            )

    with open(detail_path, "w", encoding="utf-8") as f:
        json.dump(results, f, ensure_ascii=False, indent=2)

    by_case: Dict[str, Dict[str, Dict]] = {}
    for r in results:
        by_case.setdefault(r["case_id"], {})[r["backend"]] = r

    mismatch = 0
    both_pass = 0
    for case_id, pair in sorted(by_case.items()):
        go_r = pair.get("go")
        up_r = pair.get("upstream")
        if not go_r or not up_r:
            mismatch += 1
            continue
        if go_r["pass"] and up_r["pass"]:
            both_pass += 1
        same_shape = (
            go_r["saw_clarification"] == up_r["saw_clarification"]
            and len(go_r["thread"]["artifacts"]) == len(up_r["thread"]["artifacts"])
            and set(go_r["tools"]) == set(up_r["tools"])
        )
        if not same_shape:
            mismatch += 1

    print()
    print(f"Summary TSV:   {summary_path}")
    print(f"Detail JSON:   {detail_path}")
    print(f"Cases total:   {len(cases)}")
    print(f"Both pass:     {both_pass}/{len(cases)}")
    print(f"Shape mismatch:{mismatch}/{len(cases)}")


if __name__ == "__main__":
    main()
