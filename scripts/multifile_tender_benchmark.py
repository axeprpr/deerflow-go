#!/usr/bin/env python3
import json
import os
import re
import tempfile
import time
import uuid
from dataclasses import dataclass
from typing import Dict, List, Tuple

import requests


@dataclass
class Backend:
    name: str
    base_url: str


CRITICAL_FACTS = {
    "tender_id": "TB-2026-041",
    "deadline": "2026-05-30 17:00 CST",
    "bid_bond_cny": "1200000",
    "mandatory_cert": "ISO27001,CMMI3",
    "pm_name": "Alice Chen",
    "liquidated_damage": "0.5% per day, cap 10%",
    "data_residency": "Beijing region only",
    "sla_p1": "response 15min, fix 4h",
    "cluster_nodes": "128",
    "payment_terms": "30/60/10",
}


def noisy_block(title: str, repeat: int = 140) -> str:
    line = (
        "This paragraph is contextual background for tender evaluation, "
        "including non-critical supplier history, process narratives, and descriptive text."
    )
    return f"# {title}\n\n" + "\n".join(f"{i+1}. {line}" for i in range(repeat))


def build_tender_files(tmpdir: str, repeat_main: int, repeat_contact: int) -> List[Tuple[str, bytes]]:
    files = []
    docs = {
        "01_rfp_overview.md": (
            noisy_block("RFP Overview", repeat_main)
            + f"\n\nCRITICAL_FACT: Tender ID is {CRITICAL_FACTS['tender_id']}.\n"
            + f"CRITICAL_FACT: Submission deadline is {CRITICAL_FACTS['deadline']}.\n"
        ),
        "02_commercial_terms.md": (
            noisy_block("Commercial Terms", repeat_main)
            + f"\n\nCRITICAL_FACT: Bid bond amount is CNY {CRITICAL_FACTS['bid_bond_cny']}.\n"
            + f"CRITICAL_FACT: Payment terms are {CRITICAL_FACTS['payment_terms']}.\n"
        ),
        "03_compliance_requirements.md": (
            noisy_block("Compliance", repeat_main)
            + f"\n\nCRITICAL_FACT: Mandatory certifications are {CRITICAL_FACTS['mandatory_cert']}.\n"
            + f"CRITICAL_FACT: Data residency requirement is {CRITICAL_FACTS['data_residency']}.\n"
        ),
        "04_delivery_sla.md": (
            noisy_block("Delivery and SLA", repeat_main)
            + f"\n\nCRITICAL_FACT: SLA for P1 is {CRITICAL_FACTS['sla_p1']}.\n"
            + f"CRITICAL_FACT: Required cluster node count is {CRITICAL_FACTS['cluster_nodes']}.\n"
        ),
        "05_risk_legal.md": (
            noisy_block("Risk and Legal", repeat_main)
            + f"\n\nCRITICAL_FACT: Liquidated damage is {CRITICAL_FACTS['liquidated_damage']}.\n"
        ),
        "06_contacts.md": (
            noisy_block("Contacts", repeat_contact)
            + f"\n\nCRITICAL_FACT: Project manager is {CRITICAL_FACTS['pm_name']}.\n"
        ),
    }

    for name, content in docs.items():
        path = os.path.join(tmpdir, name)
        with open(path, "wb") as f:
            f.write(content.encode("utf-8"))
        files.append((name, content.encode("utf-8")))
    return files


def upload_files(base_url: str, thread_id: str, files: List[Tuple[str, bytes]]) -> None:
    multipart = [("files", (name, content)) for name, content in files]
    resp = requests.post(f"{base_url}/api/threads/{thread_id}/uploads", files=multipart, timeout=240)
    resp.raise_for_status()


def stream_turn(base_url: str, thread_id: str, prompt: str, model_name: str, timeout_s: int = 420) -> Dict:
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
        "config": {"recursion_limit": 1200},
    }
    t0 = time.time()
    try:
        resp = requests.post(
            f"{base_url}/api/threads/{thread_id}/runs/stream",
            headers={"Content-Type": "application/json", "Accept": "text/event-stream"},
            data=json.dumps(payload),
            timeout=timeout_s,
        )
        elapsed = round(time.time() - t0, 2)
        text = resp.text
        error_message = ""
        if "event: error" in text:
            matches = re.findall(r"event: error\s+data: (\{.*?\})", text, flags=re.DOTALL)
            if matches:
                try:
                    payload = json.loads(matches[-1])
                    error_message = str(payload.get("message", ""))[:500]
                except Exception:
                    error_message = matches[-1][:500]
        return {
            "http_status": resp.status_code,
            "elapsed_s": elapsed,
            "has_end": "event: end" in text,
            "has_error": "event: error" in text,
            "read_file_calls": len(re.findall(r'"name"\s*:\s*"read_file"', text)),
            "raw_size": len(text),
            "error_message": error_message,
        }
    except Exception as exc:
        return {
            "http_status": 0,
            "elapsed_s": round(time.time() - t0, 2),
            "has_end": False,
            "has_error": True,
            "read_file_calls": 0,
            "raw_size": 0,
            "error_message": str(exc)[:500],
        }


def get_last_assistant(base_url: str, thread_id: str) -> str:
    resp = requests.get(f"{base_url}/api/threads/{thread_id}", timeout=60)
    resp.raise_for_status()
    data = resp.json()
    last = ""
    for msg in data.get("values", {}).get("messages", []) or []:
        role = msg.get("role") or msg.get("type")
        content = msg.get("content")
        text = ""
        if isinstance(content, str):
            text = content
        elif isinstance(content, list):
            parts = []
            for p in content:
                if isinstance(p, dict) and isinstance(p.get("text"), str):
                    parts.append(p["text"])
            text = " ".join(parts)
        if role in ("assistant", "ai") and text.strip():
            last = text.strip()
    return last


def score_recall(answer: str) -> Dict:
    low = answer.lower()
    checks = {
        "tender_id": CRITICAL_FACTS["tender_id"].lower() in low,
        "deadline": CRITICAL_FACTS["deadline"].lower() in low,
        "bid_bond_cny": ("1200000" in low) or ("1,200,000" in low),
        "mandatory_cert": ("iso27001" in low) and ("cmmi3" in low),
        "pm_name": CRITICAL_FACTS["pm_name"].lower() in low,
        "liquidated_damage": ("0.5%" in low) and ("10%" in low),
        "data_residency": "beijing" in low and "region" in low,
        "sla_p1": ("15min" in low or "15 min" in low) and ("4h" in low or "4 h" in low),
        "cluster_nodes": re.search(r"\b128\b", low) is not None,
        "payment_terms": ("30/60/10" in low) or ("30-60-10" in low),
    }
    hit = sum(1 for v in checks.values() if v)
    return {"hit": hit, "total": len(checks), "checks": checks}


def run_benchmark(backend: Backend, model_name: str, repeat_main: int, repeat_contact: int) -> Dict:
    thread_id = str(uuid.uuid4())
    with tempfile.TemporaryDirectory(prefix="tender-benchmark-") as tmpdir:
        files = build_tender_files(tmpdir, repeat_main=repeat_main, repeat_contact=repeat_contact)
        upload_files(backend.base_url, thread_id, files)

    turns = [
        (
            "turn1_extract",
            "请严格从我上传的文件中提取这10个字段并输出JSON："
            "tender_id, deadline, bid_bond_cny, mandatory_cert, pm_name, liquidated_damage, "
            "data_residency, sla_p1, cluster_nodes, payment_terms。不要遗漏。",
        ),
        (
            "turn2_risk_matrix",
            "请按“技术/商务/法务/交付”四类，逐文件给详细风险矩阵（至少1200字）。",
        ),
        (
            "turn3_long_review",
            "请写不少于1500字的综合评审意见，要求包含争议点、取舍依据、缓释方案。",
        ),
        (
            "turn4_milestone",
            "请给出实施季度里程碑、资源计划、关键依赖，尽量详细。",
        ),
        (
            "turn5_recall",
            "现在不需要解释，直接再次输出最初10个关键字段JSON（同样字段名），确保和原文一致。",
        ),
    ]

    out = {"backend": backend.name, "base_url": backend.base_url, "thread_id": thread_id, "turns": {}}
    for turn_id, prompt in turns:
        metrics = stream_turn(backend.base_url, thread_id, prompt, model_name=model_name, timeout_s=420)
        answer = get_last_assistant(backend.base_url, thread_id)
        out["turns"][turn_id] = {"metrics": metrics, "answer": answer}
        print(
            f"[{backend.name}] {turn_id}: http={metrics['http_status']} end={metrics['has_end']} "
            f"err={metrics['has_error']} elapsed={metrics['elapsed_s']}s read_file_calls={metrics['read_file_calls']}"
        )

    s1 = score_recall(out["turns"]["turn1_extract"]["answer"])
    s5 = score_recall(out["turns"]["turn5_recall"]["answer"])
    out["score_turn1"] = s1
    out["score_turn5"] = s5
    out["degrade"] = s1["hit"] - s5["hit"]
    return out


def main():
    model_name = os.environ.get("MODEL_NAME", "qwen3.5-27b")
    go_base = os.environ.get("GO_BASE_URL", "http://127.0.0.1:58080").rstrip("/")
    upstream_base = os.environ.get("UPSTREAM_BASE_URL", "http://192.168.23.35:8001").rstrip("/")
    repeat_main = int(os.environ.get("TENDER_REPEAT_MAIN", "120"))
    repeat_contact = int(os.environ.get("TENDER_REPEAT_CONTACT", "80"))

    backends = [
        Backend("go", go_base),
        Backend("upstream", upstream_base),
    ]
    results = []
    for backend in backends:
        print(f"\n=== Running benchmark on {backend.name} ({backend.base_url}) ===")
        results.append(run_benchmark(backend, model_name=model_name, repeat_main=repeat_main, repeat_contact=repeat_contact))

    out_json = os.environ.get("TENDER_BENCHMARK_JSON", "/tmp/tender_multifile_benchmark.json")
    with open(out_json, "w", encoding="utf-8") as f:
        json.dump(results, f, ensure_ascii=False, indent=2)

    print("\n=== Recall Score Summary ===")
    for r in results:
        print(
            f"{r['backend']}: turn1={r['score_turn1']['hit']}/{r['score_turn1']['total']} "
            f"turn5={r['score_turn5']['hit']}/{r['score_turn5']['total']} degrade={r['degrade']}"
        )
    print(f"\nDetail JSON: {out_json}")
    print(f"Dataset scale: repeat_main={repeat_main}, repeat_contact={repeat_contact}")


if __name__ == "__main__":
    main()
