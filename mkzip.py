"""配布用の zip を作る。

日本語のファイル名がどの展開ソフトでも化けないよう、
  - 本来のファイル名欄には Shift_JIS(CP932) で入れる（Windows標準の展開機能・Lhaplus などはこれを読む）
  - UTF-8 の名前も「Info-ZIP Unicode Path」(0x7075) として添える（7-Zip などはこちらを読む）
という、日本語の zip でよく使われる形にしている。

使い方: python3 mkzip.py 出力.zip 入れるフォルダ
"""
import os
import struct
import sys
import zipfile
import zlib


class CP932Info(zipfile.ZipInfo):
    def _encodeFilenameFlags(self):
        # 英数字だけの名前はどの形式でも同じなので印は付けない
        try:
            return self.filename.encode('ascii'), self.flag_bits & ~0x800
        except UnicodeEncodeError:
            # UTF-8 の印(0x800)は付けない。付けると Shift_JIS ではなく UTF-8 と解釈されてしまう
            return self.filename.encode('cp932'), self.flag_bits & ~0x800


def add(z, path, name):
    info = CP932Info.from_file(path, name)
    info.compress_type = zipfile.ZIP_DEFLATED
    try:
        name.encode('ascii')
    except UnicodeEncodeError:
        raw = name.encode('cp932')
        utf8 = name.encode('utf-8')
        # 0x7075: version(1) + 元の名前のCRC32 + UTF-8の名前
        body = struct.pack('<BI', 1, zlib.crc32(raw) & 0xffffffff) + utf8
        info.extra = struct.pack('<HH', 0x7075, len(body)) + body
    with open(path, 'rb') as f:
        z.writestr(info, f.read())


def main():
    out, src = sys.argv[1], sys.argv[2]
    with zipfile.ZipFile(out, 'w') as z:
        for name in sorted(os.listdir(src)):
            add(z, os.path.join(src, name), name)


if __name__ == '__main__':
    main()
