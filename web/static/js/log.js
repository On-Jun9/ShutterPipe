// Log Module
// 로그 뷰어 기능

// 로그 항목 추가
function addLogEntry(message, type = 'info') {
    const logContent = document.getElementById('logContent');
    const entry = document.createElement('div');
    entry.className = 'log-entry';

    const time = new Date().toLocaleTimeString('ko-KR', { hour12: false });

    entry.innerHTML = `<span class="log-time">[${time}]</span><span class="log-${type}">${message}</span>`;

    logContent.appendChild(entry);
    logContent.scrollTop = logContent.scrollHeight;
}

// 로그 뷰어 토글
function toggleLogViewer() {
    const viewer = document.getElementById('logViewer');
    const icon = document.getElementById('logToggleIcon');

    if (viewer.style.display === 'none') {
        viewer.style.display = 'block';
        icon.textContent = '▲';
    } else {
        viewer.style.display = 'none';
        icon.textContent = '▼';
    }
}

// 로그 복사
function copyLogToClipboard() {
    const logContent = document.getElementById('logContent');
    const text = logContent.innerText;

    navigator.clipboard.writeText(text).then(() => {
        const btn = document.querySelector('button[title="로그 복사"]');
        const originalText = btn.textContent;
        btn.textContent = '✅';
        setTimeout(() => {
            btn.textContent = originalText;
        }, 2000);
    }).catch(err => {
        console.error('로그 복사 실패:', err);
        alert('로그 복사에 실패했습니다.');
    });
}
