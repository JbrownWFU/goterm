# gt: run goterm, and when you quit with `q`, cd this shell to the folder you ended in.
# Add this function to your PowerShell profile (notepad $PROFILE).
function gt {
    $tmp = [System.IO.Path]::GetTempFileName()
    try {
        goterm --choosedir $tmp @args
        $dir = (Get-Content -LiteralPath $tmp -Raw -ErrorAction SilentlyContinue)
        if ($dir -and (Test-Path -LiteralPath $dir)) { Set-Location -LiteralPath $dir }
    } finally {
        Remove-Item -LiteralPath $tmp -ErrorAction SilentlyContinue
    }
}
