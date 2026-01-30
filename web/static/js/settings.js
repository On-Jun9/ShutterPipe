// Settings Module
// 설정 저장 및 로드

// ==================== Path Validation ====================

function validatePath(path) {
    if (!path) return null; // Empty is allowed

    const lowerPath = path.toLowerCase();

    // Check for HTML tags (not just < or >, but actual tag patterns)
    const htmlTagPatterns = [
        { pattern: '<script', msg: '스크립트 태그를 사용할 수 없습니다' },
        { pattern: '</script', msg: '스크립트 태그를 사용할 수 없습니다' },
        { pattern: '<iframe', msg: 'iframe 태그를 사용할 수 없습니다' },
        { pattern: '<object', msg: 'object 태그를 사용할 수 없습니다' },
        { pattern: '<embed', msg: 'embed 태그를 사용할 수 없습니다' },
        { pattern: '<img', msg: 'img 태그를 사용할 수 없습니다' }
    ];

    for (const { pattern, msg } of htmlTagPatterns) {
        if (lowerPath.includes(pattern)) {
            return msg;
        }
    }

    // Check for event handlers and javascript
    const dangerousPatterns = [
        { pattern: 'javascript:', msg: 'JavaScript URL을 사용할 수 없습니다' },
        { pattern: 'onerror=', msg: '이벤트 핸들러를 사용할 수 없습니다' },
        { pattern: 'onload=', msg: '이벤트 핸들러를 사용할 수 없습니다' },
        { pattern: 'onclick=', msg: '이벤트 핸들러를 사용할 수 없습니다' },
        { pattern: 'onmouseover=', msg: '이벤트 핸들러를 사용할 수 없습니다' }
    ];

    for (const { pattern, msg } of dangerousPatterns) {
        if (lowerPath.includes(pattern)) {
            return msg;
        }
    }

    // Check maximum length
    if (path.length > 4096) {
        return '경로가 너무 깁니다 (최대 4096자)';
    }

    return null; // Valid
}

// Validate and show error for a field
function validateField(fieldId) {
    const input = document.getElementById(fieldId);
    if (!input) return true;

    const error = validatePath(input.value);
    const group = input.closest('.path-input-group');

    if (error) {
        if (group) group.classList.add('has-error');
        input.title = error;
        disableBackupButton();
        return false;
    } else {
        if (group) group.classList.remove('has-error');
        input.title = '';
        // Check if both fields are valid before enabling
        const sourceValid = !validatePath(document.getElementById('source').value);
        const destValid = !validatePath(document.getElementById('dest').value);
        if (sourceValid && destValid) {
            enableBackupButton();
        }
        return true;
    }
}

// ==================== Settings Load/Save ====================

// 설정 로드
async function loadSettings() {
    // 서버에서 설정 로드
    const config = await loadSettingsFromServer();
    if (config) {
        // 기본 설정
        document.getElementById('source').value = config.source || '';
        document.getElementById('dest').value = config.dest || '';
        document.getElementById('organizeStrategy').value = config.organize_strategy || 'date';
        document.getElementById('eventName').value = config.event_name || '';
        document.getElementById('conflictPolicy').value = config.conflict_policy || 'skip';
        document.getElementById('dedupMethod').value = config.dedup_method || 'name-size';
        document.getElementById('dryRun').checked = config.dry_run || false;
        document.getElementById('hashVerify').checked = config.hash_verify || false;
        document.getElementById('ignoreState').checked = config.ignore_state || false;

        // 날짜 필터
        document.getElementById('dateFilterStart').value = config.date_filter_start || '';
        document.getElementById('dateFilterEnd').value = config.date_filter_end || '';

        // 고급 설정
        document.getElementById('jobs').value = config.jobs || 0;
        document.getElementById('unclassifiedDir').value = config.unclassified_dir || 'unclassified';
        document.getElementById('quarantineDir').value = config.quarantine_dir || 'quarantine';
        document.getElementById('stateFile').value = config.state_file || '';
        document.getElementById('logFile').value = config.log_file || '';
        document.getElementById('logJson').checked = config.log_json || false;

        // 확장자 목록 로드
        if (config.include_extensions && Array.isArray(config.include_extensions)) {
            includeExtensions = config.include_extensions;
        }

        // renderExtensionTags 함수가 있으면 호출
        if (typeof renderExtensionTags === 'function') {
            renderExtensionTags();
        }

        // 이벤트명 입력 필드 표시/숨김
        toggleEventNameInput();

        // 날짜 필터 버튼 상태 업데이트
        updateDateFilterButtons();
    } else {
        // 기본값 로드 시에도 태그 렌더링
        if (typeof renderExtensionTags === 'function') {
            renderExtensionTags();
        }
        // 날짜 필터 버튼 상태 업데이트
        updateDateFilterButtons();
    }

    // 경로 히스토리 로드
    const historyData = await loadPathHistoryFromServer();
    if (historyData) {
        pathHistory = historyData;
    }

    // 북마크 로드
    const bookmarksData = await loadBookmarksFromServer();
    if (bookmarksData) {
        bookmarks = bookmarksData;
    }

    // 북마크 버튼 상태 업데이트
    if (typeof updateBookmarkButtons === 'function') {
        updateBookmarkButtons();
    }
}

// 설정 저장
async function saveSettings() {
    // Clear previous errors
    clearPathErrors();

    // Jobs 값 검증
    const jobsInput = document.getElementById('jobs');
    let jobsValue = parseInt(jobsInput.value) || 0;
    if (jobsValue < 0) jobsValue = 0;
    if (jobsValue > 32) jobsValue = 32;
    jobsInput.value = jobsValue;

    const config = {
        // 기본 설정
        source: document.getElementById('source').value,
        dest: document.getElementById('dest').value,
        organize_strategy: document.getElementById('organizeStrategy').value,
        event_name: document.getElementById('eventName').value,
        conflict_policy: document.getElementById('conflictPolicy').value,
        dedup_method: document.getElementById('dedupMethod').value,
        dry_run: document.getElementById('dryRun').checked,
        hash_verify: document.getElementById('hashVerify').checked,
        ignore_state: document.getElementById('ignoreState').checked,

        // 날짜 필터
        date_filter_start: document.getElementById('dateFilterStart').value || null,
        date_filter_end: document.getElementById('dateFilterEnd').value || null,

        // 고급 설정
        jobs: jobsValue,
        include_extensions: includeExtensions,
        unclassified_dir: document.getElementById('unclassifiedDir').value || 'unclassified',
        quarantine_dir: document.getElementById('quarantineDir').value || 'quarantine',
        state_file: document.getElementById('stateFile').value,
        log_file: document.getElementById('logFile').value,
        log_json: document.getElementById('logJson').checked
    };

    const result = await saveSettingsToServer(config);
    if (!result.success) {
        // Mark error fields and disable backup button
        markPathErrors(result.field, result.error);
        disableBackupButton();
    } else {
        enableBackupButton();
    }
}

// Clear path input errors
function clearPathErrors() {
    const sourceGroup = document.getElementById('source').closest('.path-input-group');
    const destGroup = document.getElementById('dest').closest('.path-input-group');
    if (sourceGroup) sourceGroup.classList.remove('has-error');
    if (destGroup) destGroup.classList.remove('has-error');
}

// Mark path inputs with errors based on field identifier
function markPathErrors(field, errorMessage) {
    const sourceGroup = document.getElementById('source').closest('.path-input-group');
    const destGroup = document.getElementById('dest').closest('.path-input-group');

    // Use structured field identifier from backend
    if (field === 'source') {
        if (sourceGroup) sourceGroup.classList.add('has-error');
        document.getElementById('source').title = errorMessage || '';
    } else if (field === 'dest') {
        if (destGroup) destGroup.classList.add('has-error');
        document.getElementById('dest').title = errorMessage || '';
    } else {
        // Fallback: try parsing error message
        if (errorMessage) {
            const lowerError = errorMessage.toLowerCase();
            if (lowerError.includes('source')) {
                if (sourceGroup) sourceGroup.classList.add('has-error');
            } else if (lowerError.includes('destination') || lowerError.includes('dest')) {
                if (destGroup) destGroup.classList.add('has-error');
            }
        }
    }
}

// Disable backup button
function disableBackupButton() {
    const startBtn = document.getElementById('startBtn');
    if (startBtn) {
        startBtn.disabled = true;
        startBtn.title = '설정에 오류가 있습니다';
    }
}

// Enable backup button (only if paths are valid)
function enableBackupButton() {
    const startBtn = document.getElementById('startBtn');
    if (startBtn && !isRunning) {
        // Check if both source and dest are valid before enabling
        const sourceValid = !validatePath(document.getElementById('source').value);
        const destValid = !validatePath(document.getElementById('dest').value);

        if (sourceValid && destValid) {
            startBtn.disabled = false;
            startBtn.title = '';
        } else {
            startBtn.disabled = true;
            startBtn.title = '설정에 오류가 있습니다';
        }
    }
}

// 이벤트명 입력 필드 표시/숨김
function toggleEventNameInput() {
    const strategy = document.getElementById('organizeStrategy').value;
    const eventNameContainer = document.getElementById('eventNameContainer');

    if (strategy === 'event') {
        eventNameContainer.style.display = 'block';
    } else {
        eventNameContainer.style.display = 'none';
    }
}

// 날짜 필터 설정 (빠른 선택)
function setDateFilter(mode) {
    const today = new Date();
    const startInput = document.getElementById('dateFilterStart');
    const endInput = document.getElementById('dateFilterEnd');

    if (mode === 'all') {
        // 전체: 필터 없음
        startInput.value = '';
        endInput.value = '';
    } else if (mode === 'today') {
        // 오늘
        const dateStr = today.toISOString().split('T')[0];
        startInput.value = dateStr;
        endInput.value = dateStr;
    } else if (mode === 'week') {
        // 최근 7일
        const weekAgo = new Date(today);
        weekAgo.setDate(today.getDate() - 7);
        startInput.value = weekAgo.toISOString().split('T')[0];
        endInput.value = today.toISOString().split('T')[0];
    } else if (mode === 'month') {
        // 최근 30일
        const monthAgo = new Date(today);
        monthAgo.setDate(today.getDate() - 30);
        startInput.value = monthAgo.toISOString().split('T')[0];
        endInput.value = today.toISOString().split('T')[0];
    }

    saveSettings();
    updateDateFilterButtons();
}

// 날짜 필터 버튼 상태 업데이트
function updateDateFilterButtons() {
    const startInput = document.getElementById('dateFilterStart');
    const endInput = document.getElementById('dateFilterEnd');
    const startDate = startInput.value;
    const endDate = endInput.value;

    const today = new Date().toISOString().split('T')[0];
    const weekAgo = new Date();
    weekAgo.setDate(weekAgo.getDate() - 7);
    const weekAgoStr = weekAgo.toISOString().split('T')[0];
    const monthAgo = new Date();
    monthAgo.setDate(monthAgo.getDate() - 30);
    const monthAgoStr = monthAgo.toISOString().split('T')[0];

    const buttons = document.querySelectorAll('.btn-date-filter');
    buttons.forEach(btn => btn.classList.remove('active'));

    if (!startDate && !endDate) {
        // 전체
        buttons[3].classList.add('active');
    } else if (startDate === today && endDate === today) {
        // 오늘
        buttons[0].classList.add('active');
    } else if (startDate === weekAgoStr && endDate === today) {
        // 최근 7일
        buttons[1].classList.add('active');
    } else if (startDate === monthAgoStr && endDate === today) {
        // 최근 30일
        buttons[2].classList.add('active');
    }
    // 사용자 정의 날짜인 경우 버튼 활성화 안함
}

// 페이지 로드 시 설정 로드
window.addEventListener('DOMContentLoaded', loadSettings);

// Real-time path validation
window.addEventListener('DOMContentLoaded', () => {
    const sourceInput = document.getElementById('source');
    const destInput = document.getElementById('dest');

    if (sourceInput) {
        sourceInput.addEventListener('input', () => validateField('source'));
        sourceInput.addEventListener('blur', () => validateField('source'));
    }

    if (destInput) {
        destInput.addEventListener('input', () => validateField('dest'));
        destInput.addEventListener('blur', () => validateField('dest'));
    }
});
