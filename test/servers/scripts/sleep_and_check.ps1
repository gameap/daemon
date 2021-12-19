$fileExists = 0
if (Test-Path -path "sleep_and_check.txt") {
  $fileExists = 1
}

if ($fileExists -eq 0) {
   Start-Sleep -Seconds 5

   if (Test-Path -path "sleep_and_check.txt") {
       New-Item "sleep_and_check_fail.txt" -ItemType File -Value ("File unexpectedly appeared`n")
       echo "File unexpectedly appeared"
       exit 1
   }

   New-Item "sleep_and_check.txt" -ItemType File -Value ("file_contents`n")
}