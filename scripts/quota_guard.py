#!/usr/bin/env python3
"""
CLIProxyAPI-Lite Quota Guard
Automatically monitors 5-hour rolling usage for Claude and Antigravity OAuth accounts.
If usage reaches >= 95%, proactively pauses (disables) the account to prevent 100% exhaustion
and anti-abuse bans. Automatically resumes the account once the 5-hour window resets.
"""

import os
import sys
import glob
import json
import time
import random
import urllib.request
import urllib.error
from datetime import datetime, timezone

AUTH_DIR = "/Users/bao/Library/Application Support/CLIProxyAPI-Lite/auth"
STATE_FILE = "/Users/bao/Library/Application Support/CLIProxyAPI-Lite/scripts/quota_guard_state.json"
LOG_FILE = "/Users/bao/Library/Application Support/CLIProxyAPI-Lite/logs/quota_guard.log"
PROXY = "http://127.0.0.1:7890"
THRESHOLD = 0.95  # 95%
CHECK_INTERVAL = 300  # 5 minutes (gentle polling to avoid hitting Anthropic usage rate limit)

# Per-account backoff tracking: state_key -> consecutive_429_count
_backoff_counters = {}

MAX_LOG_BYTES = 5 * 1024 * 1024  # 5 MB
LOG_BACKUP_COUNT = 3


def log(msg):
    now = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    print(f"[{now}] {msg}", flush=True)


def rotate_log_if_needed():
    """Simple log rotation: when log exceeds MAX_LOG_BYTES, rotate files."""
    if not os.path.exists(LOG_FILE):
        return
    try:
        size = os.path.getsize(LOG_FILE)
        if size < MAX_LOG_BYTES:
            return
        # Rotate: .log.3 -> delete, .log.2 -> .log.3, .log.1 -> .log.2, .log -> .log.1
        for i in range(LOG_BACKUP_COUNT, 0, -1):
            src = f"{LOG_FILE}.{i}" if i > 1 else LOG_FILE if i == 1 else None
            dst = f"{LOG_FILE}.{i}"
            if i == LOG_BACKUP_COUNT and os.path.exists(dst):
                os.remove(dst)
            src = f"{LOG_FILE}.{i - 1}" if i > 1 else LOG_FILE
            if os.path.exists(src):
                os.rename(src, dst)
        # After rotation, stdout still points to the old fd.
        # Since we use launchd stdout redirect, just truncate & let launchd handle it.
        # Create empty new log file so launchd can keep writing.
        open(LOG_FILE, "w").close()
        log("Log rotated.")
    except Exception as e:
        # Don't crash the guard over log rotation failure
        pass


def parse_iso(ts_str):
    if not ts_str:
        return None
    try:
        clean_ts = ts_str.replace("Z", "+00:00")
        dt = datetime.fromisoformat(clean_ts)
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=timezone.utc)
        return dt
    except Exception:
        return None


def get_opener():
    proxy_handler = urllib.request.ProxyHandler({"http": PROXY, "https": PROXY})
    return urllib.request.build_opener(proxy_handler)


def state_key(auth_type, email):
    """Generate a unique state key using type:email to avoid collision
    when the same email has both Claude and Antigravity accounts."""
    return f"{auth_type}:{email}"


def load_state():
    if os.path.exists(STATE_FILE):
        try:
            with open(STATE_FILE, "r", encoding="utf-8") as f:
                return json.load(f)
        except Exception:
            return {}
    return {}


def migrate_state(state):
    """Migrate old bare-email keys to type-prefixed keys.
    If a bare email key exists and no prefixed key exists, try to infer type
    from context (auto_disabled presence) and migrate. Only runs once."""
    migrated = False
    bare_keys = [k for k in list(state.keys()) if ":" not in k and "@" in k]
    for bare_email in bare_keys:
        old_data = state[bare_email]
        if not old_data or not old_data.get("auto_disabled"):
            # Empty or inactive entry, just remove
            del state[bare_email]
            migrated = True
            continue
        # Check which auth files exist for this email to determine type
        for auth_type in ("antigravity", "claude"):
            pattern = os.path.join(AUTH_DIR, f"{auth_type}-{bare_email}.json")
            if os.path.exists(pattern):
                new_key = state_key(auth_type, bare_email)
                if new_key not in state:
                    state[new_key] = old_data
                    log(f"Migrated state key '{bare_email}' -> '{new_key}'")
        del state[bare_email]
        migrated = True
    return migrated


def save_state(state):
    try:
        with open(STATE_FILE, "w", encoding="utf-8") as f:
            json.dump(state, f, indent=2, ensure_ascii=False)
    except Exception as e:
        log(f"Error saving state: {e}")


def update_auth_disabled(filepath, disabled_status):
    try:
        with open(filepath, "r", encoding="utf-8") as f:
            data = json.load(f)
        if data.get("disabled") == disabled_status:
            return False
        data["disabled"] = disabled_status
        with open(filepath, "w", encoding="utf-8") as f:
            json.dump(data, f, indent=4, ensure_ascii=False)
        return True
    except Exception as e:
        log(f"Failed to update {os.path.basename(filepath)}: {e}")
        return False


def should_backoff(sk):
    """Check if this account should skip this cycle due to 429 backoff."""
    count = _backoff_counters.get(sk, 0)
    if count <= 0:
        return False
    # Exponential backoff: skip 2^(count-1) cycles, capped at 16 (~32 min)
    skip_cycles = min(2 ** (count - 1), 16)
    # Use a simple modulo to decide; not perfect but avoids extra state
    # Actually, store the "skip until" timestamp instead
    return False  # handled via _backoff_until below


# Per-account backoff: state_key -> skip_until_timestamp
_backoff_until = {}


def record_429(sk):
    """Record a 429 for this account and set exponential backoff."""
    count = _backoff_counters.get(sk, 0) + 1
    _backoff_counters[sk] = min(count, 5)  # cap at 5 (max ~45 min backoff)
    # Ensure minimum backoff is 360s (6 min) so Anthropic's 5-minute penalty window completely expires
    base_delay = max(360, CHECK_INTERVAL * min(2 ** (count - 1), 8))
    delay = base_delay + random.uniform(10, 60)
    _backoff_until[sk] = time.time() + delay
    log(f"  ⏳ Backing off for {delay:.0f}s after 429 (consecutive: {count})")


def clear_429(sk):
    """Clear backoff counter on successful request."""
    if sk in _backoff_counters:
        del _backoff_counters[sk]
    if sk in _backoff_until:
        del _backoff_until[sk]


def is_backed_off(sk):
    """Check if this account is currently in backoff."""
    until = _backoff_until.get(sk)
    if until and time.time() < until:
        return True
    return False


def check_claude(filepath, data, state, opener):
    email = data.get("email", os.path.basename(filepath))
    token = data.get("access_token")
    if not token:
        return

    sk = state_key("claude", email)

    if is_backed_off(sk):
        return

    req = urllib.request.Request(
        "https://api.anthropic.com/api/oauth/usage",
        headers={
            "Authorization": f"Bearer {token}",
            "anthropic-beta": "oauth-2025-04-20",
            "User-Agent": "claude-cli/2.1.25 (external, cli)"
        }
    )

    try:
        with opener.open(req, timeout=12) as resp:
            quota = json.loads(resp.read().decode())
        clear_429(sk)
    except urllib.error.HTTPError as e:
        if e.code == 429:
            log(f"[Claude: {email}] Quota query rate-limited (429).")
            record_429(sk)
        elif e.code == 401:
            log(f"[Claude: {email}] Token expired (401), waiting for CLIProxyAPI refresh.")
        else:
            log(f"[Claude: {email}] Quota query failed: HTTP {e.code}")
        return
    except Exception as e:
        log(f"[Claude: {email}] Quota query failed: {e}")
        return

    five_hour = quota.get("five_hour", {})
    utilization_5h = five_hour.get("utilization", 0.0)
    resets_at_str_5h = five_hour.get("resets_at")
    resets_at_dt_5h = parse_iso(resets_at_str_5h)

    seven_day = quota.get("seven_day", {})
    utilization_7d = seven_day.get("utilization", 0.0)
    resets_at_str_7d = seven_day.get("resets_at")
    resets_at_dt_7d = parse_iso(resets_at_str_7d)

    now_dt = datetime.now(timezone.utc)

    account_state = state.setdefault(sk, {})
    is_auto_disabled = account_state.get("auto_disabled", False)

    # 1. Check if either 5h or 7d usage >= 95%
    if utilization_5h >= THRESHOLD or utilization_7d >= THRESHOLD:
        reason = "5-hour" if utilization_5h >= THRESHOLD else "7-day"
        curr_util = max(utilization_5h, utilization_7d)
        resets_at_str = resets_at_str_5h if utilization_5h >= THRESHOLD else resets_at_str_7d
        if not data.get("disabled", False) or not is_auto_disabled:
            log(f"🚨 [Claude: {email}] {reason} usage reached {curr_util*100:.1f}% (>= 95%). PROACTIVELY PAUSING account to protect against ban!")
            log(f"   Reset scheduled at: {resets_at_str}")
            if update_auth_disabled(filepath, True):
                account_state["auto_disabled"] = True
                account_state["resets_at"] = resets_at_str
                account_state["paused_at"] = now_dt.isoformat()
    else:
        # 2. Check resume: both 5h and 7d must be below threshold
        if is_auto_disabled:
            should_resume = False
            # If 5-hour reset has passed and utilization dropped
            resets_at_dt = parse_iso(account_state.get("resets_at"))
            if resets_at_dt and now_dt >= resets_at_dt:
                should_resume = True
            elif utilization_5h < 0.5 and utilization_7d < 0.8:
                should_resume = True

            if should_resume:
                log(f"✅ [Claude: {email}] Window reset passed (5h: {utilization_5h*100:.1f}%, 7d: {utilization_7d*100:.1f}%). RESUMING account!")
                if update_auth_disabled(filepath, False):
                    account_state["auto_disabled"] = False
                    account_state["resets_at"] = None


def check_antigravity(filepath, data, state, opener):
    email = data.get("email", os.path.basename(filepath))
    token = data.get("access_token")
    project = data.get("project_id") or data.get("projectId", "aicode-consumers")
    if not token:
        return

    sk = state_key("antigravity", email)

    if is_backed_off(sk):
        return

    req = urllib.request.Request(
        "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary",
        data=json.dumps({"project": project}).encode(),
        headers={
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
            "User-Agent": "antigravity/cli/1.0.13 (aidev_client; os_type=darwin; arch=arm64)"
        }
    )

    try:
        with opener.open(req, timeout=12) as resp:
            quota = json.loads(resp.read().decode())
        clear_429(sk)
    except urllib.error.HTTPError as e:
        if e.code == 429:
            log(f"[Antigravity: {email}] Quota query rate-limited (429).")
            record_429(sk)
        elif e.code == 401:
            log(f"[Antigravity: {email}] Token expired (401), waiting for CLIProxyAPI refresh.")
        else:
            log(f"[Antigravity: {email}] Quota query failed: HTTP {e.code}")
        return
    except Exception as e:
        log(f"[Antigravity: {email}] Quota query failed: {e}")
        return

    rem_fraction = 1.0
    resets_at_str = None
    for g in quota.get("groups", []):
        for b in g.get("buckets", []):
            if b.get("window") == "5h":
                rem_fraction = b.get("remainingFraction", 1.0)
                resets_at_str = b.get("resetTime")
                break

    used_fraction = 1.0 - rem_fraction
    resets_at_dt = parse_iso(resets_at_str)
    now_dt = datetime.now(timezone.utc)

    account_state = state.setdefault(sk, {})
    was_warned = account_state.get("warned", False)

    # Antigravity accounts: WARN ONLY, do NOT disable.
    # Disabling the auth file would also block Gemini models (gemini-3.8-flash etc.)
    # which share the same auth credential. Google doesn't ban for hitting 100% —
    # it just returns 429 + cooldown, which CLIProxyAPI handles reactively.
    if used_fraction >= THRESHOLD:
        if not was_warned:
            log(f"⚠️  [Antigravity: {email}] 5-hour usage reached {used_fraction*100:.1f}% (>= 95%). Logged warning only (account stays enabled for Gemini).")
            log(f"   Reset scheduled at: {resets_at_str}")
            account_state["warned"] = True
            account_state["resets_at"] = resets_at_str
    else:
        if was_warned:
            log(f"✅ [Antigravity: {email}] 5-hour window reset (usage: {used_fraction*100:.1f}%). Warning cleared.")
            account_state["warned"] = False
            account_state["resets_at"] = None


def run_once():
    opener = get_opener()
    state = load_state()

    # One-time migration of old bare-email state keys
    migrate_state(state)

    auth_files = sorted(glob.glob(os.path.join(AUTH_DIR, "*.json")))
    for filepath in auth_files:
        if "bak" in filepath or "state" in filepath:
            continue
        try:
            with open(filepath, "r", encoding="utf-8") as f:
                data = json.load(f)
        except Exception:
            continue

        auth_type = data.get("type")
        if auth_type == "claude":
            check_claude(filepath, data, state, opener)
        elif auth_type == "antigravity":
            check_antigravity(filepath, data, state, opener)

    save_state(state)
    rotate_log_if_needed()


def main():
    log("CLIProxyAPI-Lite Quota Guard started (v2 - fixed state keys, backoff, log rotation).")
    log(f"Monitoring threshold: {int(THRESHOLD*100)}% 5-hour rolling limit.")
    log(f"Proxy egress: {PROXY}, Check interval: {CHECK_INTERVAL}s.")

    while True:
        try:
            run_once()
        except Exception as e:
            log(f"Unexpected error in guard loop: {e}")
        time.sleep(CHECK_INTERVAL)


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "--once":
        run_once()
    else:
        main()
