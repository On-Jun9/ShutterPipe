// Settings Module
// 설정 저장 및 로드

// 설정 로드
function loadSettings() {
    const saved = localStorage.getItem('shutterpipe-config');
    if (saved) {
        const config = JSON.parse(saved);

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
    } else {
        // 기본값 로드 시에도 태그 렌더링
        if (typeof renderExtensionTags === 'function') {
            renderExtensionTags();
        }
    }

    // 경로 히스토리 로드
    const savedHistory = localStorage.getItem('shutterpipe-path-history');
    if (savedHistory) {
        pathHistory = JSON.parse(savedHistory);
    }

    // 북마크 로드
    const savedBookmarks = localStorage.getItem('shutterpipe-bookmarks');
    if (savedBookmarks) {
        bookmarks = JSON.parse(savedBookmarks);
    }

    // 북마크 버튼 상태 업데이트
    if (typeof updateBookmarkButtons === 'function') {
        updateBookmarkButtons();
    }
}

// 설정 저장
function saveSettings() {
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

        // 고급 설정
        jobs: jobsValue,
        include_extensions: includeExtensions,
        unclassified_dir: document.getElementById('unclassifiedDir').value || 'unclassified',
        quarantine_dir: document.getElementById('quarantineDir').value || 'quarantine',
        state_file: document.getElementById('stateFile').value,
        log_file: document.getElementById('logFile').value,
        log_json: document.getElementById('logJson').checked
    };
    localStorage.setItem('shutterpipe-config', JSON.stringify(config));
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

// 페이지 로드 시 설정 로드
window.addEventListener('DOMContentLoaded', loadSettings);
