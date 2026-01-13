// Extensions Module
// 파일 확장자 태그 관리

// 확장자 태그 렌더링
function renderExtensionTags() {
    const container = document.getElementById('extensionTagsContainer');
    if (!container) return;

    container.innerHTML = '';

    includeExtensions.forEach(ext => {
        const tag = document.createElement('div');
        tag.className = 'tag';
        tag.innerHTML = `
            ${ext}
            <span class="tag-remove" onclick="removeExtension('${ext}')">&times;</span>
        `;
        container.appendChild(tag);
    });
}

// 확장자 추가
function addExtension(ext) {
    ext = ext.toLowerCase().trim().replace(/^\./, '');

    if (!ext) return;

    // 중복 체크
    if (includeExtensions.includes(ext)) {
        alert(`"${ext}"는 이미 추가되었습니다.`);
        return;
    }

    includeExtensions.push(ext);
    renderExtensionTags();

    // saveSettings 함수가 있으면 호출
    if (typeof saveSettings === 'function') {
        saveSettings();
    }
}

// 확장자 제거
function removeExtension(ext) {
    includeExtensions = includeExtensions.filter(e => e !== ext);
    renderExtensionTags();

    // saveSettings 함수가 있으면 호출
    if (typeof saveSettings === 'function') {
        saveSettings();
    }
}

// 확장자 입력 핸들러 (Enter 키)
function handleExtensionInput(event) {
    if (event.key === 'Enter') {
        event.preventDefault();
        const input = document.getElementById('extensionInput');
        const value = input.value.trim();

        // 쉼표 또는 스페이스로 분리된 여러 확장자 처리
        const extensions = value.split(/[,\s]+/).filter(e => e);

        extensions.forEach(ext => addExtension(ext));

        input.value = '';
    }
}
