# ShutterPipe

사진/영상 파일을 촬영일시 기준으로 자동 백업 및 분류하는 도구

## 주요 기능

- **메타데이터 기반 분류**: EXIF/XMP 데이터를 읽어 촬영 날짜별로 자동 분류
- **웹 UI**: 브라우저에서 설정 및 실시간 진행 상황 확인
- **프리셋 관리**: 자주 사용하는 설정을 저장하고 불러오기
- **중복 검사**: 파일명+크기 또는 해시 기반 중복 파일 감지
- **충돌 처리**: Skip/Rename/Overwrite/Quarantine 정책 선택
- **경로 북마크**: 자주 사용하는 경로를 저장하여 빠른 접근

## 설치

### 필요 환경

- Go 1.21 이상

### 설치

```bash
git clone https://github.com/On-Jun9/ShutterPipe.git
cd ShutterPipe
```

## 사용법

### 1. 서버 실행

```bash
./start.sh
```

서버가 백그라운드에서 실행되며 `http://localhost:8080`에 접속할 수 있습니다.

#### 주소/포트 변경

```bash
./bin/shutterpipe-web -addr "0.0.0.0:8080"
```

`start.sh`는 기본값인 `localhost:8080`으로 실행합니다.

### 2. 웹 UI 접속

브라우저에서 `http://localhost:8080` 접속

### 3. 경로 설정

- **원본 경로**: SD 카드 또는 소스 디렉토리 경로 입력
- **목적지**: 백업할 NAS 또는 로컬 디렉토리 경로 입력

### 4. 분류 방식 선택

#### 날짜별 분류 (기본)
```
목적지/
├── 2025/
│   ├── 01/
│   │   ├── 01/
│   │   │   ├── IMG_1234.jpg
│   │   │   └── VID_5678.mp4
│   │   └── 02/
│   └── 02/
```

#### 이벤트별 분류
```
목적지/
├── 2025/
│   ├── 0101-크리스마스/
│   │   ├── JPG/
│   │   │   └── IMG_1234.jpg
│   │   ├── MP4/
│   │   │   └── VID_5678.mp4
│   │   └── RAW/
│   │       └── IMG_9999.cr2
```

- 이벤트명이 비어 있으면 `0101`처럼 날짜만 사용합니다.
- 파일 타입 폴더는 `JPG`, `MP4`, `RAW`로 분류됩니다.

### 5. 백업 시작

설정 완료 후 "백업 시작" 버튼 클릭

### 6. CLI 모드

```bash
go build -o bin/shutterpipe ./cmd/shutterpipe
./bin/shutterpipe run -s /Volumes/SD_CARD -d /Volumes/NAS/Photos
```

주요 옵션
- `-c, --config`: 설정 파일 경로 (YAML/JSON)
- `-s, --source`: 원본 경로
- `-d, --dest`: 목적지 경로
- `-e, --include-ext`: 포함할 확장자 목록 (예: `-e jpg -e mp4`)
- `-j, --jobs`: 병렬 워커 수 (0=자동)
- `--dedup`: `name-size` 또는 `hash`
- `--conflict`: `skip`, `rename`, `overwrite`, `quarantine`
- `--unclassified-dir`: 분류 불가 폴더명
- `--quarantine-dir`: 격리 폴더명
- `--state-file`: 상태 파일 경로
- `--log-file`: 로그 파일 경로
- `--log-json`: JSON 로그 출력
- `--dry-run`: 복사 없이 시뮬레이션
- `--hash-verify`: 해시 검증

### 버전 확인

```bash
./bin/shutterpipe version
```

## 설정 옵션

### 기본 설정

| 옵션 | 설명 | 기본값 |
|------|------|--------|
| 분류 방식 | 날짜별 또는 이벤트별 | 날짜별 |
| 이벤트명 | 이벤트별 분류 시 폴더명에 추가 | (비어 있음) |
| 충돌 정책 | Skip/Rename/Overwrite/Quarantine | Skip |
| 중복 검사 방법 | 이름+크기 또는 해시 | 이름+크기 |
| Dry Run | 실제 복사 없이 시뮬레이션 | Off |
| 해시 검증 | 복사 후 파일 무결성 검증 | Off |
| 이전 기록 무시 | 상태 파일 무시하고 전체 재수행 | Off |

### 고급 설정

| 옵션 | 설명 | 기본값 |
|------|------|--------|
| 병렬 워커 수 | 동시 처리 파일 개수 (0=자동) | 자동 (CPU 코어 수) |
| 포함할 확장자 | 특정 확장자만 필터링 | jpg, jpeg, heic, heif, png, raw, arw, cr2, nef, dng, mp4, mov, avi, mkv, mxf, xml |
| 분류 불가 폴더명 | 메타데이터 없는 파일 저장 폴더 | unclassified |
| 격리 폴더명 | 충돌 시 격리 정책 사용 폴더 | quarantine |
| 상태 파일 경로 | 처리 이력 저장 파일 | ~/.shutterpipe/state.json |
| 로그 파일 경로 | 로그 저장 경로 | ~/.shutterpipe/shutterpipe.log |
| JSON 형식 로그 | 로그를 JSON 형식으로 저장 | Off |

## 설정 파일

CLI에서 `--config`로 YAML/JSON 설정 파일을 불러올 수 있습니다.

```yaml
source: "/Volumes/SD_CARD"
dest: "/Volumes/NAS/Photos"
organize_strategy: "date" # date | event
event_name: "크리스마스"
include_extensions: ["jpg", "mp4"]
jobs: 0
conflict_policy: "skip"
dedup_method: "name-size"
unclassified_dir: "unclassified"
quarantine_dir: "quarantine"
state_file: "~/.shutterpipe/state.json"
log_file: "~/.shutterpipe/shutterpipe.log"
log_json: false
dry_run: false
hash_verify: false
ignore_state: false
```

## 프리셋

자주 사용하는 설정을 프리셋으로 저장할 수 있습니다.

- 우측 상단 톱니바퀴 아이콘 클릭
- "현재 설정 저장" 버튼으로 저장
- 저장된 프리셋 클릭으로 불러오기
- 프리셋 파일 위치: `~/.shutterpipe/presets/`

## 서버 관리

### 서버 시작

```bash
./start.sh
```

`shutterpipe.pid` 파일이 생성됩니다.

### 서버 종료

```bash
./stop.sh
```

### 로그 확인

- 서버 로그: 프로젝트 루트의 `shutterpipe.log` (`start.sh`가 생성)
- 파이프라인 로그: `~/.shutterpipe/shutterpipe.log` (설정에서 변경 가능)

```bash
tail -f shutterpipe.log
```
