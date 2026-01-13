#!/bin/bash

if [ ! -f "shutterpipe.pid" ]; then
    echo "[WARN] PID 파일이 없습니다"
    echo "       수동으로 종료하려면: ps aux | grep shutterpipe-web"
    exit 1
fi

PID=$(cat shutterpipe.pid)

if ! ps -p $PID > /dev/null 2>&1; then
    echo "[WARN] 프로세스가 실행 중이 아닙니다 (PID: $PID)"
    rm -f shutterpipe.pid
    exit 1
fi

echo "[STOP] 서버 종료 중... (PID: $PID)"
kill $PID

sleep 1

if ps -p $PID > /dev/null 2>&1; then
    echo "[WARN] 정상 종료 실패, 강제 종료 중..."
    kill -9 $PID
fi

rm -f shutterpipe.pid
echo "[SUCCESS] 서버가 종료되었습니다"
