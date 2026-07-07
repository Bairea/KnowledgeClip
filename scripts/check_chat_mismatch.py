import os
import sys
import json
import glob
import re
from openai import OpenAI


def get_client():
    api_key = os.environ.get("XF_API_KEY", "")
    base_url = os.environ.get("LLM_BASE_URL", "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2")
    if not api_key:
        raise RuntimeError("请设置环境变量 XF_API_KEY")
    return OpenAI(api_key=api_key, base_url=base_url)


MODEL = os.environ.get("LLM_MODEL", "xopqwen36v35b")


def check_match(prompt, answer, site_name):
    if not answer or len(answer.strip()) < 10:
        return False, "answer too short or empty"

    content_text = answer[:3000]

    user_msg = (
        f"用户向AI助手「{site_name}」提出了以下问题：\n\n"
        f"问题：{prompt}\n\n"
        f"AI助手的回答如下（前3000字符）：\n\n"
        f"{content_text}\n\n"
        f"请判断这个回答是否与问题对应（即回答是否在回答该问题，而不是答非所问或返回了上一个问题的回答）。"
        f"只返回一行，格式为：对应|不对应|无法判断，后面跟简短原因。"
    )

    try:
        client = get_client()
        response = client.chat.completions.create(
            model=MODEL,
            messages=[{"role": "user", "content": user_msg}],
            max_tokens=100,
        )
        reply = response.choices[0].message.content.strip()
        is_match = reply.startswith("对应") and not reply.startswith("不对应")
        return is_match, reply
    except Exception as e:
        return False, f"API error: {e}"


def check_markdown_quality(answer):
    result = {
        "has_code_block": bool(re.search(r'```[\s\S]*?```', answer)),
        "has_table": bool(re.search(r'\|[^\n]+\|\n\|[-:\s|]+\|', answer)),
        "has_list": bool(re.search(r'^(\s*[-*]|\s*\d+\.)', answer, re.M)),
        "has_blockquote": bool(re.search(r'^\s*>', answer, re.M)),
        "has_mermaid": bool(re.search(r'```mermaid[\s\S]*?```', answer)),
        "has_heading": bool(re.search(r'^#{1,6}\s', answer, re.M)),
    }
    return result


def detect_extraction_defects(prompt, answer, site_name):
    """Heuristic detection of common content-extraction defects.
    Returns a list of defect strings (empty = clean)."""
    defects = []
    a = answer.strip()
    p = prompt.strip()

    # 1. Answer begins with the user's question (question echo leakage)
    p_norm = re.sub(r'\s+', '', p).lower()
    a_norm = re.sub(r'\s+', '', a[:len(p) + 50]).lower()
    if p_norm and len(p_norm) > 15 and a_norm.startswith(p_norm[:min(60, len(p_norm))]):
        defects.append("question_echoed")

    # 2. Answer is suspiciously short for a non-trivial prompt
    if len(a) < 40 and not any(k in p for k in ["yes", "no", "对", "错", "是", "否"]):
        defects.append("too_short")

    # 3. Code block opened but never closed (truncated extraction)
    fence_open = len(re.findall(r'```', a))
    if fence_open % 2 != 0:
        defects.append("unclosed_code_fence")

    # 4. Table row without closing pipe (malformed table)
    table_lines = [l for l in a.split('\n') if l.count('|') >= 2]
    if table_lines:
        bad_rows = [l for l in table_lines if not l.strip().endswith('|')]
        if len(bad_rows) > len(table_lines) // 2:
            defects.append("malformed_table")

    # 5. Contains raw HTML that looks like un-converted site chrome
    if re.search(r'<(button|svg|path|nav|header|footer)\b', a, re.I):
        defects.append("raw_html_leak")

    # 6. Contains UI label leaks (copy/download/code-lang on their own line)
    ui_leak = re.search(r'^\s*(复制|copy|下载|download|代码|python|javascript|java|go|rust|typescript|sql)\s*$', a, re.M | re.I)
    if ui_leak:
        defects.append("ui_label_leak")

    return defects


def check_quality_llm(prompt, answer, site_name, expected_formats):
    """LLM-based quality judgment: does the answer properly deliver the
    requested markdown formats and is it free of extraction artifacts?"""
    if not answer or len(answer.strip()) < 10:
        return "fail", "answer too short or empty"

    expected_str = "、".join(expected_formats) if expected_formats else "无特殊格式要求"
    content_text = answer[:4000]

    user_msg = (
        f"用户向AI助手「{site_name}」提问，期望回答包含以下Markdown格式：{expected_str}\n\n"
        f"用户问题：{prompt}\n\n"
        f"实际回答（前4000字符）：\n\n{content_text}\n\n"
        f"请从「内容提取质量」角度判定，只返回一行，格式为：合格|不合格|无法判断，后跟简短原因。\n"
        f"判定为不合格的情形包括：\n"
        f"- 缺少期望的格式（如要求代码块但没有代码块）\n"
        f"- 回答开头混入了用户的问题原文\n"
        f"- 代码块缺少语言标签或结尾三反引号\n"
        f"- 表格格式损坏\n"
        f"- 混入了网站的UI元素文本（如「复制」「copy」「下载」等单独成行）\n"
        f"- 内容明显被截断\n"
        f"判定为合格的情形：回答完整、格式正确、没有提取瑕疵。"
    )

    try:
        client = get_client()
        response = client.chat.completions.create(
            model=MODEL,
            messages=[{"role": "user", "content": user_msg}],
            max_tokens=150,
        )
        reply = response.choices[0].message.content.strip()
        if reply.startswith("合格") and not reply.startswith("不合格"):
            return "pass", reply
        if reply.startswith("无法判断"):
            return "unknown", reply
        return "fail", reply
    except Exception as e:
        return "unknown", f"API error: {e}"


# Expected formats per prompt keyword — used to drive quality checks.
# Test prompts should include these keywords so the checker knows what to look for.
FORMAT_KEYWORDS = {
    "代码": "code_block",
    "code": "code_block",
    "表格": "table",
    "table": "table",
    "mermaid": "mermaid",
    "流程图": "mermaid",
    "引用": "blockquote",
    "blockquote": "blockquote",
    "列表": "list",
    "list": "list",
}


def infer_expected_formats(prompt):
    p = prompt.lower()
    found = []
    for kw, fmt in FORMAT_KEYWORDS.items():
        if kw in p and fmt not in found:
            found.append(fmt)
    return found


def run_check(export_path, verify_markdown=False, use_llm_quality=False):
    with open(export_path, "r", encoding="utf-8") as f:
        sessions = json.load(f)

    total = 0
    matched = 0
    mismatched = 0
    errors = 0
    quality_pass = 0
    quality_fail = 0
    results = []

    for sess in sessions:
        print(f"\n{'='*80}")
        print(f"Session: {sess['session_id'][:12]}...  Prompt: {sess['session_prompt'][:50]}")
        print(f"{'='*80}")

        for turn in sess["turns"]:
            expected = infer_expected_formats(turn["prompt"])
            print(f"\n  Turn {turn['turn']}: {turn['prompt'][:60]}")
            if expected:
                print(f"  expected formats: {', '.join(expected)}")
            print(f"  {'-'*70}")

            for ans in turn["answers"]:
                total += 1
                site = ans["site_name"]
                record = {
                    "session_id": sess["session_id"],
                    "turn": turn["turn"],
                    "prompt": turn["prompt"],
                    "site_id": ans["site_id"],
                    "site_name": site,
                    "content": ans["content"],
                    "error": ans.get("error", ""),
                    "elapsed_ms": ans.get("elapsed_ms", 0),
                    "expected_formats": expected,
                }

                if ans["error"]:
                    errors += 1
                    status = "ERROR"
                    detail = ans["error"][:80]
                    print(f"    [{site:<12}] {status}: {detail}")
                    record["status"] = "ERROR"
                    record["reason"] = detail
                    results.append(record)
                    continue

                if not ans["content"]:
                    errors += 1
                    print(f"    [{site:<12}] EMPTY: no content")
                    record["status"] = "EMPTY"
                    record["reason"] = "no content"
                    results.append(record)
                    continue

                is_match, reason = check_match(turn["prompt"], ans["content"], site)
                if is_match:
                    matched += 1
                    status = "OK"
                else:
                    mismatched += 1
                    status = "MISMATCH"

                defects = detect_extraction_defects(turn["prompt"], ans["content"], site)
                if defects:
                    status = status + "+DEFECT" if status == "OK" else status

                content_preview = ans["content"][:80].replace("\n", " ")
                print(f"    [{site:<12}] {status}: {content_preview}...")
                print(f"      match: {reason}")
                if defects:
                    print(f"      defects: {', '.join(defects)}")

                md = check_markdown_quality(ans["content"]) if verify_markdown else {}
                if verify_markdown:
                    present = [k.replace("has_", "") for k, v in md.items() if v]
                    print(f"      markdown elements: {', '.join(present) if present else 'none'}")

                q_status, q_reason = ("skip", "")
                if use_llm_quality and expected:
                    q_status, q_reason = check_quality_llm(
                        turn["prompt"], ans["content"], site, expected
                    )
                    if q_status == "pass":
                        quality_pass += 1
                    elif q_status == "fail":
                        quality_fail += 1
                    print(f"      quality: {q_status} - {q_reason}")

                record["status"] = status
                record["reason"] = reason
                record["defects"] = defects
                record["markdown"] = md
                record["quality"] = q_status
                record["quality_reason"] = q_reason
                results.append(record)

    print(f"\n{'='*80}")
    print(f"Summary: {total} total, {matched} matched, {mismatched} mismatched, {errors} errors")
    if use_llm_quality:
        print(f"Quality: {quality_pass} pass, {quality_fail} fail")
    print(f"{'='*80}")

    report_path = export_path.replace(".json", "_report.json")
    with open(report_path, "w", encoding="utf-8") as f:
        json.dump({
            "source": export_path,
            "summary": {
                "total": total,
                "matched": matched,
                "mismatched": mismatched,
                "errors": errors,
                "quality_pass": quality_pass,
                "quality_fail": quality_fail,
            },
            "results": results,
        }, f, ensure_ascii=False, indent=2)
    print(f"Report saved: {report_path}")


if __name__ == "__main__":
    verify_md = "--markdown" in sys.argv
    if verify_md:
        sys.argv.remove("--markdown")
    use_llm_q = "--quality" in sys.argv
    if use_llm_q:
        sys.argv.remove("--quality")

    if len(sys.argv) < 2:
        export_dir = os.path.dirname(os.path.abspath(__file__))
        files = sorted(glob.glob(os.path.join(export_dir, "session_*.json")) +
                       glob.glob(os.path.join(export_dir, "sessions_export_*.json")))
        if not files:
            print("No export files found. Run 'python export_session.py export' first.")
            sys.exit(1)
        export_path = files[-1]
        print(f"Using latest export: {export_path}")
    else:
        export_path = sys.argv[1]

    if not os.path.exists(export_path):
        print(f"File not found: {export_path}")
        sys.exit(1)

    run_check(export_path, verify_markdown=verify_md, use_llm_quality=use_llm_q)
