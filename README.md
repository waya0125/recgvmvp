# recgvmvp

I-O DATA **GV-MVP/VZ**（`04bb:052b`、地上デジタル・BS・110度CS）および **GV-MVP/HZ**（`04bb:0522`、地上デジタル専用）を Linux 単体で操作し、MPEG-2 TS を出力する純 Go 製コマンドです（cgo・libusb 不要、Linux `usbfs` 直叩き）。

* 本体スロットに挿入した B-CAS カードでハードウェア復号された TS が出力されます（外部カードリーダー・`b25` デコーダ不要）。
* IVDRM セッション制御・TS 復号手順は nuIlpointer 氏の [BonDriver_GVMVPVZ_T](https://github.com/nuIlpointer/BonDriver_GVMVPVZ)（MIT）および Linux 移植版を参考にしています。
* デバイスの技術仕様・特性の詳細は [docs/GV-MVP-VZ.md](./docs/GV-MVP-VZ.md) および [docs/GV-MVP-HZ.md](./docs/GV-MVP-HZ.md) を参照してください。

## ビルドとインストール

```bash
go build -o bin/recgvmvp ./cmd/recgvmvp
go build -o bin/gvmvp-firmware ./cmd/gvmvp-firmware
go test ./...
```

※ Docker を使う場合は `./scripts/go.sh build -o bin/recgvmvp ./cmd/recgvmvp` のようにビルドできます。

### udev ルール（一般ユーザーで実行する場合）

```bash
sudo cp udev/99-gvmvpvz.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules
```

## ファームウェア

電源投入直後（コールド状態 / DFU 状態）の機器を使う場合は、純正ドライバ等から対応ファームウェアを取り出して次のいずれかに配置してください（上から順に探索します）。すでにウォーム状態の場合は不要です。

* GV-MVP/VZ（`04bb:052b`）: `NXP7164F_052B04BB.bin`
* GV-MVP/HZ（`04bb:0522`）: `NXP7164F_052204BB.bin`

1. `--firmware` / `--fw` で指定したパス
2. 環境変数 `GVMVP_FW_PATH` / `GVMVPVZ_FW_PATH` / `GVMVPHZ_FW_PATH`
3. `recgvmvp` 実行ファイルと同じディレクトリ
4. `/usr/local/share/gvmvpvz/<ファイル名>`
5. `/lib/firmware/<ファイル名>`

付属の `gvmvp-firmware` コマンドでドライバフォルダからの探索・SHA-256 照合・配置ができます：

```bash
./bin/gvmvp-firmware verify /path/to/Driver
sudo ./bin/gvmvp-firmware install /path/to/Driver
```

## 使い方

```text
recgvmvp [オプション] チャンネル 秒 出力先
  チャンネル  13〜62（地デジ UHF）、BS01_0〜BS23_2（BS）、CS2〜CS24（110度CS）
  秒          録画秒数。'-' を指定すると SIGINT/SIGTERM まで連続受信
  出力先      出力ファイルパス。'-' を指定すると標準出力

オプション:
  --device PATH     使う機器ファイル（/dev/bus/usb/BBB/DDD）。空なら自動選択
  --model MODEL     使う機種（auto, vz, hz。既定: auto）
  --firmware PATH   ファームウェア（.bin）のパス（--fw も可）
  --tsid HEX        BS/CS の TSID を 16 進で明示指定（例: 0x40f1）
  --check           選局してロック状態と受信目安を表示するだけ
  --reset           開く前に USB ポートリセットを発行する
  --debug           詳細ログを stderr に出力（GVMVPVZ_DEBUG=1 でも可）
  --b25 / --strip / --sid   recpt1 互換用オプション（受け付けるが無視）
```

### 実行例

```bash
# 地デジ 25ch の受信確認
recgvmvp --check 25 3

# 地デジ 25ch を 10 秒録画
recgvmvp 25 10 out.ts

# BS (NHK BS) を標準出力へ流す
recgvmvp BS15_0 - - > bs.ts

# 110度CS (CS4) を 10 秒録画
recgvmvp CS4 10 cs4.ts
```

### Mirakurun (`tuners.yml`) の設定例

VZ（BS/CS・地デジ）と HZ（地デジ専用）を同時に接続して運用する場合は、`--model` を明示するとチューナーが競合しません（同一 USB バス上で VZ/HZ の同時録画・ドロップ 0 を確認済み）。

```yaml
- name: GV-MVP-VZ-BS
  types:
    - BS
    - CS
  command: recgvmvp --model vz <channel> - -
  decoder: ~
  isDisabled: false

- name: GV-MVP-HZ-GR
  types:
    - GR
  command: recgvmvp --model hz <channel> - -
  decoder: ~
  isDisabled: false
```

## 注意事項

* **BS/CS アンテナ電源**: 本機はアンテナ線への電源供給を行いません。BS/CS を受信する場合は、分配器や別機器からアンテナへ給電されている環境でご使用ください。
* **強制終了の禁止**: `kill -9` 等で終了すると機器側の WatchDog によりロックされる場合があります。通常は `SIGINT` / `SIGTERM` で終了し、ロックされた場合は `--reset` または USB の抜き差しで回復してください。

## 付属コマンド

* `cmd/recgvmvp`: 本番録画コマンド（`recpt1` 互換 CLI）
* `cmd/gvmvp-firmware`: ファームウェア検証・配置・手動転送
* `cmd/gvmvp-probe`: 低レベル USB コマンド / B-CAS ステータス確認（開発用）
* `cmd/gvmvp-tune`: 選局・ロック確認（開発用）

## ライセンス

MIT License（[LICENSE](./LICENSE)）
