#!/usr/bin/env bash
# retry_command.sh — run a command, retry it only on a known transient failure.
#
# Used by docs-pdf.sh for mmdc. Chrome's screenshot call intermittently hangs
# on shared runners (#960) and surfaces as
#
#   ProtocolError: Page.captureScreenshot timed out.
#
# A hang is not slowness: the same page renders in 3-23 s when it succeeds and
# then stops at the cap. So the answer is a bounded per-attempt cap plus a
# retry, never a longer cap — a 600 s cap turned a 3-minute red build into a
# 10-minute one without changing the outcome.
#
# The retry predicate is deliberately narrow: only the timeout signature is
# retried. A Mermaid parse error, a missing browser, a missing input file and
# every other failure exit immediately, so a real content break is never
# masked by a second attempt.
#
# Usage:
#   retry_command.sh --attempts N --log-dir DIR --label NAME \
#                    --retry-pattern ERE [--pause SECONDS] -- command [args...]
#
# Every attempt's combined stdout+stderr goes to DIR/NAME-attemptK.log, which
# is what the workflow uploads when the job fails. Exit status is 0 on the
# first success, otherwise the last attempt's status.

set -uo pipefail   # no -e: a failed attempt is data, not the end of the script

attempts=3
log_dir=""
label="command"
retry_pattern=""
pause=2

while [[ $# -gt 0 ]]; do
  case "$1" in
    --attempts) attempts="$2"; shift 2 ;;
    --log-dir) log_dir="$2"; shift 2 ;;
    --label) label="$2"; shift 2 ;;
    --retry-pattern) retry_pattern="$2"; shift 2 ;;
    --pause) pause="$2"; shift 2 ;;
    --) shift; break ;;
    *)
      echo "retry_command: unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

if [[ $# -eq 0 ]]; then
  echo "retry_command: no command given (put it after --)" >&2
  exit 2
fi
if [[ -z "$log_dir" || -z "$retry_pattern" ]]; then
  echo "retry_command: --log-dir and --retry-pattern are required" >&2
  exit 2
fi
if ! [[ "$attempts" =~ ^[1-9][0-9]*$ ]]; then
  echo "retry_command: --attempts must be a positive integer, got: $attempts" >&2
  exit 2
fi

mkdir -p "$log_dir"

status=1
for ((attempt = 1; attempt <= attempts; attempt++)); do
  log="$log_dir/$label-attempt$attempt.log"
  "$@" >"$log" 2>&1
  status=$?

  if [[ $status -eq 0 ]]; then
    if [[ $attempt -gt 1 ]]; then
      echo "  retry_command: $label recovered on attempt $attempt/$attempts" >&2
    fi
    exit 0
  fi

  if ! grep -Eq "$retry_pattern" "$log"; then
    echo "  retry_command: $label failed with a non-retryable error on attempt $attempt; log: $log" >&2
    cat "$log" >&2
    exit "$status"
  fi

  if [[ $attempt -lt $attempts ]]; then
    echo "  retry_command: $label hit a transient failure on attempt $attempt/$attempts; retrying in ${pause}s (log: $log)" >&2
    sleep "$pause"
  fi
done

echo "  retry_command: $label still failing after $attempts attempts; log: $log" >&2
cat "$log" >&2
exit "$status"
