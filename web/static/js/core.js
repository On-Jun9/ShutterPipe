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
