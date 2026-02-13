// Path Autocomplete Module
// 경로 자동완성 및 북마크 기능

// 경로 히스토리에 추가
async function addToPathHistory(fieldId, path) {
    if (!path || path.trim() === '') return;

    path = path.trim();

    // 기존 상태 백업
    const previousHistory = JSON.parse(JSON.stringify(pathHistory));

    // 중복 제거
    pathHistory[fieldId] = pathHistory[fieldId].filter(p => p !== path);

    // 최신 경로를 맨 앞에 추가
    pathHistory[fieldId].unshift(path);

    // 최대 10개만 유지
    if (pathHistory[fieldId].length > 10) {
        pathHistory[fieldId] = pathHistory[fieldId].slice(0, 10);
    }

    // 서버에 저장
    const result = await savePathHistoryToServer(pathHistory);
    if (!result.success) {
        // 저장 실패 시 롤백
        pathHistory = previousHistory;
    }
}

// 경로 입력 핸들러 (자동완성 + cleanPath)
function handlePathInput(input, fieldId) {
    cleanPath(input);
    showAutocomplete(input, fieldId);
    updateBookmarkButtons();
}

// 자동완성 표시
function showAutocomplete(input, fieldId) {
    const value = input.value.toLowerCase();
    const dropdown = document.getElementById(`${fieldId}-autocomplete`);

    if (!value) {
        dropdown.style.display = 'none';
        autocompleteState.currentField = null;
        return;
    }

    // 히스토리에서 매칭되는 경로 필터링
    const matches = pathHistory[fieldId].filter(path =>
        path.toLowerCase().includes(value)
    );

    if (matches.length === 0) {
        dropdown.style.display = 'none';
        autocompleteState.currentField = null;
        return;
    }

    // 드롭다운 렌더링
    dropdown.innerHTML = '';
    matches.forEach((path, index) => {
        const item = document.createElement('div');
        item.className = 'autocomplete-item';
        item.setAttribute('data-index', index);
        item.textContent = path;
        item.onclick = () => selectAutocompletePath(fieldId, path);
        dropdown.appendChild(item);
    });

    dropdown.style.display = 'block';
    autocompleteState.currentField = fieldId;
    autocompleteState.selectedIndex = -1;
}

// 자동완성 경로 선택
function selectAutocompletePath(fieldId, path) {
    const input = document.getElementById(fieldId);
    input.value = path;

    const dropdown = document.getElementById(`${fieldId}-autocomplete`);
    dropdown.style.display = 'none';

    autocompleteState.currentField = null;
    autocompleteState.selectedIndex = -1;

    updateBookmarkButtons();

    if (typeof saveSettings === 'function') {
        saveSettings();
    }
}

// 북마크 토글
async function toggleBookmark(fieldId) {
    const input = document.getElementById(fieldId);
    const path = input.value.trim();

    if (!path) {
        alert('경로를 먼저 입력해주세요.');
        return;
    }

    // 기존 상태 백업
    const previousBookmarks = JSON.parse(JSON.stringify(bookmarks));

    const index = bookmarks[fieldId].indexOf(path);

    if (index > -1) {
        // 북마크 제거
        bookmarks[fieldId].splice(index, 1);
    } else {
        // 북마크 추가
        bookmarks[fieldId].push(path);
    }

    // 서버에 저장
    const result = await saveBookmarksToServer(bookmarks);

    if (result.success) {
        // 성공 시 알림
        if (index > -1) {
            alert('북마크에서 제거되었습니다.');
        } else {
            alert('북마크에 추가되었습니다.');
        }
    } else {
        // 실패 시 롤백
        bookmarks = previousBookmarks;
        const details = result.error ? `\n${result.error}` : '';
        alert(`북마크 저장에 실패했습니다.${details}`);
    }

    // 북마크 버튼 상태 업데이트
    updateBookmarkButtons();
}

// 북마크 드롭다운 토글
function toggleBookmarkDropdown(fieldId) {
    const dropdown = document.getElementById(`${fieldId}-bookmarks`);
    const button = document.getElementById(`${fieldId}-bookmark-btn`);

    // 다른 드롭다운 닫기
    document.querySelectorAll('.bookmark-dropdown').forEach(d => {
        if (d.id !== `${fieldId}-bookmarks`) {
            d.style.display = 'none';
        }
    });

    if (dropdown.style.display === 'block') {
        dropdown.style.display = 'none';
        return;
    }

    // 버튼 위치 계산
    const buttonRect = button.getBoundingClientRect();

    // 드롭다운 위치 설정 (버튼 아래, 오른쪽 정렬)
    dropdown.style.top = `${buttonRect.bottom + 8}px`;
    dropdown.style.right = `${window.innerWidth - buttonRect.right}px`;

    // 북마크 목록 렌더링
    renderBookmarkDropdown(fieldId);
    dropdown.style.display = 'block';
}

// 북마크 드롭다운 렌더링
function renderBookmarkDropdown(fieldId) {
    const dropdown = document.getElementById(`${fieldId}-bookmarks`);
    const items = bookmarks[fieldId];

    if (items.length === 0) {
        dropdown.innerHTML = '<div class="bookmark-empty">저장된 북마크가 없습니다.</div>';
        return;
    }

    // 헤더
    const header = document.createElement('div');
    header.className = 'bookmark-dropdown-header';
    header.textContent = `북마크 (${items.length}개)`;

    dropdown.innerHTML = '';
    dropdown.appendChild(header);

    // 각 북마크 아이템
    items.forEach(path => {
        const itemDiv = document.createElement('div');
        itemDiv.className = 'bookmark-item';

        const pathDiv = document.createElement('div');
        pathDiv.className = 'bookmark-path';
        pathDiv.textContent = path;
        pathDiv.title = path;
        pathDiv.onclick = () => selectBookmarkPath(fieldId, path);

        const removeDiv = document.createElement('div');
        removeDiv.className = 'bookmark-remove';
        removeDiv.textContent = '×';
        removeDiv.onclick = (e) => {
            e.stopPropagation();
            removeBookmark(fieldId, path);
        };

        itemDiv.appendChild(pathDiv);
        itemDiv.appendChild(removeDiv);
        dropdown.appendChild(itemDiv);
    });
}

// 북마크 경로 선택
function selectBookmarkPath(fieldId, path) {
    const input = document.getElementById(fieldId);
    input.value = path;

    const dropdown = document.getElementById(`${fieldId}-bookmarks`);
    dropdown.style.display = 'none';

    updateBookmarkButtons();

    if (typeof saveSettings === 'function') {
        saveSettings();
    }
}

// 북마크 제거
async function removeBookmark(fieldId, path) {
    // 기존 상태 백업
    const previousBookmarks = JSON.parse(JSON.stringify(bookmarks));

    bookmarks[fieldId] = bookmarks[fieldId].filter(p => p !== path);
    const result = await saveBookmarksToServer(bookmarks);

    if (!result.success) {
        // 저장 실패 시 롤백
        bookmarks = previousBookmarks;
        const details = result.error ? `\n${result.error}` : '';
        alert(`북마크 삭제에 실패했습니다.${details}`);
    }

    renderBookmarkDropdown(fieldId);
    updateBookmarkButtons();
}

// 북마크 버튼 상태 업데이트
function updateBookmarkButtons() {
    ['source', 'dest'].forEach(fieldId => {
        const input = document.getElementById(fieldId);
        if (!input) return;

        const path = input.value.trim();
        const pathInputGroup = input.closest('.path-input-group');
        if (!pathInputGroup) return;

        // 첫 번째 북마크 버튼 찾기
        const bookmarkBtn = pathInputGroup.querySelector('.btn-bookmark[title="북마크"]');
        if (!bookmarkBtn) return;

        if (path && bookmarks[fieldId] && bookmarks[fieldId].includes(path)) {
            bookmarkBtn.classList.add('active');
        } else {
            bookmarkBtn.classList.remove('active');
        }
    });
}

// 키보드 이벤트 핸들러 (자동완성 내비게이션)
document.addEventListener('keydown', (e) => {
    if (!autocompleteState.currentField) return;

    const dropdown = document.getElementById(`${autocompleteState.currentField}-autocomplete`);
    if (dropdown.style.display === 'none') return;

    const items = dropdown.querySelectorAll('.autocomplete-item');
    if (items.length === 0) return;

    // 화살표 키 처리
    if (e.key === 'ArrowDown') {
        e.preventDefault();
        autocompleteState.selectedIndex = Math.min(autocompleteState.selectedIndex + 1, items.length - 1);
        updateAutocompleteSelection(items);
    } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        autocompleteState.selectedIndex = Math.max(autocompleteState.selectedIndex - 1, -1);
        updateAutocompleteSelection(items);
    } else if (e.key === 'Enter') {
        e.preventDefault();
        if (autocompleteState.selectedIndex >= 0) {
            items[autocompleteState.selectedIndex].click();
        }
    } else if (e.key === 'Escape') {
        dropdown.style.display = 'none';
        autocompleteState.currentField = null;
        autocompleteState.selectedIndex = -1;
    }
});

// 자동완성 선택 상태 업데이트
function updateAutocompleteSelection(items) {
    items.forEach((item, index) => {
        if (index === autocompleteState.selectedIndex) {
            item.classList.add('selected');
            item.scrollIntoView({ block: 'nearest' });
        } else {
            item.classList.remove('selected');
        }
    });
}

// 외부 클릭 시 드롭다운 닫기
document.addEventListener('click', (e) => {
    // 자동완성 드롭다운 닫기
    if (!e.target.closest('.path-input-wrapper')) {
        document.querySelectorAll('.autocomplete-dropdown').forEach(d => {
            d.style.display = 'none';
        });
        autocompleteState.currentField = null;
        autocompleteState.selectedIndex = -1;
    }

    // 북마크 드롭다운 닫기
    if (!e.target.closest('.btn-bookmark') && !e.target.closest('.bookmark-dropdown')) {
        document.querySelectorAll('.bookmark-dropdown').forEach(d => {
            d.style.display = 'none';
        });
    }
});
