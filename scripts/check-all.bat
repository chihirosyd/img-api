@echo off
rem 全量工具链检查：vet / test / race / staticcheck / mod verify / 交叉编译
rem 输出重定向到 check-report.txt 便于阅读
rem 需要 go 在 PATH 中；race 与 Windows GUI 交叉编译需要本地 gcc（MSYS2 ucrt64 等）
cd /d "%~dp0.."
set PATH=D:\Development\msys64\ucrt64\bin;%PATH%
set OUT=check-report.txt
del %OUT% 2>nul

echo === go vet === >> %OUT%
go vet ./... >> %OUT% 2>&1
echo VET_EXIT=%ERRORLEVEL% >> %OUT%

echo === go test === >> %OUT%
go test ./... -count=1 >> %OUT% 2>&1
echo TEST_EXIT=%ERRORLEVEL% >> %OUT%

echo === go test -race (internal) === >> %OUT%
go test -race ./internal/... -count=1 >> %OUT% 2>&1
echo RACE_EXIT=%ERRORLEVEL% >> %OUT%

echo === staticcheck === >> %OUT%
where staticcheck >nul 2>nul
if %ERRORLEVEL%==0 (
  staticcheck ./... >> %OUT% 2>&1
  echo SC_EXIT=%ERRORLEVEL% >> %OUT%
) else (
  echo staticcheck NOT INSTALLED (go install honnef.co/go/tools/cmd/staticcheck@latest) >> %OUT%
)

echo === go mod verify === >> %OUT%
go mod verify >> %OUT% 2>&1
echo MOD_EXIT=%ERRORLEVEL% >> %OUT%

echo === coverage === >> %OUT%
go test ./... -cover >> %OUT% 2>&1
echo COV_EXIT=%ERRORLEVEL% >> %OUT%

echo === cross build server === >> %OUT%
set GOOS=linux
set GOARCH=amd64
go build -o NUL ./cmd/server/ >> %OUT% 2>&1
echo LINUX_AMD64=%ERRORLEVEL% >> %OUT%
set GOARCH=arm64
go build -o NUL ./cmd/server/ >> %OUT% 2>&1
echo LINUX_ARM64=%ERRORLEVEL% >> %OUT%
set GOOS=darwin
set GOARCH=amd64
go build -o NUL ./cmd/server/ >> %OUT% 2>&1
echo DARWIN_AMD64=%ERRORLEVEL% >> %OUT%
set GOARCH=arm64
go build -o NUL ./cmd/server/ >> %OUT% 2>&1
echo DARWIN_ARM64=%ERRORLEVEL% >> %OUT%
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=1
set CC=gcc
go build -o NUL ./cmd/gui/ >> %OUT% 2>&1
echo WINDOWS_GUI_CGO=%ERRORLEVEL% >> %OUT%
set GOOS=
set GOARCH=
set CGO_ENABLED=
set CC=

echo === DONE === >> %OUT%
type %OUT%
