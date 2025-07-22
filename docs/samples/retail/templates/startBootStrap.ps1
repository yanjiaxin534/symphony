
$cert_password = Read-Host -Prompt "Enter certificate password" 


# Convert SecureString to plain text for passing to bootstrap.ps1 Convert plain text password back to SecureString
$secure_cert_password = ConvertTo-SecureString -String $cert_password -AsPlainText -Force

Write-Host "Secure Password: $secure_cert_password"
