$ErrorActionPreference = "Stop"

function Write-Section {
    param([string]$Name)
    Write-Output ""
    Write-Output "## $Name"
}

function Write-KeyValue {
    param(
        [string]$Key,
        [string]$Value
    )
    Write-Output "$Key=$Value"
}

function Test-OneDrivePath {
    param([string]$Path)
    $oneDrivePaths = @($env:OneDrive, $env:OneDriveCommercial, $env:OneDriveConsumer) |
        Where-Object { $_ -and $_.Length -gt 0 }
    foreach ($oneDrivePath in $oneDrivePaths) {
        if ($Path.StartsWith($oneDrivePath, [System.StringComparison]::OrdinalIgnoreCase)) {
            return $true
        }
    }
    return $false
}

function Get-DriveRootFromPath {
    param([string]$Path)
    try {
        return [System.IO.Path]::GetPathRoot($Path)
    } catch {
        return ""
    }
}

function Write-DefenderStatus {
    param([string]$Path)
    try {
        $preference = Get-MpPreference
        $matches = @($preference.ExclusionPath) |
            Where-Object { $_ -and $Path.StartsWith($_, [System.StringComparison]::OrdinalIgnoreCase) }
        if ($matches.Count -gt 0) {
            Write-KeyValue "defender_exclusion" "matched"
        } else {
            Write-KeyValue "defender_exclusion" "not-matched"
        }
    } catch {
        Write-KeyValue "defender_exclusion" "unknown"
    }
}

function Write-VolumeStatus {
    param([string]$Path)
    $root = Get-DriveRootFromPath -Path $Path
    if (-not $root) {
        Write-KeyValue "drive_root" "unknown"
        return
    }
    Write-KeyValue "drive_root" $root
    $driveLetter = $root.Substring(0, 1)
    try {
        $volume = Get-Volume -DriveLetter $driveLetter
        Write-KeyValue "filesystem" "$($volume.FileSystem)"
        Write-KeyValue "size_remaining" "$($volume.SizeRemaining)"
        Write-KeyValue "size" "$($volume.Size)"
    } catch {
        Write-KeyValue "volume" "unknown"
    }
    try {
        $partition = Get-Partition -DriveLetter $driveLetter
        $disk = Get-Disk -Number $partition.DiskNumber
        Write-KeyValue "bus_type" "$($disk.BusType)"
        Write-KeyValue "media_type" "$($disk.MediaType)"
    } catch {
        Write-KeyValue "disk_type" "unknown"
    }
}

function Test-NormalizedPath {
    param([string]$Path)
    if ($Path -match "(^|[\\/])\.\.?([\\/]|$)") {
        return $false
    }
    return $true
}

function Check-CandidatePath {
    param(
        [string]$Name,
        [string]$Path
    )
    Write-Section $Name
    if (-not $Path) {
        Write-KeyValue "value" "unset"
        return
    }
    Write-KeyValue "value" $Path
    Write-KeyValue "rooted" "$([System.IO.Path]::IsPathRooted($Path))"
    Write-KeyValue "normalized" "$(Test-NormalizedPath -Path $Path)"
    if ($Path.StartsWith("\\", [System.StringComparison]::Ordinal)) {
        Write-KeyValue "network_path_warning" "avoid-network-paths-for-pgdata"
    }
    $root = Get-DriveRootFromPath -Path $Path
    if ($root -and $root.Length -ge 2 -and $root.Substring(1).StartsWith(":", [System.StringComparison]::Ordinal)) {
        $driveLetter = $root.Substring(0, 1)
        try {
            $drive = Get-PSDrive -Name $driveLetter -ErrorAction Stop
            if ($drive.DisplayRoot) {
                Write-KeyValue "mapped_drive_warning" "avoid-mapped-network-drives-for-pgdata"
                Write-KeyValue "mapped_drive_root" "$($drive.DisplayRoot)"
            }
        } catch {
            Write-KeyValue "mapped_drive" "unknown"
        }
    }
    if (Test-OneDrivePath -Path $Path) {
        Write-KeyValue "onedrive_warning" "avoid-onedrive-for-pgdata"
    } else {
        Write-KeyValue "onedrive_warning" "none"
    }
    if (Test-Path -LiteralPath $Path -PathType Container) {
        Write-KeyValue "exists" "true"
        Write-VolumeStatus -Path $Path
        Write-DefenderStatus -Path $Path
    } else {
        Write-KeyValue "exists" "false"
        $parent = Split-Path -Parent $Path
        if ($parent -and (Test-Path -LiteralPath $parent -PathType Container)) {
            Write-KeyValue "parent_exists" "true"
            Write-VolumeStatus -Path $parent
            Write-DefenderStatus -Path $parent
        } else {
            Write-KeyValue "parent_exists" "false"
        }
    }
}

function Check-Docker {
    Write-Section "docker"
    try {
        docker version --format "client={{.Client.Version}} server={{.Server.Version}}"
    } catch {
        Write-KeyValue "docker" "unavailable"
        return
    }
    try {
        docker info --format "data_root={{.DockerRootDir}} driver={{.Driver}} os={{.OperatingSystem}} kernel={{.KernelVersion}}"
    } catch {
        Write-KeyValue "docker_info" "unavailable"
    }
}

function Check-Wsl {
    Write-Section "wsl"
    try {
        wsl.exe -l -v
        Write-Output "recommendation=for WSL2 Docker deployments, keep PGDATA inside the distro ext4 filesystem, not under /mnt/c"
    } catch {
        Write-KeyValue "wsl" "unavailable"
    }
}

Write-Section "summary"
Write-KeyValue "date_utc" ([DateTime]::UtcNow.ToString("yyyy-MM-ddTHH:mm:ssZ"))
Write-KeyValue "os" ([System.Environment]::OSVersion.VersionString)
Check-Docker
Check-Wsl
Check-CandidatePath "NEW_API_POSTGRES_DATA_DIR" $env:NEW_API_POSTGRES_DATA_DIR
Check-CandidatePath "NEW_API_POSTGRES_WAL_DIR" $env:NEW_API_POSTGRES_WAL_DIR
Check-CandidatePath "NEW_API_DATA_DIR" $env:NEW_API_DATA_DIR
Check-CandidatePath "NEW_API_LOG_DIR" $env:NEW_API_LOG_DIR
Check-CandidatePath "NEW_API_DOCKER_DATA_DIR" $env:NEW_API_DOCKER_DATA_DIR
Write-Section "recommendation"
Write-Output "standard=move NEW_API_POSTGRES_DATA_DIR to a fast local SSD or WSL2 ext4 path with a marker file"
Write-Output "logs=set NEW_API_LOG_DIR to a fast local path when log writes are heavy"
Write-Output "avoid=OneDrive, network paths, Defender-scanned hot paths, and WSL2 /mnt/c paths for PGDATA"
Write-Output "advanced=separate pg_wal only with a stopped-cluster or initdb-level plan"
