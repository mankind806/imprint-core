#!/usr/bin/env python3
"""Antigravity CLI Custom Statusline.

Modeled after Claude Code's statusline:
Model (Effort) · CWD · Context % (Used/Limit) · 5h Quota (Reset) · 7d Quota (Reset) · Mode
"""
import sys
import os
import json
import pathlib
import datetime

def format_duration(seconds):
    if seconds <= 0:
        return "now"
    hours = int(seconds // 3600)
    minutes = int((seconds % 3600) // 60)
    if hours > 0:
        return f"{hours}h{minutes:02d}m"
    return f"{minutes}m"

def format_tokens(n):
    if n >= 1_000_000:
        val = f"{n/1_000_000:.1f}".rstrip("0").rstrip(".")
        return f"{val}M"
    if n >= 1_000:
        return f"{n/1_000:.0f}k"
    return str(n)

def main():
    raw_stdin = sys.stdin.read()
    data = {}
    if raw_stdin.strip():
        try:
            data = json.loads(raw_stdin)
        except Exception:
            pass

    # 1. Model & Effort
    model_name = "Gemini"
    effort = ""
    model_obj = data.get("model")
    if isinstance(model_obj, dict):
        model_name = model_obj.get("display_name") or model_obj.get("id") or "Gemini"
        effort_val = model_obj.get("effort")
        if effort_val:
            effort = f" ({effort_val.capitalize()})"
    elif "model_name" in data:
        model_name = data["model_name"]

    # Shorten model name if too long
    model_disp = model_name.replace(" (High)", "").replace(" (Medium)", "").replace(" (Low)", "")
    model_part = f"{model_disp}{effort}"

    # 2. CWD / Workspace
    cwd = data.get("cwd") or os.getcwd()
    home = str(pathlib.Path.home())
    if cwd == home:
        cwd_disp = "~"
    elif cwd.startswith(home + "/"):
        cwd_disp = "~/" + cwd[len(home) + 1:]
    else:
        cwd_disp = cwd

    # 3. Context Window Usage
    ctx = data.get("context_window") or {}
    total_in = ctx.get("total_input_tokens") or 0
    total_out = ctx.get("total_output_tokens") or 0
    used_tokens = total_in + total_out
    ctx_size = ctx.get("context_window_size") or 1048576
    used_pct = ctx.get("used_percentage")
    if used_pct is None:
        used_pct = int((used_tokens / ctx_size) * 100) if ctx_size else 0

    if used_tokens > 0:
        ctx_part = f"Context: {used_pct}% ({format_tokens(used_tokens)}/{format_tokens(ctx_size)})"
    else:
        ctx_part = f"Context: 0% (0/{format_tokens(ctx_size)})"

    # 4. Quota (5h and 7d)
    quota = data.get("quota") or {}
    quota_parts = []
    # Primary quota for gemini
    q5h = quota.get("gemini-5h")
    if q5h:
        rem_frac = q5h.get("remaining_fraction", 1.0)
        used_q5h_pct = int(round((1.0 - rem_frac) * 100))
        rem_sec = q5h.get("reset_in_seconds", 0)
        dur_str = format_duration(rem_sec)
        quota_parts.append(f"5h: {used_q5h_pct}% (Reset in {dur_str})")

    q_weekly = quota.get("gemini-weekly")
    if q_weekly:
        rem_frac_w = q_weekly.get("remaining_fraction", 1.0)
        used_qw_pct = int(round((1.0 - rem_frac_w) * 100))
        reset_time_str = q_weekly.get("reset_time", "")
        # Format reset time e.g. "Mo 20:44"
        reset_day = ""
        if reset_time_str:
            try:
                # Parse ISO timestamp
                dt = datetime.datetime.fromisoformat(reset_time_str.replace("Z", "+00:00")).astimezone()
                weekday = ["Mo", "Di", "Mi", "Do", "Fr", "Sa", "So"][dt.weekday()]
                reset_day = f" ({weekday} {dt.strftime('%H:%M')})"
            except Exception:
                pass
        quota_parts.append(f"7d: {used_qw_pct}%{reset_day}")

    # 5. Permission / Sandbox Mode
    sandbox = data.get("sandbox") or {}
    if sandbox.get("enabled"):
        mode_part = "Sandbox"
    else:
        mode_part = "YOLO / Auto-Approve"

    # Assemble line
    sections = [
        f"⚡ {model_part}",
        f"📁 {cwd_disp}",
        f"🧠 {ctx_part}",
    ]
    if quota_parts:
        sections.append("⏳ " + " · ".join(quota_parts))
    sections.append(f"🛡️ {mode_part}")

    line = " · ".join(sections)
    print(line)

if __name__ == "__main__":
    main()
