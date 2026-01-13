#!/bin/bash

set -e

echo "[BUILD] 빌드 중..."
go build -o bin/shutterpipe-web ./cmd/shutterpipe-web

if [ -f "shutterpipe.pid" ]; then
    PID=$(cat shutterpipe.pid)
    if ps -p $PID > /dev/null 2>&1; then
        echo "[WARN] 이미 실행 중입니다 (PID: $PID)"
        echo "       종료하려면: ./stop.sh"
        exit 1
    fi
fi

echo "[START] 서버 시작 중..."
nohup ./bin/shutterpipe-web > shutterpipe.log 2>&1 &
echo $! > shutterpipe.pid

sleep 1

if ps -p $(cat shutterpipe.pid) > /dev/null 2>&1; then
    echo "[SUCCESS] ShutterPipe 서버가 시작되었습니다"
    echo "          URL: http://localhost:8080"
    echo "          PID: $(cat shutterpipe.pid)"
    echo "          로그: tail -f shutterpipe.log"
else
    echo "[ERROR] 서버 시작 실패"
    cat shutterpipe.log
    exit 1
fi
