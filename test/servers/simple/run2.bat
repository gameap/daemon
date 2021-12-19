@echo off

echo Server starting...

: sleep =)
ping 192.50.50.50 -n 1 -w 300 >nul

echo Loading configuration...

: sleep =)
ping 192.50.50.50 -n 1 -w 2000 >nul

echo Server started

: sleep =)
ping 192.50.50.50 -n 1 -w 1000 >nul