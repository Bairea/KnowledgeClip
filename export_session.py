import sqlite3
import json
import sys
import os
from datetime import datetime


def export_session(db_path, session_id=None, output_dir=None):
    if not os.path.exists(db_path):
        print(f"Error: database not found: {db_path}")
        sys.exit(1)

    conn = sqlite3.connect(db_path)
    conn.row_factory = sqlite3.Row
    cursor = conn.cursor()

    if session_id:
        sessions = cursor.execute(
            "SELECT * FROM sessions WHERE id = ?", (session_id,)
        ).fetchall()
        if not sessions:
            print(f"Error: session not found: {session_id}")
            sys.exit(1)
    else:
        sessions = cursor.execute(
            "SELECT * FROM sessions ORDER BY created_at DESC"
        ).fetchall()

    sites = {}
    for row in cursor.execute("SELECT id, name, url FROM sites").fetchall():
        sites[row["id"]] = {"name": row["name"], "url": row["url"]}

    result = []
    for sess in sessions:
        messages = cursor.execute(
            "SELECT * FROM messages WHERE session_id = ? ORDER BY turn, created_at",
            (sess["id"],),
        ).fetchall()

        turns = {}
        for msg in messages:
            turn = msg["turn"]
            if turn not in turns:
                turns[turn] = {
                    "turn": turn,
                    "prompt": msg["prompt"],
                    "answers": [],
                }
            site_info = sites.get(msg["site_id"], {"name": msg["site_id"], "url": ""})
            turns[turn]["answers"].append({
                "site_id": msg["site_id"],
                "site_name": site_info["name"],
                "content": msg["content"] or "",
                "error": msg["error"] or "",
                "elapsed_ms": msg["elapsed_ms"] or 0,
                "kept": bool(msg["kept"]),
            })

        session_data = {
            "session_id": sess["id"],
            "session_prompt": sess["prompt"],
            "created_at": sess["created_at"],
            "turns": [turns[k] for k in sorted(turns.keys())],
        }
        result.append(session_data)

    conn.close()

    if output_dir is None:
        output_dir = os.path.dirname(os.path.abspath(db_path))

    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    if session_id:
        filename = f"session_{session_id[:8]}_{timestamp}.json"
    else:
        filename = f"sessions_export_{timestamp}.json"

    output_path = os.path.join(output_dir, filename)
    with open(output_path, "w", encoding="utf-8") as f:
        json.dump(result, f, ensure_ascii=False, indent=2)

    total_turns = sum(len(s["turns"]) for s in result)
    total_answers = sum(len(t["answers"]) for s in result for t in s["turns"])
    print(f"Exported {len(result)} session(s), {total_turns} turn(s), {total_answers} answer(s)")
    print(f"Output: {output_path}")
    return output_path


def print_summary(db_path, session_id=None):
    conn = sqlite3.connect(db_path)
    conn.row_factory = sqlite3.Row
    cursor = conn.cursor()

    if session_id:
        sessions = cursor.execute(
            "SELECT * FROM sessions WHERE id = ?", (session_id,)
        ).fetchall()
    else:
        sessions = cursor.execute(
            "SELECT * FROM sessions ORDER BY created_at DESC LIMIT 20"
        ).fetchall()

    print(f"\n{'='*80}")
    print(f"{'Session':<40} {'Turns':<8} {'Answers':<10} {'Created'}")
    print(f"{'='*80}")

    for sess in sessions:
        messages = cursor.execute(
            "SELECT turn, site_id, error, prompt, length(content) as clen FROM messages WHERE session_id = ?",
            (sess["id"],),
        ).fetchall()
        turns = set(m["turn"] for m in messages)
        errors = sum(1 for m in messages if m["error"])
        print(f"{sess['id'][:36]:<40} {len(turns):<8} {len(messages):<10} {sess['created_at']}")
        if errors:
            print(f"  -> {errors} answer(s) with errors")
        for turn in sorted(turns):
            turn_msgs = [m for m in messages if m["turn"] == turn]
            prompt = turn_msgs[0]["prompt"][:40] if turn_msgs else ""
            print(f"  Turn {turn}: {prompt}...")

    conn.close()


if __name__ == "__main__":
    db_path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "data.db")

    if len(sys.argv) < 2:
        print("Usage:")
        print(f"  python {sys.argv[0]} list              - List all sessions")
        print(f"  python {sys.argv[0]} export [session_id] - Export session(s) to JSON")
        print(f"  python {sys.argv[0]} summary [session_id] - Show session summary")
        sys.exit(0)

    cmd = sys.argv[1]
    if cmd == "list":
        print_summary(db_path)
    elif cmd == "export":
        sid = sys.argv[2] if len(sys.argv) > 2 else None
        export_session(db_path, sid)
    elif cmd == "summary":
        sid = sys.argv[2] if len(sys.argv) > 2 else None
        print_summary(db_path, sid)
    else:
        print(f"Unknown command: {cmd}")
        sys.exit(1)
