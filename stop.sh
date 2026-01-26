#!/bin/bash

PID_FILE="shutterpipe.pid"
TARGET_PIDS=""

if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE")
    if [ -n "$PID" ] && ps -p "$PID" > /dev/null 2>&1; then
        TARGET_PIDS="$PID"
    else
        echo "[WARN] PID 파일이 오래되었거나 프로세스가 없습니다 (PID: $PID)"
    fi
else
    echo "[WARN] PID 파일이 없습니다"
fi

if [ -z "$TARGET_PIDS" ]; then
    TARGET_PIDS=$(pgrep -f "shutterpipe-web")
fi

if [ -z "$TARGET_PIDS" ]; then
    echo "[WARN] 실행 중인 shutterpipe-web 프로세스를 찾을 수 없습니다"
    rm -f "$PID_FILE"
    exit 1
fi

for pid in $TARGET_PIDS; do
    echo "[STOP] 서버 종료 중... (PID: $pid)"
    kill "$pid"
done

sleep 1

for pid in $TARGET_PIDS; do
    if ps -p "$pid" > /dev/null 2>&1; then
        echo "[WARN] 정상 종료 실패, 강제 종료 중... (PID: $pid)"
        kill -9 "$pid"
    fi
done

rm -f "$PID_FILE"
echo "[SUCCESS] 서버가 종료되었습니다"
