@echo off
rem Windows上でビルドする場合（Go 1.24以降が必要）
go build -trimpath -ldflags "-H=windowsgui -s -w" -o "NWログ取得マクロ（TeraTerm）.exe" .
