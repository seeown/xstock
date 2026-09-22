#!/bin/bash
# NStock 每日增量更新（launchd 于工作日 17:00 调用，错过时段唤醒后补跑）
set -a; source /Users/wyf/developer/NStock/.env.produce; set +a
export GOTOOLCHAIN=auto
export PATH=/opt/homebrew/bin:$PATH
cd /Users/wyf/developer/NStock || exit 1
mkdir -p logs
LOG="logs/daily-$(date +%F).log"
echo "===== $(date '+%F %T') 定时更新触发 =====" >> "$LOG"
if go build -o bin/dailyupdate ./cmd/dailyupdate >> "$LOG" 2>&1; then
  ./bin/dailyupdate >> "$LOG" 2>&1
  code=$?
else
  code=$?
fi
if curl -s -m 5 http://localhost:8080/health >/dev/null 2>&1; then
  if curl -s -m 300 -X POST http://localhost:8080/api/admin/reload >> "$LOG" 2>&1; then
    echo "$(date '+%F %T') 服务缓存已热刷新" >> "$LOG"
  fi
fi
echo "===== $(date '+%F %T') 结束（退出码 $code） =====" >> "$LOG"
