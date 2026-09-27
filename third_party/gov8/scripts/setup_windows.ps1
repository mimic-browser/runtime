# Build the shim using a verified patched V8 source tree and its matching archive.
# See README.mimic.md for the pinned crate, source patch and native build commands.
# Publication remains atomic and serialized by the setup build lock.
param(
    [Parameter(Mandatory = $true)][string]$NativeSource,
    [Parameter(Mandatory = $true)][string]$NativeArchive
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2

$CrateVersion = '152.2.0'

$ArtifactName  = 'rusty_v8_release_x86_64-pc-windows-msvc.lib.gz'
$ArtifactSha   = '0b17ca072bae37dd4ff00e6014d2b413becb031c9342ee11cb8226a5881f62b2'
$ArtifactBytes = 39957087
$ArtifactUrl   = 'https://github.com/denoland/rusty_v8/releases/download/v' + $CrateVersion + '/' + $ArtifactName

$CrateName = "v8-$CrateVersion.crate"
$CrateSha  = 'a10fe1a92da5c32c7c7f838ce36c0ccfcfd5edf0865b58bdde820aa64cea9888'
$CrateUrl  = "https://static.crates.io/crates/v8/$CrateName"

# The prebuilt V8 artifact implements ECMAScript Temporal on top of the
# temporal_rs Rust library (exposed through temporal_capi), which ships as a
# separate static library. The helper crate that links it is checked in under
# internal/shim/temporal with an exact dependency pin and a committed
# Cargo.lock; setup builds it with `cargo build --locked`, so resolution is
# reproducible and any drift fails closed. This must be the same version
# recorded in rust-oracle/Cargo.lock for the oracle's v8 =152.2.0 build.
$TemporalCapiVersion = '0.2.6'

# Escaped cache key used by the v8 crate's build script for the prebuilt
# artifact (non-alphanumerics escaped to '_').
$CargoArchiveCache = Join-Path $env:USERPROFILE '.cargo\.rusty_v8\v152_2_0_rusty_v8_release_x86_64_pc_windows_msvc_lib_gz'

# Build-lock settings.
$LockMutexName = 'gov8-setup-windows-amd64'
$LockTimeoutMinutes = 30

# A decompressed static library smaller than this is treated as corrupt
# leftover from a pre-atomicity run and re-decompressed. The pinned release
# library is ~235 MB.
$MinPlausibleLibBytes = 50MB

# --- locate repo root (this script lives in <root>/scripts) ------------------
$Root = Split-Path -Parent $PSScriptRoot
$ThirdParty = Join-Path $Root 'build\third_party'
$ShimDir = Join-Path $Root 'build\shim'
$ShimSrc = Join-Path $Root 'internal\shim'
$TemporalHelperDir = Join-Path $Root 'internal\shim\temporal'

$script:BuildMutex = $null

# --- target guard -------------------------------------------------------------
$goos = (& go env GOOS)
$goarch = (& go env GOARCH)
if ($goos -ne 'windows' -or $goarch -ne 'amd64') {
    throw "setup_windows.ps1 requires GOOS=windows GOARCH=amd64 (found GOOS=$goos GOARCH=$goarch). " +
        'Set the environment and re-run scripts/setup_windows.ps1.'
}

function Get-Sha256([string]$Path) {
    return (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash.ToLower()
}

function Enter-BuildLock {
    # Serialize the whole run against other invocations of this script. A named
    # OS mutex is process-safe, auto-released on process death (an abandoned
    # mutex is detectable), and works across shells and CI agent processes.
    foreach ($name in @("Global\$LockMutexName", $LockMutexName)) {
        try {
            $mutex = New-Object System.Threading.Mutex($false, $name)
        } catch {
            # Creating a Global\ mutex requires SeCreateGlobalPrivilege; fall
            # back to the session-local namespace, which still serializes all
            # concurrent setups of the same user (shells, IDE tasks, CI jobs
            # sharing a session).
            Write-Host "[setup] cannot open lock '$name' ($($_.Exception.GetType().Name)); trying next namespace"
            continue
        }
        $acquired = $false
        try {
            $acquired = $mutex.WaitOne([TimeSpan]::FromMinutes($LockTimeoutMinutes))
        } catch [System.Threading.AbandonedMutexException] {
            # The previous holder died while holding the lock. Ownership is
            # granted to us; because every published file is written to a
            # staging path and moved into place atomically, no half-written
            # state can exist under the canonical paths.
            Write-Host '[setup] previous lock holder exited abruptly; taking over the build lock'
            $acquired = $true
        }
        if (-not $acquired) {
            $mutex.Dispose()
            throw ("another setup_windows.ps1 run is holding the build lock '{0}' (waited {1} minutes). " +
                   'Wait for it to finish, then retry.') -f $name, $LockTimeoutMinutes
        }
        $script:BuildMutex = $mutex
        Write-Host "[setup] build lock acquired: $name"
        return
    }
    throw 'could not open any build-lock mutex (Global\ and session-local).'
}

function Exit-BuildLock {
    if ($null -ne $script:BuildMutex) {
        try { $script:BuildMutex.ReleaseMutex() } catch { }
        $script:BuildMutex.Dispose()
        $script:BuildMutex = $null
        Write-Host '[setup] build lock released'
    }
}

function Publish-FileAtomic([string]$Staged, [string]$Final) {
    # Move a fully written staging file onto its canonical path. The move is
    # same-volume and atomic. If the final file exists and is held open (a
    # loaded gov8_shim.dll cannot be overwritten), rename the old file aside
    # first - renaming an in-use file is allowed on NTFS, so processes that
    # already mapped the old image keep running while the new file takes the
    # canonical path.
    $parent = Split-Path -Parent $Final
    if (-not (Test-Path -LiteralPath $parent)) {
        New-Item -ItemType Directory -Path $parent -Force | Out-Null
    }
    if (-not (Test-Path -LiteralPath $Final)) {
        Move-Item -LiteralPath $Staged -Destination $Final
        return
    }
    try {
        Move-Item -LiteralPath $Staged -Destination $Final -Force -ErrorAction Stop
        return
    } catch {
        $aside = Join-Path $parent ('{0}.superseded-{1}' -f (Split-Path -Leaf $Final), [guid]::NewGuid().ToString('N'))
        Move-Item -LiteralPath $Final -Destination $aside -Force
        try {
            Move-Item -LiteralPath $Staged -Destination $Final -Force -ErrorAction Stop
        } catch {
            Move-Item -LiteralPath $aside -Destination $Final -Force
            throw
        }
        try {
            Remove-Item -LiteralPath $aside -Force -ErrorAction SilentlyContinue
        } catch {
            Write-Host "[setup] note: could not delete $aside (still in use); it is safe to delete later"
        }
    }
}

function Remove-StaleStaging {
    # Only runs while the build lock is held. Sweeps partial staging state left
    # by an abruptly killed previous run. Files still open in other processes
    # (e.g. a superseded DLL mapped by a running test binary) are skipped.
    foreach ($dir in @($ShimDir, $ThirdParty)) {
        if (-not (Test-Path -LiteralPath $dir)) { continue }
        Get-ChildItem -LiteralPath $dir -Force -ErrorAction SilentlyContinue |
            Where-Object { $_.Name -like '.staging-*' -or $_.Name -like '.tmp-*' -or $_.Name -like '.superseded-*' } |
            Remove-Item -Recurse -Force -ErrorAction SilentlyContinue
    }
}

function Get-ArtifactGz {
    $dest = Join-Path $ThirdParty $ArtifactName
    $candidates = @()
    if ($env:GOV8_ARTIFACT_GZ) { $candidates += $env:GOV8_ARTIFACT_GZ }
    $candidates += $CargoArchiveCache
    $candidates += $dest

    foreach ($cand in $candidates) {
        if ($cand -and (Test-Path -LiteralPath $cand)) {
            $actual = Get-Sha256 $cand
            if ($actual -ne $ArtifactSha) {
                throw ("SHA-256 mismatch for '{0}': expected {1}, found {2}`n" +
                       'Delete the file and re-run scripts/setup_windows.ps1.') -f $cand, $ArtifactSha, $actual
            }
            if ((Get-Item -LiteralPath $cand).Length -ne $ArtifactBytes) {
                throw ("Size mismatch for '{0}': expected {1} bytes.") -f $cand, $ArtifactBytes
            }
            Write-Host "[setup] artifact (verified): $cand"
            if ($cand -ne $dest) {
                $staged = Join-Path $ThirdParty ('.tmp-' + [guid]::NewGuid().ToString('N'))
                Copy-Item -LiteralPath $cand -Destination $staged
                Publish-FileAtomic $staged $dest
            }
            return $dest
        }
    }

    Write-Host "[setup] downloading $ArtifactUrl"
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    $staged = Join-Path $ThirdParty ('.tmp-' + [guid]::NewGuid().ToString('N'))
    Invoke-WebRequest -Uri $ArtifactUrl -OutFile $staged -UseBasicParsing
    $actual = Get-Sha256 $staged
    if ($actual -ne $ArtifactSha) {
        Remove-Item -LiteralPath $staged -Force
        throw ("SHA-256 mismatch for downloaded artifact: expected {0}, found {1}") -f $ArtifactSha, $actual
    }
    if ((Get-Item -LiteralPath $staged).Length -ne $ArtifactBytes) {
        throw ("Size mismatch for downloaded artifact: expected {0} bytes.") -f $ArtifactBytes
    }
    Publish-FileAtomic $staged $dest
    return $dest
}

function Seed-CargoArtifactCache([string]$VerifiedGz) {
    # Pre-populate the rusty_v8 build-script cache from the verified copy so
    # oracle `cargo` builds never re-download the artifact (they also work
    # offline). The bytes are hash-identical and the crate's build.rs
    # re-verifies the digest on cache reuse, so this cannot mask corruption.
    if (Test-Path -LiteralPath $CargoArchiveCache) { return }
    $parent = Split-Path -Parent $CargoArchiveCache
    if (-not (Test-Path -LiteralPath $parent)) {
        New-Item -ItemType Directory -Path $parent -Force | Out-Null
    }
    $staged = Join-Path $parent ('.tmp-' + [guid]::NewGuid().ToString('N'))
    Copy-Item -LiteralPath $VerifiedGz -Destination $staged
    Move-Item -LiteralPath $staged -Destination $CargoArchiveCache -Force
    Write-Host "[setup] seeded cargo artifact cache: $CargoArchiveCache"
}

function Get-Crate {
    $dest = Join-Path $ThirdParty $CrateName
    $candidates = @()
    if ($env:GOV8_CRATE) { $candidates += $env:GOV8_CRATE }
    $registryCache = Join-Path $env:USERPROFILE '.cargo\registry\cache'
    if (Test-Path -LiteralPath $registryCache) {
        $found = Get-ChildItem -LiteralPath $registryCache -Recurse -Filter $CrateName -ErrorAction SilentlyContinue |
            Select-Object -ExpandProperty FullName
        foreach ($f in $found) { $candidates += $f }
    }
    $candidates += $dest

    foreach ($cand in $candidates) {
        if ($cand -and (Test-Path -LiteralPath $cand)) {
            $actual = Get-Sha256 $cand
            if ($actual -ne $CrateSha) {
                throw ("SHA-256 mismatch for crate '{0}': expected {1}, found {2}") -f $cand, $CrateSha, $actual
            }
            Write-Host "[setup] crate (verified): $cand"
            if ($cand -ne $dest) {
                $staged = Join-Path $ThirdParty ('.tmp-' + [guid]::NewGuid().ToString('N'))
                Copy-Item -LiteralPath $cand -Destination $staged
                Publish-FileAtomic $staged $dest
            }
            return $dest
        }
    }

    Write-Host "[setup] downloading $CrateUrl"
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    $staged = Join-Path $ThirdParty ('.tmp-' + [guid]::NewGuid().ToString('N'))
    Invoke-WebRequest -Uri $CrateUrl -OutFile $staged -UseBasicParsing
    $actual = Get-Sha256 $staged
    if ($actual -ne $CrateSha) {
        Remove-Item -LiteralPath $staged -Force
        throw ("SHA-256 mismatch for downloaded crate: expected {0}, found {1}") -f $CrateSha, $actual
    }
    Publish-FileAtomic $staged $dest
    return $dest
}

function Expand-Gzip([string]$GzPath, [string]$OutPath) {
    $input_ = New-Object System.IO.FileStream($GzPath, [System.IO.FileMode]::Open, [System.IO.FileAccess]::Read)
    try {
        $gzip = New-Object System.IO.Compression.GzipStream($input_, [System.IO.Compression.CompressionMode]::Decompress)
        try {
            $output = [System.IO.File]::Create($OutPath)
            try {
                $gzip.CopyTo($output)
            } finally {
                $output.Dispose()
            }
        } finally {
            $gzip.Dispose()
        }
    } finally {
        $input_.Dispose()
    }
}

function Expand-CrateHeaders([string]$CratePath) {
    $includeDir = Join-Path $ThirdParty 'v8-include'
    if (Test-Path -LiteralPath (Join-Path $includeDir 'v8.h')) {
        Write-Host "[setup] v8 headers already extracted: $includeDir"
        return $includeDir
    }
    $tmp = Join-Path $ThirdParty ('tmp-' + [System.IO.Path]::GetRandomFileName())
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        # bsdtar treats command-line arguments as glob patterns; extract only
        # the include subtree of the vendored V8 source.
        & tar -xzf $CratePath -C $tmp ("v8-$CrateVersion/v8/include/*")
        if ($LASTEXITCODE -ne 0) { throw 'tar extraction of crate headers failed' }
        $srcInclude = Join-Path $tmp "v8-$CrateVersion\v8\include"
        if (-not (Test-Path -LiteralPath (Join-Path $srcInclude 'v8.h'))) {
            throw 'extracted crate tree does not contain v8/include/v8.h'
        }
        # Publish-FileAtomic handles the directory swap: if a previous
        # include tree exists it is renamed aside, the new tree takes the
        # canonical path, and the old tree is deleted best-effort.
        Publish-FileAtomic $srcInclude $includeDir
    } finally {
        if (Test-Path -LiteralPath $tmp) { Remove-Item -LiteralPath $tmp -Recurse -Force }
    }
    Write-Host "[setup] v8 headers extracted: $includeDir"
    return $includeDir
}

function Get-LockPackageVersion([string[]]$Lines, [string]$Package) {
    # Returns the version recorded for a package in a Cargo.lock, or $null.
    for ($i = 0; $i -lt $Lines.Count; $i++) {
        if ($Lines[$i] -eq ('name = "{0}"' -f $Package)) {
            for ($j = $i + 1; $j -lt $Lines.Count; $j++) {
                if ($Lines[$j] -eq '[[package]]') { break }
                if ($Lines[$j] -match '^version = "(.+)"$') {
                    return $Matches[1]
                }
            }
            break
        }
    }
    return $null
}

function Assert-LockPackage([string[]]$Lines, [string]$LockName, [string]$Package, [string]$Expected) {
    $actual = Get-LockPackageVersion $Lines $Package
    if ($null -eq $actual) {
        throw ("{0} entry missing from {1}" -f $Package, $LockName)
    }
    if ($actual -ne $Expected) {
        throw ("{0} pins {1} {2}, expected {3}; regenerate the lock against the pinned manifest and commit it.") -f
            $LockName, $Package, $actual, $Expected
    }
}

function Assert-TemporalLockPinned {
    # Fail closed unless the committed Cargo.lock pins temporal_capi to the
    # exact version above. cargo --locked would also reject a stale lock,
    # but this check reports the actual problem directly.
    $lockPath = Join-Path $TemporalHelperDir 'Cargo.lock'
    $lines = Get-Content -LiteralPath $lockPath
    Assert-LockPackage $lines $lockPath 'temporal_capi' $TemporalCapiVersion
}

function Assert-OracleLockPinned {
    # Cross-check the oracle's committed Cargo.lock: it must pin the same v8
    # crate version and the same temporal_capi version this script builds the
    # shim against. If the two lockfiles disagree, the shim and the oracle
    # would be running different engines, so this fails closed.
    $lockPath = Join-Path $Root 'rust-oracle\Cargo.lock'
    if (-not (Test-Path -LiteralPath $lockPath)) { throw "missing oracle lockfile: $lockPath" }
    $lines = Get-Content -LiteralPath $lockPath
    Assert-LockPackage $lines $lockPath 'v8' $CrateVersion
    Assert-LockPackage $lines $lockPath 'temporal_capi' $TemporalCapiVersion
    Write-Host ("[setup] oracle lock verified: v8 = {0}, temporal_capi = {1}" -f $CrateVersion, $TemporalCapiVersion)
}

function Build-TemporalStaticLib {
    # The helper crate (manifest, src, Cargo.lock) is checked in under
    # internal/shim/temporal with an exact =pin; nothing is generated here.
    $manifest = Join-Path $TemporalHelperDir 'Cargo.toml'
    foreach ($f in @(
        $manifest,
        (Join-Path $TemporalHelperDir 'Cargo.lock'),
        (Join-Path $TemporalHelperDir 'src\lib.rs'))) {
        if (-not (Test-Path -LiteralPath $f)) {
            throw "missing checked-in temporal helper file: $f"
        }
    }
    Assert-TemporalLockPinned

    $targetDir = Join-Path $Root 'build\temporal-target'
    Write-Host "[setup] building temporal_capi closure (cargo --locked, pinned temporal_capi =$TemporalCapiVersion)..."
    $cargoArgs = @(
        'build', '--release', '--locked',
        '--manifest-path', $manifest,
        '--target-dir', $targetDir
    )
    # Rust debug/panic location strings otherwise contain the builder's user
    # profile and checkout path. Encoded flags preserve paths with spaces and
    # make the packaged DLL reproducible without leaking builder identity.
    $savedRustFlags = $env:RUSTFLAGS
    $savedEncodedRustFlags = $env:CARGO_ENCODED_RUSTFLAGS
    $cargoHome = Join-Path $env:USERPROFILE '.cargo'
    $env:RUSTFLAGS = $null
    $env:CARGO_ENCODED_RUSTFLAGS = (@(
        ('--remap-path-prefix={0}=/gov8' -f $Root),
        ('--remap-path-prefix={0}=/cargo' -f $cargoHome)
    ) -join [char]0x1f)
    try {
        & cargo @cargoArgs --offline
        if ($LASTEXITCODE -ne 0) {
            Write-Host '[setup] offline locked build failed; retrying with network (still --locked)'
            & cargo @cargoArgs
            if ($LASTEXITCODE -ne 0) { throw 'cargo build --locked of the temporal closure failed' }
        }
    } finally {
        $env:RUSTFLAGS = $savedRustFlags
        $env:CARGO_ENCODED_RUSTFLAGS = $savedEncodedRustFlags
    }
    $staticlib = Join-Path $targetDir 'release\temporal_link.lib'
    if (-not (Test-Path -LiteralPath $staticlib)) { throw "temporal_link.lib missing at $staticlib" }
    # crate-type=staticlib already bundles the Rust dependency closure. Do not
    # also glob target/release/deps: Cargo retains stale fingerprint variants
    # there, which makes the link nondeterministic and can exceed cmd.exe's
    # command-line limit after a flag or toolchain change.
    Write-Host "[setup] temporal closure: $staticlib"
    return $staticlib
}

function Find-VCVars {
    $vswhere = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\Installer\vswhere.exe'
    if (-not (Test-Path -LiteralPath $vswhere)) {
        throw 'vswhere.exe not found; install Visual Studio 2022/2026 with the C++ build tools (MSVC v143+).'
    }
    $vsPath = & $vswhere -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
    if (-not $vsPath) { throw 'no Visual Studio installation with MSVC C++ tools found via vswhere.' }
    $vcvars = Join-Path $vsPath 'VC\Auxiliary\Build\vcvars64.bat'
    if (-not (Test-Path -LiteralPath $vcvars)) { throw "vcvars64.bat not found under $vsPath" }
    return $vcvars
}

function Build-Shim([string]$V8Include, [string]$LibPath, [string[]]$TemporalLibs, [string]$VCVars, [string]$Staging) {
    # Compile and link inside the staging directory, then publish the finished
    # DLL atomically. build\shim\gov8_shim.dll therefore only ever contains a
    # complete, linkable image - never a partially written one.
    $dll = Join-Path $Staging 'gov8_shim.dll'
    $obj = Join-Path $Staging 'shim.obj'
    $clArgs = @(
        '/nologo', '/std:c++20', '/Zc:__cplusplus', '/EHsc', '/O2', '/MT', '/Brepro', '/DNDEBUG', '/DV8_GN_HEADER',
        # The pinned crate builds V8 with the "-rusty" embedder string (it is
        # part of the observable version identity; V8::GetVersion() in the
        # artifact already reports it at runtime).
        '/DV8_EMBEDDER_STRING=\"-rusty\"',
        '/W3',
        ('/I' + $ShimSrc),
        ('/I' + $V8Include),
        '/c',
        ('/Fo' + $obj),
        (Join-Path $ShimSrc 'shim.cc')
    )
    $clLine = 'cl ' + ($clArgs -join ' ')
    $linkLine = ('link /nologo /Brepro /DLL /OUT:{0} {1} {2} {3} winmm.lib dbghelp.lib ws2_32.lib ntdll.lib userenv.lib bcrypt.lib synchronization.lib advapi32.lib' -f
        $dll, $obj, $LibPath, ($TemporalLibs -join ' '))
    $cmdLine = '"{0}" >NUL && {1} && {2}' -f $VCVars, $clLine, $linkLine
    Write-Host '[setup] compiling shim with MSVC...'
    $output = & cmd.exe /s /c $cmdLine 2>&1
    if ($LASTEXITCODE -ne 0) {
        $output | ForEach-Object { Write-Host $_ }
        throw 'MSVC build of the shim failed (see output above).'
    }
    if ($output -match 'LNK\d{4}|error C\d{4}') {
        $output | ForEach-Object { Write-Host $_ }
        throw 'MSVC emitted errors while building the shim.'
    }
    if (-not (Test-Path -LiteralPath $dll)) { throw 'shim DLL missing after build' }

    $finalDll = Join-Path $ShimDir 'gov8_shim.dll'
    Publish-FileAtomic $dll $finalDll
    Publish-FileAtomic $obj (Join-Path $ShimDir 'shim.obj')
    Write-Host "[setup] shim built and published atomically: $finalDll"
    return $finalDll
}

# --- main ---------------------------------------------------------------------
try {
    Enter-BuildLock

    foreach ($d in @($ThirdParty, $ShimDir)) {
        if (-not (Test-Path -LiteralPath $d)) {
            New-Item -ItemType Directory -Path $d | Out-Null
        }
    }
    Remove-StaleStaging

    Assert-OracleLockPinned

    # The stock release archive lacks the native observation and receiver
    # semantics. Never silently rebuild against it. Headers and the library
    # must come from the same patched source build described in README.mimic.md.
    $nativeSourcePath = (Resolve-Path -LiteralPath $NativeSource).Path
    $lib = (Resolve-Path -LiteralPath $NativeArchive).Path
    & python (Join-Path $PSScriptRoot 'verify_native_source.py') $nativeSourcePath
    if ($LASTEXITCODE -ne 0) { throw 'native V8 source verification failed' }
    $v8Include = Join-Path $nativeSourcePath 'v8\include'

    $temporalLib = Build-TemporalStaticLib

    $vcvars = Find-VCVars
    Write-Host "[setup] MSVC environment: $vcvars"
    $staging = Join-Path $ShimDir ('.staging-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $staging | Out-Null
    try {
        Build-Shim $v8Include $lib $temporalLib $vcvars $staging | Out-Null
    } finally {
        if (Test-Path -LiteralPath $staging) {
            Remove-Item -LiteralPath $staging -Recurse -Force -ErrorAction SilentlyContinue
        }
    }

    Write-Host ("[setup] pins: v8 crate {0}; native archive sha256 {1}; temporal_capi {2}" -f
        $CrateVersion, (Get-Sha256 $lib), $TemporalCapiVersion)
    Write-Host '[setup] done. Run: go test ./...'
} finally {
    Exit-BuildLock
}
