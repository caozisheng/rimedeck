@echo off
setlocal EnableExtensions EnableDelayedExpansion

cd /d "%~dp0"

set "DISTRO=%~1"
if "%DISTRO%"=="" set "DISTRO=Ubuntu-24.04"
set "SKIP_BUNDLE=0"
if /I "%~1"=="--no-bundle" (
  set "DISTRO=Ubuntu-24.04"
  set "SKIP_BUNDLE=1"
)
if /I "%~2"=="--no-bundle" set "SKIP_BUNDLE=1"

set "PATH=C:\Program Files\nodejs;C:\Program Files\Go\bin;%APPDATA%\npm;%PATH%"
set "ROOT=%CD%"
set "LOGDIR=%USERPROFILE%\.rimedeck\debug"
set "LOGFILE=%LOGDIR%\wsl-runtime-dev.log"

for /f "usebackq delims=" %%P in (`powershell -NoProfile -Command "$d='%DISTRO%'.Trim().ToLowerInvariant(); $d = $d -replace '[^a-z0-9._-]+','-'; $d = $d -replace '^-+|-+$',''; if ($d) { 'wsl-' + $d } else { 'wsl-default' }"`) do set "PROFILE=%%P"
if "%PROFILE%"=="" set "PROFILE=wsl-default"

if not exist "%LOGDIR%" mkdir "%LOGDIR%" >NUL 2>NUL
> "%LOGFILE%" echo [rimedeck] WSL runtime debug log
>>"%LOGFILE%" echo [rimedeck] Started at %DATE% %TIME%
>>"%LOGFILE%" echo [rimedeck] Repo: %ROOT%
>>"%LOGFILE%" echo [rimedeck] WSL distro: %DISTRO%
>>"%LOGFILE%" echo [rimedeck] WSL profile: %PROFILE%
>>"%LOGFILE%" echo.

echo [rimedeck] Repo: %ROOT%
echo [rimedeck] WSL distro: %DISTRO%
echo [rimedeck] WSL profile: %PROFILE%
if "%SKIP_BUNDLE%"=="1" echo [rimedeck] Bundle refresh: skipped
echo [rimedeck] Log: %LOGFILE%
echo.

where node.exe >NUL 2>NUL
if errorlevel 1 (
  echo [rimedeck] ERROR: node.exe not found. Install Node.js or fix PATH.
  exit /b 1
)

where pnpm.cmd >NUL 2>NUL
if errorlevel 1 (
  echo [rimedeck] ERROR: pnpm.cmd not found. Run: npm install -g pnpm
  exit /b 1
)

where go.exe >NUL 2>NUL
if errorlevel 1 (
  echo [rimedeck] ERROR: go.exe not found. Install Go or fix PATH.
  exit /b 1
)

where wsl.exe >NUL 2>NUL
if errorlevel 1 (
  echo [rimedeck] ERROR: wsl.exe not found. Enable WSL first.
  exit /b 1
)

echo [rimedeck] Checking Electron binary...
call pnpm.cmd -C apps/desktop exec electron --version >NUL 2>NUL
if errorlevel 1 (
  echo [rimedeck] Electron binary is missing. Installing Electron binary...
  if exist "node_modules\electron\path.txt" del /F /Q "node_modules\electron\path.txt" >NUL 2>NUL
  if exist "node_modules\electron\dist" rmdir /S /Q "node_modules\electron\dist" >NUL 2>NUL
  call node node_modules\electron\install.js
  call pnpm.cmd -C apps/desktop exec electron --version >NUL 2>NUL
  if errorlevel 1 (
    echo [rimedeck] Electron install script did not produce a runnable binary.
    echo [rimedeck] Electron package contents:
    if exist "node_modules\electron\path.txt" type "node_modules\electron\path.txt"
    if exist "node_modules\electron\dist" dir "node_modules\electron\dist"
    echo [rimedeck] Falling back to tar extraction from Electron cache...
    set "ELECTRON_ZIP="
    for /f "usebackq delims=" %%Z in (`node -e "const {downloadArtifact}=require('@electron/get'); const {version}=require('./node_modules/electron/package.json'); downloadArtifact({version, artifactName:'electron', platform:'win32', arch:'x64', force:false, checksums:require('./node_modules/electron/checksums.json')}).then(p=>console.log(p)).catch(e=>{console.error(e.stack||e); process.exit(1);})"`) do set "ELECTRON_ZIP=%%Z"
    if not "!ELECTRON_ZIP!"=="" (
      if exist "node_modules\electron\dist" rmdir /S /Q "node_modules\electron\dist" >NUL 2>NUL
      mkdir "node_modules\electron\dist" >NUL 2>NUL
      tar -xf "!ELECTRON_ZIP!" -C "node_modules\electron\dist"
      > "node_modules\electron\path.txt" <NUL set /p="electron.exe"
      call pnpm.cmd -C apps/desktop exec electron --version >NUL 2>NUL
    )
  )
  call pnpm.cmd -C apps/desktop exec electron --version >NUL 2>NUL
  if errorlevel 1 (
    echo [rimedeck] Running pnpm install...
    call pnpm.cmd install
    if errorlevel 1 (
      echo [rimedeck] ERROR: failed to install Electron dependency.
      exit /b 1
    )
    call pnpm.cmd -C apps/desktop exec electron --version >NUL 2>NUL
    if errorlevel 1 (
      echo [rimedeck] ERROR: Electron binary is still missing after pnpm install.
      echo [rimedeck] Try manually: node node_modules\electron\install.js
      exit /b 1
    )
  )
)

echo [rimedeck] Available WSL distros:
wsl.exe -l -q
echo.

echo [rimedeck] Checking WSL distro "%DISTRO%"...
wsl.exe -d "%DISTRO%" -e sh -lc "printf 'user=%s home=%s arch=' ""$USER"" ""$HOME""; uname -m" >>"%LOGFILE%" 2>&1
if errorlevel 1 (
  echo [rimedeck] ERROR: cannot run commands in WSL distro "%DISTRO%".
  echo [rimedeck] Check log: %LOGFILE%
  exit /b 1
)

if "%SKIP_BUNDLE%"=="1" (
  echo [rimedeck] Skipping bundle-cli because --no-bundle was provided.
) else (
  echo [rimedeck] Building Desktop CLI bundles, including WSL Linux multica...
  call pnpm.cmd -C apps/desktop run bundle-cli
  if errorlevel 1 (
    echo [rimedeck] ERROR: bundle-cli failed.
    exit /b 1
  )
)

if not exist "apps\desktop\resources\pgsql\bin\pg_ctl.exe" (
  echo [rimedeck] PostgreSQL resources missing. Bundling PostgreSQL once...
  call pnpm.cmd -C apps/desktop run bundle-pg
  if errorlevel 1 (
    echo [rimedeck] ERROR: bundle-pg failed.
    exit /b 1
  )
) else (
  echo [rimedeck] PostgreSQL resources already exist. Skipping bundle-pg.
)

echo [rimedeck] Stopping old Desktop/backend/daemon processes if present...
taskkill /IM RimeDeck.exe /F >NUL 2>NUL
taskkill /IM multica.exe /F >NUL 2>NUL
taskkill /IM multica-server.exe /F >NUL 2>NUL

echo [rimedeck] Current WSL multica state before Desktop Start:
wsl.exe -d "%DISTRO%" -e sh -lc "if [ -x ""$HOME/.local/bin/multica"" ]; then ""$HOME/.local/bin/multica"" version --output json; else echo 'not installed yet'; fi" >>"%LOGFILE%" 2>&1
type "%LOGFILE%"
echo.

echo [rimedeck] Starting Desktop dev mode.
echo [rimedeck] In the app, open: Runtimes - WSL runtimes - %DISTRO% - Start
echo [rimedeck] Keep this cmd window open. Main-process errors will print here.
echo.

call pnpm.cmd -C apps/desktop exec electron-vite dev
set "EXITCODE=%ERRORLEVEL%"

echo.
echo [rimedeck] Desktop dev exited with code %EXITCODE%.
echo [rimedeck] Useful post-run checks:
echo   wsl.exe -d %DISTRO% -e sh -lc "cat $HOME/.rimedeck/profiles/%PROFILE%/config.json"
echo   wsl.exe -d %DISTRO% -e sh -lc "$HOME/.local/bin/multica daemon status --profile %PROFILE% 2^>^&1 ^|^| true"
echo   wsl.exe -d %DISTRO% -e sh -lc "tail -n 120 $HOME/.rimedeck/profiles/%PROFILE%/daemon.log 2^>/dev/null ^|^| true"

exit /b %EXITCODE%
