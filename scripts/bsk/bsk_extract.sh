#!/usr/bin/env bash
# bsk_extract.sh — 纯 bsk 调用完成单次内容摘取（无 LLM 参与）
#
# 一个理想的尝试：不用任何 LLM，仅用 browser-skill 的 bsk CLI 命令（会话、
# 开标签、evaluate DOM→Markdown）直接完成每次的内容摘取。脚本里没有任何
# 模型调用 —— bsk 驱动用户自己的 Chromium（含登录态），evaluate 在页面内
# 执行 dom_to_md.js 完成提取。
#
# 用法:
#   bsk_extract.sh <url> [--selector <css>] [--out <file>] [--timeout <sec>]
#
# 示例:
#   bsk_extract.sh "https://github.com/Bairea/KnowledgeClip"
#   bsk_extract.sh "https://chat.qwen.ai" --selector ".chat-answer" --out out/qwen.md
#
# 每次执行都是独立生命周期: session start → tab create → evaluate 轮询 →
# evaluate 提取 → session stop（即使失败也保证 stop）。

set -euo pipefail

URL="${1:?usage: bsk_extract.sh <url> [--selector <css>] [--out <file>] [--timeout <sec>]}"
shift

SELECTOR="article"
OUT=""
TIMEOUT=30
while [[ $# -gt 0 ]]; do
	case "$1" in
		--selector) SELECTOR="${2:?selector value required}"; shift 2 ;;
		--out) OUT="${2:?out path required}"; shift 2 ;;
		--timeout) TIMEOUT="${2:?timeout seconds required}"; shift 2 ;;
		*) echo "unknown arg: $1" >&2; exit 2 ;;
	esac
done

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SID=""

cleanup() {
	if [[ -n "$SID" ]]; then
		echo "[bsk-extract] stop session $SID" >&2
		bsk session stop "$SID" >/dev/null 2>&1 || true
	fi
}
trap cleanup EXIT

# --json 反序列化: 取 value 字段；CDP 对对象按字符串序列化，需二次解析。
decode_value() {
	python3 -c '
import json, sys
raw = json.load(sys.stdin)
val = raw.get("value")
if isinstance(val, str):
    try:
        val = json.loads(val)
    except Exception:
        pass
print(json.dumps(val, ensure_ascii=False))
'
}

bsk_eval() {
	# bsk_eval "<js expression>" → decoded JSON value on stdout
	local expr="$1"
	bsk evaluate "$expr" --session "$SID" --tab-id "$TAB" --json 2>/dev/null | decode_value
}

# 把 selector 安全嵌入 JS 字符串字面量。
SEL_JSON="$(python3 -c 'import json, sys; print(json.dumps(sys.argv[1]))' "$SELECTOR")"

START=$(date +%s)
echo "[bsk-extract] session start" >&2
SID="$(bsk session start --no-focus 2>/dev/null | tail -1)"
echo "[bsk-extract] session=$SID url=$URL selector=$SELECTOR" >&2
[ -n "$SID" ] || { echo "failed to start session" >&2; exit 1; }

echo "[bsk-extract] open tab: $URL" >&2
TAB="$(bsk tab create --session "$SID" --url "$URL" --json 2>/dev/null | python3 -c 'import json, sys; print(json.load(sys.stdin)["tab_id"])')"
echo "[bsk-extract] tab=$TAB" >&2
[ -n "$TAB" ] || { echo "failed to create tab" >&2; exit 1; }

# 轮询等待: 页面 complete 且 selector 命中（纯 bsk evaluate）。
echo "[bsk-extract] wait for selector (timeout ${TIMEOUT}s)..." >&2
WAIT_EXPR="document.readyState === 'complete' && !!document.querySelector(${SEL_JSON})"
DEADLINE=$(( $(date +%s) + TIMEOUT ))
FOUND=0
while [[ $(date +%s) -lt $DEADLINE ]]; do
	V="$(bsk_eval "$WAIT_EXPR" || true)"
	if [[ "$V" == "true" ]]; then FOUND=1; break; fi
	sleep 2
done
if [[ $FOUND -ne 1 ]]; then
	echo "[bsk-extract] FAIL: selector not found after ${TIMEOUT}s" >&2
	exit 1
fi
T_WAIT=$(( $(date +%s) - START ))

# 提取: evaluate 注入 dom_to_md.js 并调用 bskExtract(selector)。
echo "[bsk-extract] extract via evaluate (dom_to_md.js)..." >&2
EXPR="$(cat "$DIR/dom_to_md.js")

bskExtract(${SEL_JSON})"
RESULT="$(bsk_eval "$EXPR")"
T_EXTRACT=$(( $(date +%s) - START ))

OK="$(echo "$RESULT" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("ok"))' 2>/dev/null || echo '')"
if [[ "$OK" != "True" ]]; then
	echo "[bsk-extract] FAIL: extraction error, result=$RESULT" >&2
	exit 1
fi

TEXT="$(echo "$RESULT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["text"])')"
LEN="$(echo "$RESULT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["len"])')"
TITLE="$(echo "$RESULT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["title"])')"

if [[ -n "$OUT" ]]; then
	echo "$TEXT" > "$OUT"
fi

TOTAL=$(( $(date +%s) - START ))
echo "[bsk-extract] OK: len=${LEN} chars, wait=${T_WAIT}s, extract_ready=${T_EXTRACT}s, total=${TOTAL}s, title=${TITLE}" >&2
echo "[bsk-extract] source: ${URL} -> $([ -n "$OUT" ] && echo "$OUT" || echo stdout)" >&2

# 结果正文（无 --out 时打印到 stdout）。
if [[ -z "$OUT" ]]; then
	echo "$TEXT"
fi