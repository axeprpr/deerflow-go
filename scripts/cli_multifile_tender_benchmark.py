#!/usr/bin/env python3
import json
import os
import re
import subprocess
import time
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, List, Tuple


FACTS = {
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


def load_dotenv(path: str) -> Dict[str, str]:
    out: Dict[str, str] = {}
    p = Path(path)
    if not p.exists():
        return out
    for raw in p.read_text(encoding="utf-8", errors="ignore").splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        k, v = line.split("=", 1)
        k = k.strip()
        v = v.strip().strip("'").strip('"')
        out[k] = v
    return out


def one_line(text: str) -> str:
    return re.sub(r"\s+", " ", text).strip()


def noisy_block(title: str, repeat: int) -> str:
    unit = (
        "本段为背景叙述，包含非关键历史材料、流程描述、资质说明、术语解释，"
        "用于增加上下文长度但不影响关键字段。"
    )
    return "\n".join(f"{title}-{i+1}:{unit}" for i in range(repeat))


def build_docs(repeat_main: int, repeat_contact: int) -> List[Tuple[str, str]]:
    docs = [
        (
            "01_rfp_overview.md",
            f"{noisy_block('RFP', repeat_main)} CRITICAL_FACT:tender_id={FACTS['tender_id']}; "
            f"CRITICAL_FACT:deadline={FACTS['deadline']};",
        ),
        (
            "02_commercial_terms.md",
            f"{noisy_block('COMMERCIAL', repeat_main)} CRITICAL_FACT:bid_bond_cny={FACTS['bid_bond_cny']}; "
            f"CRITICAL_FACT:payment_terms={FACTS['payment_terms']};",
        ),
        (
            "03_compliance_requirements.md",
            f"{noisy_block('COMPLIANCE', repeat_main)} CRITICAL_FACT:mandatory_cert={FACTS['mandatory_cert']}; "
            f"CRITICAL_FACT:data_residency={FACTS['data_residency']};",
        ),
        (
            "04_delivery_sla.md",
            f"{noisy_block('DELIVERY', repeat_main)} CRITICAL_FACT:sla_p1={FACTS['sla_p1']}; "
            f"CRITICAL_FACT:cluster_nodes={FACTS['cluster_nodes']};",
        ),
        (
            "05_risk_legal.md",
            f"{noisy_block('LEGAL', repeat_main)} CRITICAL_FACT:liquidated_damage={FACTS['liquidated_damage']};",
        ),
        (
            "06_contacts.md",
            f"{noisy_block('CONTACT', repeat_contact)} CRITICAL_FACT:pm_name={FACTS['pm_name']};",
        ),
    ]
    return docs


def materialize_docs(base_dir: str, repeat_main: int, repeat_contact: int) -> List[str]:
    docs = build_docs(repeat_main=repeat_main, repeat_contact=repeat_contact)
    root = Path(base_dir)
    root.mkdir(parents=True, exist_ok=True)
    out: List[str] = []
    for name, content in docs:
        p = root / name
        p.write_text(content, encoding="utf-8")
        out.append(str(p))
    return out


def build_turns_from_paths(paths: List[str]) -> List[Tuple[str, str]]:
    files_line = " ".join(paths)
    turn1 = (
        "请读取以下6个文件并严格从原文提取关键字段，只输出JSON，不要解释。"
        "字段名必须是:tender_id,deadline,bid_bond_cny,mandatory_cert,pm_name,"
        "liquidated_damage,data_residency,sla_p1,cluster_nodes,payment_terms。"
        f"文件路径:{files_line}"
    )
    return [
        ("turn1_extract", one_line(turn1)),
        ("turn2_risk_matrix", one_line("基于这些文件内容，按技术/商务/法务/交付四类输出详细风险矩阵，要求不少于1200字。")),
        ("turn3_long_review", one_line("继续基于同一批文件，给出不少于1500字的综合评审，包含争议点、取舍依据、缓释方案、合同建议。")),
        ("turn4_milestone", one_line("继续基于同一批文件，输出实施季度里程碑与资源计划，写成详细行动表。")),
        (
            "turn5_recall",
            one_line(
                "现在不要解释，直接再次输出最初10个关键字段JSON，字段名保持一致，值必须与原文完全一致。"
            ),
        ),
    ]


def parse_first_json(text: str) -> Dict[str, str]:
    if not text:
        return {}
    text = text.strip()
    if text.startswith("{") and text.endswith("}"):
        try:
            obj = json.loads(text)
            if isinstance(obj, dict):
                return {str(k): str(v) for k, v in obj.items()}
        except Exception:
            pass
    m = re.search(r"\{.*\}", text, flags=re.DOTALL)
    if not m:
        return {}
    try:
        obj = json.loads(m.group(0))
        if isinstance(obj, dict):
            return {str(k): str(v) for k, v in obj.items()}
    except Exception:
        return {}
    return {}


def score(answer: str) -> Dict:
    parsed = parse_first_json(answer)
    low = answer.lower()

    def getv(key: str) -> str:
        return str(parsed.get(key, "")).strip().lower()

    checks = {
        "tender_id": getv("tender_id") == FACTS["tender_id"].lower() or FACTS["tender_id"].lower() in low,
        "deadline": getv("deadline") == FACTS["deadline"].lower() or FACTS["deadline"].lower() in low,
        "bid_bond_cny": getv("bid_bond_cny") == FACTS["bid_bond_cny"].lower() or "1200000" in low,
        "mandatory_cert": ("iso27001" in getv("mandatory_cert") and "cmmi3" in getv("mandatory_cert"))
        or ("iso27001" in low and "cmmi3" in low),
        "pm_name": getv("pm_name") == FACTS["pm_name"].lower() or FACTS["pm_name"].lower() in low,
        "liquidated_damage": ("0.5%" in getv("liquidated_damage") and "10%" in getv("liquidated_damage"))
        or ("0.5%" in low and "10%" in low),
        "data_residency": ("beijing" in getv("data_residency")) or ("beijing" in low),
        "sla_p1": (("15min" in getv("sla_p1") or "15 min" in getv("sla_p1")) and ("4h" in getv("sla_p1") or "4 h" in getv("sla_p1")))
        or (("15min" in low or "15 min" in low) and ("4h" in low or "4 h" in low)),
        "cluster_nodes": getv("cluster_nodes") == FACTS["cluster_nodes"] or re.search(r"\b128\b", low) is not None,
        "payment_terms": getv("payment_terms") == FACTS["payment_terms"].lower() or "30/60/10" in low,
    }
    hit = sum(1 for v in checks.values() if v)
    return {"hit": hit, "total": len(checks), "checks": checks, "parsed": parsed}


@dataclass
class TurnResult:
    text: str
    elapsed_s: float
    rc: int
    stderr: str


def run_cmd(cmd: List[str], env: Dict[str, str], input_text: str = "", timeout_s: int = 420) -> TurnResult:
    t0 = time.time()
    try:
        proc = subprocess.run(
            cmd,
            input=input_text,
            text=True,
            encoding="utf-8",
            errors="replace",
            capture_output=True,
            env=env,
            timeout=timeout_s,
        )
        return TurnResult(
            text=(proc.stdout or "").strip(),
            elapsed_s=round(time.time() - t0, 2),
            rc=proc.returncode,
            stderr=(proc.stderr or "").strip(),
        )
    except subprocess.TimeoutExpired as e:
        stdout = (e.stdout or "").strip() if isinstance(e.stdout, str) else ""
        stderr = (e.stderr or "").strip() if isinstance(e.stderr, str) else "timeout"
        return TurnResult(
            text=stdout,
            elapsed_s=round(time.time() - t0, 2),
            rc=124,
            stderr=stderr,
        )


def run_tacli(repeat_main: int, repeat_contact: int, env: Dict[str, str], timeout_s: int) -> Dict:
    session = f"bench-{uuid.uuid4().hex[:10]}"
    workdir = f"/tmp/tacli-bench-{uuid.uuid4().hex[:8]}"
    paths = materialize_docs(workdir, repeat_main=repeat_main, repeat_contact=repeat_contact)
    turns = build_turns_from_paths(paths)
    base_url = env.get("OPENAI_API_BASE_URL", "https://api.qingyuntop.top/v1")
    api_key = env.get("OPENAI_API_KEY", "")
    model = env.get("DEFAULT_LLM_MODEL", "qwen3.5-27b")
    result = {"tool": "tacli", "session": session, "model": model, "workdir": workdir, "turns": {}}
    for turn_id, prompt in turns:
        cmd = [
            "tacli",
            "chat",
            "--session",
            session,
            "--dangerously",
            "--base-url",
            base_url,
            "--api-key",
            api_key,
            "--model",
            model,
            "--workdir",
            workdir,
        ]
        tr = run_cmd(cmd, env=env, input_text=prompt + "\n", timeout_s=timeout_s)
        result["turns"][turn_id] = {
            "elapsed_s": tr.elapsed_s,
            "rc": tr.rc,
            "answer": tr.text,
            "stderr": tr.stderr[-1200:],
        }
        print(f"[tacli] {turn_id} rc={tr.rc} elapsed={tr.elapsed_s}s chars={len(tr.text)}", flush=True)
    return result


def run_openclaw(repeat_main: int, repeat_contact: int, env: Dict[str, str], timeout_s: int) -> Dict:
    session = str(uuid.uuid4())
    workdir = f"/root/.openclaw/workspace/bench-{uuid.uuid4().hex[:8]}"
    paths = materialize_docs(workdir, repeat_main=repeat_main, repeat_contact=repeat_contact)
    turns = build_turns_from_paths(paths)
    result = {"tool": "openclaw", "session": session, "workdir": workdir, "turns": {}}
    for turn_id, prompt in turns:
        cmd = [
            "openclaw",
            "agent",
            "--local",
            "--session-id",
            session,
            "--message",
            prompt,
        ]
        tr = run_cmd(cmd, env=env, timeout_s=timeout_s)
        result["turns"][turn_id] = {
            "elapsed_s": tr.elapsed_s,
            "rc": tr.rc,
            "answer": tr.text,
            "stderr": tr.stderr[-1200:],
        }
        print(f"[openclaw] {turn_id} rc={tr.rc} elapsed={tr.elapsed_s}s chars={len(tr.text)}", flush=True)
    return result


def extract_opencode_text(stdout: str) -> str:
    text_parts: List[str] = []
    for line in (stdout or "").splitlines():
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            obj = json.loads(line)
        except Exception:
            continue
        typ = obj.get("type")
        part = obj.get("part") or {}
        if typ == "text":
            v = part.get("text")
            if isinstance(v, str):
                text_parts.append(v)
        elif typ == "error":
            err = obj.get("error") or {}
            msg = err.get("data", {}).get("message") or err.get("name")
            if msg:
                text_parts.append(f"[ERROR]{msg}")
    return "\n".join(text_parts).strip()


def run_opencode(repeat_main: int, repeat_contact: int, env: Dict[str, str], timeout_s: int) -> Dict:
    model = os.environ.get("OPENCODE_MODEL", "opencode/minimax-m2.5-free")
    workdir = f"/tmp/opencode-bench-{uuid.uuid4().hex[:8]}"
    paths = materialize_docs(workdir, repeat_main=repeat_main, repeat_contact=repeat_contact)
    turns = build_turns_from_paths(paths)
    result = {"tool": "opencode", "model": model, "workdir": workdir, "turns": {}}

    for i, (turn_id, prompt) in enumerate(turns):
        cmd = [
            "opencode",
            "run",
            "--dangerously-skip-permissions",
            "--format",
            "json",
            "--dir",
            workdir,
            "-m",
            model,
        ]
        if i > 0:
            cmd.append("--continue")
        cmd.append(prompt)
        tr = run_cmd(cmd, env=env, timeout_s=timeout_s)
        answer = extract_opencode_text(tr.text)
        result["turns"][turn_id] = {
            "elapsed_s": tr.elapsed_s,
            "rc": tr.rc,
            "answer": answer,
            "stderr": tr.stderr[-1200:],
        }
        print(f"[opencode] {turn_id} rc={tr.rc} elapsed={tr.elapsed_s}s chars={len(answer)}", flush=True)
    return result


def attach_scores(data: Dict) -> None:
    t1 = data["turns"].get("turn1_extract", {}).get("answer", "")
    t5 = data["turns"].get("turn5_recall", {}).get("answer", "")
    s1 = score(t1)
    s5 = score(t5)
    data["score_turn1"] = s1
    data["score_turn5"] = s5
    data["degrade"] = s1["hit"] - s5["hit"]


def main() -> None:
    dotenv = load_dotenv("/root/.env")
    env = os.environ.copy()
    env.update(dotenv)

    repeat_main = int(os.environ.get("TENDER_REPEAT_MAIN", "80"))
    repeat_contact = int(os.environ.get("TENDER_REPEAT_CONTACT", "50"))
    timeout_s = int(os.environ.get("CLI_BENCH_TIMEOUT_S", "420"))
    targets = [t.strip().lower() for t in os.environ.get("CLI_BENCH_TARGETS", "tacli,openclaw,opencode").split(",") if t.strip()]
    out_path = os.environ.get("CLI_TENDER_BENCHMARK_JSON", "/tmp/cli_tender_benchmark.json")

    all_results: List[Dict] = []

    for t in targets:
        if t == "tacli":
            r = run_tacli(repeat_main, repeat_contact, env, timeout_s=timeout_s)
        elif t == "openclaw":
            r = run_openclaw(repeat_main, repeat_contact, env, timeout_s=timeout_s)
        elif t == "opencode":
            r = run_opencode(repeat_main, repeat_contact, env, timeout_s=timeout_s)
        else:
            print(f"[skip] unknown target={t}")
            continue
        attach_scores(r)
        all_results.append(r)
        print(
            f"[score] {r['tool']}: turn1={r['score_turn1']['hit']}/{r['score_turn1']['total']} "
            f"turn5={r['score_turn5']['hit']}/{r['score_turn5']['total']} degrade={r['degrade']}"
        , flush=True)

    Path(out_path).write_text(json.dumps(all_results, ensure_ascii=False, indent=2), encoding="utf-8")
    print(f"\nSaved: {out_path}")
    print("\nSummary:")
    for r in all_results:
        print(
            f"  {r['tool']:<10} turn1={r['score_turn1']['hit']}/{r['score_turn1']['total']} "
            f"turn5={r['score_turn5']['hit']}/{r['score_turn5']['total']} degrade={r['degrade']}"
        )


if __name__ == "__main__":
    main()
