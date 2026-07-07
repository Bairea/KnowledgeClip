import requests
import time
import json
import sys
import os
import subprocess

API_BASE = "http://localhost:8080/api"


def get_enabled_site_count():
    try:
        resp = requests.get(f"{API_BASE}/sites", timeout=10)
        if resp.status_code == 200:
            sites = resp.json()
            return len([s for s in sites if s.get("enabled")])
    except Exception:
        pass
    return 7


def wait_for_completion(session_id, expected_count, turn, timeout=200):
    start = time.time()
    seen = set()
    while time.time() - start < timeout:
        try:
            resp = requests.get(f"{API_BASE}/sessions/{session_id}/messages", timeout=10)
            if resp.status_code == 200:
                messages = resp.json()
                turn_msgs = {}
                for msg in messages:
                    if msg.get("turn", 0) != turn:
                        continue
                    key = msg.get("site_id", "")
                    turn_msgs[key] = {
                        "content_len": len(msg.get("content") or ""),
                        "error": msg.get("error") or "",
                        "site": msg.get("site_id"),
                    }
                new_keys = set(turn_msgs.keys()) - seen
                if new_keys:
                    for k in sorted(new_keys):
                        m = turn_msgs[k]
                        status = "ERROR" if m["error"] else f"{m['content_len']} chars"
                        print(f"    [{m['site']:<12}] {status}")
                        if m["error"]:
                            print(f"      -> {m['error'][:80]}")
                    seen.update(new_keys)
                done = len(turn_msgs) >= expected_count and all(
                    m["content_len"] > 0 or m["error"]
                    for m in turn_msgs.values()
                )
                if done:
                    print(f"  All {len(turn_msgs)} sites completed")
                    return True
        except Exception as e:
            print(f"  Poll error: {e}")
        time.sleep(3)
    print(f"  Timeout after {timeout}s ({len(seen)}/{expected_count} sites)")
    return False


def run_test(prompts):
    print(f"\n{'='*60}")
    print(f"Multi-turn conversation test: {len(prompts)} turns")
    print(f"{'='*60}")

    expected_count = get_enabled_site_count()
    print(f"Expected sites per turn: {expected_count}")

    session_id = None
    for i, prompt in enumerate(prompts):
        turn = i + 1
        print(f"\nTurn {turn}: {prompt}")

        payload = {"prompt": prompt, "turn": turn}
        if session_id:
            payload["session_id"] = session_id

        try:
            resp = requests.post(
                f"{API_BASE}/chat", json=payload, timeout=10
            )
            if resp.status_code != 200:
                print(f"  API error: {resp.status_code} {resp.text[:200]}")
                return None
            data = resp.json()
            session_id = data.get("session_id")
            print(f"  Session: {session_id}")
        except Exception as e:
            print(f"  Request failed: {e}")
            return None

        print(f"  Waiting for responses...")
        wait_for_completion(session_id, expected_count, turn, timeout=200)

    return session_id


def export_and_verify(session_id, verify_markdown=False, use_llm_quality=False):
    if not session_id:
        print("\nNo session to verify")
        return

    print(f"\n{'='*60}")
    print("Exporting session...")
    print(f"{'='*60}")

    result = subprocess.run(
        [sys.executable, "export_session.py", "export", session_id],
        capture_output=True, text=True, cwd=os.path.dirname(os.path.abspath(__file__))
    )
    print(result.stdout)
    if result.stderr:
        print(result.stderr[:200])

    files = [f for f in os.listdir(".") if f.startswith(f"session_{session_id[:8]}")]
    if not files:
        print("No export file found!")
        return

    export_file = sorted(files)[-1]
    print(f"\n{'='*60}")
    print(f"Running LLM verification on {export_file}...")
    print(f"{'='*60}")

    cmd = [sys.executable, "check_chat_mismatch.py", export_file]
    if verify_markdown:
        cmd.append("--markdown")
    if use_llm_quality:
        cmd.append("--quality")
    subprocess.run(cmd, cwd=os.path.dirname(os.path.abspath(__file__)))



# Test prompts designed to stress content extraction across turns.
# Each prompt asks for specific markdown formats so the LLM quality checker
# can judge whether extraction preserved them. Keywords like 代码/表格/mermaid/
# 引用/列表 are picked up by check_chat_mismatch.infer_expected_formats.

DEFAULT_MARKDOWN_PROMPT = (
    "请用标准 Markdown 格式回答，必须同时包含：\n"
    "1. 一个 ### 开头的三级标题\n"
    "2. 一段 Python 代码块（包含 def 或 import）\n"
    "3. 一个三行以上的表格\n"
    "4. 一段 mermaid 流程图代码块\n"
    "5. 一段引用（> 开头）\n"
    "6. 一个有序或无序列表\n"
    "主题：简述如何设计一个高并发 Web 后端。"
)

# Turn 2 — code + table only (tests code-block language tag & table extraction)
PROMPT_CODE_TABLE = (
    "请用 Markdown 回答，要求包含代码和表格：\n"
    "- 一段 JavaScript 代码块（带 ```js 语言标签），演示 Promise.all 的用法\n"
    "- 一个对比表格，列出 Promise.all / Promise.race / Promise.allSettled 的区别\n"
    "请确保代码块有开头和结尾的三反引号。"
)

# Turn 3 — mermaid + blockquote (tests mermaid fence & quote extraction)
PROMPT_MERMAID_QUOTE = (
    "请用 Markdown 回答，要求包含 mermaid 流程图和引用：\n"
    "- 一段 mermaid 流程图代码块，描绘 HTTP 请求从浏览器到服务端的流转\n"
    "- 引用一段关于 RESTful 设计原则的名言（> 开头）\n"
    "mermaid 代码必须包裹在 ```mermaid 和 ``` 之间。"
)

# Turn 4 — all formats combined, longer content (tests stability detection)
PROMPT_FULL_COMBO = (
    "请用标准 Markdown 格式做一份技术简报，主题是「SQLite WAL 模式原理」。\n"
    "必须包含：三级标题、Python 或 Go 代码块、至少四行的数据表格、mermaid 流程图、"
    "引用块、有序列表和无序列表各一个。内容请尽量完整，不少于 400 字。"
)

# Turn 5 — short follow-up (tests multi-turn beforeCount: must not return turn 4)
PROMPT_FOLLOW_UP = (
    "基于上一条回答，用一句话总结 WAL 模式最大的优点是什么？"
)


if __name__ == "__main__":
    verify_md = "--markdown" in sys.argv
    if verify_md:
        sys.argv.remove("--markdown")
    use_quality = "--quality" in sys.argv
    if use_quality:
        sys.argv.remove("--quality")

    if len(sys.argv) > 1:
        prompts = sys.argv[1:]
    else:
        prompts = [
            "你是什么模型？请简要介绍自己。",
            "3+1 等于多少？请只给出答案和一句话解释。",
            DEFAULT_MARKDOWN_PROMPT,
            PROMPT_CODE_TABLE,
            PROMPT_MERMAID_QUOTE,
            PROMPT_FULL_COMBO,
            PROMPT_FOLLOW_UP,
        ]

    session_id = run_test(prompts)
    export_and_verify(session_id, verify_markdown=verify_md, use_llm_quality=use_quality)
