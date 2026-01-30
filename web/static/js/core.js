// Core Module
// 전역 상태 및 유틸리티 함수

// WebSocket 연결
let ws = null;

// 백업 실행 상태
let isRunning = false;

// 확장자 목록
let includeExtensions = [
    'jpg', 'jpeg', 'heic', 'heif', 'png', 'raw', 'arw', 'cr2', 'nef', 'dng',
    'mp4', 'mov', 'avi', 'mkv', 'mxf', 'xml'
];

// 경로 히스토리 (최대 10개)
let pathHistory = {
    source: [],
    dest: []
};

// 북마크
let bookmarks = {
    source: [],
    dest: []
};

// 자동완성 상태
let autocompleteState = {
    selectedIndex: -1,
    currentField: null
};

// 경로 입력 값 정제 (따옴표 제거)
function cleanPath(input) {
    let value = input.value.trim();
    value = value.replace(/^['"]|['"]$/g, '');
    input.value = value;
}

// HTML escape to prevent XSS
function escapeHtml(unsafe) {
    if (!unsafe) return '';
    return String(unsafe)
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#039;");
}

// 용량을 적절한 단위로 포맷
function formatBytes(bytes) {
    if (bytes === 0) return '0 B';
    const gb = bytes / (1024 * 1024 * 1024);
    const mb = bytes / (1024 * 1024);

    if (gb >= 1) {
        return gb.toFixed(2) + ' GB';
    } else if (mb >= 1) {
        return mb.toFixed(2) + ' MB';
    } else {
        return (bytes / 1024).toFixed(2) + ' KB';
    }
}

// 속도를 적절한 단위로 포맷
function formatSpeed(bytesPerSecond) {
    if (bytesPerSecond === 0) return '0 B/s';

    const gbps = bytesPerSecond / (1024 * 1024 * 1024);
    const mbps = bytesPerSecond / (1024 * 1024);
    const kbps = bytesPerSecond / 1024;

    if (gbps >= 1) {
        return gbps.toFixed(2) + ' GB/s';
    } else if (mbps >= 1) {
        return mbps.toFixed(2) + ' MB/s';
    } else if (kbps >= 1) {
        return kbps.toFixed(2) + ' KB/s';
    } else {
        return bytesPerSecond.toFixed(2) + ' B/s';
    }
}

// 시간을 적절한 단위로 포맷
function formatDuration(seconds) {
    if (seconds === 0) return '0초';

    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    const secs = seconds % 60;

    const parts = [];
    if (hours > 0) parts.push(`${hours}시간`);
    if (minutes > 0) parts.push(`${minutes}분`);
    if (secs > 0 || parts.length === 0) parts.push(`${secs}초`);

    return parts.join(' ');
}
