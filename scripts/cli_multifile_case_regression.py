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


FACT_KEYS = [
    "tender_id",
    "deadline",
    "bid_bond_cny",
    "mandatory_cert",
    "pm_name",
    "liquidated_damage",
    "data_residency",
    "sla_p1",
    "cluster_nodes",
    "payment_terms",
]


BASE_FACTS = {
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


CONFLICT_FACTS = {
    "tender_id": "TB-2026-179",
    "deadline": "2026-06-20 17:00 CST",
    "bid_bond_cny": "1800000",
    "mandatory_cert": "ISO27001,CMMI5",
    "pm_name": "Bob Lin",
    "liquidated_damage": "0.3% per day, cap 8%",
    "data_residency": "Shanghai region only",
    "sla_p1": "response 10min, fix 2h",
    "cluster_nodes": "256",
    "payment_terms": "20/70/10",
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
        out[k.strip()] = v.strip().strip("'").strip('"')
    return out


def one_line(s: str) -> str:
    return re.sub(r"\s+", " ", s).strip()


def noisy_lines(tag: str, n: int) -> str:
    unit = "背景条目：流程、职责、历史说明，非关键字段。"
    return "\n".join(f"{tag}-{i+1}: {unit}" for i in range(n))


@dataclass
class CaseSpec:
    name: str
    docs: List[Tuple[str, str]]
    expected: Dict[str, str]
    hint: str
    turn2_prompt: str = ""


def case_late_facts(noise: int) -> CaseSpec:
    docs = [
        ("01_overview.md", noisy_lines("OV", noise) + f"\nCRITICAL:tender_id={BASE_FACTS['tender_id']}\nCRITICAL:deadline={BASE_FACTS['deadline']}\n"),
        ("02_commercial.md", noisy_lines("CM", noise) + f"\nCRITICAL:bid_bond_cny={BASE_FACTS['bid_bond_cny']}\nCRITICAL:payment_terms={BASE_FACTS['payment_terms']}\n"),
        ("03_compliance.md", noisy_lines("CP", noise) + f"\nCRITICAL:mandatory_cert={BASE_FACTS['mandatory_cert']}\nCRITICAL:data_residency={BASE_FACTS['data_residency']}\n"),
        ("04_delivery.md", noisy_lines("DL", noise) + f"\nCRITICAL:sla_p1={BASE_FACTS['sla_p1']}\nCRITICAL:cluster_nodes={BASE_FACTS['cluster_nodes']}\n"),
        ("05_legal.md", noisy_lines("LG", noise) + f"\nCRITICAL:liquidated_damage={BASE_FACTS['liquidated_damage']}\n"),
        ("06_contacts.md", noisy_lines("CT", max(20, noise // 2)) + f"\nCRITICAL:pm_name={BASE_FACTS['pm_name']}\n"),
    ]
    return CaseSpec(
        name="late_facts",
        docs=docs,
        expected=BASE_FACTS.copy(),
        hint="关键字段出现在文件末尾。",
    )


def case_conflict_override(noise: int) -> CaseSpec:
    old = BASE_FACTS
    new = CONFLICT_FACTS
    docs = [
        (
            "01_overview_revision.md",
            noisy_lines("OVR", noise)
            + f"\nHISTORY:tender_id={old['tender_id']}\nFINAL_OVERRIDE:tender_id={new['tender_id']}\n"
            + f"HISTORY:deadline={old['deadline']}\nFINAL_OVERRIDE:deadline={new['deadline']}\n",
        ),
        (
            "02_finance_revision.md",
            noisy_lines("FNR", noise)
            + f"\nHISTORY:bid_bond_cny={old['bid_bond_cny']}\nFINAL_OVERRIDE:bid_bond_cny={new['bid_bond_cny']}\n"
            + f"HISTORY:payment_terms={old['payment_terms']}\nFINAL_OVERRIDE:payment_terms={new['payment_terms']}\n",
        ),
        (
            "03_compliance_revision.md",
            noisy_lines("CPR", noise)
            + f"\nHISTORY:mandatory_cert={old['mandatory_cert']}\nFINAL_OVERRIDE:mandatory_cert={new['mandatory_cert']}\n"
            + f"HISTORY:data_residency={old['data_residency']}\nFINAL_OVERRIDE:data_residency={new['data_residency']}\n",
        ),
        (
            "04_delivery_revision.md",
            noisy_lines("DLR", noise)
            + f"\nHISTORY:sla_p1={old['sla_p1']}\nFINAL_OVERRIDE:sla_p1={new['sla_p1']}\n"
            + f"HISTORY:cluster_nodes={old['cluster_nodes']}\nFINAL_OVERRIDE:cluster_nodes={new['cluster_nodes']}\n",
        ),
        (
            "05_legal_revision.md",
            noisy_lines("LGR", noise)
            + f"\nHISTORY:liquidated_damage={old['liquidated_damage']}\nFINAL_OVERRIDE:liquidated_damage={new['liquidated_damage']}\n",
        ),
        (
            "06_contacts_revision.md",
            noisy_lines("CTR", max(20, noise // 2))
            + f"\nHISTORY:pm_name={old['pm_name']}\nFINAL_OVERRIDE:pm_name={new['pm_name']}\n",
        ),
    ]
    return CaseSpec(
        name="conflict_override",
        docs=docs,
        expected=new.copy(),
        hint="同字段存在旧值/新值，必须以 FINAL_OVERRIDE 为准。",
    )


def case_many_files(noise: int) -> CaseSpec:
    facts = BASE_FACTS.copy()
    docs = []
    for i in range(1, 11):
        docs.append((f"{i:02d}_annex_{i}.md", noisy_lines(f"ANN{i}", noise // 2) + f"\nDECOY:value={i*111}\n"))
    docs.append(
        ("11_main_facts_a.md", noisy_lines("MFA", noise) + f"\nCRITICAL:tender_id={facts['tender_id']}\nCRITICAL:deadline={facts['deadline']}\nCRITICAL:pm_name={facts['pm_name']}\n")
    )
    docs.append(
        (
            "12_main_facts_b.md",
            noisy_lines("MFB", noise)
            + f"\nCRITICAL:bid_bond_cny={facts['bid_bond_cny']}\nCRITICAL:mandatory_cert={facts['mandatory_cert']}\nCRITICAL:liquidated_damage={facts['liquidated_damage']}\n"
            + f"CRITICAL:data_residency={facts['data_residency']}\nCRITICAL:sla_p1={facts['sla_p1']}\nCRITICAL:cluster_nodes={facts['cluster_nodes']}\nCRITICAL:payment_terms={facts['payment_terms']}\n",
        )
    )
    return CaseSpec(
        name="many_files",
        docs=docs,
        expected=facts,
        hint="文件数量增加且有大量干扰附件。",
    )


def case_near_miss_numbers(noise: int) -> CaseSpec:
    facts = BASE_FACTS.copy()
    docs = [
        (
            "01_budget_notes.md",
            noisy_lines("BN", noise)
            + "\n历史版本写法: bid_bond_cny=120000 (作废)\n"
            + "\n预算草案写法: bid_bond_cny=12000000 (作废)\n"
            + f"\n最终生效: bid_bond_cny={facts['bid_bond_cny']}\n",
        ),
        (
            "02_terms_notes.md",
            noisy_lines("TN", noise)
            + "\n草案: payment_terms=30/50/20 (作废)\n"
            + f"\n最终生效: payment_terms={facts['payment_terms']}\n",
        ),
        (
            "03_cluster_notes.md",
            noisy_lines("CN", noise)
            + "\n候选: cluster_nodes=28 (作废)\n"
            + "\n候选: cluster_nodes=182 (作废)\n"
            + f"\n最终生效: cluster_nodes={facts['cluster_nodes']}\n",
        ),
        (
            "04_deadline_notes.md",
            noisy_lines("DN", noise)
            + "\n旧版: deadline=2026-05-03 17:00 CST (作废)\n"
            + "\n旧版: deadline=2026-05-30 07:00 CST (作废)\n"
            + f"\n最终生效: deadline={facts['deadline']}\n",
        ),
        (
            "05_misc.md",
            noisy_lines("MS", noise)
            + f"\n最终生效: tender_id={facts['tender_id']}\n"
            + f"最终生效: mandatory_cert={facts['mandatory_cert']}\n"
            + f"最终生效: pm_name={facts['pm_name']}\n"
            + f"最终生效: liquidated_damage={facts['liquidated_damage']}\n"
            + f"最终生效: data_residency={facts['data_residency']}\n"
            + f"最终生效: sla_p1={facts['sla_p1']}\n",
        ),
    ]
    return CaseSpec(
        name="near_miss_numbers",
        docs=docs,
        expected=facts,
        hint="同字段存在近似数字干扰，必须按“最终生效”值提取。",
    )


def case_irrelevant_interleave(noise: int) -> CaseSpec:
    docs = case_late_facts(noise).docs
    return CaseSpec(
        name="irrelevant_interleave",
        docs=docs,
        expected=BASE_FACTS.copy(),
        hint="关键字段仍以文件原文为准。",
        turn2_prompt=(
            "现在暂时忽略投标文件，请写一篇不少于1200字的说明文，主题是“离岸风电运维体系演进”，"
            "要求分五节并给出每节示例。"
        ),
    )


def build_cases(noise: int, names: List[str]) -> List[CaseSpec]:
    factory = {
        "late_facts": case_late_facts,
        "conflict_override": case_conflict_override,
        "many_files": case_many_files,
        "near_miss_numbers": case_near_miss_numbers,
        "irrelevant_interleave": case_irrelevant_interleave,
    }
    out: List[CaseSpec] = []
    for n in names:
        fn = factory.get(n)
        if fn:
            out.append(fn(noise))
    return out


def write_docs(base_dir: str, docs: List[Tuple[str, str]]) -> List[str]:
    root = Path(base_dir)
    root.mkdir(parents=True, exist_ok=True)
    paths = []
    for name, content in docs:
        p = root / name
        p.write_text(content, encoding="utf-8")
        paths.append(str(p))
    return paths


def build_turns(case: CaseSpec, paths: List[str]) -> List[Tuple[str, str]]:
    file_list = " ".join(paths)
    turn1 = (
        "读取以下文件并提取字段，仅输出JSON。"
        "字段名必须为:tender_id,deadline,bid_bond_cny,mandatory_cert,pm_name,liquidated_damage,data_residency,sla_p1,cluster_nodes,payment_terms。"
        f"规则:{case.hint} 文件:{file_list}"
    )
    turn2 = case.turn2_prompt or "继续基于同一批文件，写详细评审（不少于1200字），包含技术、商务、法务、交付四类风险与缓释建议。"
    turn3 = "现在不要解释，再次输出同样10个字段JSON，值必须与原文规则一致。"
    return [("turn1_extract", one_line(turn1)), ("turn2_review", one_line(turn2)), ("turn3_recall", one_line(turn3))]


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


def score_answer(answer: str, expected: Dict[str, str]) -> Dict:
    parsed = parse_first_json(answer)
    checks = {}
    answer_low = answer.lower()
    for k in FACT_KEYS:
        exp = str(expected.get(k, "")).strip().lower()
        got = str(parsed.get(k, "")).strip().lower()
        checks[k] = (exp != "" and got == exp) or (exp != "" and exp in answer_low)
    hit = sum(1 for v in checks.values() if v)
    return {"hit": hit, "total": len(FACT_KEYS), "checks": checks, "parsed": parsed}


@dataclass
class CmdResult:
    rc: int
    out: str
    err: str
    elapsed_s: float


def run_cmd(cmd: List[str], env: Dict[str, str], input_text: str = "", timeout_s: int = 240) -> CmdResult:
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
        return CmdResult(proc.returncode, (proc.stdout or "").strip(), (proc.stderr or "").strip(), round(time.time() - t0, 2))
    except subprocess.TimeoutExpired as e:
        out = (e.stdout or "").strip() if isinstance(e.stdout, str) else ""
        err = (e.stderr or "").strip() if isinstance(e.stderr, str) else "timeout"
        return CmdResult(124, out, err, round(time.time() - t0, 2))


def extract_opencode_text(stream: str) -> str:
    parts: List[str] = []
    for line in (stream or "").splitlines():
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            obj = json.loads(line)
        except Exception:
            continue
        typ = obj.get("type")
        part = obj.get("part") or {}
        if typ == "text" and isinstance(part.get("text"), str):
            parts.append(part["text"])
    return "\n".join(parts).strip()


def run_case_tacli(case: CaseSpec, env: Dict[str, str], timeout_s: int, noise: int) -> Dict:
    workdir = f"/tmp/tacli-case-{case.name}-{uuid.uuid4().hex[:8]}"
    paths = write_docs(workdir, case.docs)
    turns = build_turns(case, paths)
    session = f"case-{case.name}-{uuid.uuid4().hex[:6]}"
    base_url = env.get("OPENAI_API_BASE_URL", "https://api.qingyuntop.top/v1")
    api_key = env.get("OPENAI_API_KEY", "")
    model = env.get("DEFAULT_LLM_MODEL", "qwen3.5-27b")
    out = {"tool": "tacli", "case": case.name, "workdir": workdir, "turns": {}}
    for tid, prompt in turns:
        cmd = ["tacli", "chat", "--session", session, "--dangerously", "--base-url", base_url, "--api-key", api_key, "--model", model, "--workdir", workdir]
        r = run_cmd(cmd, env, input_text=prompt + "\n", timeout_s=timeout_s)
        out["turns"][tid] = {"rc": r.rc, "elapsed_s": r.elapsed_s, "answer": r.out, "stderr": r.err[-800:]}
        print(f"[tacli][{case.name}] {tid} rc={r.rc} elapsed={r.elapsed_s}s chars={len(r.out)}", flush=True)
    return out


def run_case_openclaw(case: CaseSpec, env: Dict[str, str], timeout_s: int, noise: int) -> Dict:
    workdir = f"/root/.openclaw/workspace/case-{case.name}-{uuid.uuid4().hex[:8]}"
    paths = write_docs(workdir, case.docs)
    turns = build_turns(case, paths)
    session = str(uuid.uuid4())
    out = {"tool": "openclaw", "case": case.name, "workdir": workdir, "turns": {}}
    for tid, prompt in turns:
        cmd = ["openclaw", "agent", "--local", "--session-id", session, "--message", prompt]
        r = run_cmd(cmd, env, timeout_s=timeout_s)
        out["turns"][tid] = {"rc": r.rc, "elapsed_s": r.elapsed_s, "answer": r.out, "stderr": r.err[-800:]}
        print(f"[openclaw][{case.name}] {tid} rc={r.rc} elapsed={r.elapsed_s}s chars={len(r.out)}", flush=True)
    return out


def run_case_opencode(case: CaseSpec, env: Dict[str, str], timeout_s: int, noise: int) -> Dict:
    workdir = f"/tmp/opencode-case-{case.name}-{uuid.uuid4().hex[:8]}"
    paths = write_docs(workdir, case.docs)
    turns = build_turns(case, paths)
    model = env.get("OPENCODE_MODEL", "opencode/minimax-m2.5-free")
    out = {"tool": "opencode", "case": case.name, "workdir": workdir, "turns": {}}
    for i, (tid, prompt) in enumerate(turns):
        cmd = ["opencode", "run", "--dangerously-skip-permissions", "--format", "json", "--dir", workdir, "-m", model]
        if i > 0:
            cmd.append("--continue")
        cmd.append(prompt)
        r = run_cmd(cmd, env, timeout_s=timeout_s)
        ans = extract_opencode_text(r.out)
        out["turns"][tid] = {"rc": r.rc, "elapsed_s": r.elapsed_s, "answer": ans, "stderr": r.err[-800:]}
        print(f"[opencode][{case.name}] {tid} rc={r.rc} elapsed={r.elapsed_s}s chars={len(ans)}", flush=True)
    return out


def finalize_case_result(data: Dict, expected: Dict[str, str]) -> None:
    s1 = score_answer(data["turns"].get("turn1_extract", {}).get("answer", ""), expected)
    s3 = score_answer(data["turns"].get("turn3_recall", {}).get("answer", ""), expected)
    data["score_turn1"] = s1
    data["score_turn3"] = s3
    data["degrade"] = s1["hit"] - s3["hit"]
    data["total_elapsed_s"] = round(sum(v.get("elapsed_s", 0) for v in data.get("turns", {}).values()), 2)


def main() -> None:
    dotenv = load_dotenv("/root/.env")
    env = os.environ.copy()
    env.update(dotenv)

    targets = [x.strip().lower() for x in os.environ.get("CLI_CASE_TARGETS", "tacli,openclaw,opencode").split(",") if x.strip()]
    case_names = [x.strip().lower() for x in os.environ.get("CLI_CASES", "late_facts,conflict_override,many_files").split(",") if x.strip()]
    timeout_s = int(os.environ.get("CLI_CASE_TIMEOUT_S", "240"))
    noise = int(os.environ.get("CLI_CASE_NOISE_REPEAT", "90"))
    out_path = os.environ.get("CLI_CASE_REGRESSION_JSON", "/tmp/cli_case_regression.json")

    cases = build_cases(noise=noise, names=case_names)
    results: List[Dict] = []

    for case in cases:
        print(f"\n=== Case: {case.name} ===", flush=True)
        for t in targets:
            if t == "tacli":
                r = run_case_tacli(case, env, timeout_s, noise)
            elif t == "openclaw":
                r = run_case_openclaw(case, env, timeout_s, noise)
            elif t == "opencode":
                r = run_case_opencode(case, env, timeout_s, noise)
            else:
                print(f"[skip] unknown target={t}", flush=True)
                continue
            finalize_case_result(r, case.expected)
            results.append(r)
            print(
                f"[score][{r['tool']}][{case.name}] "
                f"turn1={r['score_turn1']['hit']}/{r['score_turn1']['total']} "
                f"turn3={r['score_turn3']['hit']}/{r['score_turn3']['total']} "
                f"degrade={r['degrade']} total={r['total_elapsed_s']}s",
                flush=True,
            )

    Path(out_path).write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding="utf-8")
    print(f"\nSaved: {out_path}", flush=True)

    print("\nSummary:", flush=True)
    for r in results:
        print(
            f"  {r['tool']:<9} {r['case']:<17} "
            f"turn1={r['score_turn1']['hit']}/{r['score_turn1']['total']} "
            f"turn3={r['score_turn3']['hit']}/{r['score_turn3']['total']} "
            f"degrade={r['degrade']} total={r['total_elapsed_s']}s",
            flush=True,
        )


if __name__ == "__main__":
    main()
